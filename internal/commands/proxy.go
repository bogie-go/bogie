package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Marker is the file that says "this directory is a generated app". Written
// by `bogie new`, read by every command that runs inside an app.
const Marker = "bogie.toml"

// ExitError carries a child's exit status up to main, which exits with it.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// Proxy runs one of the generated app's own commands. The runtime commands
// belong to the app's binary, which also runs in the deployed container; the
// tool only adds the Rails spelling and finds the app from any subdirectory.
func Proxy(command string, args []string, out io.Writer) error {
	root, err := appRoot(".")
	if err != nil {
		return err
	}
	argv, err := plan(command, args)
	if err != nil {
		return err
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = root
	cmd.Stdin = os.Stdin
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return &ExitError{Code: exit.ExitCode()}
	}
	if err != nil {
		return fmt.Errorf("%s: %w", command, err)
	}
	return nil
}

// plan maps a bogie command to what runs in the app. Extra args pass through,
// so `bogie test ./config/...` and `bogie server --help` do what they say.
func plan(command string, args []string) ([]string, error) {
	var argv []string
	switch command {
	case "server":
		argv = []string{"go", "run", ".", "serve"}
	case "test":
		argv = []string{"go", "test", "-race", "-cover"}
		if len(args) == 0 {
			args = []string{"./..."}
		}
	case "lint":
		argv = []string{"make", "lint"}
	case "ci":
		argv = []string{"bin/ci"}
	default:
		return nil, fmt.Errorf("%s: not a command the app provides", command)
	}
	return append(argv, args...), nil
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
