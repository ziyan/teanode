package apigraph

import (
	"fmt"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// Calendars in one list are painted in different colors while there are
// colors left, whatever their identifiers hash to, and the same list always
// gives the same colors, the person's own first.
func TestCalendarsInAListGetDifferentColors(t *testing.T) {
	build := func(identifiers ...string) []*CalendarView {
		views := []*CalendarView{}
		for _, identifier := range identifiers {
			shareLevel := models.ShareLevelDetails
			if identifier == "own" {
				shareLevel = models.ShareLevelOwner
			}
			views = append(views, &CalendarView{ID: identifier, ShareLevel: string(shareLevel), ColorIndex: calendarColorIndex(identifier)})
		}
		return views
	}
	colorsOf := func(views []*CalendarView) map[string]int {
		colors := map[string]int{}
		for _, view := range views {
			colors[view.ID] = view.ColorIndex
		}
		return colors
	}

	identifiers := []string{"own"}
	for index := 0; index < 7; index++ {
		identifiers = append(identifiers, fmt.Sprintf("shared-%d", index))
	}
	views := build(identifiers...)
	spreadCalendarColors(views)
	used := map[int]string{}
	for _, view := range views {
		if view.ColorIndex < 0 || view.ColorIndex >= calendarColorCount {
			t.Fatalf("%s has color %d, which is not one of the %d", view.ID, view.ColorIndex, calendarColorCount)
		}
		if other, taken := used[view.ColorIndex]; taken {
			t.Errorf("%s and %s are both color %d with colors to spare", view.ID, other, view.ColorIndex)
		}
		used[view.ColorIndex] = view.ID
	}

	// The person's own calendar keeps the color it hashes to, wherever it
	// stands in the list, and the order of the list changes nothing.
	if colorsOf(views)["own"] != calendarColorIndex("own") {
		t.Errorf("the person's own calendar lost its own color")
	}
	reversed := build(identifiers...)
	for first, second := 0, len(reversed)-1; first < second; first, second = first+1, second-1 {
		reversed[first], reversed[second] = reversed[second], reversed[first]
	}
	spreadCalendarColors(reversed)
	for identifier, colorIndex := range colorsOf(views) {
		if colorsOf(reversed)[identifier] != colorIndex {
			t.Errorf("%s is color %d in one order and %d in the other", identifier, colorIndex, colorsOf(reversed)[identifier])
		}
	}

	// More calendars than colors: nobody is left without one.
	many := []string{}
	for index := 0; index < 12; index++ {
		many = append(many, fmt.Sprintf("many-%02d", index))
	}
	crowded := build(many...)
	spreadCalendarColors(crowded)
	for _, view := range crowded {
		if view.ColorIndex < 0 || view.ColorIndex >= calendarColorCount {
			t.Errorf("%s has color %d", view.ID, view.ColorIndex)
		}
	}
}
