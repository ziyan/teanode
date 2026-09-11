//go:build !windows

package cmd

import (
	"errors"
	"os/exec"
	"syscall"
)

// detach puts the background program in a session of its own, so that
// closing the terminal that started it does not end it.
func detach(child *exec.Cmd) {
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// processAlive says whether a process is there to be signalled.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// stopProcess asks a process to end.
func stopProcess(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}
