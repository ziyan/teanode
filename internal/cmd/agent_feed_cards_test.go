package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// A card with a chart, a camera still, a resolution and notes is printed as
// words a terminal can read: the chart's numbers with where they were read,
// the still's camera and time, and when and by whom it was resolved.
func TestACardWithChartsAStillAndAResolutionIsPrinted(t *testing.T) {
	value := 1200.5
	resolvedAt := time.Date(2026, 11, 19, 6, 41, 0, 0, time.UTC)
	item := &client.AgentFeedItem{
		FeedItemID: "card-1", Title: "Money this week", FeedItemKind: "report", FeedSourceKind: "agent", PostedAt: resolvedAt,
		ChartBlocks: []client.AgentFeedChartBlock{
			{
				ChartKind: "series", ChartTitle: "Balance", Unit: "USD", Provenance: "Read from the bank at 09:40 EST", XLabels: []string{"Mon", "Tue"},
				Series:  []client.AgentFeedChartSeries{{SeriesName: "This week", SeriesRole: "measured", Values: []*float64{&value, nil}}},
				Markers: []client.AgentFeedChartMarker{{MarkerAt: "Tue", MarkerLabel: "Rent paid"}},
			},
			{ChartKind: "bars", ChartTitle: "Shops", Provenance: "Read at 18:05 PST", Bars: []client.AgentFeedChartBar{{BarLabel: "Market", BarValue: 80.25}}},
		},
		FeedMedia:  []client.AgentFeedMedia{{FeedMediaKind: "camera_still", ImageURL: "/api/v1/feed/items/card-1/media/m1", CameraName: "Front door", CapturedAt: "2026-11-19T14:02:11-05:00", StillReason: "Where the parcel was left"}},
		IsResolved: true, ResolvedAt: &resolvedAt, ResolvedByKind: "goal", ResolutionNote: "It arrived",
		PostNotes: []string{"factRows[0].factLabel was cut to 80 characters"},
	}
	var written bytes.Buffer
	if err := printFeedItem(&cli.Command{Writer: &written}, item); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"chart, series: Balance (USD)", "This week (measured): Mon=1200.5 Tue=none", "marker at Tue: Rent paid", "Read from the bank at 09:40 EST",
		"chart, bars: Shops", "Market: 80.25", "camera still: /api/v1/feed/items/card-1/media/m1, Front door at 2026-11-19T14:02:11-05:00: Where the parcel was left",
		"by goal: It arrived", "note: factRows[0].factLabel was cut to 80 characters",
	} {
		if !strings.Contains(written.String(), expected) {
			t.Errorf("the output does not say %q:\n%s", expected, written.String())
		}
	}
}

// The post command takes charts and camera stills as JSON and refuses what is
// not a list.
func TestThePostCommandHasTheChartAndStillFlagsAndResolveTakesANote(t *testing.T) {
	commands := map[string]*cli.Command{}
	for _, subcommand := range newAgentFeedCommand().Commands {
		commands[subcommand.Name] = subcommand
	}
	flags := map[string]bool{}
	for _, flag := range commands["post"].Flags {
		for _, name := range flag.Names() {
			flags[name] = true
		}
	}
	if !flags["chart-blocks"] || !flags["camera-still"] {
		t.Errorf("the post flags are %v", flags)
	}
	hasNote := false
	for _, flag := range commands["resolve"].Flags {
		for _, name := range flag.Names() {
			hasNote = hasNote || name == "note"
		}
	}
	if !hasNote || commands["unresolve"] == nil {
		t.Error("resolve has no --note or there is no unresolve")
	}
}
