// Package scaffold renders an embedded template tree onto disk the way Rails
// generators do: one line per file saying what happened, nothing overwritten
// without --force, and --pretend to see the plan without writing anything.
package scaffold

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"
)

// Vars is what a template can refer to.
type Vars struct {
	// Name is the app name: a Go identifier, lowercase, e.g. "blog".
	Name string
	// Module is the Go module path of the generated app.
	Module string
	// EnvPrefix is Name upper-cased: BLOG_ENV, BLOG_ADDR.
	EnvPrefix string
	// LayoutVersion is recorded in bogie.toml for a later `bogie app:update`.
	LayoutVersion string
	// Jobs adds River: the worker role, app/jobs, the schema at boot.
	Jobs bool
	// Mascot is the welcome page's image as a data URI (templates.MascotDataURI).
	Mascot string
}

// Op is what happened to one file.
type Op string

const (
	OpCreate    Op = "create"    // did not exist; written
	OpIdentical Op = "identical" // existed with the same content; untouched
	OpConflict  Op = "conflict"  // existed with different content; untouched
	OpForce     Op = "force"     // existed with different content; overwritten
)

// Action is one file's outcome, with its path relative to the destination.
type Action struct {
	Op   Op
	Path string
}

// Options controls a render.
type Options struct {
	// Force overwrites files that differ instead of reporting a conflict.
	Force bool
	// Pretend reports every action and writes nothing.
	Pretend bool
	// Report, if set, is called with each action as it happens.
	Report func(Action)
}

// ErrConflict is returned, after every file has been considered, when at least
// one existing file differed and Force was not set. Nothing was overwritten.
var ErrConflict = errors.New("files differ; rerun with --force to overwrite them")

// Render walks src under root and writes each file under dest. Files ending in
// .tmpl go through text/template with vars; everything else is copied as is.
// Every file is considered even after a conflict, so the report is complete.
func Render(src fs.FS, root, dest string, vars Vars, opts Options) ([]Action, error) {
	var actions []Action
	conflicts := 0

	err := fs.WalkDir(src, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(p, root+"/")
		out := OutputPath(rel)

		content, err := render(src, p, vars)
		if err != nil {
			return err
		}
		// A template that renders to nothing but whitespace is a file the
		// options left out, such as app/jobs without --jobs.
		if len(bytes.TrimSpace(content)) == 0 {
			return nil
		}
		act, err := write(filepath.Join(dest, filepath.FromSlash(out)), out, content, opts)
		if err != nil {
			return err
		}
		if act.Op == OpConflict {
			conflicts++
		}
		actions = append(actions, act)
		if opts.Report != nil {
			opts.Report(act)
		}
		return nil
	})
	if err != nil {
		return actions, err
	}
	if conflicts > 0 {
		return actions, fmt.Errorf("%d %w", conflicts, ErrConflict)
	}
	return actions, nil
}

// File is one file to write, with its path relative to dest.
type File struct {
	Path    string
	Content []byte
}

// Write puts files under dest with the same per-file semantics as Render:
// create, identical, conflict or force, every file considered, ErrConflict
// at the end if any differed without Force.
func Write(dest string, files []File, opts Options) ([]Action, error) {
	var actions []Action
	conflicts := 0
	for _, f := range files {
		act, err := write(filepath.Join(dest, filepath.FromSlash(f.Path)), f.Path, f.Content, opts)
		if err != nil {
			return actions, err
		}
		if act.Op == OpConflict {
			conflicts++
		}
		actions = append(actions, act)
		if opts.Report != nil {
			opts.Report(act)
		}
	}
	if conflicts > 0 {
		return actions, fmt.Errorf("%d %w", conflicts, ErrConflict)
	}
	return actions, nil
}

// OutputPath maps a template path to the file it produces: a trailing .tmpl is
// dropped, and a path segment beginning with dot_ becomes a dotfile.
//
// The prefix exists because go:embed skips real dotfiles and git would read a
// template .gitignore as its own.
func OutputPath(rel string) string {
	rel = strings.TrimSuffix(rel, ".tmpl")
	segs := strings.Split(rel, "/")
	for i, s := range segs {
		if strings.HasPrefix(s, "dot_") {
			segs[i] = "." + strings.TrimPrefix(s, "dot_")
		}
	}
	return strings.Join(segs, "/")
}

func render(src fs.FS, p string, vars Vars) ([]byte, error) {
	b, err := fs.ReadFile(src, p)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(p, ".tmpl") {
		return b, nil
	}
	t, err := template.New(path.Base(p)).Parse(string(b))
	if err != nil {
		return nil, fmt.Errorf("scaffold: parse %s: %w", p, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return nil, fmt.Errorf("scaffold: render %s: %w", p, err)
	}
	return buf.Bytes(), nil
}

func write(abs, rel string, content []byte, opts Options) (Action, error) {
	var op Op
	existing, err := os.ReadFile(abs)
	switch {
	case err == nil && bytes.Equal(existing, content):
		return Action{OpIdentical, rel}, nil
	case err == nil && !opts.Force:
		return Action{OpConflict, rel}, nil
	case err == nil:
		op = OpForce
	case errors.Is(err, fs.ErrNotExist):
		op = OpCreate
	default:
		return Action{}, err
	}

	if opts.Pretend {
		return Action{op, rel}, nil
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return Action{}, err
	}
	if err := os.WriteFile(abs, content, mode(rel)); err != nil {
		return Action{}, err
	}
	return Action{op, rel}, nil
}

// mode gives scripts under bin/ the executable bit. go:embed keeps no file
// modes, so the convention carries it: bin/ holds things you run.
func mode(rel string) os.FileMode {
	if strings.HasPrefix(rel, "bin/") {
		return 0o755
	}
	return 0o644
}
