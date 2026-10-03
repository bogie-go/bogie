package commands

import "runtime/debug"

// version is set by a release build:
//
//	go build -ldflags "-X github.com/bogie-go/bogie/internal/commands.version=v0.1.0"
//
// which is what the Homebrew formula does. Left empty, Version reads the
// build info instead, so `go install github.com/bogie-go/bogie@v0.1.0` says
// v0.1.0 without any flag, and a build from a clone says which commit it is:
// Go stamps it as the pseudo-version v0.0.0-<date>-<commit>, with "+dirty"
// when the tree had uncommitted changes.
var version string

// Version is the tool's version, one string from one of three sources: the
// release flag, the module version the build carried, or, for a toolchain
// that stamps none, "dev+<short hash>" from the VCS settings. It is printed
// by `bogie version` and written to every generated bogie.toml, so an app
// always knows what made it (docs/DESIGN.md §5a: one version, one source of
// truth).
func Version() string {
	if version != "" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "dev"
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if dirty {
		return "dev+" + rev + "-dirty"
	}
	return "dev+" + rev
}
