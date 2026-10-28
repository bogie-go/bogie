package commands

// aliases are the short forms `s`, `t`, `g` and `d`, and only those. `c`
// has no console to point at (there is none, by design; see docs/DESIGN.md
// §2).
var aliases = map[string]string{
	"s": "server",
	"t": "test",
	"g": "generate",
	"d": "destroy",
}

// Expand turns a short form into the command it stands for, and leaves
// anything else as it is.
func Expand(command string) string {
	if full, ok := aliases[command]; ok {
		return full
	}
	return command
}
