package main

import (
	"fmt"
	"runtime"
)

const (
	DebugNone uint32          = iota
	Debug1
	Debug2
	Debug3
	Debug4
	Debug5
	Debug6
	Debug7
)

//const DebugLevel uint32 = debugIO | (1<<debugAddFunctionName-1)
var DebugLevel uint32 = DebugNone

func DPrintf(level uint32, format string, a ...interface{}) {
	var s string

	if DebugLevel>=level {
		pc, _, _, ok := runtime.Caller(1)
		s = "?"
		if ok {
			fn := runtime.FuncForPC(pc)
			if fn != nil {
				s = fn.Name()
			}
		}
		newformat := "[" + s + "] " + format
		fmt.Printf(newformat, a...)
	}
}

