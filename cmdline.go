package main

import (
	"flag"
	"os"
)

type CommandLine struct {
	ConfigPath   string
	LogLevel     string
	PrintVersion bool
}

func ParseCommandLine(args []string) (CommandLine, error) {
	var cl CommandLine

	fs := flag.NewFlagSet("hc", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	fs.StringVar(&cl.ConfigPath, "config", "", "Path to JSON config file (optional). If empty, built-in defaults apply.")

	fs.StringVar(&cl.LogLevel, "loglevel", "info", "Log level (e.g. debug, info, warn, error).")
	fs.BoolVar(&cl.PrintVersion, "version", false, "Print version and exit.")

	if err := fs.Parse(args); err != nil {
		return CommandLine{}, err
	}

	return cl, nil
}
