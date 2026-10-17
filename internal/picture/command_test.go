package picture

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestBoundedWriterStopsAtItsLimit(t *testing.T) {
	writer := &boundedWriter{maximumBytes: 1000}
	// io.Copy is what os/exec uses, and it must not find a way around Write.
	if _, err := io.Copy(writer, bytes.NewReader(make([]byte, 999))); err != nil || writer.isExceeded {
		t.Fatalf("within the limit: %v, exceeded %v", err, writer.isExceeded)
	}
	if _, err := io.Copy(writer, bytes.NewReader(make([]byte, 2))); !errors.Is(err, errOutputTooLarge) || !writer.isExceeded {
		t.Errorf("past the limit: %v, exceeded %v", err, writer.isExceeded)
	}
	if writer.buffer.Len() != 999 {
		t.Errorf("kept %d bytes", writer.buffer.Len())
	}
}

func TestTailWriterKeepsTheEnd(t *testing.T) {
	writer := &tailWriter{maximumBytes: 20}
	_, _ = writer.Write([]byte("first line that is long\nsecond line\n"))
	_, _ = writer.Write([]byte("last words here\n\n"))
	if got := writer.lastLine(); got != "last words here" {
		t.Errorf("lastLine = %q", got)
	}
	if len(writer.tail) > 20 {
		t.Errorf("kept %d bytes", len(writer.tail))
	}
	if got := (&tailWriter{maximumBytes: 20}).lastLine(); got != "" {
		t.Errorf("lastLine of nothing = %q", got)
	}
}
