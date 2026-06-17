// Command lk is the Linkana CLI.
package main

import "github.com/linkanalabs/cli/internal/commands"

// version is overridden at build time via -ldflags.
var version = "dev"

func main() {
	commands.SetVersion(version)
	commands.Execute()
}
