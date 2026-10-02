package commands

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNextStampSkipsTakenVersions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "db", "migrate")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 2, 9, 30, 0, 0, time.UTC)
	for _, name := range []string{"20261002093000_a.sql", "20261002093001_b.sql"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := nextStamp(root, now)
	if got.Format("20060102150405") != "20261002093002" {
		t.Errorf("nextStamp = %s, want 20261002093002", got.Format("20060102150405"))
	}
}
