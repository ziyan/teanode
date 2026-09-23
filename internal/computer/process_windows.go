//go:build windows

package computer

import "os/exec"

// prepare has nothing to add on Windows: the process is killed by its
// context, and WaitDelay lets go of what it left running.
func prepare(command *exec.Cmd) {}

// terminate ends the command; Windows has no polite signal to send first.
func terminate(command *exec.Cmd) {
	if command.Process != nil {
		_ = command.Process.Kill()
	}
}
