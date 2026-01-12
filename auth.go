package main

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type authRes uint8

const (
	AuthNoMatch authRes = iota
	AuthAllow
	AuthDeny
)

type cidrRule struct {
	Net netip.Prefix
}

type connData struct {
	// Network / transport
	SrcAddr     netip.Addr
	SrcPort     uint16
	DstAddr     netip.Addr
	DstPort     uint16
	IsTLS       bool
	ServerName  string

	// HTTP
	Method  string
	Path    string
	Host    string
	Headers http.Header

	// TLS
	TLS              *tls.ConnectionState
	PeerCertificates []*tls.Certificate
	PeerCerts        []*tls.Certificate
}

type authFn func(c connData) authRes

func runAuthPipeline(c connData, fns []authFn) authRes {
	if len(fns) == 0 {
		return AuthDeny
	}
	for _, fn := range fns {
		if fn == nil {
			continue
		}
		r := fn(c)
		switch r {
		case AuthDeny:
			return AuthDeny
		case AuthAllow:
			return AuthAllow
		case AuthNoMatch:
			continue
		default:
			return AuthDeny
		}
	}
	return AuthDeny
}

func connDataFromRequest(r *http.Request) connData {
	var cd connData

	// HTTP
	cd.Method = r.Method
	cd.Path = r.URL.Path
	cd.Host = r.Host
	cd.Headers = r.Header.Clone()

	// Transport/TLS
	cd.IsTLS = r.TLS != nil
	cd.TLS = r.TLS
	if r.TLS != nil {
		cd.ServerName = r.TLS.ServerName
	} else {
		cd.ServerName = r.Host
	}

	if host, portStr, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil {
		if ap, err := netip.ParseAddrPort(host + ":" + portStr); err == nil {
			cd.SrcAddr = ap.Addr()
			cd.SrcPort = ap.Port()
		} else {
			if ip := net.ParseIP(host); ip != nil {
				if a, ok := netip.AddrFromSlice(ip); ok {
					cd.SrcAddr = a
				}
			}
		}
	}

	return cd
}
