package main

import (
	"fmt"
	"regexp"
	"strings"
)

type GrepPipeline struct {
	re1 *regexp.Regexp
	re2 *regexp.Regexp
	re3 *regexp.Regexp

	color string
}

func CompileGrepPipeline(g1, g2, g3, color string) (*GrepPipeline, error) {
	p := &GrepPipeline{
		color: color,
	}

	var err error
	if strings.TrimSpace(g1) != "" {
		p.re1, err = regexp.Compile(g1)
		if err != nil {
			return nil, fmt.Errorf("invalid grep1: %w", err)
		}
	}
	if strings.TrimSpace(g2) != "" {
		p.re2, err = regexp.Compile(g2)
		if err != nil {
			return nil, fmt.Errorf("invalid grep2: %w", err)
		}
	}
	if strings.TrimSpace(g3) != "" {
		p.re3, err = regexp.Compile(g3)
		if err != nil {
			return nil, fmt.Errorf("invalid grep3: %w", err)
		}
	}

	return p, nil
}

func (p *GrepPipeline) ColorEnabled() bool {
	return p != nil && p.color == "always"
}

func (p *GrepPipeline) Match(line string) bool {
	if p == nil {
		return true
	}
	if p.re1 != nil && !p.re1.MatchString(line) {
		return false
	}
	if p.re2 != nil && !p.re2.MatchString(line) {
		return false
	}
	if p.re3 != nil && !p.re3.MatchString(line) {
		return false
	}
	return true
}

func (p *GrepPipeline) Highlight(line string) string {
	if p == nil || p.color != "always" {
		return line
	}
	const reset = "\x1b[0m"
	const red = "\x1b[31m"
	const green = "\x1b[32m"
	const yellow = "\x1b[33m"

	s := line
	if p.re1 != nil {
		s = p.re1.ReplaceAllStringFunc(s, func(m string) string { return red + m + reset })
	}
	if p.re2 != nil {
		s = p.re2.ReplaceAllStringFunc(s, func(m string) string { return green + m + reset })
	}
	if p.re3 != nil {
		s = p.re3.ReplaceAllStringFunc(s, func(m string) string { return yellow + m + reset })
	}
	return s
}

func IsPlainSubstring(s string) bool {
	if s == "" {
		return true
	}
	return !strings.ContainsAny(s, `.+*?()|[]{}^$\`)
}
