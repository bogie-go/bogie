//go:build unix

package commands

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// runStoppable runs a long-lived command (`go run . serve`, or Air) in a
// process group of its own and passes SIGINT, SIGTERM and SIGHUP on to the
// whole group, the way `rails s` stops when it is told to.
//
// Without this, only the direct child hears a signal sent to `bogie server`.
// `go run` dies on SIGTERM without telling the app it started, so the app
// kept :8080 after a process manager, an IDE's stop button or a plain `kill`
// stopped bogie, and the next `bogie server` failed with "address already in
// use". Ctrl-C in a terminal is unchanged: it still reaches every process
// once, now by way of this forwarder.
func runStoppable(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)

	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case s := <-signals:
				// The group's id is the child's pid, given Setpgid.
				_ = syscall.Kill(-cmd.Process.Pid, s.(syscall.Signal))
			case <-done:
				return
			}
		}
	}()
	return cmd.Wait()
}
