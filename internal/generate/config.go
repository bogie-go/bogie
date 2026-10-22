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

// IsCoreConfigGroup reports whether group is one of the app's own (server,
// database): its struct is in config/config.go, not in a file of its own, so
// keys can be added to it but the group cannot be created or destroyed.
func IsCoreConfigGroup(group string) bool {
	for _, r := range ReservedConfigGroups {
		if group == r {
			return true
		}
	}
	return false
}

// ParseConfigGroup checks the name of a group this generator can create and
// destroy: a core group is the app's own.
func ParseConfigGroup(group string) error {
	if !configName.MatchString(group) {
		return fmt.Errorf("%q is not a group name; lowercase words joined by underscores, like smtp or mail_gateway", group)
	}
	if IsCoreConfigGroup(group) {
		return fmt.Errorf("%s is one of the app's own groups, in config/config.go: add keys to it with g config, but it is not created or removed", group)
	}
	return nil
}

// CheckConfigGroupName is ParseConfigGroup for adding keys, where a core
// group is fine.
func CheckConfigGroupName(group string) error {
	if IsCoreConfigGroup(group) {
		return nil
	}
	return ParseConfigGroup(group)
}

// CoreConfigKeyWires add keys to a core group: the field above the marker
// inside its struct in config/config.go, and the production check, which
// reaches the key through the group (c.Database.Username) from Config's own
// validate.
func CoreConfigKeyWires(group string, keys []ConfigKey) []Wire {
	var ws []Wire
	for _, k := range keys {
		w := Wire{File: "config/config.go", Marker: "config:" + group, Line: k.fieldLine()}
		if k.Type == "duration" {
			w.Import = "time"
		}
		ws = append(ws, w)
		if l := k.validateLine(group, "c.Env"); l != "" {
			ws = append(ws, Wire{File: "config/config.go", Marker: "validate", Line: strings.Replace(l, `", c.`+k.Field(), `", c.`+Camel(group)+"."+k.Field(), 1)})
		}
	}
	return ws
}

// ConfigStructHas reports whether the struct named typeName in src already
// declares key, looking only inside that struct, so a key with the same name
// in another group is not mistaken for it.
func ConfigStructHas(src []byte, typeName, key string) bool {
	s := string(src)
	i := strings.Index(s, "type "+typeName+" struct {")
	if i < 0 {
		return false
	}
	end := strings.Index(s[i:], "\n}")
	if end < 0 {
		return false
	}
	return ConfigGroupHas([]byte(s[i:i+end]), key)
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
		if k.Type == "bool" && k.HasValue {
			if b, ok := parseBoolWord(k.Value); ok {
				k.Value = strconv.FormatBool(b)
			}
		}
		if err := k.checkValue(); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// parseBoolWord reads the words people type for a boolean: true/false, and
// yes/no, on/off, y/n.
func parseBoolWord(v string) (value, ok bool) {
	switch strings.ToLower(v) {
	case "true", "yes", "on", "y", "1":
		return true, true
	case "false", "no", "off", "n", "0":
		return false, true
	}
	return false, false
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

// Env is the environment variable that overrides it: SMTP_ADDRESS in a group,
// just HELLO for a flat key (group "").
func (k ConfigKey) Env(group string) string {
	if group == "" {
		return strings.ToUpper(k.Name)
	}
	return strings.ToUpper(group + "_" + k.Name)
}

// YAML is the line for the development credentials file: the value if one
// was given, otherwise a commented placeholder, because a key present with an
// empty value would override the default in config.go with nothing.
func (k ConfigKey) YAML() string {
	if k.HasValue {
		switch k.Type {
		case "string", "duration":
			return fmt.Sprintf("%s: %s", k.Name, yamlScalar(k.Value))
		}
		return fmt.Sprintf("%s: %s", k.Name, k.Value)
	}
	placeholder := map[string]string{"string": `""`, "int": "0", "bool": "false", "duration": `"10s"`}[k.Type]
	return fmt.Sprintf("# %s: %s", k.Name, placeholder)
}

var bareYAML = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_./@+-]*$|^[0-9]+[A-Za-z]+$`)

// yamlScalar writes a string value the way a person would: bare when that is
// safe (xxx, help@example.com, 5m), quoted when YAML would read it as
// something else. YAML 1.1 parsers take yes, no, on, off, y, n, true, false and
// null as booleans or null, and a bare number as a number, and the value was
// typed as a string, so those are quoted. So is anything with a colon, a space
// or a quote in it.
func yamlScalar(v string) string {
	switch strings.ToLower(v) {
	case "y", "n", "yes", "no", "on", "off", "true", "false", "null", "~":
		return strconv.Quote(v)
	}
	if _, err := strconv.ParseFloat(v, 64); err == nil || !bareYAML.MatchString(v) {
		return strconv.Quote(v)
	}
	return v
}

func (k ConfigKey) fieldLine() string {
	return fmt.Sprintf("%s %s `mapstructure:%q`", k.Field(), k.goType(), k.Name)
}

// validateLine is the production check for a required key, or "". env is the
// Go expression for the environment: the parameter inside a group's validate,
// c.Env on Config itself.
func (k ConfigKey) validateLine(group, env string) string {
	switch {
	case k.Secret:
		return fmt.Sprintf("requireSecret(%s, %q, c.%s, %d),", env, k.Env(group), k.Field(), k.Min)
	case k.Req && k.Type == "string":
		return fmt.Sprintf("requireKey(%s, %q, c.%s != \"\"),", env, k.Env(group), k.Field())
	case k.Req:
		return fmt.Sprintf("requireKey(%s, %q, c.%s != 0),", env, k.Env(group), k.Field())
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
		if l := k.validateLine(group, "env"); l != "" {
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
		{File: "config/config.go", Marker: "config", Sorted: true, Line: fmt.Sprintf("%s %s `mapstructure:%q`", f, groupType(group), group)},
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
		if l := k.validateLine(group, "env"); l != "" {
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

// A flat key is a top-level key of the credentials file with no group around
// it (hello: world): a field directly on Config, overridden by the variable
// of its own name (HELLO). Its defaults go in config.go's defaults() by hand.

// reservedFlatNames cannot be flat keys: they are Config's own fields, or the
// app's own groups.
var reservedFlatNames = []string{"env", "source", "key_file", "server", "database"}

// IsFlatConfigArg reports whether the first argument of `g config` is a flat
// key (hello=world, retries:int) rather than the name of a group.
func IsFlatConfigArg(arg string) bool { return strings.ContainsAny(arg, "=:") }

// ParseFlatConfigKeys reads flat keys, with the same grammar as a group's.
func ParseFlatConfigKeys(args []string) ([]ConfigKey, error) {
	keys, err := ParseConfigKeys(args)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		for _, r := range reservedFlatNames {
			if k.Name == r {
				return nil, fmt.Errorf("%s cannot be a flat key: it is one of Config's own names", k.Name)
			}
		}
	}
	return keys, nil
}

// ConfigFlatWires register flat keys in config/config.go: the field, sorted in
// among the groups, and the production check for a required one.
func ConfigFlatWires(keys []ConfigKey) []Wire {
	var ws []Wire
	for _, k := range keys {
		w := Wire{File: "config/config.go", Marker: "config", Sorted: true, Line: k.fieldLine()}
		if k.Type == "duration" {
			w.Import = "time"
		}
		ws = append(ws, w)
		if l := k.validateLine("", "c.Env"); l != "" {
			ws = append(ws, Wire{File: "config/config.go", Marker: "validate", Line: l})
		}
	}
	return ws
}

// ConfigFlatWirePrefixes are what destroy removes for a flat key.
func ConfigFlatWirePrefixes(name string) []Wire {
	k := ConfigKey{Name: name}
	env := strings.ToUpper(name)
	return []Wire{
		{File: "config/config.go", Line: k.Field() + " "},
		{File: "config/config.go", Line: fmt.Sprintf("requireKey(c.Env, %q,", env)},
		{File: "config/config.go", Line: fmt.Sprintf("requireSecret(c.Env, %q,", env)},
	}
}
