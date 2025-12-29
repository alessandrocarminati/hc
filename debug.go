package main

import (
	"runtime"
	"fmt"
	"os"
	"strings"
)

type DebugLevels struct {
	Value	int
	Label	string
}
var (
	levelPanic	= DebugLevels{0, "panic"}
	levelError	= DebugLevels{1, "error"}
	levelWarning	= DebugLevels{2, "warning"}
	levelNotice	= DebugLevels{3, "notice"}
	levelInfo	= DebugLevels{4, "info"}
	levelDebug	= DebugLevels{5, "debug"}
	levelCrazy	= DebugLevels{6, "crazy"}
)

var debugLevels = []DebugLevels{
	levelPanic,
	levelError,
	levelWarning,
	levelNotice,
	levelInfo,
	levelDebug,
	levelCrazy,
}

var DebugLevel int
var Dacl string

type PrintFunc func(format string, a ...interface{})

func debugPrint(printFunc PrintFunc, level DebugLevels,  format string, a ...interface{}) {
	var s string

	if level.Value<=DebugLevel {
		pc, _, _, ok := runtime.Caller(1)
		s = "?"
		if ok {
			fn := runtime.FuncForPC(pc)
			if fn != nil {
				s = fn.Name()
			}
		}
		newformat := fmt.Sprintf("(%s)[" + s + "] ", level.Label) + format
		if Dacl == "All" {
			printFunc(newformat,  a...)
		} else {
			fncs := strings.Split(Dacl,",")
			for _, fnc := range fncs {
				if strings.HasSuffix(s, fnc) {
					printFunc(newformat,  a...)
				}
			}
		}
		if level.Value == 0 {
			os.Exit(-1)
		}
	}
}

func DebugLevelFromString(s string) (DebugLevels, error) {
	if s == "" {
		return levelPanic, fmt.Errorf("empty loglevel value")

	}

	key := strings.ToLower(strings.TrimSpace(s))

	for _, lvl := range debugLevels {
		if key == strings.ToLower(lvl.Label) {
			return lvl, nil
		}
	}

	return levelPanic, fmt.Errorf("Invalid loglevel value")
}
