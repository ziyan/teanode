package models

import (
	"testing"
	"time"
)

func TestAPlaceLabelIsOneOfFiveInPickerOrder(t *testing.T) {
	t.Parallel()

	if len(PlaceLabels) != 5 || PlaceLabels[0] != PlaceLabelHome || PlaceLabels[1] != PlaceLabelWork || PlaceLabels[2] != PlaceLabelSchool ||
		PlaceLabels[3] != PlaceLabelFamily || PlaceLabels[4] != PlaceLabelGym {
		t.Errorf("the labels are %v", PlaceLabels)
	}
	for _, label := range PlaceLabels {
		if !label.IsValid() {
			t.Errorf("%q is not valid", label)
		}
	}
	for _, label := range []PlaceLabel{"", "office", "Home"} {
		if label.IsValid() {
			t.Errorf("%q should not be valid", label)
		}
	}
}

func TestLocationShareLevelsRankFromCurrentToHistory(t *testing.T) {
	t.Parallel()

	if !LocationShareLevelCurrent.IsGrantable() || !LocationShareLevelHistory.IsGrantable() || LocationShareLevel("").IsGrantable() || LocationShareLevel("edit").IsGrantable() {
		t.Error("only current and history can be given")
	}
	if !LocationShareLevelHistory.AtLeast(LocationShareLevelCurrent) || !LocationShareLevelHistory.AtLeast(LocationShareLevelHistory) || LocationShareLevelCurrent.AtLeast(LocationShareLevelHistory) {
		t.Error("history reaches current and not the other way round")
	}
	if LocationShareLevel("").AtLeast(LocationShareLevel("")) || LocationShareLevel("").Rank() != 0 {
		t.Error("no level reaches anything, not even no level")
	}
}

func TestTheSourcesOfPositionsAndVisitsAreClosedLists(t *testing.T) {
	t.Parallel()

	for _, source := range []LocationSource{LocationSourcePhone, LocationSourceBrowser} {
		if !source.IsValid() {
			t.Errorf("%q is not valid", source)
		}
	}
	for _, source := range []VisitSource{VisitSourceVisit, VisitSourceRegion, VisitSourceCheckIn, VisitSourceBrowser} {
		if !source.IsValid() {
			t.Errorf("%q is not valid", source)
		}
	}
	if LocationSource("").IsValid() || LocationSource("watch").IsValid() || VisitSource("").IsValid() || VisitSource("guess").IsValid() {
		t.Error("a source nobody knows is valid")
	}
}

func TestALocationPauseIsInEffectFromItsStartUntilItsEnd(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 3, 4, 14, 0, 0, 0, time.UTC)
	endedAt := startedAt.Add(time.Hour)
	timed := &LocationPause{StartedAt: startedAt, EndedAt: &endedAt}
	open := &LocationPause{StartedAt: startedAt}
	for name, test := range map[string]struct {
		pause   *LocationPause
		instant time.Time
		want    bool
	}{
		"before the start":          {timed, startedAt.Add(-time.Second), false},
		"at the start":              {timed, startedAt, true},
		"inside":                    {timed, startedAt.Add(30 * time.Minute), true},
		"at the end":                {timed, endedAt, false},
		"after the end":             {timed, endedAt.Add(time.Second), false},
		"open one before the start": {open, startedAt.Add(-time.Second), false},
		"open one a year later":     {open, startedAt.AddDate(1, 0, 0), true},
	} {
		if got := test.pause.IsInEffectAt(test.instant); got != test.want {
			t.Errorf("%s: in effect is %v, want %v", name, got, test.want)
		}
	}
}

// The limit a phone and a pause put on a person's location: a pause or a
// switch off shares nothing, While Using shares no visits, an app that never
// reported is not limited.
func TestALocationSharingLimitComesFromThePhoneAndThePause(t *testing.T) {
	always := &LocationPermission{PhonePermission: LocationPhonePermissionAlways, IsRecordingOn: true}
	whileUsing := &LocationPermission{PhonePermission: LocationPhonePermissionWhileUsing, IsRecordingOn: true}
	off := &LocationPermission{PhonePermission: LocationPhonePermissionOff, IsRecordingOn: true}
	switchedOff := &LocationPermission{PhonePermission: LocationPhonePermissionAlways}
	for _, each := range []struct {
		permission *LocationPermission
		pause      *LocationPause
		want       LocationSharingLimit
	}{
		{nil, nil, LocationSharingLimit{ShareLevelLimit: LocationShareLevelHistory}},
		{always, nil, LocationSharingLimit{ShareLevelLimit: LocationShareLevelHistory}},
		{whileUsing, nil, LocationSharingLimit{ShareLevelLimit: LocationShareLevelCurrent, SharingLimitReason: LocationSharingLimitReasonPhoneWhileUsing}},
		{off, nil, LocationSharingLimit{SharingLimitReason: LocationSharingLimitReasonPhoneLocationOff}},
		{switchedOff, nil, LocationSharingLimit{SharingLimitReason: LocationSharingLimitReasonRecordingOff}},
		{always, &LocationPause{}, LocationSharingLimit{SharingLimitReason: LocationSharingLimitReasonPaused}},
		{nil, &LocationPause{}, LocationSharingLimit{SharingLimitReason: LocationSharingLimitReasonPaused}},
	} {
		if got := LocationSharingLimitOf(each.permission, each.pause); got != each.want {
			t.Errorf("%+v paused %v: %+v, want %+v", each.permission, each.pause != nil, got, each.want)
		}
	}
	if LessLocationShareLevel(LocationShareLevelHistory, LocationShareLevelCurrent) != LocationShareLevelCurrent ||
		LessLocationShareLevel(LocationShareLevelCurrent, "") != "" {
		t.Error("the lower of two levels")
	}
}

// What a person is told: a limit of their own phone or pause in its own
// words, and a share they did not make back only when that is why.
func TestTheWordsSayWhyLessIsSeen(t *testing.T) {
	for _, reason := range []LocationSharingLimitReason{LocationSharingLimitReasonPaused, LocationSharingLimitReasonRecordingOff, LocationSharingLimitReasonPhoneLocationOff, LocationSharingLimitReasonPhoneWhileUsing} {
		if reason.Words() == "" {
			t.Errorf("%q has no words", reason)
		}
	}
	if LocationSharingLimitReasonNone.Words() != "" {
		t.Error("no limit, no words")
	}
	history, current := LocationShareLevelHistory, LocationShareLevelCurrent
	if words := LocationShareBackWords("Sam", "", history, history); words == "" {
		t.Error("nothing shared back")
	}
	if words := LocationShareBackWords("Sam", current, history, history); words == "" {
		t.Error("only where they are shared back")
	}
	if words := LocationShareBackWords("Sam", current, history, current); words != "" {
		t.Errorf("the phone is why, not the shares: %q", words)
	}
	if words := LocationShareBackWords("Sam", current, current, history); words != "" {
		t.Errorf("Sam shares only where they are: %q", words)
	}
}
