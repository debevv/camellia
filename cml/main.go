package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	cml "github.com/debevv/camellia"
)

const (
	defaultDBPath = "./camellia.db"
	dbPathFile    = "/tmp/camellia.db.path"
)

var initialized = false

func getDBPath() (string, error) {
	// Try to get it from an environment variable first
	path := os.Getenv("CAMELLIA_DB_PATH")
	if path != "" {
		return path, nil
	}

	// Try to get it from tmpfile
	f, err := os.OpenFile(dbPathFile, os.O_RDONLY, 0666)
	if err == nil {
		defer f.Close()
		path, err := io.ReadAll(io.LimitReader(f, 10000))
		if err != nil {
			return string(path), nil
		}
	}

	// We give up
	return defaultDBPath, nil
}

func getFlags(from uint) map[string]bool {
	params := make(map[string]bool)
	for i := int(from); i < len(os.Args); i++ {
		arg := os.Args[i]
		if !params[arg] {
			if strings.HasPrefix(arg, "-") {
				params[arg] = true
			}
		} else {
			return nil
		}
	}

	return params
}

func printStderrLn(format string, a ...any) {
	os.Stderr.WriteString(fmt.Sprintf(format+"\n", a...))
}

func errExit(format string, a ...any) int {
	printStderrLn(format, a...)
	return 1
}

func usageExit() int {
	printStderrLn(
		`cml - The camellia hierarchical key-value store utility
Usage:
cfg get [-e] [-v] <path>        Displays the configuration entry (and its children) at <path> in JSON format
                                -e        Displays entries in the extended JSON format
                                -v        Fails (returns nonzero) if the entry is not a value
cfg set [-f] <path> <value>     Sets the configuration entry at <path> to <value>
                                -f        Forces overwrite of non-value entries
cfg delete <path>               Deletes a configuration entry (and its children)
cfg import <file>               Imports config entries from JSON <file>
                                -e        Use the extended JSON format
cfg merge <file>                Imports only non-existing config entries from JSON <file>
                                -e        Use the extended JSON format
cfg migrate                     Migrates the DB to the current supported version
cfg wipe [-y]                   Wipes the DB
                                -y        Does not ask for confirmation
cfg help                        Displays this help message

DB path is selected in this order:
- Reading the CONFIG_DB_PATH env variable
- Reading %s
- cml.db in the working directory`,
		dbPathFile)

	return 1
}

func initialize() {
	dbPath, err := getDBPath()
	if err != nil {
		os.Exit(errExit("Error getting DB path from environment - %v", err))
	}

	created, err := cml.Init(dbPath)
	if err != nil {
		if errors.Is(err, cml.ErrDBVersionMismatch) {
			os.Exit(errExit("DB version mismatch, needs migration (cml migrate)"))
		} else {
			os.Exit(errExit("Error intializing camellia (DB path: %s) - %v", dbPath, err))
		}
	}

	if created {
		printStderrLn("Created new DB file at %s - version %d", dbPath, cml.GetSupportedDBVersion())
	}

	initialized = true
}

func run() int {
	if len(os.Args) < 2 {
		return usageExit()
	}
	var onlyMerge bool

	switch os.Args[1] {
	case "get":

		var path string
		if len(os.Args) > 2 {
			path = os.Args[len(os.Args)-1]
		}

		var flags map[string]bool
		if len(os.Args) > 3 {
			flags = getFlags(2)
			if flags == nil {
				return usageExit()
			}
		}

		initialize()

		var out string
		var err error

		if flags["-v"] {
			out, err = cml.GetValue[string](path)
			if err != nil {
				return errExit("Error getting value - %v", err)
			}
		}

		if flags["-e"] {
			out, err = cml.EntryToJSON(path)
			if err != nil {
				return errExit("Error getting value - %v", err)
			}
		} else {
			out, err = cml.ValuesToJSON(path)
			if err != nil {
				return errExit("Error getting value - %v", err)
			}
		}

		out = strings.Trim(out, "\n")
		out = strings.Trim(out, "\"")
		os.Stdout.WriteString(out)
		os.Stdout.WriteString("\n")

	case "set":
		if len(os.Args) < 4 {
			return usageExit()
		}

		path := os.Args[len(os.Args)-2]
		value := os.Args[len(os.Args)-1]

		var flags map[string]bool
		if len(os.Args) > 4 {
			flags = getFlags(2)
			if flags == nil {
				return usageExit()
			}
		}

		initialize()

		if flags["-f"] {
			err := cml.ForceValue(path, value)
			if err != nil {
				return errExit("Error forcing value - %v", err)
			}
		} else {
			err := cml.SetValue(path, value)
			if err != nil {
				return errExit("Error setting value - %v", err)
			}
		}

	case "delete":
		if len(os.Args) < 3 {
			return usageExit()
		}

		initialize()

		path := os.Args[2]

		err := cml.DeleteEntry(path)
		if err != nil {
			return errExit("Error deleting entry - %v", err)
		}

	case "merge":
		onlyMerge = true
		fallthrough

	case "import":
		if len(os.Args) < 3 {
			return usageExit()
		}

		filePath := os.Args[len(os.Args)-1]
		if filePath == "" {
			return errExit("Invalid file path specified")
		}

		var flags map[string]bool
		if len(os.Args) > 3 {
			flags := getFlags(2)
			if flags == nil {
				return usageExit()
			}
		}

		file, err := os.Open(filePath)
		if err != nil {
			return errExit("Error opening file %s - %v", filePath, err)
		}

		initialize()

		if flags["-e"] {
			err = cml.SetEntriesFromJSON(file, onlyMerge)
		} else {
			err = cml.SetValuesFromJSON(file, onlyMerge)
		}

		if err != nil {
			return errExit("Error merging file %s - %v", filePath, err)
		}

	case "migrate":
		initialize()

		migrated, err := cml.Migrate()
		if err != nil {
			return errExit("Error migrating DB - %v", err)
		}

		if migrated {
			printStderrLn("Migrated DB to version %d", cml.GetSupportedDBVersion())
		}

	case "wipe":
		flags := getFlags(2)
		if flags == nil {
			return usageExit()
		}

		c := 'n'

		if !flags["-y"] {
			printStderrLn("Do you really want to wipe the DB at %s ? [y/n] ", cml.GetDBPath())
			var c rune
			fmt.Scanf("%c\n", &c)
		} else {
			c = 'y'
		}

		if c == 'y' {
			initialize()

			err := cml.Wipe()
			if err != nil {
				return errExit("Error wiping the DB - %v", err)
			}
		}

	case "info":
		/* TODO
		type info struct {
			Path    string
			Version uint64
			Entries uint
			Size    uint
		}

		initialize()

		var i info
		i.Path = cml.GetDBPath()
		i.Version = cml.GetSupportedDBVersion()
		*/
		fallthrough

	case "help":
		return usageExit()

	default:
		return usageExit()
	}

	return 0
}

func main() {
	ret := run()

	if initialized {
		err := cml.Close()
		if err != nil {
			os.Exit(errExit("Error closing camellia - %v", err))
		}
	}

	os.Exit(ret)
}
