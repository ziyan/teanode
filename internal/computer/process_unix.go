//go:build !windows

package computer

import (
	"os/exec"
	"syscall"
)

// prepare puts the command in a process group of its own and, when its time
// is up, ends the whole group: a shell that started a long program would
// otherwise be killed alone, and the program would keep the output pipes
// open until it finished on its own.
func prepare(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
}
