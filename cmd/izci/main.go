// Command izci is the single binary of the izci error-tracking platform.
package main

import (
	"os"
	// Organization time zones must resolve on hosts and images without a
	// zoneinfo database.
	_ "time/tzdata"

	"github.com/mcihad/kentos-izci/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
