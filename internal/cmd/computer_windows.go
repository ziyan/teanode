//go:build windows

package cmd

import (
	"os"
	"os/exec"
	"syscall"
)

// detach starts the background program in a process group and without
// the console that started it, so that closing that window, or a Ctrl+C
// in it later, does not end the program.
func detach(child *exec.Cmd) {
	const detachedProcess = 0x00000008
	child.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess}
}

// processAlive says whether a process is there.
func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = process.Release()
	return true
}

// stopProcess ends a process; Windows has no gentler signal for a program
// without a console.
func stopProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}
