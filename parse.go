package main

import (
	"regexp"
	"time"
)

type LogEntry struct {
	Timestamp	time.Time
	SessionID	string
	Message		string
	Detail		string
	Raw		string
}

var logRegex = regexp.MustCompile(`^([0-9]{8}\.[0-9]{6}) - ([0-9a-f]{8}) - (.*)\> (.*)$`)

