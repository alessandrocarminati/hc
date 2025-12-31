package main

import (
	"io"
	"os"
	"net"
	"time"
	"fmt"
	"strconv"
	"strings"
	"log"
)

var bufsiz int = 4 * 1048576


type data struct {
	Str []byte
	Size int
	Keep  bool
}

func doRunServe(version string, args []string) {

	opts, err := getRuntimeConf(version, args)
	if err != nil {
		fmt.Printf("%v", err)
		os.Exit(1)
	}

	debugPrint(log.Printf, levelDebug, "Start serving\n")

	serve(opts)
}

func ResolveOptions(cfg Config, cl CommandLine, verstr string) (*Options, error) {
	o := Options{
		Cfg:	     cfg,
		LogLevel:	cl.LogLevel,
	}

	if !ValidateServer(o.Cfg.Server.ListnerClear) {
		return  nil, fmt.Errorf("Invalid clear listner")
	}

	if !ValidateServer(o.Cfg.Server.ListnerTLS) {
		return  nil, fmt.Errorf("Invalid tls listner")
	}

	if !ValidateServer(o.Cfg.Server.ListnerSearch) {
		return  nil, fmt.Errorf("Invalid search listner")
	}

	if !ValidateServer(o.Cfg.Server.HTTP) {
		return  nil, fmt.Errorf("Invalid http listner")
	}

	if !ValidateServer(o.Cfg.Server.HTTPS) {
		return  nil, fmt.Errorf("Invalid https listner")
	}

	if !ValidateTags(o.Cfg.Parser) {
		return  nil, fmt.Errorf("Invalid tag configuration")
	}

	if o.Cfg.HistoryFile == "" {
		f, err := os.CreateTemp("", "tmp")
		if err != nil {
			return  nil, fmt.Errorf("cant create temp file")
		}
		defer f.Close()
		o.Cfg.HistoryFile = f.Name()
	}

	o.LogLevel = cl.LogLevel
	o.Verstr = verstr

	return &o, nil
}

func ValidateTags(Parser ParserConfig) bool {
	if Parser.TagsFile != "" {
		return true
	}
	return false
}

func ValidateServer(s ListenerConfig) bool {
	debugPrint(log.Printf, levelCrazy, "ValidateServer: addr=%s enabled=%t\n", s.Addr, s.Enabled)

	if s.Enabled && s.Addr == "" {
		return false
	}
	return true
}

func serve(opts *Options) {

	ch := make(chan data, 100)
	var conn net.Conn
	var err error

	history, err := NewHistory(opts.Cfg.Parser.TagsFile, opts.Cfg.HistoryFile)
	if err != nil {
		panic(err)
	}
	history.LoadLogFromFile()
	go cwdata(history, ch)
	go searcher(history, opts.Cfg.Server.ListnerSearch.Addr)
	go http_present(history, opts.Cfg.Server.HTTP.Addr, opts.Verstr)
	ln, err := net.Listen("tcp", opts.Cfg.Server.ListnerClear.Addr)
	if err != nil {
		panic(err)
	}
	for {
		debugPrint(log.Printf, levelDebug, "Connection\n")
		conn, err = ln.Accept()
		if err != nil {
			panic( err)
			}
		go receivedata(conn, ch)
		}

	debugPrint(log.Printf, levelDebug, "exit\n")
	close(ch)
}

func receivedata(rd net.Conn, ch chan data) {

	defer 	debugPrint(log.Printf, levelCrazy, "dead\n")
	debugPrint(log.Printf, levelCrazy, "alive\n")
	b := make([]byte, bufsiz)

	for {
		_ = rd.SetReadDeadline(time.Now().Add(500 * time.Millisecond))

		m, err := rd.Read(b)

		if m == 0 || err == io.EOF {
			break
			}
		debugPrint(log.Printf, levelCrazy, "sent 2 channel\n")
		ch <- data{Str:b, Size:m, Keep: true}
	}
	debugPrint(log.Printf, levelCrazy, "Connection closed\n")
	rd.Close()
}

func cwdata(h *History, ch chan data) {

	keep:=true
	defer 	debugPrint(log.Printf, levelCrazy, "dead\n")
	debugPrint(log.Printf, levelCrazy, "alive\n")

	for keep==true {
		debugPrint(log.Printf, levelCrazy, "Look Checkpoint1\n")
		b := <- ch
		debugPrint(log.Printf, levelCrazy, "Look Checkpoint2\n")
		cmd := string(b.Str[0:b.Size])
		h.SaveLog(strings.TrimSuffix(cmd, "\n"))
		h.ProcessCommand(strings.TrimSuffix(cmd, "\n"), false)
		debugPrint(log.Printf, levelCrazy, "Look Checkpoint3\n")
		keep=b.Keep;
	}

}

func searcher(h *History, port string) {

	listener, err := net.Listen("tcp", port)
	if err != nil {
		fmt.Println("Error listening:", err.Error())
		return
	}
	defer listener.Close()
	 debugPrint(log.Printf, levelCrazy, "Server listening on %s\n", port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error accepting connection:", err.Error())
			return
		}

		go handleConnection(conn, h)
	}
}

func handleConnection(conn net.Conn, h *History) {
	defer conn.Close()

	buf := make([]byte, 1024)

	n, err := conn.Read(buf)
	if err != nil {
		fmt.Println("Error reading:", err.Error())
		return
	}

	receivedString := string(buf[:n])
	tokens := parseTokens(receivedString)
	result := do_search(h, tokens[0], strings.TrimSuffix(tokens[1], "\n"))
	_, err = conn.Write([]byte(result))
	if err != nil {
		fmt.Println("Error writing:", err.Error())
		return
	}
}

func parseTokens(s string) []string {
	return strings.Split(s, ":")
}

func do_search(h *History, type_, text string) string {
	ret := ""
	switch type_ {
		case "tag":
		for _, item := range h.ParsedItems {
			if v, ok := item.Tags[text];  v && ok {
				ret = ret + fmt.Sprintf("%s %08x - %s, ==> %s\n", item.Date.Format("2006-01-02 15:04:05"), item.SessionID, item.HostName, item.Command)
			}
		}
		case "text":
		for _, item := range h.ParsedItems {
			if strings.Contains(item.Command, text) {
				ret = ret + fmt.Sprintf("%s %08x - %s, ==> %s\n", item.Date.Format("2006-01-02 15:04:05"), item.SessionID, item.HostName, item.Command)
			}
		}
		case "last":
			n, err := strconv.Atoi(text)
			if err != nil {
				ret = "error\n"
				break
			}
			for i:= len(h.ParsedItems)-n; i<len(h.ParsedItems); i++ {
				ret = ret + fmt.Sprintf("%s %08x - %s, ==> %s\n", h.ParsedItems[i].Date.Format("2006-01-02 15:04:05"), h.ParsedItems[i].SessionID, h.ParsedItems[i].HostName, h.ParsedItems[i].Command)
			}
		case "raw":
		for _, line := range h.RawLog {
			if strings.Contains(line, text) {
				ret = ret + fmt.Sprintf("%s\n", line)
			}
		}
		case "debug":
			n, err := strconv.Atoi(text)
			if err != nil {
				ret = "error\n"
				break
			}
			switch n {
				case 0: DebugLevel = levelPanic.Value
				case 1: DebugLevel = levelError.Value
				case 2: DebugLevel = levelWarning.Value
				case 3: DebugLevel = levelNotice.Value
				case 4: DebugLevel = levelInfo.Value
				case 5: DebugLevel = levelDebug.Value
				case 6: DebugLevel = levelCrazy.Value
				default: ret = "error\n"
			}
			ret = "Done\n"
		default:
			ret = "unsupported\n"
	}
	return ret
}
