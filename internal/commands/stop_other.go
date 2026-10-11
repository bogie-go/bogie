//go:build !unix

package commands

import "os/exec"

// runStoppable is a plain Run where there are no process groups to signal.
func runStoppable(cmd *exec.Cmd) error { return cmd.Run() }
