package commands

import "testing"

func TestExpandKnowsRailsShortForms(t *testing.T) {
	cases := map[string]string{"s": "server", "t": "test", "server": "server", "db:migrate": "db:migrate", "x": "x"}
	for in, want := range cases {
		if got := Expand(in); got != want {
			t.Errorf("Expand(%q) = %q, want %q", in, got, want)
		}
	}
}
