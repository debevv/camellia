# camellia 💮 A lightweight hierarchical key-value store

`camellia` is a Go library that implements a simple, hierarchical, persistent key-value store, backed by a SQLite database.  
It is paired with the `cml` command line utility, useful to read, write and import/export a `camellia` DB.  
The project was born to be the system-wide settings registry of a Linux embedded system, similar to the one found in Windows.

- Library

  - [API at a glance](#api-at-a-glance)
  - [API reference](#api-reference)
  - [Installation and prerequisites](#installation-and-prerequisites)
  - [Overview](#overview)
  - [Types](#types)
  - [JSON import/export](#json-importexport)
  - [Hooks](#hooks)

- `cml` utility
  - [Command at a glance](#command-at-a-glance)
  - [Installation](#installation)
  - [Output of cml help](#output-of-cml-help)
  - [Database path](#database-path)

---

## Library

## API at a glance

```go
package examples

import (
	"fmt"
	"os"

	cml "github.com/debevv/camellia"
)

func main() {
	_, err := cml.Init("/home/debevv/cml.db")
	if err != nil {
		fmt.Printf("Error initializing camellia - %v", err)
		os.Exit(1)
	}

	// Set a string value
	cml.SetValue("/status/userIdentifier", "ABCDEF123456")

	// Set a boolean value
	cml.SetValue("/status/system/areWeOk", true)

	// Set a float value
	cml.SetValue("/sensors/temperature/latestValue", -48.0)

	// Set an integer value
	cml.SetValue("/sensors/saturation/latestValue", 99)

	// TODO: Set a custom struct. See issues, may be supported in future

	// Read a single value
	temp, err := cml.GetValue[float64]("/sensors/temperature/latestValue")
	fmt.Printf("Last temperature is: %f", temp)

	// Read a tree of entries
	entry, err := cml.GetEntries("/sensors")
	fmt.Printf("Last update date of saturation value: %v", entry.Children["saturation"].LastUpdate)

	// Export whole DB as JSON
	j, err := cml.ValuesToJSON("/")
	fmt.Printf("All DB values:\n%s", j)

	// Import DB from JSON file
	file, err := os.Open("db.json")
	cml.SetValuesFromJSON(file, false)
}

```

## API reference

TBD

## Installation and prerequisites

### Prerequisites

- Go `1.18` or greater, since this module makes use of generics
- A C compiler and `libsqlite3`, given the dependency to [go-sqlite3](https://github.com/mattn/go-sqlite3)

### Installation

Inside a module, run:

```
go get github.com/debevv/camellia
```

## Overview

### Entries

The data model is extremely simple.  
Every entity in the DB is ab `Entry`. An `Entry` has the following properties:

```go
LastUpdate time.Time
IsValue    bool
```

When `IsValue == true`, the `Entry` carries a value, and it's a leaf node in the hierarchy. Values are always represented as `string`s:

```go
Value string
```

When `IsValue == false`, the `Entry` does not carry a value, but it can have `Children`. It is the equivalent of a directory in a file system:

```go
Children map[string]*Entry
```

This leads to the complete definition an `Entry`, excluding the DB-specific properties:

```go
type Entry struct {
	DBEntry
	LastUpdate time.Time
	IsValue    bool
	Value      string
	Children   map[string]*Entry
}
```

### Paths

Paths are defined as strings separated by slashes (`/`). At the moment of writing this document, no limits are imposed to the length of a segment or to the length of the full path.  
The root Entry is identified as a single slash `/`.  
When specifying a path, the initial slash can be omitted, so, for example, `my/path` is equivalent to `/my/path`, and and an empty string is equivalent to `/`.

### Database versioning and migration

The schema of the DB is versioned, so after updating the library, `Init()` may return `ErrDBVersionMismatch`. In this case, you should perform the migration of the DB by calling `Migrate()`.

### Setting and forcing

When setting a value, if a an Entry at that path already exists, but it's a non-value Entry, the operation fails.  
Forcing a value instead will first delete the existing Entry (and all its children), and then replace it with the new value.

### Concurrency

The library API should be safe to be called by different goroutines.  
Regarding the usage of the same DB from different processes, it should be safe too, but more details will be added in the future (TBD).

## Types

The internal data format for `Entries`' values is `string`. For this reason, the library API offers a set of methods that accept a type parameter and automatically serializes/deserializes values to/from `string`. Example:

```go
// Gets the value at `path` and converts it to T
func GetValue[T Stringable](path string) (T, error)

// Converts `value` from T to `string` and sets it at `path`
func SetValue[T Stringable](path string, value T) error
```

The constraint of the type parameter is the `Stringable` `interface`:

```go
type Stringable interface {
	BaseType
}
```

that in turn is composed by the `BaseType` `interface`, the collection of almost all Go supported base types.  
Data satisfying the `BaseType` interface is serialized using `fmt.Sprint()` and deserialized using `fmt.Scan`.

### Note on custom types

The library defines an additional `interface` for serialization:

```go
type CustomStringable interface {
	String() string
	FromString(s string) error
}
```

intended to be used as a base for user-defined serializable types.  
Unfortunately, support to custom types is not implemented at the moment, since go 1.18 does not allow to define `Stringable` in this way:

```go
type Stringable interface {
  BaseType | CustomStringable
}
```

since unions of interfaces defining methods are not supported for now.

Please refer to this [comment](https://github.com/golang/go/issues/45346#issuecomment-862505803) for more details.

## JSON import/export

### Formats

Entries can be imported/exported from/to JSON.  
Two different formats are supported:

- **Default**: meant to represent only the hierarchical relationship of Entries and their values. This will be the format used in most cases:

```json
{
  "status": {
    "userIdentifier": "ABCDEF123456",
    "system": {
      "areWeOk": "true"
    }
  },
  "sensors": {
    "temperature": {
      "lastValue": "-48.0"
    },
    "saturation": {
      "lastValue": "99"
    }
  }
}
```

This format is used by the following methods:

```go
func SetValuesFromJSON(reader io.Reader, onlyMerge bool) error
func ValuesToJSON(path string) (string, error)
```

- **Extended**: carrying the all the properties of each Entry. The format was created to accommodate any future addition of useful metadata:

```json
{
  "status": {
    "last_update_ms": "1641488635512",
    "children": {
      "userIdentifier": {
        "last_update_ms": "1641488675539",
        "value": "ABCDEF123456"
      },
      "system": {
        "last_update_ms": "1641453675583",
        "children": {
          "areWeOk": {
            "last_update_ms": "1641488659275",
            "value": "true"
          }
        }
      }
    }
  },
  "sensors": {
    "last_update_ms": "1641453582957",
    "children": {
      "temperature": {
        "last_update_ms": "1641453582957",
        "children": {
          "lastValue": {
            "last_update_ms": "1641453582957",
            "value": "-48.0"
          }
        }
      },
      "saturation": {
        "last_update_ms": "1641453582957",
        "children": {
          "lastValue": {
            "last_update_ms": "1641453582957",
            "value": "99"
          }
        }
      }
    }
  }
}
```

This format is used by the following methods:

```go
func SetEntriesFromJSON(reader io.Reader, onlyMerge bool) error
func EntriesToJSON(path string) (string, error)
```

A note on `last_update_ms`: this property will be put in the JSON when exporting, but ignored when importing. The value of this property will be set to the timestamp of the actual moment of setting the Entry.

### Import and merge

When importing from JSON, two distinct modes of operation are supported:

- **Import**: the default operation. Overwrites any existing value with the one found in the input JSON. When overwriting, it forces values instead of just attempting to set them.
- **Merge**: like import, but does not overwrite existing values with the ones found in the input JSON

## Hooks

Hooks are callback methods that can be registered to run before (pre) and after (post) the setting of a certain value:

```go
// Register a pre set hook to check the value before it is set
cml.SetPreSetHook("/sensors/temperature/saturation", func(path, value string) error {
    saturation, err := strconv.Atoi(value)
    if err != nil {
        return fmt.Errorf("invalid saturation value")
    }

    // Block the setting of the value if it's out of range
    if saturation < 0 || saturation > 100 {
        return fmt.Errorf("invalid saturation value. Must be a percentage value")
    }

    return nil
})

// Register an async post set hook and react to changes
cml.SetPostSetHook("/status/system/areWeOk", func(path, value string) error {
    if value == "true" {
        fmt.Printf("System went back to normal")
    } else {
        fmt.Printf("Something bad happened")
    }

    return nil
}, true)
```

Hooks can be synchronous or asynchronous:

- Synchronous hooks are run on the same thread calling the `Set()` method. They can block the setting of a value by returning a non-`nil` error.
- Asynchronous hooks are run on a new goroutine, and their return value is ignored (so the can't block the setting). Only post set hooks can be asynchronous.

---

## `cml` utility

## Command at a glance

```sh
# Set some values
cml set status/userIdentifier "ABCDEF123456"
cml set /status/system/areWeOk "true"
cml set "sensors/saturation/latestValue" 99
cml set /sensors/temperature/latestValue "-48.0"

# Get a value
cml get /sensors/temperature/latestValue
# -48.0

# Get some values
cml get /sensors
# {
#   "saturation": {
#       "latestValue": "99"
#   },
#   "temperature": {
#       "latestValue": "-48.0"
#   }
# }

# Get Entries in the extended format
cml get -e sensors/temperature
# {
#    "last_update_ms": "1641453582957",
#    "children": {
#      "lastValue": {
#        "last_update_ms": "1641453582957",
#        "value": "-48.0"
#      }
#    }
# }

# Try to get a value, fail if it's a non-value
cml get -v sensors
# Error getting value - path is not a value

# Merge values from JSON file
cml merge path/to/file.json
```

## Installation

Install `cml` globally with:

```
go install github.com/debevv/camellia/cml@latest
```

## Output of `cml help`

```
cml - The camellia hierarchical key-value store utility
Usage:
cfg get [-e] [-v] <path>        Displays the configuration entry (and its children) at <path> in JSON format
                                -e        Displays entries in the extended JSON format
                                -v        Fails (returns nonzero) if the entry is not a value
cfg set [-f] <path> <value>     Sets the configuration entry at <path> to <value>
                                -f        Forces overwrite of non-value entries
cfg delete <path>               Deletes a configuration entry (and its children)
cfg import [-e] <file>          Imports config entries from JSON <file>
                                -e        Use the extended JSON format
cfg merge [-e] <file>           Imports only non-existing config entries from JSON <file>
                                -e        Use the extended JSON format
cfg migrate                     Migrates the DB to the current supported version
cfg wipe [-y]                   Wipes the DB
                                -y        Does not ask for confirmation
cfg help                        Displays this help message
```

## Database path

`cml` attempts to automatically determine the path of the SQLite database by reading it from different sources, in the following order:

- From the `CAMELLIA_DB_PATH` environment variable, then
- From the file `/tmp/camellia.db.path`, then
- If the steps above fail, the path used is `./camellia.db`
