package main

import (
	"log"
)

type Options struct {
	Cfg               Config
	LogLevel          DebugLevels
	LegacyHistoryFile string
	Verstr            string
}

func getRuntimeConf(version string, args []string) (*Options, error) {
	debugPrint(log.Printf, levelCrazy, "Args=%s, %v\n", version, args)
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

func ResolveOptions(cfg Config, cl CommandLine, verstr string) (*Options, error) {
	o := Options{
		Cfg:	   cfg,
		LogLevel:  cl.LogLevel,
	}

	err := cfg.validate()
	if  err != nil {
		return  nil, err
	}

	o.LogLevel = cl.LogLevel
	o.Verstr = verstr
	o.LegacyHistoryFile = cl.HistoryFile
	return &o, nil
}

func ValidateServer(s ListenerConfig) bool {
	debugPrint(log.Printf, levelCrazy, "Args=%v\n", s)

	if s.Enabled && s.Addr == "" {
		return false
	}
	return true
}

