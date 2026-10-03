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

// appName is the name bogie.toml records, or the directory's as a fallback.
func appName(root string) string {
	if s, err := readSettings(root); err == nil {
		return s.Name
	}
	return filepath.Base(root)
}

// addDevelopmentSetting appends `key: <random>` to the app's section of the
// development credentials file, so a secret a generator added has a value
// in development without anyone typing one. The file is flat, one section,
// so appending at the end lands inside it. A key already there is left
// alone; a missing master key means development reads the environment, and
// the setting is reported for the developer to set.
func addDevelopmentSetting(root, key string, pretend bool, report func(scaffold.Action)) error {
	dir := filepath.Join(root, "config")
	keyPath := filepath.Join(dir, "master.key")
	rel := "config/credentials.yml.enc: " + key
	if _, err := os.Stat(keyPath); err != nil {
		report(scaffold.Action{Op: "skip", Path: rel + " (no config/master.key; set it in the environment)"})
		return nil
	}
	editor := credentials.NewConfigEditor(dir, "credentials.yml.enc", "master.key", "")
	plain, err := editor.Show()
	if err != nil {
		return fmt.Errorf("credentials: %w", err)
	}
	for _, line := range strings.Split(string(plain), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), key+":") {
			report(scaffold.Action{Op: scaffold.OpIdentical, Path: rel})
			return nil
		}
	}
	report(scaffold.Action{Op: "insert", Path: rel})
	if pretend {
		return nil
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	text := strings.TrimRight(string(plain), "\n") + fmt.Sprintf("\n  %s: %q\n", key, hex.EncodeToString(secret))
	masterKey, err := credentials.ReadMasterKey(keyPath)
	if err != nil {
		return fmt.Errorf("credentials: %w", err)
	}
	if err := editor.EncryptAndSave([]byte(text), hex.EncodeToString(masterKey)); err != nil {
		return fmt.Errorf("credentials: %w", err)
	}
	return nil
}
