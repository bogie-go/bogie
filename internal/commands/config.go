package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bogie-go/bogie/internal/generate"
)

// planConfigGroup says what adding keys to a group of the credentials file
// takes. A group that is new gets its file and the lines that register it in
// config/config.go; one that exists gets only the keys it does not have yet,
// above its own markers, so running a generator twice changes nothing and
// config/<group>.go, whose defaults method is hand-written, is never
// overwritten. fresh is the keys actually added, which are the ones the
// development credentials need a line for.
func planConfigGroup(root, group string, keys []generate.ConfigKey) (files []generate.File, wires []generate.Wire, fresh []generate.ConfigKey, err error) {
	// server and database are the app's own: their structs sit in
	// config/config.go, with a marker inside each, so keys are added there.
	if generate.IsCoreConfigGroup(group) {
		src, err := os.ReadFile(filepath.Join(root, "config", "config.go"))
		if err != nil {
			return nil, nil, nil, err
		}
		for _, k := range keys {
			if !generate.ConfigStructHas(src, generate.Camel(group)+"Config", k.Name) {
				fresh = append(fresh, k)
			}
		}
		return nil, generate.CoreConfigKeyWires(group, fresh), fresh, nil
	}
	src, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(generate.ConfigPath(group))))
	if readErr != nil {
		f, w, err := generate.ConfigGroup(group, keys)
		if err != nil {
			return nil, nil, nil, err
		}
		return []generate.File{f}, w, keys, nil
	}
	for _, k := range keys {
		if !generate.ConfigGroupHas(src, k.Name) {
			fresh = append(fresh, k)
		}
	}
	return nil, generate.ConfigKeyWires(group, fresh), fresh, nil
}

// developmentKeys are the lines the development credentials file gets for
// keys: the value given, or a commented placeholder. A secret a generator
// added (authentication) gets a random value instead, so the app boots as it
// did without anyone typing one; a secret typed with `g config` never does,
// since nothing but the provider knows it.
func developmentKeys(keys []generate.ConfigKey, randomSecrets bool) ([]devKey, error) {
	var out []devKey
	for _, k := range keys {
		if k.Secret && randomSecrets {
			d, err := randomSecretLine(k.Name)
			if err != nil {
				return nil, err
			}
			out = append(out, d)
			continue
		}
		out = append(out, devKey{Name: k.Name, Line: k.YAML()})
	}
	return out, nil
}

// planFlatConfig is planConfigGroup for flat keys: they are fields on Config
// itself, so there is no file, only the lines in config/config.go, and a key
// already there (or a group of the same name) is left alone.
func planFlatConfig(root string, keys []generate.ConfigKey) (wires []generate.Wire, fresh []generate.ConfigKey, err error) {
	src, err := os.ReadFile(filepath.Join(root, "config", "config.go"))
	if err != nil {
		return nil, nil, err
	}
	for _, k := range keys {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(generate.ConfigPath(k.Name)))); err == nil {
			return nil, nil, fmt.Errorf("%s is already a group (config/%s.go); a flat key needs another name", k.Name, k.Name)
		}
		if !generate.ConfigGroupHas(src, k.Name) {
			fresh = append(fresh, k)
		}
	}
	return generate.ConfigFlatWires(fresh), fresh, nil
}
