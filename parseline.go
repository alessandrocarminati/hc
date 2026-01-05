package main

import (
	"regexp"
	"strings"
	"log"
)

type regexpmatch uint8

const (
	reCompl regexpmatch = iota
	reSess
	reSessLoose
	reNoSess
	reNoSessLoose
	reTSOnly
	noMatch
)

func (t regexpmatch) String() string {
	switch t {
	case reCompl:
		return "reCompl"
	case reSess:
		return "reSess"
	case reSessLoose:
		return "reSessLoose"
	case reNoSess:
		return "reNoSess"
	case reNoSessLoose:
		return "reNoSessLoose"
	case reTSOnly:
		return "reTSOnly"
	case noMatch:
		return "noMatch"
	default:
		return "unknown"
	}
}

var ingestRegexes = []struct {
	kind regexpmatch
	re   *regexp.Regexp
}{
	{
		kind: reCompl,
		// ts - sid - host [cwd=...] > payload
		re: regexp.MustCompile(
			`^` +
				`(?P<ts>\d{8}\.\d{6})` +
				`\s*-\s*` +
				`(?P<sid>[0-9a-fA-F]{8})` +
				`\s*-\s*` +
				`(?P<host>[A-Za-z0-9._-]+)` +
				`(?:\s+\[cwd=(?P<cwd>[^\]]+)\])?` +
				`\s+>\s+` +
				`(?P<payload>.*)` +
				`$`,
		),
	},
	{
		kind: reSess,
		// ts - sid - host  <two+ spaces> payload  (older formatting)
		re: regexp.MustCompile(`^(\d{8}\.\d{6})\s*-\s*([0-9a-fA-F]{8})\s*-\s*(.+?)\s{2,}(.*)$`),
	},
	{
		kind: reSessLoose,
		// ts - sid - host <one+ spaces> payload
		re: regexp.MustCompile(`^(\d{8}\.\d{6})\s*-\s*([0-9a-fA-F]{8})\s*-\s*(.+?)\s+(.*)$`),
	},
	{
		kind: reNoSess,
		// ts - host  <two+ spaces> payload
		re: regexp.MustCompile(`^(\d{8}\.\d{6})\s*-\s*(.+?)\s{2,}(.*)$`),
	},
	{
		kind: reNoSessLoose,
		// ts - host <one+ spaces> payload
		re: regexp.MustCompile(`^(\d{8}\.\d{6})\s*-\s*(.+?)\s+(.*)$`),
	},
	{
		kind: reTSOnly,
		// ts <spaces> payload
		re: regexp.MustCompile(`^(\d{8}\.\d{6})\s+(.*)$`),
	},
}

func ParseIngestLine(tenantID, line string) (Event, regexpmatch) {
	debugPrint(log.Printf, levelCrazy, "Args=%s, %s\n", tenantID, line)

	ev := Event{
		TenantID: tenantID,
		RawLine:  line,
	}

	s := strings.TrimRight(line, "\r\n")
	s = strings.TrimSpace(s)
	if s == "" {
		ev.SessionID = "unknown"
		ev.HostFQDN = "unknown"
		ev.ParseOK = false
		ev.RawLine = sanitizeUTF8(ev.RawLine)
		return ev, noMatch
	}

	var (
		tsStr   string
		sid     string
		host    string
		cwd     string
		payload string
		mKind   = noMatch
	)

	for _, rr := range ingestRegexes {
		m := rr.re.FindStringSubmatch(s)
		if m == nil {
			continue
		}

		mKind = rr.kind

		switch rr.kind {
		case reCompl:
			tsStr = m[1]
			sid = m[2]
			host = m[3]
			cwd = m[4]
			payload = m[5]

		case reSess, reSessLoose:
			tsStr = m[1]
			sid = m[2]
			host = m[3]
			payload = m[4]

		case reNoSess, reNoSessLoose:
			tsStr = m[1]
			host = m[2]
			payload = m[3]

		case reTSOnly:
			tsStr = m[1]
			payload = m[2]
		}

		break
	}

	if mKind == noMatch {
		ev.SessionID = "unknown"
		ev.HostFQDN = "unknown"
		ev.ParseOK = false
		ev.RawLine = sanitizeUTF8(ev.RawLine)
		return ev, noMatch
	}

	if tsStr != "" {
		if t, ok := parseTS(tsStr); ok {
			ev.TSClient = &t
		} else {
			ev.ParseOK = false
		}
	}

	host = strings.TrimSpace(host)
	if host == "" || strings.ContainsAny(host, " \t\r\n") {
		ev.HostFQDN = "unknown"
		ev.ParseOK = false
	} else {
		ev.HostFQDN = host
	}

	sid = strings.TrimSpace(sid)
	if sid == "" {
		ev.SessionID = "unknown"
		if mKind == reCompl || mKind == reSess || mKind == reSessLoose {
			ev.ParseOK = false
		}
	} else {
		ev.SessionID = strings.ToLower(sid)
	}

	cwd = strings.TrimSpace(cwd)
	if cwd != "" {
		ev.CWD = strPtr(cwd)
	}

	cmdText := strings.TrimSpace(payload)
	if strings.HasPrefix(cmdText, ">") {
		cmdText = strings.TrimSpace(strings.TrimPrefix(cmdText, ">"))
	}
	if cmdText != "" {
		ev.Cmd = strPtr(cmdText)
	}

	switch mKind {
	case reCompl, reSess, reSessLoose:
		ev.ParseOK = ev.TSClient != nil && ev.HostFQDN != "unknown" && ev.SessionID != "unknown" && ev.Cmd != nil && strings.TrimSpace(*ev.Cmd) != ""
	case reNoSess, reNoSessLoose:
		ev.ParseOK = ev.TSClient != nil && ev.HostFQDN != "unknown" && ev.Cmd != nil && strings.TrimSpace(*ev.Cmd) != ""
	case reTSOnly:
		ev.ParseOK = false
	}

	ev.RawLine = sanitizeUTF8(ev.RawLine)
	if ev.Cmd != nil {
		c := sanitizeUTF8(*ev.Cmd)
		c = strings.TrimSpace(c)
		ev.Cmd = &c
	}

	return ev, mKind
}
