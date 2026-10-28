package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// An instance holds its name while it runs: a second one under the same name
// waits for it, starts once it has stopped, and is refused while it runs on.
// Another name is no obstacle.
func TestASecondInstanceUnderOneNameIsRefused(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	previousWait, previousPoll := instanceNameWait, instanceNamePoll
	instanceNameWait, instanceNamePoll = 300*time.Millisecond, 50*time.Millisecond
	defer func() { instanceNameWait, instanceNamePoll = previousWait, previousPoll }()

	ctx := context.Background()
	release, err := holdInstanceName(ctx, database, "teanode-1")
	if err != nil {
		t.Fatalf("the first instance did not get its name: %s", err)
	}
	if _, err := holdInstanceName(ctx, database, "teanode-1"); err == nil || !strings.Contains(err.Error(), "INSTANCE_ID") ||
		!strings.Contains(err.Error(), "a session from") {
		t.Fatalf("a second instance under the same name was not refused, saying who holds it: %v", err)
	}
	other, err := holdInstanceName(ctx, database, "teanode-2")
	if err != nil {
		t.Fatalf("another name was refused: %s", err)
	}
	other()

	time.AfterFunc(100*time.Millisecond, release)
	again, err := holdInstanceName(ctx, database, "teanode-1")
	if err != nil {
		t.Fatalf("the name was not free once the first instance stopped: %s", err)
	}
	again()
}
