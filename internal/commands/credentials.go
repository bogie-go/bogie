package commands

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bogie-go/credentials/pkg/credentials"

	"github.com/bogie-go/bogie/internal/scaffold"
)

// The development credentials file is the one thing `new` cannot render from
// a template: its key is random, as rails new's master.key is. So it is made
// here, after the templates, with the settings development starts from.
//
// Only development. Staging and production get their own files and keys when
// someone runs `make credentials ENV=production`, so a key that opens
// development can never also open production.
const developmentSettings = `# Development settings for %[1]s. Edit with: make credentials
#
# Groups at the top level, as Rails credentials are. A key's environment
# variable is its path, upper-cased: server.addr is SERVER_ADDR, database.url
# is DATABASE_URL, and the environment wins over this file. Keys you leave out
# keep the defaults in config/config.go. Add a group with bogie g config.
#
# Staging and production live in their own files with their own keys:
#   make credentials ENV=production
server:
  addr: ":8080"
  log_level: debug
database:
  url: postgres://postgres:postgres@localhost:5440/%[1]s_development?sslmode=disable
`

// writeCredentials creates config/master.key and config/credentials.yml.enc
// under dest. An existing key is never overwritten, since every file sealed
// with it would become unreadable; both files are reported as skipped then.
func writeCredentials(dest string, vars scaffold.Vars, pretend bool, report func(scaffold.Action)) error {
	dir := filepath.Join(dest, "config")
	keyPath := filepath.Join(dir, "master.key")
	keyRel, credsRel := "config/master.key", "config/credentials.yml.enc"

	if _, err := os.Stat(keyPath); err == nil {
		report(scaffold.Action{Op: scaffold.OpIdentical, Path: keyRel})
		report(scaffold.Action{Op: scaffold.OpIdentical, Path: credsRel})
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	report(scaffold.Action{Op: scaffold.OpCreate, Path: keyRel})
	report(scaffold.Action{Op: scaffold.OpCreate, Path: credsRel})
	if pretend {
		return nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	key, err := credentials.GenerateMasterKey(keyPath)
	if err != nil {
		return fmt.Errorf("new: %w", err)
	}
	editor := credentials.NewConfigEditor(dir, "credentials.yml.enc", "master.key", "")
	yaml := fmt.Sprintf(developmentSettings, vars.Name)
	if err := editor.EncryptAndSave([]byte(yaml), hex.EncodeToString(key)); err != nil {
		return fmt.Errorf("new: %w", err)
	}
	return nil
}
