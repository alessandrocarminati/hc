package main

import (
	"flag"
)

type CommandLine struct {
	ConfigPath   string
	LogLevel     DebugLevels
	PrintVersion bool
}

func ParseCommandLine(args []string) (CommandLine, error) {
	var cl CommandLine
	var lL string

	fs := flag.NewFlagSet("hc", flag.ContinueOnError)

	fs.StringVar(&cl.ConfigPath, "config", "", "Path to JSON config file (optional). If empty, built-in defaults apply.")

	fs.StringVar(&lL, "loglevel", "info", "Log level (e.g. debug, info, warn, error).")
	fs.BoolVar(&cl.PrintVersion, "version", false, "Print version and exit.")

	if err := fs.Parse(args); err != nil {
		return CommandLine{}, err
	}
	l, err := DebugLevelFromString(lL)
	if err != nil {
		 return CommandLine{}, err
	}

	cl.LogLevel = l

	return cl, nil
}

