package examples

import (
	"fmt"
	"os"

	cml "github.com/debevv/camellia"
)

func main() {
	_, err := cml.Init("/home/debevv/camellia.db")
	if err != nil {
		fmt.Printf("Error initializing camellia - %v", err)
		os.Exit(1)
	}

	// Set a string value
	cml.Set("status/userIdentifier", "ABCDEF123456")

	// Set a boolean value
	cml.Set("status/system/areWeOk", true)

	// Set a float value
	cml.Set("sensors/temperature/latestValue", -48.0)

	// Set an integer value
	cml.Set("sensors/saturation/latestValue", 99)

	// Read a single float64 value
	temp, err := cml.Get[float64]("sensors/temperature/latestValue")
	fmt.Printf("Last temperature is: %f", temp)

	// Read a single bool value
	ok, err := cml.Get[bool]("sensors/temperature/latestValue")
	fmt.Printf("Are we ok? %t", ok)

	// Delete an entry and its children
	err = cml.Delete("sensors")

	// Read a tree of entries
	sensors, err := cml.GetEntry("sensors")
	fmt.Printf("Last update date of saturation value: %v", sensors.Children["saturation"].LastUpdate)

	// Export whole DB as JSON
	j, err := cml.ValuesToJSON("")
	fmt.Printf("All DB values:\n%s", j)

	// Import DB from JSON file
	file, err := os.Open("db.json")
	cml.SetValuesFromJSON(file, false)

	// Register a callback called after a value is set
	cml.SetPostSetHook("status/system/areWeOk", func(path, value string) error {
		if value == "true" {
			fmt.Printf("System went back to normal")
		} else {
			fmt.Printf("Something bad happened")
		}

		return nil
	}, true)

	// Close the DB
	cml.Close()
}
