package main

import (
	"fmt"
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

func parseLogEntry(logStr string) (LogEntry, error) {
	matches := logRegex.FindStringSubmatch(logStr)
	if len(matches) != 5 {
		return LogEntry{}, fmt.Errorf("invalid log format")
	}

	timestamp, err := time.Parse("20060102.150405", matches[1])
	if err != nil {
		return LogEntry{}, err
	}

	return LogEntry{
		Timestamp:	timestamp,
		SessionID:	matches[2],
		Message:	matches[3],
		Detail:		matches[4],
		Raw:		logStr,
	}, nil
}
