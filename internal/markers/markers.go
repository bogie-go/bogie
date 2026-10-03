// Package markers edits the three hand-written files a new controller must
// be registered in, by inserting one line above a `// bogie:<name>` comment.
//
// Markers rather than AST surgery, by design (docs/DESIGN.md §5): greppable,
// obvious, easy to test, and predictable for the person or agent reading the
// diff. A missing or duplicated marker is an error that names the file; the
// generator stops rather than guessing.
package markers

import (
	"bytes"
	"fmt"
	"go/format"
	"regexp"
	"strings"
)

// Insert puts line directly above the `// bogie:<marker>` comment in src,
// with the marker's indentation. It is idempotent: if an identical line is
// already in the file nothing changes and changed is false.
func Insert(src []byte, file, marker, line string) (out []byte, changed bool, err error) {
	lines := strings.Split(string(src), "\n")
	at, indent, err := find(lines, file, marker)
	if err != nil {
		return nil, false, err
	}
	want := strings.TrimSpace(line)
	for _, l := range lines {
		if strings.TrimSpace(l) == want {
			return src, false, nil
		}
	}
	lines = append(lines[:at], append([]string{indent + want}, lines[at:]...)...)
	return []byte(strings.Join(lines, "\n")), true, nil
}

// Remove deletes the first line whose text starts with prefix, so a wire
// line can be removed whatever arguments it was given. Runs of spaces and
// tabs count as one: gofmt aligns a struct's fields, so the line a generator
// inserted as "Posts *PostsController" may read "Posts    *PostsController"
// once a longer field joins it. changed is false when no such line exists.
func Remove(src []byte, prefix string) (out []byte, changed bool) {
	lines := strings.Split(string(src), "\n")
	want := strings.Join(strings.Fields(prefix), " ")
	for i, l := range lines {
		if strings.HasPrefix(strings.Join(strings.Fields(l), " "), want) {
			return []byte(strings.Join(append(lines[:i], lines[i+1:]...), "\n")), true
		}
	}
	return src, false
}

// Check reports whether the marker appears exactly once in src.
func Check(src []byte, file, marker string) error {
	_, _, err := find(strings.Split(string(src), "\n"), file, marker)
	return err
}

func find(lines []string, file, marker string) (at int, indent string, err error) {
	comment := "// bogie:" + marker
	at = -1
	for i, l := range lines {
		if strings.TrimSpace(l) != comment {
			continue
		}
		if at >= 0 {
			return 0, "", fmt.Errorf("%s: marker %s appears more than once; keep exactly one", file, comment)
		}
		at = i
		indent = l[:len(l)-len(strings.TrimLeft(l, " \t"))]
	}
	if at < 0 {
		return 0, "", fmt.Errorf("%s: marker %s not found; the generator needs it to register the code it writes (restore it, or wire by hand as AGENTS.md describes)", file, comment)
	}
	return at, indent, nil
}

// AddImport adds path to the file's import block, if not already there.
// Format afterwards sorts it into place.
func AddImport(src []byte, path string) (out []byte, changed bool, err error) {
	quoted := fmt.Sprintf("%q", path)
	if bytes.Contains(src, []byte(quoted)) {
		return src, false, nil
	}
	s := string(src)
	open := strings.Index(s, "import (")
	if open < 0 {
		return nil, false, fmt.Errorf("no import block to add %s to", path)
	}
	closeAt := strings.Index(s[open:], "\n)")
	if closeAt < 0 {
		return nil, false, fmt.Errorf("unterminated import block")
	}
	at := open + closeAt
	return []byte(s[:at] + "\n\t" + quoted + s[at:]), true, nil
}

var importLine = regexp.MustCompile(`^\s*(?:(\w+)\s+)?"([^"]+)"\s*$`)

// PruneImports drops imports whose package is no longer referenced, after a
// wire line has been removed. The package name is the import's alias or its
// last path segment; a reference is that name followed by a dot anywhere
// outside the import block.
func PruneImports(src []byte) []byte {
	s := string(src)
	open := strings.Index(s, "import (")
	if open < 0 {
		return src
	}
	closeAt := strings.Index(s[open:], "\n)")
	if closeAt < 0 {
		return src
	}
	block := s[open : open+closeAt]
	rest := s[:open] + s[open+closeAt:]

	var kept []string
	for _, l := range strings.Split(block, "\n") {
		m := importLine.FindStringSubmatch(l)
		if m == nil {
			kept = append(kept, l)
			continue
		}
		name := m[1]
		if name == "" {
			name = m[2][strings.LastIndex(m[2], "/")+1:]
		}
		if name == "_" || regexp.MustCompile(`\b`+regexp.QuoteMeta(name)+`\.`).MatchString(rest) {
			kept = append(kept, l)
		}
	}
	return []byte(s[:open] + strings.Join(kept, "\n") + s[open+closeAt:])
}

// Format is gofmt, in process: sorts imports and realigns what an insertion
// disturbed, so the file passes the gofmt check bin/ci runs.
func Format(src []byte) ([]byte, error) {
	out, err := format.Source(src)
	if err != nil {
		return nil, fmt.Errorf("the edited file does not parse: %w", err)
	}
	return out, nil
}
