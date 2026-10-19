package db

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

type recordedLines struct{ lines []string }

func (self *recordedLines) Printf(format string, arguments ...interface{}) {
	self.lines = append(self.lines, strings.TrimSpace(format))
	for _, argument := range arguments {
		if text, isText := argument.(string); isText {
			self.lines = append(self.lines, text)
		}
	}
}

// The values bound to a statement are what a person keeps, so a statement that
// fails or is slow is logged with its placeholders and never with them.
func TestLoggerKeepsBoundValuesOutOfTheLog(t *testing.T) {
	recorded := &recordedLines{}
	configured := newLogger(recorded)

	filter, isFilter := configured.(gorm.ParamsFilter)
	if !isFilter {
		t.Fatal("the logger does not filter the parameters of a statement")
	}
	statement, parameters := filter.ParamsFilter(context.Background(), "INSERT INTO health_sample (quantity_value) VALUES (?)", 72.5)
	if len(parameters) != 0 {
		t.Errorf("the logger kept the bound values: %v", parameters)
	}
	if !strings.Contains(statement, "?") {
		t.Errorf("the statement lost its placeholders: %q", statement)
	}

	// A slow statement and a failed one are the two that reach the log by default.
	configured.Trace(context.Background(), time.Now().Add(-time.Second), func() (string, int64) { return statement, 1 }, nil)
	configured.Trace(context.Background(), time.Now(), func() (string, int64) { return statement, 0 }, context.DeadlineExceeded)
	if len(recorded.lines) == 0 {
		t.Fatal("neither a slow nor a failed statement was logged")
	}
	for _, line := range recorded.lines {
		if strings.Contains(line, "72.5") {
			t.Errorf("a bound value reached the log: %q", line)
		}
	}
}
