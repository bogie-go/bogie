package commands

// aliases are Rails's own short forms, and only those: a Rails developer's
// fingers already know `rails s`, `rails t` and `rails g`. `d` arrives with
// destroy; `c` has no console to point at (there is none, by design; see
// docs/DESIGN.md §2).
var aliases = map[string]string{
	"s": "server",
	"t": "test",
	"g": "generate",
}

// Expand turns a Rails short form into the command it stands for, and leaves
// anything else as it is.
func Expand(command string) string {
	if full, ok := aliases[command]; ok {
		return full
	}
	return command
}
