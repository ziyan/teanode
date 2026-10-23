//go:build linux

package computer

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// clockTicksPerSecond is the unit of a process's start in /proc: USER_HZ,
// which is 100 on every Linux this program runs on.
const clockTicksPerSecond = 100

// startTimeOfProcess is when a process started, from /proc: the clock
// ticks since boot it started at, after the boot time.
func startTimeOfProcess(processId int) (time.Time, error) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", processId))
	if err != nil {
		return time.Time{}, err
	}
	// The command's name is in parentheses and may hold spaces: the fields
	// are counted from after its closing one, the third field first.
	nameEnd := bytes.LastIndexByte(stat, ')')
	if nameEnd < 0 {
		return time.Time{}, errors.New("cannot read the process's start")
	}
	fields := strings.Fields(string(stat[nameEnd+1:]))
	const startTimeIndex = 22 - 3
	if len(fields) <= startTimeIndex {
		return time.Time{}, errors.New("cannot read the process's start")
	}
	startTicks, err := strconv.ParseInt(fields[startTimeIndex], 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	bootedAt, err := bootTime()
	if err != nil {
		return time.Time{}, err
	}
	return bootedAt.Add(time.Duration(startTicks) * time.Second / clockTicksPerSecond), nil
}

// bootTime is when the machine started, from /proc/stat.
func bootTime() (time.Time, error) {
	stat, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}, err
	}
	for _, line := range strings.Split(string(stat), "\n") {
		if rest, isBootTime := strings.CutPrefix(line, "btime "); isBootTime {
			seconds, err := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
			if err != nil {
				return time.Time{}, err
			}
			return time.Unix(seconds, 0), nil
		}
	}
	return time.Time{}, errors.New("cannot read when the machine started")
}
