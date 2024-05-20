package main

import (
	"time"
)

type LogTree struct {
	Year    map[int]*YearNode
}

type YearNode struct {
	Month map[time.Month]*MonthNode
}

type MonthNode struct {
	Day map[int]*DayNode
}

type DayNode struct {
	Session map[string][]LogEntry
}

func buildLogTree(logs []string) (*LogTree, error) {
	tree := &LogTree{Year: make(map[int]*YearNode)}

	for _, logStr := range logs {
		err := ProcessEntryTree(logStr, tree)
		if err!=nil {
			return nil, err
		}
	}

	return tree, nil
}

func ProcessEntryTree(logStr string, tree *LogTree) (error) {
	entry, err := parseLogEntry(logStr)
	if err != nil {
		return err
	}

	year := entry.Timestamp.Year()
	month := entry.Timestamp.Month()
	day := entry.Timestamp.Day()

	if tree.Year[year] == nil {
		tree.Year[year] = &YearNode{Month: make(map[time.Month]*MonthNode)}
	}
	if tree.Year[year].Month[month] == nil {
		tree.Year[year].Month[month] = &MonthNode{Day: make(map[int]*DayNode)}
	}
	if tree.Year[year].Month[month].Day[day] == nil {
		tree.Year[year].Month[month].Day[day] = &DayNode{Session: make(map[string][]LogEntry)}
	}

	tree.Year[year].Month[month].Day[day].Session[entry.SessionID] = append(tree.Year[year].Month[month].Day[day].Session[entry.SessionID], entry)
	return nil
}

