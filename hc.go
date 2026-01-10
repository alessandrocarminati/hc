package main

import (
	"fmt"
	"os"
)

var (
	Build string
	Version string
	Hash string
	Dirty string
)

type Command struct {
	Name            string
	Handler         func(string, []string)
	Description     string
}


var commands = []Command{
	{
		Name:        "help",
		Handler:     doHelp,
		Description: "prints information on the tools.",
	},
	{
		Name:        "serve",
		Handler:     doRunServe,
		Description: "Runs hc as server. Collects history and store in a database.",
	},
	{
		Name:        "import",
		Handler:     doImport,
		Description: "Imports legacy history into the database.",
	},
	{
		Name:        "export",
		Handler:     doExport,
		Description: "Exports a grep friendly history.",
	},
	{
		Name:        "apy_key",
		Handler:     doRunAPIKeyCreate,
		Description: "generates an apikey for you",
	},
}

func doHelp(version string, args []string) {
}

func doExport(version string, args []string) {
}

func main() {
	cmdIndx := -1
	verstr := fmt.Sprintf("hc Ver. %s.%s (%s) %s\n", Version, Build, Hash, Dirty)

	if len(os.Args) > 1 {
		for i, cmd := range commands {
			if os.Args[1] == cmd.Name {
				cmdIndx = i
				break
			}
		}
	} else {
		fmt.Printf("hc needs a command. Use:\n")
		for _, cmd := range commands {
			fmt.Printf("%s -> %s\n", cmd.Name, cmd.Description)
		}
		os.Exit(2)
	}

	if cmdIndx != -1 {
		commands[cmdIndx].Handler(verstr, os.Args[2:])
	} else {
		fmt.Println("unknown command")
	}
}

