package generate

import (
	"fmt"
	"go/format"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A group is one top-level section of the credentials file (smtp:), written
// as a typed struct in config/<group>.go and held by config.Config. Its keys
// are overridden by the environment variable named after their path:
// smtp.address is SMTP_ADDRESS. The user chooses every name, so nothing here
// adds an app prefix to them.

// ConfigKey is one key of a group, typed on the command line as
//
//	name[:type][:flag][=value]
//
// type is string (the default), int, bool or duration; flag is req (production
// refuses to boot without it) or secret (the same, and the development
// credentials get a commented placeholder, never an invented value); =value is
// what the development credentials file gets.
type ConfigKey struct {
	Name     string
	Type     string
	Req      bool
	Secret   bool
	Min      int // secrets: shortest accepted length in production
	Value    string
	HasValue bool
}

var (
	configName  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	configTypes = map[string]string{"string": "string", "int": "int", "bool": "bool", "duration": "time.Duration"}
)

// ReservedConfigGroups are the groups every app starts with: their structs
// live in config.go itself, not in a file this generator owns.
var ReservedConfigGroups = []string{"server", "database"}

// ParseConfigGroup checks a group name.
func ParseConfigGroup(group string) error {
	if !configName.MatchString(group) {
		return fmt.Errorf("%q is not a group name; lowercase words joined by underscores, like smtp or mail_gateway", group)
	}
	for _, r := range ReservedConfigGroups {
		if group == r {
			return fmt.Errorf("%s is one of the app's own groups, in config/config.go; add your keys to a group of your own", group)
		}
	}
	return nil
}

// ParseConfigKeys reads `name[:type][:flag][=value]` arguments.
func ParseConfigKeys(args []string) ([]ConfigKey, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("at least one key is required: bogie g config smtp address port:int")
	}
	var keys []ConfigKey
	seen := map[string]bool{}
	for _, arg := range args {
		spec, value, hasValue := strings.Cut(arg, "=")
		parts := strings.Split(spec, ":")
		k := ConfigKey{Name: parts[0], Type: "string", Value: value, HasValue: hasValue, Min: 1}
		if !configName.MatchString(k.Name) {
			return nil, fmt.Errorf("%q is not a key name; lowercase words joined by underscores, like address or retry_limit", k.Name)
		}
		if seen[k.Name] {
			return nil, fmt.Errorf("key %s is given twice", k.Name)
		}
		seen[k.Name] = true
		for _, p := range parts[1:] {
			switch p {
			case "string", "int", "bool", "duration":
				k.Type = p
			case "req":
				k.Req = true
			case "secret":
				k.Secret = true
			default:
				return nil, fmt.Errorf("%s: %q is neither a type (string int bool duration) nor a flag (req secret)", k.Name, p)
			}
		}
		if (k.Req || k.Secret) && k.Type == "bool" {
			return nil, fmt.Errorf("%s: a bool cannot be required, since false is a value", k.Name)
		}
		if k.Secret && k.Type != "string" {
			return nil, fmt.Errorf("%s: a secret is a string", k.Name)
		}
		if k.Secret && k.HasValue {
			return nil, fmt.Errorf("%s: a secret gets no development value here; set it with make credentials", k.Name)
		}
		if err := k.checkValue(); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}

func (k ConfigKey) checkValue() error {
	if !k.HasValue {
		return nil
	}
	var err error
	switch k.Type {
	case "int":
		_, err = strconv.Atoi(k.Value)
	case "bool":
		_, err = strconv.ParseBool(k.Value)
	case "duration":
		_, err = time.ParseDuration(k.Value)
	}
	if err != nil {
		return fmt.Errorf("%s: %q is not a valid %s", k.Name, k.Value, k.Type)
	}
	return nil
}

// Field is the Go field name, Smtp's Address.
func (k ConfigKey) Field() string { return Camel(k.Name) }

func (k ConfigKey) goType() string { return configTypes[k.Type] }

// Env is the environment variable that overrides it.
func (k ConfigKey) Env(group string) string { return strings.ToUpper(group + "_" + k.Name) }

// YAML is the line for the development credentials file: the value if one
// was given, otherwise a commented placeholder, because a key present with an
// empty value would override the default in config.go with nothing.
func (k ConfigKey) YAML() string {
	if k.HasValue {
		switch k.Type {
		case "string", "duration":
			return fmt.Sprintf("%s: %q", k.Name, k.Value)
		}
		return fmt.Sprintf("%s: %s", k.Name, k.Value)
	}
	placeholder := map[string]string{"string": `""`, "int": "0", "bool": "false", "duration": `"10s"`}[k.Type]
	return fmt.Sprintf("# %s: %s", k.Name, placeholder)
}

func (k ConfigKey) fieldLine() string {
	return fmt.Sprintf("%s %s `mapstructure:%q`", k.Field(), k.goType(), k.Name)
}

// validateLine is the production check for a required key, or "".
func (k ConfigKey) validateLine(group string) string {
	switch {
	case k.Secret:
		return fmt.Sprintf("requireSecret(env, %q, c.%s, %d),", k.Env(group), k.Field(), k.Min)
	case k.Req && k.Type == "string":
		return fmt.Sprintf("requireKey(env, %q, c.%s != \"\"),", k.Env(group), k.Field())
	case k.Req:
		return fmt.Sprintf("requireKey(env, %q, c.%s != 0),", k.Env(group), k.Field())
	}
	return ""
}

// ConfigPath is the group's file.
func ConfigPath(group string) string { return "config/" + group + ".go" }

func groupType(group string) string { return Camel(group) + "Config" }

// ConfigGroup writes config/<group>.go and the lines that register the group
// in config/config.go. Run again for a group that exists, bogie adds only
// ConfigKeyWires: this file is written once, and its defaults method is yours.
func ConfigGroup(group string, keys []ConfigKey) (File, []Wire, error) {
	if err := ParseConfigGroup(group); err != nil {
		return File{}, nil, err
	}
	typ := groupType(group)
	var b strings.Builder
	b.WriteString("package config\n\n")
	for _, k := range keys {
		if k.Type == "duration" {
			b.WriteString("import \"time\"\n\n")
			break
		}
	}
	fmt.Fprintf(&b, "// %s is the %s: group of the credentials file. Each key is overridden by\n", typ, group)
	fmt.Fprintf(&b, "// the environment variable named after it: %s.%s is %s.\n", group, keys[0].Name, keys[0].Env(group))
	b.WriteString("//\n// Written by `bogie g config`, once: `bogie g config " + group + " KEY` adds a key above\n")
	b.WriteString("// the marker and never overwrites this file, so defaults is yours to edit.\n")
	fmt.Fprintf(&b, "type %s struct {\n", typ)
	for _, k := range keys {
		fmt.Fprintf(&b, "\t%s\n", k.fieldLine())
	}
	fmt.Fprintf(&b, "\t// bogie:config:%s\n}\n\n", group)

	fmt.Fprintf(&b, "// defaults is what this group starts from in env. The credentials file and\n")
	fmt.Fprintf(&b, "// the environment change only the keys they name, so a key absent from both\n")
	fmt.Fprintf(&b, "// keeps what is set here. Per-environment policy goes here, by hand: a fake\n")
	fmt.Fprintf(&b, "// service in development, nothing in production.\n")
	fmt.Fprintf(&b, "func (c *%s) defaults(env string) {}\n\n", typ)

	fmt.Fprintf(&b, "// validate refuses to boot in production without a required key.\n")
	fmt.Fprintf(&b, "func (c %s) validate(env string) error {\n\tfor _, err := range []error{\n", typ)
	for _, k := range keys {
		if l := k.validateLine(group); l != "" {
			fmt.Fprintf(&b, "\t\t%s\n", l)
		}
	}
	fmt.Fprintf(&b, "\t\t// bogie:validate:%s\n\t} {\n\t\tif err != nil {\n\t\t\treturn err\n\t\t}\n\t}\n\treturn nil\n}\n", group)

	out, err := format.Source([]byte(b.String()))
	if err != nil {
		return File{}, nil, fmt.Errorf("config %s: generated %s does not parse: %w", group, ConfigPath(group), err)
	}
	return File{Path: ConfigPath(group), Content: string(out)}, ConfigGroupWires(group), nil
}

// ConfigGroupWires are the three lines a group needs in config/config.go:
// the field, its defaults, and its production check.
func ConfigGroupWires(group string) []Wire {
	f := Camel(group)
	return []Wire{
		{File: "config/config.go", Marker: "config", Line: fmt.Sprintf("%s %s `mapstructure:%q`", f, groupType(group), group)},
		{File: "config/config.go", Marker: "defaults", Line: fmt.Sprintf("c.%s.defaults(env)", f)},
		{File: "config/config.go", Marker: "validate", Line: fmt.Sprintf("c.%s.validate(c.Env),", f)},
	}
}

// ConfigGroupWirePrefixes are what destroy removes from config/config.go.
func ConfigGroupWirePrefixes(group string) []Wire {
	f := Camel(group)
	return []Wire{
		{File: "config/config.go", Line: f + " " + groupType(group)},
		{File: "config/config.go", Line: "c." + f + ".defaults("},
		{File: "config/config.go", Line: "c." + f + ".validate("},
	}
}

// ConfigKeyWires add keys to a group that already has its file: the field
// above the group's marker, the check above its validate marker, and the
// import a duration needs.
func ConfigKeyWires(group string, keys []ConfigKey) []Wire {
	var ws []Wire
	for _, k := range keys {
		w := Wire{File: ConfigPath(group), Marker: "config:" + group, Line: k.fieldLine()}
		if k.Type == "duration" {
			w.Import = "time"
		}
		ws = append(ws, w)
		if l := k.validateLine(group); l != "" {
			ws = append(ws, Wire{File: ConfigPath(group), Marker: "validate:" + group, Line: l})
		}
	}
	return ws
}

// ConfigGroupHas reports whether the group's source already declares key, so
// a second run adds only what is new.
func ConfigGroupHas(src []byte, key string) bool {
	return strings.Contains(string(src), fmt.Sprintf("`mapstructure:%q`", key))
}
