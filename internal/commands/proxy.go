package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Marker is the file that says "this directory is a generated app". Written
// by `bogie new`, read by every command that runs inside an app.
const Marker = "bogie.toml"

// ExitError carries a child's exit status up to main, which exits with it.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// Proxy runs one of the generated app's own commands. The runtime commands
// belong to the app's binary, which also runs in the deployed container; the
// tool adds the Rails spelling and finds the app from any subdirectory.
func Proxy(command string, args []string, out io.Writer) error {
	root, err := appRoot(".")
	if err != nil {
		return err
	}
	steps, err := plan(command, args)
	if err != nil {
		return err
	}

	for _, argv := range steps {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = root
		cmd.Stdin = os.Stdin
		cmd.Stdout = out
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return &ExitError{Code: exit.ExitCode()}
		}
		if err != nil {
			return fmt.Errorf("%s: %w", command, err)
		}
	}
	return nil
}

// Rails spells its Rake-derived tasks with colons (db:migrate,
// credentials:edit) and its commands with spaces (server, generate, new).
// Bogie spells each one exactly as Rails does, so nothing has to be
// translated. The app's own binary stays a plain Go CLI with spaces
// (`blog db prepare`), and that form is accepted here too.
//
// Tasks that destroy pass --yes on the developer's behalf: rails db:rollback
// does not ask either, and this tool runs on a laptop. The binary that runs in
// a container keeps asking, because there a mistake is not undoable.
var tasks = map[string][][]string{
	"db:create":         {{"go", "run", ".", "db", "create"}},
	"db:drop":           {{"go", "run", ".", "db", "drop", "--yes"}},
	"db:prepare":        {{"go", "run", ".", "db", "prepare"}},
	"db:setup":          {{"go", "run", ".", "db", "prepare"}},
	"db:reset":          {{"go", "run", ".", "db", "drop", "--yes"}, {"go", "run", ".", "db", "prepare"}},
	"db:migrate":        {{"go", "run", ".", "migrate", "up"}},
	"db:migrate:status": {{"go", "run", ".", "migrate", "status"}},
	"db:migrate:redo":   {{"go", "run", ".", "migrate", "down", "--yes"}, {"go", "run", ".", "migrate", "up"}},
	"db:rollback":       {{"go", "run", ".", "migrate", "down", "--yes"}},
	"db:version":        {{"go", "run", ".", "migrate", "version"}},
	"db:seed":           {{"go", "run", ".", "db", "seed"}},
	"db:seed:rollback":  {{"go", "run", ".", "db", "seed-down", "--yes"}},
	"credentials:edit":  {{"go", "tool", "credentials", "edit"}},
	"credentials:show":  {{"go", "tool", "credentials", "show"}},
	"credentials:get":   {{"go", "tool", "credentials", "get"}},
}

// plan maps a bogie command to what runs in the app, one or more steps that
// run in order and stop at the first failure. Extra args pass through to the
// last step, so `bogie test ./config/...`, `bogie server --help` and
// `bogie credentials:edit -e production` do what they say.
func plan(command string, args []string) ([][]string, error) {
	if steps, ok := tasks[command]; ok {
		return withArgs(steps, args), nil
	}

	var argv []string
	switch command {
	case "server":
		argv = []string{"go", "run", ".", "serve"}
	case "worker":
		argv = []string{"go", "run", ".", "worker"}
	case "test":
		argv = []string{"go", "test", "-race", "-cover"}
		if len(args) == 0 {
			args = []string{"./..."}
		}
	case "lint":
		argv = []string{"make", "lint"}
	case "ci":
		argv = []string{"bin/ci"}
	case "db", "migrate":
		// The app's own subcommands, exactly as the Makefile calls them.
		argv = []string{"go", "run", ".", command}
	case "credentials":
		// The pinned credentials tool, from the app's own go.mod.
		argv = []string{"go", "tool", "credentials"}
	default:
		// A colon task in a family Bogie spells itself (db:, credentials:)
		// that is not one of them is a typo, and the nearest is named. Any
		// other colon task is one of the app binary's own subcommands spelled
		// the Rails way: api_keys:create is `blog api_keys create`, and the
		// binary says so if it has no such command.
		if strings.Contains(command, ":") && !knownFamily(command) {
			argv = append([]string{"go", "run", "."}, strings.Split(command, ":")...)
			break
		}
		return nil, unknown(command)
	}
	return [][]string{append(argv, args...)}, nil
}

// withArgs copies steps and appends args to the last one.
func withArgs(steps [][]string, args []string) [][]string {
	out := make([][]string, len(steps))
	for i, s := range steps {
		out[i] = append([]string(nil), s...)
	}
	last := len(out) - 1
	out[last] = append(out[last], args...)
	return out
}

// unknown names the nearest Rails task when someone types one Bogie does not
// have, so `bogie db:migrate:up` points at db:migrate rather than at nothing.
// knownFamily reports whether a colon task starts like one Bogie spells
// itself, db: or credentials:.
func knownFamily(command string) bool {
	prefix := command[:strings.Index(command, ":")+1]
	for name := range tasks {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func unknown(command string) error {
	prefix := command
	if i := strings.Index(command, ":"); i > 0 {
		prefix = command[:i+1]
	}
	var near []string
	for name := range tasks {
		if strings.HasPrefix(name, prefix) {
			near = append(near, name)
		}
	}
	if len(near) == 0 {
		return fmt.Errorf("%s: not a command the app provides", command)
	}
	return fmt.Errorf("%s: not a command; Bogie has %s", command, strings.Join(sorted(near), ", "))
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// appRoot walks up from dir to the directory holding bogie.toml, the way
// Rails finds config/application.rb, so a command works from any
// subdirectory of the app.
func appRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, Marker)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not inside a bogie app: no %s in this directory or any parent (run `bogie new NAME` first)", Marker)
		}
		dir = parent
	}
}
