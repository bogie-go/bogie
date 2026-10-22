package commands

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bogie-go/credentials/pkg/credentials"

	"github.com/bogie-go/bogie/internal/scaffold"
)

// devKey is one line for a group of the development credentials file: the
// key's name, and the whole line as it should be written (a value, or a
// commented placeholder).
type devKey struct{ Name, Line string }

// addDevelopmentKeys writes keys into group of the development credentials
// file: the group is appended if it is not there, and a key already in it,
// commented or not, is left alone, so running a generator twice changes
// nothing. A missing master key means development reads the environment, and
// the keys are reported for the developer to set.
func addDevelopmentKeys(root, group string, keys []devKey, pretend bool, report func(scaffold.Action)) error {
	dir := filepath.Join(root, "config")
	keyPath := filepath.Join(dir, "master.key")
	rel := "config/credentials.yml.enc: "
	if group != "" {
		rel += group + "."
	}
	if _, err := os.Stat(keyPath); err != nil {
		for _, k := range keys {
			report(scaffold.Action{Op: "skip", Path: rel + k.Name + " (no config/master.key; set it in the environment)"})
		}
		return nil
	}
	editor := credentials.NewConfigEditor(dir, "credentials.yml.enc", "master.key", "")
	plain, err := editor.Show()
	if err != nil {
		return fmt.Errorf("credentials: %w", err)
	}
	text, added := insertGroupKeys(string(plain), group, keys)
	for _, k := range keys {
		op := scaffold.Op("insert")
		if !contains(added, k.Name) {
			op = scaffold.OpIdentical
		}
		report(scaffold.Action{Op: op, Path: rel + k.Name})
	}
	if len(added) == 0 || pretend {
		return nil
	}
	masterKey, err := credentials.ReadMasterKey(keyPath)
	if err != nil {
		return fmt.Errorf("credentials: %w", err)
	}
	if err := editor.EncryptAndSave([]byte(text), hex.EncodeToString(masterKey)); err != nil {
		return fmt.Errorf("credentials: %w", err)
	}
	return nil
}

// insertGroupKeys is the text edit. With a group it returns the file with
// every key not already in that group added at the end of its block, in the
// order given (the keys inside a group are never sorted), and the names added.
// A group that is not there yet is added as a whole at its alphabetical place
// among the top-level entries. With group "" the keys are flat: each is a
// top-level line, placed alphabetically the same way.
func insertGroupKeys(plain, group string, keys []devKey) (string, []string) {
	lines := strings.Split(strings.TrimRight(plain, "\n"), "\n")
	var added []string

	if group == "" {
		for _, k := range keys {
			if hasTopLevel(lines, k.Name) {
				continue
			}
			lines = insertTopLevel(lines, k.Name, []string{k.Line})
			added = append(added, k.Name)
		}
	} else if !hasGroup(lines, group) {
		block := []string{group + ":"}
		for _, k := range keys {
			block = append(block, "  "+k.Line)
			added = append(added, k.Name)
		}
		lines = insertTopLevel(lines, group, block)
	} else {
		start := groupStart(lines, group)
		end := start + 1 // one past the block's last non-blank line
		for i := start + 1; i < len(lines); i++ {
			l := lines[i]
			if l != "" && l[0] != ' ' && l[0] != '\t' {
				break
			}
			if strings.TrimSpace(l) != "" {
				end = i + 1
			}
		}
		var fresh []string
		for _, k := range keys {
			present := false
			for _, l := range lines[start+1 : end] {
				t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "#"))
				if strings.HasPrefix(t, k.Name+":") {
					present = true
					break
				}
			}
			if !present {
				fresh = append(fresh, "  "+k.Line)
				added = append(added, k.Name)
			}
		}
		lines = append(append(append([]string{}, lines[:end]...), fresh...), lines[end:]...)
	}
	if len(added) == 0 {
		return plain, nil
	}
	return strings.Join(lines, "\n") + "\n", added
}

var (
	topLevelLine    = regexp.MustCompile(`^([a-z][a-z0-9_]*):`)
	placeholderLine = regexp.MustCompile(`^# ([a-z][a-z0-9_]*): ("[^"]*"|0|false)$`)
)

// topName is the name a line gives a top-level entry: a group or a flat key,
// or a flat key written commented out as a placeholder. "" for anything else.
func topName(line string) string {
	if m := topLevelLine.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	if m := placeholderLine.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	return ""
}

func hasTopLevel(lines []string, name string) bool {
	for _, l := range lines {
		if topName(l) == name {
			return true
		}
	}
	return false
}

func groupStart(lines []string, group string) int {
	for i, l := range lines {
		if strings.TrimRight(l, " ") == group+":" {
			return i
		}
	}
	return -1
}

func hasGroup(lines []string, group string) bool { return groupStart(lines, group) >= 0 }

// insertTopLevel puts block where name sorts among the file's top-level
// entries: before the first entry whose name is greater, or at the end. A
// run of comment lines directly above an entry belongs to it and stays with
// it, except the file's opening comment, which belongs to the file.
func insertTopLevel(lines []string, name string, block []string) []string {
	pos := len(lines)
	for i, l := range lines {
		n := topName(l)
		if n == "" || n <= name {
			continue
		}
		at := i
		for at > 0 && strings.HasPrefix(lines[at-1], "#") && topName(lines[at-1]) == "" {
			at--
		}
		if at == 0 && i > 0 {
			at = i
		}
		pos = at
		break
	}
	return append(append(append([]string{}, lines[:pos]...), block...), lines[pos:]...)
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// randomSecretLine is `name: "<64 hex digits>"`, a development value for a
// secret a generator added, so the app boots as it did without anyone typing
// one.
func randomSecretLine(name string) (devKey, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return devKey{}, err
	}
	return devKey{Name: name, Line: fmt.Sprintf("%s: %q", name, hex.EncodeToString(secret))}, nil
}
