package main

import (
	"flag"
	"fmt"

        "github.com/google/uuid"
)

type CommandLine struct {
	ConfigPath   string
	HistoryFile  string
	AKTenantID   uuid.UUID
	AKUserID     uuid.UUID
	LogLevel     DebugLevels
	PrintVersion bool
}

func ParseCommandLine(args []string) (CommandLine, error) {
	var (
		cl		CommandLine
		lL		string
		tmpSTenantID	string
		tmpSUserID	string
		err		error
	)

	fs := flag.NewFlagSet("hc", flag.ContinueOnError)

	fs.StringVar(&cl.ConfigPath, "config", "", "Path to JSON config file (optional). If empty, built-in defaults apply.")
	fs.StringVar(&lL, "loglevel", "info", "Log level (e.g. debug, info, warn, error).")

	fs.StringVar(&cl.HistoryFile, "historyFile", "", "Specifis the file to import (import switch only, ignored elsewhere)")
	fs.StringVar(&tmpSTenantID, "api_tenantid", "", "Specifis the tenantid for the api key (api_key switch only, ignored elsewhere)")
	fs.StringVar(&tmpSUserID, "api_userid", "", "Specifis the file to import (api_key switch only, ignored elsewhere)")

	fs.BoolVar(&cl.PrintVersion, "version", false, "Print version and exit.")

	if err = fs.Parse(args); err != nil {
		return CommandLine{}, err
	}

	if tmpSTenantID!= "" {
		cl.AKTenantID, err = uuid.Parse(tmpSTenantID)
		if err != nil {
			return CommandLine{}, fmt.Errorf("apikey: invalid tenant uuid: %w", err)
		}
	}

	if tmpSTenantID!= "" {
		cl.AKUserID, err = uuid.Parse(tmpSUserID)
		if err != nil {
			return CommandLine{}, fmt.Errorf("apikey: invalid userid uuid: %w", err)
		}
	}

	l, err := DebugLevelFromString(lL)
	if err != nil {
		 return CommandLine{}, err
	}

	cl.LogLevel = l

	return cl, nil
}

