// Command bogie scaffolds the Go service next to your Rails app.
//
// The tool renders templates and shells out. It is never imported by the app
// it generates: after `bogie new`, the code is the user's.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/bogie-go/bogie/internal/commands"
)

const usage = `Usage: bogie <command> [arguments]

Every command is spelled the way Rails spells it.

  new NAME                 generate a new service in ./NAME

Inside an app (found by its bogie.toml, from any subdirectory):
  generate, g GENERATOR    migration | model | controller | service   (bogie g -h)
  destroy, d GENERATOR     remove what generate made, registration included
  doctor                   markers, pinned tools, layout: all intact?
  server, s                run it; migrates at boot
  test, t [PKGS]           go test -race -cover ./...
  lint                     gofmt, vet, golangci-lint
  ci                       every check, locally, then sign off (bin/ci)

  db:create  db:drop  db:prepare  db:reset  db:seed  db:seed:rollback
  db:migrate  db:migrate:status  db:migrate:redo  db:rollback  db:version
  credentials:edit  credentials:show  credentials:get   [-e ENVIRONMENT]

  version                  print the version
  help                     this text

The app's own binary spells these with spaces (` + "`blog db prepare`" + `), and
that form works here too: bogie db prepare, bogie migrate status.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	command := commands.Expand(os.Args[1])

	var err error
	switch command {
	case "new":
		err = commands.New(os.Args[2:], os.Stdout)
	case "generate":
		err = commands.Generate(os.Args[2:], os.Stdout)
	case "destroy":
		err = commands.Destroy(os.Args[2:], os.Stdout)
	case "doctor":
		err = commands.Doctor(os.Stdout)
	case "server", "test", "lint", "ci", "db", "migrate", "credentials":
		err = commands.Proxy(command, os.Args[2:], os.Stdout)
	case "db:create", "db:drop", "db:prepare", "db:setup", "db:reset", "db:seed", "db:seed:rollback",
		"db:migrate", "db:migrate:status", "db:migrate:redo", "db:rollback", "db:version",
		"credentials:edit", "credentials:show", "credentials:get":
		err = commands.Proxy(command, os.Args[2:], os.Stdout)
	case "version":
		fmt.Println(commands.Version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		// A colon means a Rails-style task; let the proxy name the nearest
		// one rather than printing the whole usage.
		if strings.Contains(command, ":") {
			err = commands.Proxy(command, os.Args[2:], os.Stdout)
			break
		}
		fmt.Fprintf(os.Stderr, "bogie: unknown command %q\n\n%s", command, usage)
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
