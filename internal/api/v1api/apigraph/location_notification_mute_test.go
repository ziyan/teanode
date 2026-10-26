package apigraph

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// A person mutes the arrivals and departures of somebody who shares their
// location with them, sees the mute on the people list and on the person, and
// unmutes. Muting again changes the end.
func TestAPersonMutesAndUnmutesASharersNotifications(t *testing.T) {
	world := newLocationWorld(t)
	world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "current") { id } }`, world.ben.ID))

	mutedUntil := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	muted := world.call(t, world.ben, fmt.Sprintf(`mutation { MuteLocationNotifications(userId: %q, mutedUntil: %q) { mutedUntil } }`, world.ada.ID, mutedUntil.Format(time.RFC3339)))["MuteLocationNotifications"].(map[string]any)
	if got, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(muted["mutedUntil"])); !got.Equal(mutedUntil) {
		t.Errorf("the mute ends at %v, want %s", muted["mutedUntil"], mutedUntil)
	}
	people := world.call(t, world.ben, `query { ListSharedLocations { userId notificationMute { mutedUntil } } }`)["ListSharedLocations"].([]any)
	if len(people) != 1 || people[0].(map[string]any)["notificationMute"] == nil {
		t.Errorf("the people list does not show the mute: %v", people)
	}
	seen := world.call(t, world.ben, fmt.Sprintf(`query { GetLocation(userId: %q) { notificationMute { mutedUntil } } }`, world.ada.ID))["GetLocation"].(map[string]any)
	if seen["notificationMute"] == nil {
		t.Errorf("the person does not show the mute: %v", seen)
	}

	// Muting again, with no end, mutes until unmuted.
	again := world.call(t, world.ben, fmt.Sprintf(`mutation { MuteLocationNotifications(userId: %q) { mutedUntil } }`, world.ada.ID))["MuteLocationNotifications"].(map[string]any)
	if again["mutedUntil"] != nil {
		t.Errorf("a mute with no end ends at %v", again["mutedUntil"])
	}

	// The sharer sees nothing of it on their own location.
	own := world.call(t, world.ada, `query { GetLocation { notificationMute { mutedUntil } } }`)["GetLocation"].(map[string]any)
	if own["notificationMute"] != nil {
		t.Errorf("the sharer's own location shows a mute: %v", own)
	}

	if unmuted := world.call(t, world.ben, fmt.Sprintf(`mutation { UnmuteLocationNotifications(userId: %q) }`, world.ada.ID))["UnmuteLocationNotifications"]; unmuted != true {
		t.Errorf("unmuting: %v", unmuted)
	}
	if unmuted := world.call(t, world.ben, fmt.Sprintf(`mutation { UnmuteLocationNotifications(userId: %q) }`, world.ada.ID))["UnmuteLocationNotifications"]; unmuted != false {
		t.Errorf("unmuting when not muted: %v", unmuted)
	}
	people = world.call(t, world.ben, `query { ListSharedLocations { notificationMute { mutedUntil } } }`)["ListSharedLocations"].([]any)
	if people[0].(map[string]any)["notificationMute"] != nil {
		t.Errorf("the mute is still shown after unmuting: %v", people)
	}
}

// Only somebody who shares their location with the caller can be muted, not
// the caller themselves, and a mute ends after now and within 30 days.
func TestAMuteNeedsASharerAndAnEndAheadWithinThirtyDays(t *testing.T) {
	world := newLocationWorld(t)
	world.refused(t, world.ben, fmt.Sprintf(`mutation { MuteLocationNotifications(userId: %q) { mutedUntil } }`, world.ada.ID))
	world.refused(t, world.ben, fmt.Sprintf(`mutation { MuteLocationNotifications(userId: %q) { mutedUntil } }`, world.ben.ID))

	world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "current") { id } }`, world.ben.ID))
	for name, mutedUntil := range map[string]time.Time{
		"in the past":          time.Now().Add(-time.Minute),
		"more than 30 days on": time.Now().Add(31 * 24 * time.Hour),
	} {
		refusal := world.refused(t, world.ben, fmt.Sprintf(`mutation { MuteLocationNotifications(userId: %q, mutedUntil: %q) { mutedUntil } }`, world.ada.ID, mutedUntil.UTC().Format(time.RFC3339)))
		if !strings.Contains(refusal, "mute") {
			t.Errorf("%s: the refusal does not say why: %q", name, refusal)
		}
	}
}
