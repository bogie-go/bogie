package generate

import "fmt"

// Seed writes db/seeds/<name>.go, a function that loads its data through
// db/adapter, and the line in db/seeds/seeds.go that registers it above the
// bogie:seeds marker. Unlike a migration, a seed carries no version table:
// `db seed` runs every seed every time, so the function it writes must be
// safe to run twice.
func Seed(module, name string) ([]File, []Wire, error) {
	if !namePattern.MatchString(name) {
		return nil, nil, fmt.Errorf("%q is not a seed name: lowercase letters, digits and underscores", name)
	}
	typ := Camel(name)
	files := []File{
		{Path: "db/seeds/" + name + ".go", Content: seedFile(module, name, typ)},
	}
	wires := []Wire{{
		File:   "db/seeds/seeds.go",
		Marker: "seeds",
		Line:   fmt.Sprintf("func() error { return Seed%s(ctx, s) },", typ),
	}}
	return files, wires, nil
}

// SeedWirePrefix is what destroy removes.
func SeedWirePrefix(name string) []Wire {
	return []Wire{{File: "db/seeds/seeds.go", Line: "func() error { return Seed" + Camel(name) + "(ctx, s) },"}}
}

func seedFile(module, name, typ string) string {
	return fmt.Sprintf(`package seeds

import (
	"context"

	"%[1]s/db/adapter"
)

// Seed%[3]s seeds %[2]s. db seed can run this any number of times, so
// make it safe to run twice: insert with ON CONFLICT DO NOTHING, or
// check before creating.
func Seed%[3]s(ctx context.Context, s *adapter.Store) error {
	// TODO: s.Create...(ctx, ...), written so a second run does nothing new.
	return nil
}
`, module, name, typ)
}
