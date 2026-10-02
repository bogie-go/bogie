// Command bogie scaffolds the Go service next to your Rails app.
//
// The tool renders templates and shells out. It is never imported by the app
// it generates: after `bogie new`, the code is the user's.
package main

import (
	"fmt"
	"os"

	"github.com/bogie-go/bogie/internal/commands"
)

const usage = `Usage: bogie <command> [arguments]

Commands:
  new NAME        generate a new service in ./NAME
  version         print the version
  help            this text

Run "bogie <command> -h" for a command's flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "new":
		err = commands.New(os.Args[2:], os.Stdout)
	case "version":
		fmt.Println(commands.Version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "bogie: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "bogie:", err)
		os.Exit(1)
	}
}
