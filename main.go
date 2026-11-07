// Command mireille verifies the sensor temperature of dark frames.
package main

import (
	"os"

	"github.com/hoxca/mireille/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
