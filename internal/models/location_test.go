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
