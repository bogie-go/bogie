// Command bogie scaffolds the Go service next to your Rails app.
//
// The tool renders templates and shells out. It is never imported by the app
// it generates: after `bogie new`, the code is the user's.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/bogie-go/bogie/internal/commands"
)

const usage = `Usage: bogie <command> [arguments]

Generate:
  new NAME        generate a new service in ./NAME

Inside an app (found by its bogie.toml, from any subdirectory):
  server          run it            (go run . serve)
  test [PKGS]     run the tests     (go test -race -cover ./...)
  lint            gofmt, vet, golangci-lint
  ci              every check, locally, then sign off (bin/ci)
  db CMD          create | drop --yes | prepare | seed (go run . db CMD)
  migrate CMD     up | down --yes | status | version   (go run . migrate CMD)
  credentials CMD edit | show | get [-e ENV]           (go tool credentials CMD)

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
	case "server", "test", "lint", "ci", "db", "migrate", "credentials":
		err = commands.Proxy(os.Args[1], os.Args[2:], os.Stdout)
	case "version":
		fmt.Println(commands.Version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "bogie: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	var exit *commands.ExitError
	if errors.As(err, &exit) {
		// The app already said what went wrong; repeat only its status.
		os.Exit(exit.Code)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "bogie:", err)
		os.Exit(1)
	}
}
