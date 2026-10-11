//go:build unix

package commands

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMain lets the test binary stand in for `bogie server`: with
// BOGIE_STOP_HELPER set it runs one command through runStoppable and exits.
// With BOGIE_STOP_APP set it is the app instead: it runs until SIGINT or
// SIGTERM, as a generated app's serve does.
func TestMain(m *testing.M) {
	if pidfile := os.Getenv("BOGIE_STOP_APP"); pidfile != "" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		// Ready only once it is listening for signals, as a server is once
		// it has bound its port.
		if err := os.WriteFile(pidfile, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
			os.Exit(1)
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Minute):
		}
		os.Exit(0)
	}
	if pidfile := os.Getenv("BOGIE_STOP_HELPER"); pidfile != "" {
		// sh plays `go run`, and the app it starts in the background is a
		// grandchild that a signal to the parent alone never reaches.
		cmd := exec.Command("sh", "-c", `"$0" -test.run='^$' & wait`, os.Args[0])
		cmd.Env = append(os.Environ(), "BOGIE_STOP_HELPER=", "BOGIE_STOP_APP="+pidfile)
		_ = runStoppable(cmd)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestStopSignalReachesTheApp(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			pidfile := filepath.Join(t.TempDir(), "app.pid")
			helper := exec.Command(os.Args[0], "-test.run=^$")
			helper.Env = append(os.Environ(), "BOGIE_STOP_HELPER="+pidfile)
			if err := helper.Start(); err != nil {
				t.Fatal(err)
			}
			exited := make(chan struct{})
			go func() { _ = helper.Wait(); close(exited) }()

			app := waitForPid(t, pidfile)
			t.Cleanup(func() { _ = syscall.Kill(app, syscall.SIGKILL) })

			if err := helper.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			select {
			case <-exited:
			case <-time.After(10 * time.Second):
				_ = helper.Process.Kill()
				t.Fatal("bogie did not exit after " + sig.String())
			}

			deadline := time.Now().Add(5 * time.Second)
			for syscall.Kill(app, 0) == nil {
				if time.Now().After(deadline) {
					t.Fatalf("the app (pid %d) still runs after %s reached bogie", app, sig)
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
}

func waitForPid(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the app never started")
	return 0
}
