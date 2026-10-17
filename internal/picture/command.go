package picture

import (
	"bytes"
	"errors"
	"strings"
	"time"
)

// waitDelay is how long a child process may keep its output pipes open after
// it was told to stop, so a stuck program cannot hold the caller past its
// context.
const waitDelay = 5 * time.Second

// errOutputTooLarge is what a bounded writer answers once the child has said
// more than it may. The write fails, the child gets a broken pipe and stops.
var errOutputTooLarge = errors.New("picture: a program wrote more than its limit")

// boundedWriter collects a child's output up to maximumBytes. It holds its
// buffer in a field instead of embedding it: an embedded bytes.Buffer brings
// its ReadFrom along, io.Copy prefers that to Write, and the bound would be
// skipped.
type boundedWriter struct {
	buffer       bytes.Buffer
	maximumBytes int
	isExceeded   bool
}

func (self *boundedWriter) Write(content []byte) (int, error) {
	if len(content) > self.maximumBytes-self.buffer.Len() {
		self.isExceeded = true
		return 0, errOutputTooLarge
	}
	return self.buffer.Write(content)
}

// tailWriter keeps the end of what a child says on standard error, which is
// where its reason for failing is, without letting a chatty one use memory.
type tailWriter struct {
	tail         []byte
	maximumBytes int
}

func (self *tailWriter) Write(content []byte) (int, error) {
	self.tail = append(self.tail, content...)
	if len(self.tail) > self.maximumBytes {
		self.tail = append(self.tail[:0], self.tail[len(self.tail)-self.maximumBytes:]...)
	}
	return len(content), nil
}

// lastLine returns the last non-empty line of what was written, which is
// short enough to put in an error message that a person reads.
func (self *tailWriter) lastLine() string {
	lines := strings.Split(strings.TrimSpace(strings.ToValidUTF8(string(self.tail), "")), "\n")
	line := strings.TrimSpace(lines[len(lines)-1])
	if len(line) > 300 {
		line = line[:300]
	}
	return line
}
