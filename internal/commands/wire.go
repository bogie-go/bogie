package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bogie-go/bogie/internal/generate"
	"github.com/bogie-go/bogie/internal/markers"
	"github.com/bogie-go/bogie/internal/scaffold"
)

// wire inserts each line above its marker, adds the import it needs, and
// formats the file. Every file is checked for its marker before any file is
// written, so a missing marker means nothing changed anywhere.
func wire(root string, wires []generate.Wire, pretend bool, report func(scaffold.Action)) error {
	type edit struct {
		path    string
		content []byte
		changed bool
	}
	var edits []edit
	for _, w := range wires {
		path := filepath.Join(root, filepath.FromSlash(w.File))
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, changed, err := markers.Insert(src, w.File, w.Marker, w.Line)
		if err != nil {
			return err
		}
		if w.Import != "" && changed {
			out, _, err = markers.AddImport(out, w.Import)
			if err != nil {
				return fmt.Errorf("%s: %w", w.File, err)
			}
		}
		if changed {
			if out, err = markers.Format(out); err != nil {
				return fmt.Errorf("%s: %w", w.File, err)
			}
		}
		edits = append(edits, edit{path, out, changed})
	}

	for i, e := range edits {
		op := scaffold.OpIdentical
		if e.changed {
			op = "insert"
		}
		report(scaffold.Action{Op: op, Path: wires[i].File + ": " + wires[i].Line})
		if pretend || !e.changed {
			continue
		}
		if err := os.WriteFile(e.path, e.content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// unwire removes each line by its prefix and prunes imports the removal left
// unused.
func unwire(root string, wires []generate.Wire, pretend bool, report func(scaffold.Action)) error {
	for _, w := range wires {
		path := filepath.Join(root, filepath.FromSlash(w.File))
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, changed := markers.Remove(src, w.Line)
		if !changed {
			report(scaffold.Action{Op: scaffold.OpIdentical, Path: w.File + ": " + w.Line + " (absent)"})
			continue
		}
		out = markers.PruneImports(out)
		if out, err = markers.Format(out); err != nil {
			return fmt.Errorf("%s: %w", w.File, err)
		}
		report(scaffold.Action{Op: "remove", Path: w.File + ": " + w.Line})
		if pretend {
			continue
		}
		if err := os.WriteFile(path, out, 0o644); err != nil {
			return err
		}
	}
	return nil
}
