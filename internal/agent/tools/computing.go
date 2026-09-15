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

// SessionHolder is a computer that can hold a program open: a terminal the
// agent drives, a server spoken to over its standard input. Separate from
// Computer so that what only asks and answers need not pretend to.
type SessionHolder interface {
	// StartSession runs a program and returns the session's identifier.
	// kind is "stdio" or "pty"; columns and rows size a terminal.
	StartSession(ctx context.Context, kind, command string, arguments []string,
		directory string, environment map[string]string, columns, rows int) (string, error)
	// WriteSession writes to its input.
	WriteSession(ctx context.Context, id string, data []byte) error
	// ReadScreen is a terminal's screen as it stands.
	ReadScreen(ctx context.Context, id string) (*Screen, error)
	// ResizeSession changes a terminal's size.
	ResizeSession(ctx context.Context, id string, columns, rows int) error
	// SignalSession sends int, term, kill or hup.
	SignalSession(ctx context.Context, id, signal string) error
	// CloseSession ends it.
	CloseSession(ctx context.Context, id string) error
}

// Screen is what a terminal shows at one moment.
type Screen struct {
	Columns int    `json:"columns"`
	Rows    int    `json:"rows"`
	Text    string `json:"text"`
	CursorX int    `json:"cursorX"`
	CursorY int    `json:"cursorY"`
	// Changed says it differs from the last read.
	Changed bool `json:"changed"`
	// Ended says the program finished, with Code; the screen is what it left.
	Ended bool `json:"ended"`
	Code  int  `json:"code"`
}
