//go:build !linux

package computer

import (
	"errors"
	"time"
)

// startTimeOfProcess is not known here: a Codex pane herdr knows no session
// for is then left with its screen.
func startTimeOfProcess(processId int) (time.Time, error) {
	return time.Time{}, errors.New("a process's start is not read on this system")
}
