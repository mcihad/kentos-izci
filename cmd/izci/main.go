// Command izci is the single binary of the izci error-tracking platform.
package main

import (
	"os"

	"github.com/mcihad/kentos-izci/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
