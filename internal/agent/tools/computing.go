package tools

import (
	"context"
	"encoding/json"
	"time"
)

// Computing is what a run offers the shell and filesystem tools: the
// person's own computer, when they attached one with `teanode computer`.
// A run that has no such thing does not implement it.
type Computing interface {
	// AttachedComputers are the person's computers, by name; none when
	// none is attached.
	AttachedComputers() []Computer

	// ComputersAllowed says whether the operator lets people attach a
	// computer.
	ComputersAllowed() bool
}

// Computer is the attached computer as a tool reaches it: a request sent
// across, and the answer it gives.
type Computer interface {
	// Ask sends a request and waits up to wait for the answer.
	Ask(ctx context.Context, action string, args any, wait time.Duration) (json.RawMessage, error)
	Name() string
	System() string
	// Home is the person's home directory there, which ~ and a relative
	// path are from.
	Home() string
}
