package computer

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
)

// A terminal session: the program runs in a pty and draws on a screen kept
// here.
//
// The screen, not the scrollback, and kept on this side rather than the
// server's. What a terminal is for is reading what a program is showing now,
// and a program that redraws -- a progress bar, an editor, anything that
// prompts -- says the same thing a hundred times over in escape sequences. A
// model handed all of that learns nothing; a model handed the screen as it
// stands learns what a person looking at it would. And keeping it here means
// none of the redrawing crosses the network: reading the screen is one
// request, however busy the program has been.

// The bounds of a terminal.
const (
	defaultColumns = 120
	defaultRows    = 40
	mostColumns    = 300
	mostRows       = 100
)

// SessionReadArguments ask for the screen as it stands.
type SessionReadArguments struct {
	Session string `json:"session"`
}

// SessionScreen is what a terminal shows at this moment.
type SessionScreen struct {
	Session string `json:"session"`
	Columns int    `json:"columns"`
	Rows    int    `json:"rows"`

	// Text is the screen, one line per row, trailing blank rows dropped.
	Text string `json:"text"`

	// CursorX and CursorY are where the cursor is, from the top left.
	CursorX int `json:"cursorX"`
	CursorY int `json:"cursorY"`

	// Changed says the screen has changed since it was last read, which is
	// what a wait for it to settle looks at.
	Changed bool `json:"changed"`

	// Ended says the program finished, with Code. The screen is still what
	// it left.
	Ended bool `json:"ended"`
	Code  int  `json:"code"`
}

// SessionResizeArguments change a terminal's size.
type SessionResizeArguments struct {
	Session string `json:"session"`
	Columns int    `json:"columns"`
	Rows    int    `json:"rows"`
}

// screen is the emulator and what is known about the last read of it.
type screen struct {
	mutex    sync.Mutex
	terminal vt10x.Terminal
	changed  bool
	columns  int
	rows     int
}

// startTerminal runs the program in a pty of the size asked for and keeps
// its screen. Called with the reservation already made.
func (self *sessions) startTerminal(reserved *session, release func(), command *exec.Cmd,
	arguments *SessionStartArguments, ended pushEnded) (*SessionStartResult, error) {
	return self.startTerminalWith(reserved, release, command, arguments, ended, nil)
}

// startTerminalWith is startTerminal with somebody sitting in it: tee is
// their screen, given everything the program writes.
func (self *sessions) startTerminalWith(reserved *session, release func(), command *exec.Cmd,
	arguments *SessionStartArguments, ended pushEnded, tee io.Writer) (*SessionStartResult, error) {
	columns, rows := arguments.Columns, arguments.Rows
	if columns <= 0 {
		columns = defaultColumns
	}
	if rows <= 0 {
		rows = defaultRows
	}
	if columns > mostColumns || rows > mostRows {
		release()
		return nil, fmt.Errorf("a terminal is at most %d by %d", mostColumns, mostRows)
	}

	// A program in a terminal asks what kind it is; without an answer the
	// interesting ones refuse to draw at all.
	command.Env = append(command.Env, "TERM=xterm-256color", fmt.Sprintf("COLUMNS=%d", columns), fmt.Sprintf("LINES=%d", rows))

	file, err := pty.StartWithSize(command, &pty.Winsize{Rows: uint16(rows), Cols: uint16(columns)})
	if err != nil {
		release()
		if errors.Is(err, pty.ErrUnsupported) {
			return nil, fmt.Errorf("a terminal cannot be opened on this computer")
		}
		return nil, fmt.Errorf("cannot start %s in a terminal: %w", arguments.Command, err)
	}

	drawn := &screen{terminal: vt10x.New(vt10x.WithSize(columns, rows)), columns: columns, rows: rows}
	self.mutex.Lock()
	reserved.command, reserved.pty, reserved.screen = command, file, drawn
	self.mutex.Unlock()

	// Everything the program writes goes to the screen, where it is read
	// on request; it is not streamed. A terminal is read as a screen. When
	// somebody is sitting in it, what the program writes goes to them too.
	go func() {
		var source io.Reader = file
		if tee != nil {
			source = io.TeeReader(file, tee)
		}
		reader := bufio.NewReader(source)
		for {
			if err := drawn.parse(reader); err != nil {
				return
			}
		}
	}()
	go func() {
		err := command.Wait()
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				code = exit.ExitCode()
			} else {
				code = -1
			}
		}
		self.finish(reserved, code)
		ended(arguments.Session, code)
	}()

	return &SessionStartResult{Session: arguments.Session, PID: command.Process.Pid}, nil
}

// parse feeds the emulator until the reader has nothing more for now, and
// marks the screen changed.
func (self *screen) parse(reader *bufio.Reader) error {
	// vt10x locks the state while it reads, and reads until the buffer is
	// empty, so one call here is one burst of output.
	if err := self.terminal.Parse(reader); err != nil {
		return err
	}
	self.mutex.Lock()
	self.changed = true
	self.mutex.Unlock()
	return nil
}

// read renders the screen as text and says whether it changed since the last
// read.
func (self *screen) read() (text string, cursorX, cursorY int, changed bool) {
	self.mutex.Lock()
	changed, self.changed = self.changed, false
	self.mutex.Unlock()

	// String locks the state itself; Cursor does not. Locking around both
	// was a deadlock against String's own lock, since a Go mutex does not
	// re-enter -- so the cursor is read under the lock and the rendering
	// is left to take it.
	self.terminal.Lock()
	cursor := self.terminal.Cursor()
	self.terminal.Unlock()
	rendered := self.terminal.String()

	// Trailing blank rows are what an empty screen mostly is, and nothing
	// anybody reads. Trailing spaces on a row likewise.
	lines := strings.Split(rendered, "\n")
	for index := range lines {
		lines[index] = strings.TrimRight(lines[index], " ")
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n"), cursor.X, cursor.Y, changed
}

// resize changes the pty and the screen together.
func (self *screen) resize(file *os.File, columns, rows int) error {
	if columns <= 0 || rows <= 0 || columns > mostColumns || rows > mostRows {
		return fmt.Errorf("a terminal is at most %d by %d", mostColumns, mostRows)
	}
	if err := pty.Setsize(file, &pty.Winsize{Rows: uint16(rows), Cols: uint16(columns)}); err != nil {
		return fmt.Errorf("cannot resize the terminal: %w", err)
	}
	self.terminal.Resize(columns, rows)
	self.mutex.Lock()
	self.columns, self.rows, self.changed = columns, rows, true
	self.mutex.Unlock()
	return nil
}

// readScreen answers a read of a terminal session.
func (self *sessions) readScreen(arguments *SessionReadArguments) (*SessionScreen, error) {
	held, err := self.find(arguments.Session)
	if err != nil {
		return nil, err
	}
	if held.screen == nil {
		return nil, fmt.Errorf("the session %q is not a terminal; its output arrives as it is written", arguments.Session)
	}
	text, cursorX, cursorY, changed := held.screen.read()
	held.screen.mutex.Lock()
	columns, rows := held.screen.columns, held.screen.rows
	held.screen.mutex.Unlock()
	self.mutex.Lock()
	ended, code := held.ended, held.code
	self.mutex.Unlock()
	return &SessionScreen{
		Session: arguments.Session, Columns: columns, Rows: rows,
		Text: text, CursorX: cursorX, CursorY: cursorY, Changed: changed,
		Ended: ended, Code: code,
	}, nil
}

// resizeTerminal answers a resize.
func (self *sessions) resizeTerminal(arguments *SessionResizeArguments) (*SessionResult, error) {
	held, err := self.find(arguments.Session)
	if err != nil {
		return nil, err
	}
	if held.screen == nil || held.pty == nil {
		return nil, fmt.Errorf("the session %q is not a terminal", arguments.Session)
	}
	if err := held.screen.resize(held.pty, arguments.Columns, arguments.Rows); err != nil {
		return nil, err
	}
	return &SessionResult{Session: arguments.Session, OK: true}, nil
}

// attachTerminal opens the person's own shell in a pty as the attached
// session: what they type goes to it, what it writes goes to them and to
// the screen, and the pty follows the size of the terminal they are in.
func (self *sessions) attachTerminal(ctx context.Context, options *Options, ended pushEnded) (*session, error) {
	terminal := options.Terminal
	shell := strings.TrimSpace(terminal.Shell)
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	if shell == "" {
		shell = "/bin/sh"
	}
	columns, rows := defaultColumns, defaultRows
	if terminal.Size != nil {
		if c, r := terminal.Size(); c > 0 && r > 0 {
			columns, rows = c, r
		}
	}

	self.mutex.Lock()
	reserved := &session{id: AttachedSession, kind: "pty", done: make(chan struct{})}
	self.open[AttachedSession] = reserved
	self.mutex.Unlock()
	release := func() {
		self.mutex.Lock()
		delete(self.open, AttachedSession)
		self.mutex.Unlock()
	}

	command := exec.Command(shell)
	command.Dir = options.Home
	command.Env = os.Environ()
	arguments := &SessionStartArguments{Session: AttachedSession, Kind: "pty", Command: shell, Columns: columns, Rows: rows}
	if _, err := self.startTerminalWith(reserved, release, command, arguments, ended, terminal.Output); err != nil {
		return nil, err
	}

	// What the person types goes straight to the pty.
	if terminal.Input != nil {
		go func() {
			_, _ = io.Copy(reserved.pty, terminal.Input)
		}()
	}
	// And the pty follows their window.
	if terminal.Size != nil {
		go func() {
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			lastColumns, lastRows := columns, rows
			for {
				select {
				case <-ctx.Done():
					return
				case <-reserved.done:
					return
				case <-ticker.C:
					c, r := terminal.Size()
					if c > 0 && r > 0 && (c != lastColumns || r != lastRows) {
						lastColumns, lastRows = c, r
						_ = reserved.screen.resize(reserved.pty, c, r)
					}
				}
			}
		}()
	}
	return reserved, nil
}
