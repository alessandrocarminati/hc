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
