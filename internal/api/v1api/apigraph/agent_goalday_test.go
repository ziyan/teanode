package apigraph

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// The day a goal's turns are counted in is the person's, not the
// server's.
//
// The cap the worker enforces counts from midnight in the zone of the
// person whose agent it is (internal/agent/goal.go). The drawer counted
// from the server's zone whenever the conversation was the caller's own,
// because that is the case where the resolver has no separate owner to
// read a zone from -- so for anybody not living where the server does the
// two were on different days for part of every day, and the drawer said
// the agent had turns left that it did not, or the other way round.
func TestAGoalsDayIsCountedInThePersonsZone(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 14, 3, 0, 0, 0, time.UTC)
	serverMidnight := goalDayBegan(now, nil, nil)

	// A zone the server is not in, whichever one it is in: otherwise the
	// two midnights are the same instant and the test proves nothing.
	var zone *time.Location
	var personMidnight time.Time
	for _, name := range []string{"America/Los_Angeles", "Europe/Berlin", "Pacific/Kiritimati"} {
		found, err := time.LoadLocation(name)
		if err != nil {
			t.Skipf("no zone database here: %s", err)
		}
		local := now.In(found)
		midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, found)
		if !midnight.Equal(serverMidnight) {
			zone, personMidnight = found, midnight
			break
		}
	}
	if zone == nil {
		t.Skipf("every zone tried has the server's midnight, which cannot happen")
	}

	caller := &models.User{Username: "ada", Timezone: zone.String()}
	if began := goalDayBegan(now, caller, nil); !began.Equal(personMidnight) {
		t.Errorf("their own conversation counts from %s, want their own midnight %s (the server's is %s)",
			began, personMidnight, serverMidnight)
	}

	// An operator reading somebody else's conversation counts in that
	// person's zone, which is what the resolver already did and what must
	// not change: the owner wins over the caller.
	owner := &models.User{Username: "bertie", Timezone: "UTC"}
	if began := goalDayBegan(now, caller, owner); !began.Equal(time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("somebody else's conversation counts in their zone, not the reader's: %s", began)
	}

	// A person who has never said where they are leaves only the server's
	// zone to go on.
	if began := goalDayBegan(now, &models.User{Username: "carol"}, nil); !began.Equal(serverMidnight) {
		t.Errorf("with no zone to go on it counts from %s, want %s", began, serverMidnight)
	}
}
