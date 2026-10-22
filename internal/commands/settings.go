package commands

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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
	rel := "config/credentials.yml.enc: " + group + "."
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

// insertGroupKeys is the text edit: it returns the file with every key not
// already in group added at the end of that group's block, and the names it
// added. The block is the group's line and every indented or blank line after
// it, up to the next line at column 0.
func insertGroupKeys(plain, group string, keys []devKey) (string, []string) {
	lines := strings.Split(strings.TrimRight(plain, "\n"), "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimRight(l, " ") == group+":" {
			start = i
			break
		}
	}
	var added []string
	if start < 0 {
		lines = append(lines, group+":")
		start = len(lines) - 1
	}
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
	if len(fresh) == 0 {
		return plain, nil
	}
	out := append(append(append([]string{}, lines[:end]...), fresh...), lines[end:]...)
	return strings.Join(out, "\n") + "\n", added
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
