package commands

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// `bogie doctor --help` answers from anywhere: outside an app it must not
// demand a bogie.toml, and inside one it must not run the checks.
func TestDoctorHelpOutsideAndInsideAnApp(t *testing.T) {
	outside := t.TempDir()
	inside := t.TempDir()
	if err := os.WriteFile(inside+"/"+Marker, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{outside, inside} {
		t.Chdir(dir)
		for _, flag := range []string{"-h", "--help", "help"} {
			var out bytes.Buffer
			if err := Doctor([]string{flag}, &out); err != nil {
				t.Fatalf("doctor %s in %s: %v", flag, dir, err)
			}
			if !strings.HasPrefix(out.String(), "Usage: bogie doctor") {
				t.Errorf("doctor %s in %s printed %q, want its usage", flag, dir, out.String())
			}
			if strings.Contains(out.String(), "ok  ") || strings.Contains(out.String(), "FAIL") {
				t.Errorf("doctor %s ran the checks", flag)
			}
		}
	}
}

func TestDoctorRejectsArguments(t *testing.T) {
	t.Chdir(t.TempDir())
	err := Doctor([]string{"extra"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "takes no arguments") {
		t.Fatalf("got %v, want a takes-no-arguments error", err)
	}
}

// Outside an app the error comes back once, for main to print once.
func TestDoctorOutsideAnAppReturnsTheErrorOnce(t *testing.T) {
	t.Chdir(t.TempDir())
	var out bytes.Buffer
	err := Doctor(nil, &out)
	if err == nil || strings.Count(err.Error(), "not inside a bogie app") != 1 {
		t.Fatalf("got %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("doctor also wrote %q to stdout", out.String())
	}
}
