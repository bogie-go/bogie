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
// written, so a missing marker means nothing changed anywhere. Several lines
// may go into one file (a namespace's construction, then its controller's),
// so each file is read once, every line inserted, and the file written once.
// pending holds the files this run would have written under --pretend, so a
// line can still be planned into a file that is new in the same run.
func wire(root string, wires []generate.Wire, pending map[string][]byte, pretend bool, report func(scaffold.Action)) error {
	type edit struct {
		content []byte
		changed bool
	}
	files := map[string]*edit{}
	var order []string
	ops := make([]scaffold.Op, len(wires))
	for i, w := range wires {
		e, ok := files[w.File]
		if !ok {
			src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(w.File)))
			if err != nil {
				if src, ok = pending[w.File]; !ok || !pretend {
					return err
				}
			}
			e = &edit{content: src}
			files[w.File] = e
			order = append(order, w.File)
		}
		out, changed, err := markers.Insert(e.content, w.File, w.Marker, w.Line)
		if err != nil {
			return err
		}
		if changed && w.Import != "" {
			if out, _, err = markers.AddImport(out, w.Import); err != nil {
				return fmt.Errorf("%s: %w", w.File, err)
			}
		}
		e.content, e.changed = out, e.changed || changed
		ops[i] = scaffold.OpIdentical
		if changed {
			ops[i] = "insert"
		}
	}
	for i, w := range wires {
		report(scaffold.Action{Op: ops[i], Path: w.File + ": " + w.Line})
	}
	if pretend {
		return nil
	}
	for _, file := range order {
		e := files[file]
		if !e.changed {
			continue
		}
		out, err := markers.Format(e.content)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(file)), out, 0o644); err != nil {
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
