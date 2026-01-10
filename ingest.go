package main

import (
	"regexp"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)
type Transport uint8

const (
	TransportRaw Transport = iota
	TransportTLS
)

func (t Transport) String() string {
	switch t {
	case TransportRaw:
		return "raw"
	case TransportTLS:
		return "tls"
	default:
		return "unknown"
	}
}

type authFunc func(msg *RawMsg) string

type IngestParsed struct {
	TsStr    string
	Time     time.Time
	Session  string
	Host     string
	Cwd      string
	Payload  string
}


type CIDRTenantRule struct {
	Prefix   netip.Prefix
	TenantID string
	Action   bool
	Name     string
}

type IngestConfig struct {
	// listeners
	RawEnabled	bool
	RawAddr		string

	TLSEnabled	bool
	TLSAddr		string
	TLSConfig	*tls.Config

	// worker counts / queues
	ValidateWorkers int
	DBWorkers       int
	QueueDepth      int

	MaxLineBytes	int

	// tenancy
	DefaultTenantID string
	RawCIDRRules    map[string][]CIDRTenantRule
	AuthLst		map[Transport][]AuthMode
	Pepper		string

	// spooling
	SpoolDir	string
	SpoolSyncEveryN	int
	SpoolSyncEvery	time.Duration

	// db
	PostgresDSN	string
	DBRequired	bool
}

type IngestService struct {
	cfg           IngestConfig
	db            *DB
	authFuncs     map[string]authFunc

	// channels between stages
	rawCh         chan *RawMsg
	spoolCh       chan ValidatedMsg
	dbCh          chan SeqMsg

	// cancellation
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup

	// listeners
	rawLn         net.Listener
	tlsLn         net.Listener

	// metrics
	linesAccepted uint64
	linesDropped  uint64
	linesSpooled  uint64
	linesDBOK     uint64
	linesDBFail   uint64
}

type RawMsg struct {
	Line          string
	PeerIP        netip.Addr
	Received      time.Time
	Transport     Transport
}

type ValidatedMsg struct {
	Line      string
	TenantID  string
	PeerIP    netip.Addr
	Received  time.Time
	Transport Transport
}

type SeqMsg struct {
	Line      string
	TenantID  string
	Seq       int64
	PeerIP    netip.Addr
	Received  time.Time
	Transport Transport
}

var reIngestStrict = regexp.MustCompile(
	`^` +
		`(?P<ts>\d{8}\.\d{6})` +
		`\s*-\s*` +
		`(?:(?P<sid>[0-9a-f]{8})\s*-\s*)?` +
		`(?P<host>[A-Za-z0-9._-]+)` +
		`(?:\s+\[cwd=(?P<cwd>[^\]]+)\])?` +
		`\s+>\s+` +
		`(?P<payload>.*)` +
		`$`,
)
func SetupIngestion(parent context.Context, opts *Options) (*IngestService, error) {
	debugPrint(log.Printf, levelCrazy, "Args=%v, %v\n", parent, opts)

	cfg, err := NewIngestConfigFromOptions(opts)
	if err != nil {
		return nil, err
	}
	return SetupIngestionWithConfig(parent, cfg)
}

func SetupIngestionWithConfig(parent context.Context, cfg IngestConfig) (*IngestService, error) {
	debugPrint(log.Printf, levelCrazy, "Args=%v, %v\n", parent, cfg)

	if cfg.QueueDepth <= 0 {
		cfg.QueueDepth = 10000
	}
	if cfg.ValidateWorkers <= 0 {
		cfg.ValidateWorkers = 8
	}
	if cfg.DBWorkers <= 0 {
		cfg.DBWorkers = 4
	}
	if cfg.MaxLineBytes <= 0 {
		cfg.MaxLineBytes = 16 * 1024
	}
	if cfg.SpoolDir == "" {
		cfg.SpoolDir = "./spool"
	}
	if err := os.MkdirAll(cfg.SpoolDir, 0o750); err != nil {
		return nil, fmt.Errorf("mkdir spool dir: %w", err)
	}

	ctx, cancel := context.WithCancel(parent)

	s := &IngestService{
		cfg:     cfg,
		rawCh:   make(chan *RawMsg, cfg.QueueDepth),
		spoolCh: make(chan ValidatedMsg, cfg.QueueDepth),
		dbCh:    make(chan SeqMsg, cfg.QueueDepth),
		ctx:     ctx,
		cancel:  cancel,
	}

	if strings.TrimSpace(cfg.PostgresDSN) != "" {
		dbCtx, dbCancel := context.WithTimeout(ctx, 5*time.Second)
		defer dbCancel()

		db, err := OpenDB(dbCtx, cfg.PostgresDSN)
		if err != nil {
			if cfg.DBRequired {
				cancel()
				return nil, fmt.Errorf("db connect failed (required): %w", err)
			}
			debugPrint(log.Printf, levelWarning, "warning: db connect failed (ingestion will spool but DB insert disabled): %v", err)
		} else {
			s.db = db
			if ensure := getEnsureSchemaFn(db); ensure != nil {
				if err := ensure(dbCtx); err != nil {
					if cfg.DBRequired {
						_ = db.Close()
						cancel()
						return nil, fmt.Errorf("ensure schema failed (required): %w", err)
					}
					debugPrint(log.Printf, levelWarning, "warning: ensure schema failed: %v", err)
				}
			}
		if err != nil {
			debugPrint(log.Printf, levelInfo, "warning: database has no max seq")
		}

		}
	} else if cfg.DBRequired {
		cancel()
		return nil, fmt.Errorf("db required but PostgresDSN not set")
	}

	// Start stages
	s.startValidators()
	s.startSpooler()
	s.startDBWriters()

	// Start listeners
	if cfg.RawEnabled {
		if err := s.startRawListener(); err != nil {
			s.Stop()
			return nil, err
		}
	}
	if cfg.TLSEnabled {
		if err := s.startTLSListener(); err != nil {
			s.Stop()
			return nil, err
		}
	}

	return s, nil
}

func (s *IngestService) Stop() {
	debugPrint(log.Printf, levelCrazy, "Args=none\n")

	s.cancel()

	// close listeners
	if s.rawLn != nil {
		_ = s.rawLn.Close()
	}
	if s.tlsLn != nil {
		_ = s.tlsLn.Close()
	}

	go func() {
		debugPrint(log.Printf, levelCrazy, "Args=none\n")
		time.Sleep(100 * time.Millisecond)
		s.safeCloseChannels()
	}()

	s.wg.Wait()

	if s.db != nil {
		_ = s.db.Close()
	}
}

func (s *IngestService) safeCloseChannels() {
	debugPrint(log.Printf, levelCrazy, "Args=none\n")

	defer func() { _ = recover() }()
	close(s.rawCh)
}

func (s *IngestService) startValidators() {
	debugPrint(log.Printf, levelCrazy, "Args=none\n")
	for i := 0; i < s.cfg.ValidateWorkers; i++ {
		s.wg.Add(1)
		go func(workerID int) {
			debugPrint(log.Printf, levelCrazy, "Args=%d\n", workerID)

			defer s.wg.Done()
			s.validationWorker(workerID)
		}(i)
	}

	s.wg.Add(1)
	go func() {
		debugPrint(log.Printf, levelCrazy, "Args=none\n")

		defer s.wg.Done()
		<-s.ctx.Done()
	}()
}

func (s *IngestService) validationWorker(workerID int) {
	debugPrint(log.Printf, levelCrazy, "Args=%d\n", workerID)

	for {
		select {
		case <-s.ctx.Done():
			return
		case msg, ok := <-s.rawCh:
			if !ok {
				debugPrint(log.Printf, levelWarning, "rawCh closed => stop\n")
				return
			}

			tenantID, ok := s.resolveTenant(msg)
			if !ok {
				debugPrint(log.Printf, levelWarning, "Not allowed / no tenant mapping\n")
				atomic.AddUint64(&s.linesDropped, 1)
				continue
			}

			_, mt := ParseIngestLine(tenantID, msg.Line)
			if mt != reCompl {
				atomic.AddUint64(&s.linesDropped, 1)
				continue
			}


			atomic.AddUint64(&s.linesAccepted, 1)

			out := ValidatedMsg{
				Line:      msg.Line,
				TenantID:  tenantID,
				PeerIP:    msg.PeerIP,
				Received:  msg.Received,
				Transport: msg.Transport,
			}

			select {
			case <-s.ctx.Done():
				return
			case s.spoolCh <- out:
			}
		}
	}
}

func (s *IngestService) initAuthFuncs() {
	if s.authFuncs == nil {
		s.authFuncs = make(map[string]authFunc, 8)
	}

	// "none"
	s.authFuncs[string(AuthNone)] = func(msg *RawMsg) string {
		dt := strings.TrimSpace(s.cfg.DefaultTenantID)
		if dt == "" {
			return ""
		}
		return dt
	}

	// cert
	s.authFuncs[string(AuthCert)] = func(msg *RawMsg) string {
		return ""
	}

	s.authFuncs[string(AuthAPIKey)] = func(msg *RawMsg) string {
		return s.authAPIKeyFromLine(msg)
	}
}

func (s *IngestService) ResolveTenantFromAuthList(msg *RawMsg, authLst []AuthMode) (string, bool) {
	debugPrint(log.Printf, levelCrazy, "Args=%v, authLst=%v\n", *msg, authLst)

	if len(authLst) == 0 {
		debugPrint(log.Printf, levelWarning, "auth list is empty; dropped\n")
		return "", false
	}

	if s.authFuncs == nil {
		s.initAuthFuncs()
	}

	for _, m := range authLst {
		mode := strings.ToLower(strings.TrimSpace(string(m)))
		if mode == "" {
			debugPrint(log.Printf, levelDebug, "Empty element in auth list, is this intended?\n")
			continue
		}

		fn := s.authFuncs[mode]
		if fn == nil {
			debugPrint(log.Printf, levelWarning, "no auth func for mode=%q; skipping\n", mode)
			continue
		}

		tenantID := fn(msg)
		if tenantID != "" {
			debugPrint(log.Printf, levelDebug, "auth matched mode=%q tenant=%s\n", mode, tenantID)
			return tenantID, true
		}

		debugPrint(log.Printf, levelDebug, "auth no-match mode=%q\n", mode)
	}

	debugPrint(log.Printf, levelDebug, "auth failed: no mode matched\n")
	return "", false
}

func (s *IngestService) resolveTenant(msg *RawMsg) (string, bool) {
	debugPrint(log.Printf, levelCrazy, "Args=%v\n", msg)

	switch msg.Transport {
	case TransportRaw:
		debugPrint(log.Printf, levelDebug, "Raw TCP: message received.\n")
		tenantID, ok := s.ResolveTenantFromAuthList(msg, s.cfg.AuthLst[msg.Transport])
		if !ok {
			return "", false
		}
		return tenantID, true
	case TransportTLS:
		debugPrint(log.Printf, levelDebug, "TLS: message received.\n")
		tenantID, ok := s.ResolveTenantFromAuthList(msg, s.cfg.AuthLst[msg.Transport])
		if !ok {
			return "", false
		}
		return tenantID, true
	default:
		debugPrint(log.Printf, levelWarning, "Unknown transportP: dropped.\n");
		return "", false
	}
}

type tenantSpool struct {
	tenantID string
	path     string
	file     *os.File
	seq      int64

	// sync control
	writesSinceSync int
	lastSync        time.Time
}

func (s *IngestService) startSpooler() {
	debugPrint(log.Printf, levelCrazy, "Args=none\n")

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.spoolerLoop()
	}()
}

func (s *IngestService) spoolerLoop() {
	debugPrint(log.Printf, levelCrazy, "Args=none\n")

	spools := make(map[string]*tenantSpool)

	defer func() {
		for _, sp := range spools {
			_ = sp.file.Close()
		}
		defer func() { _ = recover() }()
		close(s.dbCh)
	}()

	for {
		select {
		case <-s.ctx.Done():
			return
		case msg, ok := <-s.spoolCh:
			if !ok {
				return
			}

			sp, err := s.getOrOpenSpool(spools, msg.TenantID)
			if err != nil {
				debugPrint(log.Printf, levelWarning, "spool open failed tenant=%s: %v", msg.TenantID, err)
				atomic.AddUint64(&s.linesDropped, 1)
				continue
			}
			sp.seq++
			seq := sp.seq
			debugPrint(log.Printf, levelDebug, "Sequence number assigned (%d)\n", seq);

			record := buildSpoolRecord(seq, msg.Line)
			if _, err := sp.file.Write(record); err != nil {
				debugPrint(log.Printf, levelWarning, "spool write failed tenant=%s: %v", msg.TenantID, err)
				atomic.AddUint64(&s.linesDropped, 1)
				continue
			}
			atomic.AddUint64(&s.linesSpooled, 1)

			s.maybeSyncSpool(sp)

			out := SeqMsg{
				Line:      msg.Line,
				TenantID:  msg.TenantID,
				Seq:       seq,
				PeerIP:    msg.PeerIP,
				Received:  msg.Received,
				Transport: msg.Transport,
			}

			select {
			case <-s.ctx.Done():
				return
			case s.dbCh <- out:
			}
		}
	}
}

func (s *IngestService) getOrOpenSpool(spools map[string]*tenantSpool, tenantID string) (*tenantSpool, error) {
	debugPrint(log.Printf, levelCrazy, "Args=%v, %d\n", spools, tenantID)

	if sp, ok := spools[tenantID]; ok {
		return sp, nil
	}

	filename := tenantID + ".log"
	path := filepath.Join(s.cfg.SpoolDir, filename)
	debugPrint(log.Printf, levelDebug, "tenant spool file %s\n", filename)


	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		debugPrint(log.Printf, levelWarning, "can't create file\n")
		return nil, err
	}

	seq, err := readLastSeqFromSpoolTail(path)
	if err != nil {
		debugPrint(log.Printf, levelInfo, "cant fetch seq from spool files for Tenant %s\n", tenantID)
	}
	if s.db != nil {
		if dbSeq, derr := s.dbMaxSeq(s.ctx, tenantID); derr == nil && dbSeq > seq {
			if seq < dbSeq {
				seq = dbSeq
				debugPrint(log.Printf, levelInfo, "db ishigher than spool, Tenant %s new initial seq is %d\n", tenantID, seq)
			}
		}
	}

	sp := &tenantSpool{
		tenantID: tenantID,
		path:     path,
		file:     f,
		seq:      seq,
		lastSync: time.Now(),
	}
	spools[tenantID] = sp
	return sp, nil
}

func buildSpoolRecord(seq int64, line string) []byte {
	debugPrint(log.Printf, levelCrazy, "Args=%d, %s\n", seq, line)

	line = strings.ReplaceAll(line, "\r", `\r`)
	line = strings.ReplaceAll(line, "\n", `\n`)
	line = strings.TrimRight(line, "\r\n")

	return []byte(fmt.Sprintf("%d\t%s\n", seq, line))
}

func (s *IngestService) maybeSyncSpool(sp *tenantSpool) {
	debugPrint(log.Printf, levelCrazy, "Args=%v\n", sp)

	now := time.Now()
	if s.cfg.SpoolSyncEveryN > 0 {
		sp.writesSinceSync++
		if sp.writesSinceSync >= s.cfg.SpoolSyncEveryN {
			_ = sp.file.Sync()
			sp.writesSinceSync = 0
			sp.lastSync = now
			return
		}
	}
	if s.cfg.SpoolSyncEvery > 0 && now.Sub(sp.lastSync) >= s.cfg.SpoolSyncEvery {
		_ = sp.file.Sync()
		sp.writesSinceSync = 0
		sp.lastSync = now
	}
}

func (s *IngestService) startDBWriters() {
	debugPrint(log.Printf, levelCrazy, "Args=none\n")

	for i := 0; i < s.cfg.DBWorkers; i++ {
		s.wg.Add(1)
		go func(workerID int) {
			debugPrint(log.Printf, levelCrazy, "Args=%d\n", workerID)

			defer s.wg.Done()
			s.dbWorker(workerID)
		}(i)
	}
}

func (s *IngestService) dbWorker(workerID int) {
	debugPrint(log.Printf, levelCrazy, "Args=%d\n", workerID)

	if s.db == nil {
		for {
			select {
			case <-s.ctx.Done():
				return
			case _, ok := <-s.dbCh:
				if !ok {
					debugPrint(log.Printf, levelDebug, "Success (No DB)\n")
					return
				}
				debugPrint(log.Printf, levelWarning, "Can't access db channel (No DB)\n")
				atomic.AddUint64(&s.linesDBFail, 1)
			}
		}
	}

	backoff := 200 * time.Millisecond
	maxBackoff := 5 * time.Second

	for {
		select {
		case <-s.ctx.Done():
			return
		case msg, ok := <-s.dbCh:
			if !ok {
				debugPrint(log.Printf, levelDebug, "Can't access db channel\n")
				return
			}

			ev, _ := ParseIngestLine(msg.TenantID, msg.Line)
			tmp := msg.PeerIP.String()
			ev.Transport = msg.Transport.String()
			ev.SrcIP = &tmp

			for {
				err := s.dbInsertWithSeq(s.ctx, msg, ev)
				if err == nil {
					debugPrint(log.Printf, levelDebug, "DB insert Success\n")
					atomic.AddUint64(&s.linesDBOK, 1)
					backoff = 200 * time.Millisecond
					break
				}
				debugPrint(log.Printf, levelWarning, "DB insert Failure (%v)\n", err)
				atomic.AddUint64(&s.linesDBFail, 1)

				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return
				}

				select {
				case <-s.ctx.Done():
					return
				case <-time.After(backoff):
				}
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
		}
	}
}

func (s *IngestService) dbInsertWithSeq(ctx context.Context, msg SeqMsg, ev Event) error {
	debugPrint(log.Printf, levelCrazy, "Args=%v, %v, %v\n", ctx, msg, ev)

	if inserter := getInsertEventWithSeqFn(s.db); inserter != nil {
		return inserter(ctx, ev, msg.Seq)
	}
	return fmt.Errorf("db insert not implemented: add DB.InsertEventWithSeq")
}

func (s *IngestService) dbMaxSeq(ctx context.Context, tenantID string) (int64, error) {
	debugPrint(log.Printf, levelCrazy, "Args=%v, %s\n", ctx, tenantID)

	if maxer := getMaxSeqFn(s.db); maxer != nil {
		return maxer(ctx, tenantID)
	}
	return 0, fmt.Errorf("db max seq not implemented")
}

func (s *IngestService) startRawListener() error {
	debugPrint(log.Printf, levelCrazy, "Args=none\n")

	ln, err := net.Listen("tcp", s.cfg.RawAddr)
	if err != nil {
		return fmt.Errorf("raw listen %s: %w", s.cfg.RawAddr, err)
	}
	s.rawLn = ln

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		debugPrint(log.Printf, levelInfo, "ingest raw listening on %s", s.cfg.RawAddr)
		s.acceptLoop(ln, TransportRaw)
	}()
	return nil
}

func (s *IngestService) startTLSListener() error {
	debugPrint(log.Printf, levelCrazy, "Args=none\n")

	if s.cfg.TLSConfig == nil {
		return fmt.Errorf("tls enabled but TLSConfig is nil")
	}
	ln, err := tls.Listen("tcp", s.cfg.TLSAddr, s.cfg.TLSConfig)
	if err != nil {
		return fmt.Errorf("tls listen %s: %w", s.cfg.TLSAddr, err)
	}
	s.tlsLn = ln

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		debugPrint(log.Printf, levelInfo, "ingest tls listening on %s", s.cfg.TLSAddr)
		s.acceptLoop(ln, TransportTLS)
	}()
	return nil
}

func (s *IngestService) acceptLoop(ln net.Listener, tr Transport) {
	debugPrint(log.Printf, levelCrazy, "Args=%v, %d\n", ln, tr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
			}
			debugPrint(log.Printf, levelError, "accept error (%s): %v", tr.String(), err)
			continue
		}

		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			defer c.Close()

			peerIP := peerAddrIP(c.RemoteAddr())
			if !peerIP.IsValid() {
				return
			}

			s.readConnLines(c, peerIP, tr)
		}(conn)
	}
}

func (s *IngestService) isRawPeerAllowed(ip netip.Addr, tenantID string) bool {
	debugPrint(log.Printf, levelCrazy, "Args=%v\n", ip)

	acl, ok := s.cfg.RawCIDRRules[tenantID]
	if !ok {
		return false
	}
	for _, r := range acl {
		if r.Prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *IngestService) readConnLines(r io.Reader, peerIP netip.Addr, tr Transport) {
	debugPrint(log.Printf, levelCrazy, "Args=%v,%v, %d\n", r, peerIP, tr)
	max := s.cfg.MaxLineBytes
	if max <= 0 {
		max = 16 * 1024
	}

	data, tooBig, err := readAllLimit(r, max+1)
	if err != nil {
		debugPrint(log.Printf, levelDebug, "Line dropped due to an error: %w\n", err)
		atomic.AddUint64(&s.linesDropped, 1)
		return
	}
	if tooBig {
		debugPrint(log.Printf, levelDebug, "Line dropped due to size: too long\n")
		atomic.AddUint64(&s.linesDropped, 1)
		return
	}
	if len(data) == 0 {
		debugPrint(log.Printf, levelNotice, "Empty line received\n")
		return
	}

	lfCount := 0
	for _, b := range data {
		if b == '\n' {
			lfCount++
		}
	}

	if lfCount != 1 {
		debugPrint(log.Printf, levelDebug, "Line dropped due to carrige return: only one is allowed, there's more!\n")
		atomic.AddUint64(&s.linesDropped, 1)
		return
	}

	if data[len(data)-1] != '\n' {
		debugPrint(log.Printf, levelDebug, "Line dropped due to carrige return: Only one expected at the end\n")
		atomic.AddUint64(&s.linesDropped, 1)
		return
	}

	data = data[:len(data)-1]
	if len(data) > 0 && data[len(data)-1] == '\r' {
		debugPrint(log.Printf, levelCrazy, "Fixing line feed\n")
		data = data[:len(data)-1]
	}

	for i := 0; i < len(data); i++ {
		if data[i] == '\n' || data[i] == '\r' {
			data[i] = ' '
		}
	}

	line := strings.TrimSpace(string(data))
	if line == "" {
		return
	}

	debugPrint(log.Printf, levelDebug, "Ingested line is \"%s\"\n", line)
	msg := RawMsg{
		Line:      line,
		PeerIP:    peerIP,
		Received:  time.Now(),
		Transport: tr,
	}

	debugPrint(log.Printf, levelDebug, "Parsing complete (%v): send to next level in pipeline\n", msg)
	select {
	case <-s.ctx.Done():
		return
	case s.rawCh <- &msg:
	}
}

func readAllLimit(r io.Reader, limit int) ([]byte, bool, error) {
	if limit <= 0 {
		return nil, true, nil
	}

	const (
		defaultIdleTimeout = 750 * time.Millisecond
		readChunkSize      = 4096
	)

	var (
		buf     = make([]byte, 0, minInt(limit, readChunkSize))
		tmp     = make([]byte, readChunkSize)
		total   = 0
		conn, _ = r.(net.Conn)
	)

	for {
		if conn != nil {
			_ = conn.SetReadDeadline(time.Now().Add(defaultIdleTimeout))
		}

		n, err := r.Read(tmp)
		if n > 0 {
			total += n
			if total > limit {
				return nil, true, nil
			}
			buf = append(buf, tmp[:n]...)
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				return buf, false, nil
			}

			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return buf, false, nil
			}

			return nil, false, err
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt64(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func peerAddrIP(a net.Addr) netip.Addr {
	debugPrint(log.Printf, levelCrazy, "Args=%v\n", a)

	host, _, err := net.SplitHostPort(a.String())
	if err != nil {
		host = a.String()
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return ip
}

type ensureSchemaFn func(context.Context) error
type insertEventWithSeqFn func(context.Context, Event, int64) error
type maxSeqFn func(context.Context, string) (int64, error)

func getEnsureSchemaFn(db *DB) ensureSchemaFn {
	debugPrint(log.Printf, levelCrazy, "Args=%v\n", db)
	return nil
}

func getInsertEventWithSeqFn(db *DB) insertEventWithSeqFn {
	debugPrint(log.Printf, levelCrazy, "Args=%v\n", db)
	if db == nil {
		debugPrint(log.Printf, levelError, "No DB: failing.\n")
		return nil
	}
	return db.InsertEventWithSeq
}

func getMaxSeqFn(db *DB) maxSeqFn {
	debugPrint(log.Printf, levelCrazy, "Args=%v\n", db)
	if db == nil {
		debugPrint(log.Printf, levelError, "No DB: failing.\n")
		return nil
	}
	return db.MaxSeq
}

func readLastSeqFromSpoolTail(path string) (int64, error) {
	debugPrint(log.Printf, levelCrazy, "Args=%s\n", path)
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := st.Size()
	if size == 0 {
		return 0, fmt.Errorf("empty spool")
	}

	const maxTail = int64(64 * 1024)
	var start int64
	if size > maxTail {
		start = size - maxTail
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return 0, err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return 0, err
	}

	s := string(b)
	s = strings.TrimRight(s, "\r\n")
	if s == "" {
		return 0, fmt.Errorf("no lines")
	}
	idx := strings.LastIndexByte(s, '\n')
	var last string
	if idx >= 0 {
		last = s[idx+1:]
	} else {
		last = s
	}
	tab := strings.IndexByte(last, '\t')
	if tab <= 0 {
		return 0, fmt.Errorf("bad spool line")
	}
	seqStr := last[:tab]
	var seq int64
	_, err = fmt.Sscan(seqStr, &seq)
	if err != nil {
		return 0, err
	}
	return seq, nil
}

func NewIngestConfigFromOptions(opts *Options) (IngestConfig, error) {
	debugPrint(log.Printf, levelCrazy, "Args=%v\n", opts)
	var cfg IngestConfig
	var err error

	cfg.RawEnabled = opts.Cfg.Server.IngestClear.Enabled
	cfg.RawAddr    = opts.Cfg.Server.IngestClear.Addr
	cfg.TLSEnabled = opts.Cfg.Server.IngestTLS.Enabled
	cfg.TLSAddr    = opts.Cfg.Server.IngestTLS.Addr
	cfg.AuthLst    = make(map[Transport][]AuthMode, 2)

	cfg.AuthLst[TransportRaw] = opts.Cfg.Server.IngestClear.Auth
	cfg.AuthLst[TransportTLS] = opts.Cfg.Server.IngestTLS.Auth

	if cfg.TLSEnabled {
		cert, err := tls.LoadX509KeyPair(opts.Cfg.Globals.Identity.CertFile, opts.Cfg.Globals.Identity.KeyFile)
		if err != nil {
			return cfg, fmt.Errorf("ingestion: ssl creation error (%w)\n", err)
		}
		tlsConfig := tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion: tls.VersionTLS12,
		}
		cfg.TLSConfig  = &tlsConfig
	}
	cfg.PostgresDSN = opts.Cfg.DB.PostgresDSN
	cfg.DefaultTenantID = opts.Cfg.Globals.DefaultTenantID
	cfg.Pepper = opts.Cfg.Globals.Pepper
	cfg.RawCIDRRules, err  = parseCfgCidrLst(opts)
	if err != nil {
		return cfg, fmt.Errorf("ingestion: error parsing CIDR (%w)\n", err)
	}

	if !cfg.RawEnabled && !cfg.TLSEnabled {
		return cfg, fmt.Errorf("ingestion: no listeners enabled (raw/tls)")
	}
	return cfg, nil
}

func parseCfgCidrLst(opts *Options) (map[string][]CIDRTenantRule, error) {
	// Build ACL lookup by ID
	aclByID := make(map[string]ACL, len(opts.Cfg.ACL))
	for _, acl := range opts.Cfg.ACL {
		if acl.ID == "" {
			continue
		}
		aclByID[acl.ID] = acl
	}

	result := make(map[string][]CIDRTenantRule, len(opts.Cfg.Tenants))

	for _, tenant := range opts.Cfg.Tenants {
		if tenant.TenantID == "" {
			return nil, fmt.Errorf("tenant with empty tenantID")
		}
		if tenant.ACL == "" {
			return nil, fmt.Errorf("tenant %q has empty acl reference", tenant.TenantID)
		}

		acl, ok := aclByID[tenant.ACL]
		if !ok {
			return nil, fmt.Errorf(
				"tenant %q references unknown acl %q",
				tenant.TenantID, tenant.ACL,
			)
		}

		rules := make([]CIDRTenantRule, 0, len(acl.Rules))
		for _, r := range acl.Rules {
			prefix, err := netip.ParsePrefix(r.CIDR)
			if err != nil {
				return nil, fmt.Errorf(
					"tenant %q acl %q invalid CIDR %q: %w",
					tenant.TenantID, acl.ID, r.CIDR, err,
				)
			}

			var action bool
			switch strings.ToLower(r.Action) {
			case "allow":
				action = true
			case "deny":
				action = false
			default:
				return nil, fmt.Errorf(
					"tenant %q acl %q rule %q has invalid action %q",
					tenant.TenantID, acl.ID, r.Name, r.Action,
				)
			}

			rules = append(rules, CIDRTenantRule{
				Prefix: prefix,
				Action: action,
				Name:   r.Name,
			})
		}

		result[tenant.TenantID] = rules
	}

	return result, nil
}
