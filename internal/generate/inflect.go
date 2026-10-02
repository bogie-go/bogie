// Package generate produces the files each `bogie generate` command writes.
// Everything here is pure: inputs in, files out, the clock passed in, so a
// golden test can pin every byte.
package generate

import "strings"

// A small inflector, enough for the names a Rails developer reaches for. It
// is deliberately not clever: the common rules, a short irregular list, and
// no attempt at Latin. A name it gets wrong is spelled out in full instead.
var irregulars = map[string]string{
	"person": "people",
	"child":  "children",
	"man":    "men",
	"woman":  "women",
	"mouse":  "mice",
	"foot":   "feet",
	"tooth":  "teeth",
}

// Plural turns a singular snake_case name into its table name.
func Plural(s string) string {
	if p, ok := irregulars[s]; ok {
		return p
	}
	for _, plural := range irregulars {
		if s == plural {
			return s
		}
	}
	switch {
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "z"),
		strings.HasSuffix(s, "ch"), strings.HasSuffix(s, "sh"):
		return s + "es" // status, bus, address, box, match
	case strings.HasSuffix(s, "y") && len(s) > 1 && !isVowel(s[len(s)-2]):
		return s[:len(s)-1] + "ies"
	default:
		return s + "s"
	}
}

// Singular turns a table name into a model name.
func Singular(s string) string {
	for singular, plural := range irregulars {
		if s == plural {
			return singular
		}
	}
	switch {
	case strings.HasSuffix(s, "ies") && len(s) > 3:
		return s[:len(s)-3] + "y"
	case strings.HasSuffix(s, "sses"), strings.HasSuffix(s, "uses"), strings.HasSuffix(s, "xes"),
		strings.HasSuffix(s, "zes"), strings.HasSuffix(s, "ches"), strings.HasSuffix(s, "shes"):
		return s[:len(s)-2]
	case strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss"):
		return s[:len(s)-1]
	default:
		return s
	}
}

func isVowel(c byte) bool { return strings.IndexByte("aeiou", c) >= 0 }

// Camel turns snake_case into CamelCase the way sqlc does, so the names the
// generator writes match the names sqlc generates: "id" becomes "ID", every
// other part is title-cased as is ("url" stays "Url").
func Camel(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		if p == "id" {
			parts[i] = "ID"
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// lowerCamel is Camel with a lower first letter, for local variables.
func lowerCamel(s string) string {
	c := Camel(s)
	if c == "" {
		return c
	}
	return strings.ToLower(c[:1]) + c[1:]
}
