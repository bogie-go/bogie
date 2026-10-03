package commands

import (
	"bytes"
	"strings"
	"testing"
)

// The Next block is the first thing a new user reads after `bogie new`; it
// must list the steps a fresh machine needs before the server can start:
// Postgres up, then the database prepared.
func TestNewPrintsEveryStepToAServingApp(t *testing.T) {
	t.Chdir(t.TempDir())
	var out bytes.Buffer
	if err := New([]string{"blog", "--skip-tidy"}, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	_, next, ok := strings.Cut(got, "\nNext:\n")
	if !ok {
		t.Fatalf("no Next block in:\n%s", got)
	}
	want := []string{"cd blog", "make up", "bogie db:prepare", "bogie server", "curl localhost:8080/healthz"}
	at := 0
	for _, w := range want {
		i := strings.Index(next[at:], w)
		if i < 0 {
			t.Fatalf("Next block is missing %q (or has it out of order):\n%s", w, next)
		}
		at += i + len(w)
	}
}
