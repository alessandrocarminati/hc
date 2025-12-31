package main

import (
	"log"
)

type Options struct {
	Cfg       Config
	LogLevel  DebugLevels
	Verstr    string
}

func getRuntimeConf(version string, args []string) (*Options, error) {
	cl, err := ParseCommandLine(args)
	if err != nil {
		return nil, err
	}

	DebugLevel = cl.LogLevel.Value

	debugPrint(log.Printf, levelDebug, "reading config file %s\n", cl.ConfigPath)
	cfg, err := ReadConfig(cl)
	if err != nil {
		return nil, err
	}

	debugPrint(log.Printf, levelDebug, "Build Runtime conf\n")
	opts, err := ResolveOptions(cfg, cl, version)
	if err != nil {
		return nil, err
	}

	return opts, nil
}
