package apigraph

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// The switch writes a cron line, and the two halves of it are independent:
// changing the time keeps the days, changing the days keeps the time.
func TestTheBriefsTimeAndDaysAreKeptSeparately(t *testing.T) {
	t.Parallel()

	fresh, err := briefCron("", nil, nil)
	if err != nil || fresh != "30 7 * * 1-5" {
		t.Fatalf("half past seven on weekdays by default: %q %v", fresh, err)
	}

	existing := &models.AgentSchedule{Cron: fresh}
	later, err := briefCron("08:15", nil, existing)
	if err != nil || later != "15 8 * * 1-5" {
		t.Fatalf("a new time keeps the days: %q %v", later, err)
	}
	everyDay, err := briefCron("", []int{1, 2, 3, 4, 5, 6, 7}, &models.AgentSchedule{Cron: later})
	if err != nil || everyDay != "15 8 * * 1,2,3,4,5,6,0" {
		// Sunday is the seventh day to a person and the zeroth to cron.
		t.Fatalf("new days keep the time: %q %v", everyDay, err)
	}

	for _, bad := range []string{"25:00", "half past", "8", "08:99"} {
		if _, err := briefCron(bad, nil, nil); err == nil {
			t.Errorf("%q is not a time", bad)
		}
	}
	if _, err := briefCron("", []int{0}, nil); err == nil || !strings.Contains(err.Error(), "Monday") {
		t.Fatalf("and a day is 1 to 7: %v", err)
	}
}
