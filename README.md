# camellia 💮 - A lightweight hierarchical key-value store, written in Go

`camellia` is a Go library that implements a simple, hierarchical, persistent key-value store, backed by SQLite.  
Its simple API is paired to the `cml` command line utility, useful to read, write and import/export a `camellia` DB.  
The project was born to be used in an Linux embedded system, as a system-wide settings registry, similar to the one found in Windows.

- Library

  - [At a glance](#at-a-glance)
  - [Installation and prerequisites](#installation-and-prerequisites)
  - [Data model](#data-model)
  - [JSON import/export](#json-import-/-export)
  - [Hooks](#hooks)

- `cml` utility
  - [At a glance](#at-a-glance)

## Library

### At a glance

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

### Installation and prerequisites

**Prerequisites:**

- Go `1.18` or greater, since this module makes use of generics
- A C compiler and `libsqlite3`, given the dependency to [go-sqlite3](https://github.com/mattn/go-sqlite3)

**Installation:**

```
go get github.com/debevv/camellia
```

### Data model

`camellia` data model is extremely simple. Every entity in the DB is described as an `Entry`.  
An `Entry` has the following properties:

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

### JSON import/export

**Formats**  
Entries can be imported/exported from/to JSON.  
Two different formats are supported:

- The default one, meant to represent only the hierarchical relationship of Entries and their values. This will be the format used in most cases:

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

- The extended one, carrying the all the properties of each Entry. The format was created to accommodate any future addition of useful metadata:

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

**Import and merge**  
When importing from JSON, two distinct modes of operation are supported:

- **Import**: the default operation. Overwrites any existing value with the one found in the JSON input
- **Merge**: like import, but does not overwrite existing values with the ones found in the JSON input.

### Hooks

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

    // Register a sync post set hook to react to changes
	cml.SetPostSetHook("/status/system/areWeOk", func(path, value string) error {
		if value == "true" {
			fmt.Printf("System went back to normal")
		} else {
			fmt.Printf("Something bad happened")
		}

		return nil
	}, false)
```

Hooks can be synchronous or asynchronous:

- Synchronous hooks are run on the same thread calling the `Set()` method. They can block the setting of a value by returning a non-`nil` error.
- Asynchronous hooks are run on a new goroutine, and their return value is ignored (so the can't block the setting). Only post set hooks can be asynchronous.
