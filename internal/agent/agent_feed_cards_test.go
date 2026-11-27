package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// resolveFeed runs the rules of resolving in a transaction of the world's.
func (self *speakFirstWorld) resolveFeed(t *testing.T, resolver *agent.FeedResolver, feedItemId, note string) (*models.AgentFeedItem, error) {
	t.Helper()
	var resolved *models.AgentFeedItem
	var resolveErr error
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		item, err := tx.GetAgentFeedItem(feedItemId)
		if err != nil || item == nil {
			t.Fatalf("the card is not there: %v", err)
		}
		resolved, resolveErr = agent.ResolveFeedItem(tx, resolver, item, note, time.Now())
	})
	return resolved, resolveErr
}

func (self *speakFirstWorld) unresolveFeed(t *testing.T, resolver *agent.FeedResolver, feedItemId string) (*models.AgentFeedItem, error) {
	t.Helper()
	var unresolved *models.AgentFeedItem
	var unresolveErr error
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		item, err := tx.GetAgentFeedItem(feedItemId)
		if err != nil || item == nil {
			t.Fatalf("the card is not there: %v", err)
		}
		unresolved, unresolveErr = agent.UnresolveFeedItem(tx, resolver, item, time.Now())
	})
	return unresolved, unresolveErr
}

// A resolved card stays where it is and unread, says who resolved it and
// what cleared, and the first resolution stands: a second is refused and its
// note does not replace the first.
func TestAResolvedCardKeepsItsPlaceAndTheFirstResolutionStands(t *testing.T) {
	world := newSpeakFirstWorld(t)
	world.post(t, &models.AgentFeedItem{Title: "The freezer is warm", Body: "It read 9 degrees at breakfast.", FeedItemKind: models.FeedItemAlert})
	var card *models.AgentFeedItem
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		items, _, err := tx.ListAgentFeedItems(world.owner.ID, nil, 10, false, time.Now())
		if err != nil || len(items) != 1 {
			t.Fatalf("the feed: %v %v", items, err)
		}
		card = items[0]
	})
	agentResolver := &agent.FeedResolver{Kind: models.FeedResolverAgent}

	resolved, err := world.resolveFeed(t, agentResolver, card.ID, "It is back to -18 degrees")
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.IsResolved() || resolved.ResolutionNote != "It is back to -18 degrees" || resolved.ResolvedByKind != models.FeedResolverAgent {
		t.Fatalf("the card is not resolved as asked: %+v", resolved)
	}
	if resolved.ReadAt != nil || !resolved.CreatedAt.Equal(card.CreatedAt) || resolved.DismissedAt != nil {
		t.Fatalf("resolving moved the card or changed whether it was read: %+v", resolved)
	}
	if _, err := world.resolveFeed(t, agentResolver, card.ID, "A second note"); err == nil || !strings.Contains(err.Error(), "already resolved") {
		t.Fatalf("a second resolve: %v", err)
	}
	if stored := readFeedItem(t, world, card.ID); stored.ResolutionNote != "It is back to -18 degrees" {
		t.Fatalf("the first note did not stand: %q", stored.ResolutionNote)
	}
	unresolved, err := world.unresolveFeed(t, agentResolver, card.ID)
	if err != nil || unresolved.IsResolved() || unresolved.ResolutionNote != "" || unresolved.ResolvedByKind != "" {
		t.Fatalf("unresolve: %+v %v", unresolved, err)
	}
	if _, err := world.unresolveFeed(t, agentResolver, card.ID); err == nil || !strings.Contains(err.Error(), "not resolved") {
		t.Fatalf("unresolving a card that is not resolved: %v", err)
	}
}

// A goal resolves any card, the card saying which goal did, and the first
// resolution stands until somebody takes it off, a goal included.
func TestAGoalResolvesAnyCardAndTheFirstResolutionStands(t *testing.T) {
	world := newSpeakFirstWorld(t)
	goalCard := world.post(t, &models.AgentFeedItem{Title: "A parcel is due", FeedSourceKind: models.FeedSourceGoal, FeedSourceID: "goal-parcels"})
	otherCard := world.post(t, &models.AgentFeedItem{Title: "A different goal's card", FeedSourceKind: models.FeedSourceGoal, FeedSourceID: "goal-weather"})
	goal := &agent.FeedResolver{Kind: models.FeedResolverGoal, ID: "goal-parcels"}

	for _, feedItemId := range []string{goalCard.ID, otherCard.ID} {
		resolved, err := world.resolveFeed(t, goal, feedItemId, "The parcel arrived")
		if err != nil || resolved.ResolvedByKind != models.FeedResolverGoal || resolved.ResolvedByID != "goal-parcels" {
			t.Fatalf("a goal resolving a card: %+v %v", resolved, err)
		}
	}

	// The person's resolution stands until it is taken off, which a goal may do.
	person := &agent.FeedResolver{Kind: models.FeedResolverPerson, ID: world.owner.ID}
	if _, err := world.unresolveFeed(t, person, goalCard.ID); err != nil {
		t.Fatal(err)
	}
	resolved, err := world.resolveFeed(t, person, goalCard.ID, "Handled it myself")
	if err != nil || resolved.ResolvedByKind != models.FeedResolverPerson || resolved.ResolvedByID != world.owner.ID {
		t.Fatalf("the person resolving: %+v %v", resolved, err)
	}
	if _, err := world.resolveFeed(t, goal, goalCard.ID, "The parcel arrived"); err == nil || !strings.Contains(err.Error(), "already resolved") {
		t.Fatalf("a goal resolved over the person: %v", err)
	}
	if stored := readFeedItem(t, world, goalCard.ID); stored.ResolutionNote != "Handled it myself" {
		t.Fatalf("the person's resolution did not stand: %+v", stored)
	}
	if _, err := world.unresolveFeed(t, goal, goalCard.ID); err != nil {
		t.Fatalf("a goal taking off the person's resolution: %v", err)
	}
}

// Posting the same subject again is a new state of it and clears the
// resolution; a change of the text keeps it.
func TestRepostingASubjectClearsTheResolutionAndAnUpdateKeepsIt(t *testing.T) {
	world := newSpeakFirstWorld(t)
	card := world.post(t, &models.AgentFeedItem{Title: "The freezer is warm", CoalesceKey: "freezer"})
	if _, err := world.resolveFeed(t, &agent.FeedResolver{Kind: models.FeedResolverAgent}, card.ID, "Cooled down"); err != nil {
		t.Fatal(err)
	}
	updated, err := world.worker.UpdateFeedItem(context.Background(), world.agent.ID, card.ID, "", func(item *models.AgentFeedItem) error {
		item.Body = "It cooled down within the hour."
		return nil
	})
	if err != nil || !updated.IsResolved() || updated.ResolutionNote != "Cooled down" || updated.Body != "It cooled down within the hour." {
		t.Fatalf("an update that changes the text lost the resolution: %+v %v", updated, err)
	}
	reposted := world.post(t, &models.AgentFeedItem{Title: "The freezer is warm again", CoalesceKey: "freezer"})
	if reposted.ID != card.ID || reposted.IsResolved() || reposted.ResolutionNote != "" || reposted.ResolvedByKind != "" {
		t.Fatalf("a repost of the subject kept the resolution: %+v", reposted)
	}
}

// The note is one line of at most 160 characters; a longer one is cut and the
// answer says so.
func TestAResolutionNoteIsOneLineAndCutWithANote(t *testing.T) {
	world := newSpeakFirstWorld(t)
	card := world.post(t, &models.AgentFeedItem{Title: "A leak"})
	resolved, err := world.resolveFeed(t, &agent.FeedResolver{Kind: models.FeedResolverAgent}, card.ID, "The leak\nwas fixed "+strings.Repeat("and checked ", 30))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(resolved.ResolutionNote, "\n") || len([]rune(resolved.ResolutionNote)) > 161 || !strings.HasPrefix(resolved.ResolutionNote, "The leak was fixed") {
		t.Fatalf("the note is %q", resolved.ResolutionNote)
	}
	if len(resolved.PostNotes) != 1 || !strings.Contains(resolved.PostNotes[0], "resolutionNote was cut to 160") {
		t.Fatalf("the answer does not say what was cut: %v", resolved.PostNotes)
	}
}

func seriesChart() models.FeedChartBlock {
	return models.FeedChartBlock{
		ChartKind: models.FeedChartKindSeries, ChartTitle: "Checking balance", Unit: "USD", Provenance: "Read from the bank at 09:40 EST",
		XLabels: []string{"Mon", "Tue", "Wed", "Thu"},
		Series: []models.FeedChartSeries{
			{SeriesName: "This week", Values: []*float64{floatPointer(1200), floatPointer(1100.5), nil, floatPointer(900)}},
			{SeriesName: "Last week", SeriesRole: models.FeedSeriesReference, Values: []*float64{floatPointer(1300), floatPointer(1250), floatPointer(1200), floatPointer(1180)}},
		},
		Markers: []models.FeedChartMarker{{MarkerAt: "Tue", MarkerLabel: "Rent paid"}},
	}
}

func floatPointer(value float64) *float64 { return &value }

// A chart is stored as it was given, with its role defaulted and no list left
// nil, and read back the same.
func TestAChartIsStoredAndReadBack(t *testing.T) {
	world := newSpeakFirstWorld(t)
	bars := models.FeedChartBlock{
		ChartKind: models.FeedChartKindBars, ChartTitle: "Spending by shop", Unit: "USD", Provenance: "Read from the card statement at 18:05 PST",
		Bars: []models.FeedChartBar{{BarLabel: "Bakery", BarValue: 12}, {BarLabel: "Market", BarValue: 80.25}, {BarLabel: "Cafe", BarValue: 0}},
	}
	posted := world.post(t, &models.AgentFeedItem{Title: "Money this week", ChartBlocks: []models.FeedChartBlock{seriesChart(), bars}})
	stored := readFeedItem(t, world, posted.ID)
	if len(stored.ChartBlocks) != 2 {
		t.Fatalf("the card has %d charts", len(stored.ChartBlocks))
	}
	series := stored.ChartBlocks[0]
	if series.Series[0].SeriesRole != models.FeedSeriesMeasured || series.Series[1].SeriesRole != models.FeedSeriesReference || series.Series[0].Values[2] != nil || *series.Series[0].Values[1] != 1100.5 {
		t.Errorf("the series came back as %+v", series.Series)
	}
	if len(series.Markers) != 1 || series.Bars == nil || len(series.Bars) != 0 {
		t.Errorf("the lists of the series chart: %+v", series)
	}
	if got := stored.ChartBlocks[1]; got.Bars[0].BarLabel != "Market" || got.Bars[2].BarLabel != "Cafe" || got.XLabels == nil || got.Series == nil || got.Markers == nil {
		t.Errorf("the bars came back as %+v, expected largest first and empty lists", got)
	}
	plain := world.post(t, &models.AgentFeedItem{Title: "No chart"})
	if plain.ChartBlocks == nil || len(plain.ChartBlocks) != 0 {
		t.Errorf("a card with no chart has %+v", plain.ChartBlocks)
	}
}

// Every chart that cannot be drawn is refused with the field named.
func TestAChartThatCannotBeDrawnIsRefusedWithTheFieldNamed(t *testing.T) {
	world := newSpeakFirstWorld(t)
	change := func(modify func(chart *models.FeedChartBlock)) []models.FeedChartBlock {
		chart := seriesChart()
		modify(&chart)
		return []models.FeedChartBlock{chart}
	}
	manyLabels := make([]string, 61)
	for index := range manyLabels {
		manyLabels[index] = string(rune('a'+index%26)) + string(rune('a'+index/26))
	}
	cases := map[string]struct {
		charts   []models.FeedChartBlock
		expected string
	}{
		"three charts":  {[]models.FeedChartBlock{seriesChart(), seriesChart(), seriesChart()}, "chartBlocks has 3 charts"},
		"no provenance": {change(func(chart *models.FeedChartBlock) { chart.Provenance = "  " }), "chartBlocks[0].provenance is missing"},
		"unknown kind":  {change(func(chart *models.FeedChartBlock) { chart.ChartKind = "pie" }), "chartBlocks[0].chartKind is \"pie\""},
		"one label":     {change(func(chart *models.FeedChartBlock) { chart.XLabels = []string{"Mon"} }), "chartBlocks[0].xLabels has 1 labels"},
		"sixty one labels": {change(func(chart *models.FeedChartBlock) {
			chart.XLabels = manyLabels
			chart.Markers = nil
		}), "chartBlocks[0].xLabels has 61 labels"},
		"no series": {change(func(chart *models.FeedChartBlock) { chart.Series = nil }), "chartBlocks[0].series has 0 lines"},
		"four series": {change(func(chart *models.FeedChartBlock) {
			chart.Series = append(chart.Series, chart.Series[0], chart.Series[0])
		}), "chartBlocks[0].series has 4 lines"},
		"short values": {change(func(chart *models.FeedChartBlock) { chart.Series[1].Values = chart.Series[1].Values[:3] }), "chartBlocks[0].series[1].values has 3 entries"},
		"empty values": {change(func(chart *models.FeedChartBlock) { chart.Series[0].Values = []*float64{nil, nil, nil, nil} }), "chartBlocks[0].series[0].values has no number"},
		"bad role":     {change(func(chart *models.FeedChartBlock) { chart.Series[0].SeriesRole = "forecast" }), "chartBlocks[0].series[0].seriesRole is \"forecast\""},
		"marker off the axis": {change(func(chart *models.FeedChartBlock) {
			chart.Markers[0].MarkerAt = "Sunday"
		}), "chartBlocks[0].markers[0].markerAt is \"Sunday\", which is not one of the xLabels"},
		"four markers": {change(func(chart *models.FeedChartBlock) {
			chart.Markers = []models.FeedChartMarker{{MarkerAt: "Mon"}, {MarkerAt: "Tue"}, {MarkerAt: "Wed"}, {MarkerAt: "Thu"}}
		}), "chartBlocks[0].markers has 4 markers"},
		"bars on a series": {change(func(chart *models.FeedChartBlock) { chart.Bars = []models.FeedChartBar{{BarLabel: "A", BarValue: 1}} }), "chartBlocks[0].bars is for a bars chart"},
		"negative bar":     {[]models.FeedChartBlock{{ChartKind: models.FeedChartKindBars, Provenance: "Read at 09:40", Bars: []models.FeedChartBar{{BarLabel: "A", BarValue: 3}, {BarLabel: "B", BarValue: -1}}}}, "bars[1].barValue is -1"},
		"huge value":       {change(func(chart *models.FeedChartBlock) { chart.Series[1].Values[2] = floatPointer(-2e15) }), "chartBlocks[0].series[1].values[2] is -2e+15"},
		"huge bar":         {[]models.FeedChartBlock{{ChartKind: models.FeedChartKindBars, Provenance: "Read at 09:40", Bars: []models.FeedChartBar{{BarLabel: "A", BarValue: 3e15}}}}, "chartBlocks[0].bars[0].barValue is 3e+15"},
		"repeated label":   {change(func(chart *models.FeedChartBlock) { chart.XLabels = []string{"Mon", "Tue", "Tue", "Thu"} }), "chartBlocks[0].xLabels[2] is \"Tue\""},
		"no bars":          {[]models.FeedChartBlock{{ChartKind: models.FeedChartKindBars, Provenance: "Read at 09:40"}}, "chartBlocks[0].bars has 0 bars"},
		"nine bars":        {[]models.FeedChartBlock{{ChartKind: models.FeedChartKindBars, Provenance: "Read at 09:40", Bars: make([]models.FeedChartBar, 9)}}, "chartBlocks[0].bars has 9 bars"},
		"axis on bars":     {[]models.FeedChartBlock{{ChartKind: models.FeedChartKindBars, Provenance: "Read at 09:40", XLabels: []string{"a", "b"}, Bars: []models.FeedChartBar{{BarLabel: "A", BarValue: 3}}}}, "chartBlocks[0].xLabels, series and markers are for a series chart"},
	}
	for name, each := range cases {
		_, _, err := world.worker.PostFeedItem(context.Background(), &agent.FeedPost{AgentID: world.agent.ID, Item: &models.AgentFeedItem{Title: "Chart " + name, ChartBlocks: each.charts}})
		if err == nil || !strings.Contains(err.Error(), each.expected) {
			t.Errorf("%s: refused with %v, expected it to say %q", name, err, each.expected)
		}
	}
}

// Three or more fact rows that are each a time or a date and a number are a
// series, and are refused with a pointer to the chart; rows that are not are
// kept.
func TestFactRowsThatAreASeriesAreRefused(t *testing.T) {
	world := newSpeakFirstWorld(t)
	rows := func(pairs ...string) []models.FeedFactRow {
		var result []models.FeedFactRow
		for index := 0; index < len(pairs); index += 2 {
			result = append(result, models.FeedFactRow{FactLabel: pairs[index], FactValue: pairs[index+1]})
		}
		return result
	}
	for name, series := range map[string][]models.FeedFactRow{
		"days":    rows("Mon", "$1,200.50", "Tue", "$1,100", "Wed", "$900"),
		"dates":   rows("2026-11-17", "12,400 steps", "2026-11-18", "9,800 steps", "2026-11-19", "10,100 steps", "Total", "32,300"),
		"months":  rows("Sep 2026", "4.5%", "Oct 2026", "4.7%", "Nov 2026", "4.6%"),
		"hours":   rows("08:00", "61", "09:00", "64", "10:00", "70"),
		"written": rows("Mon Nov 17", "-12", "Tue Nov 18", "+8.5 USD", "Wed Nov 19", "3"),
	} {
		_, _, err := world.worker.PostFeedItem(context.Background(), &agent.FeedPost{AgentID: world.agent.ID, Item: &models.AgentFeedItem{Title: "Rows " + name, FactRows: series}})
		if err == nil || !strings.Contains(err.Error(), "fact_rows rows") || !strings.Contains(err.Error(), "chart_blocks") {
			t.Errorf("%s: refused with %v, expected a pointer to chart_blocks", name, err)
		}
	}
	for name, kept := range map[string][]models.FeedFactRow{
		"two dates":        rows("Mon", "$1,200", "Tue", "$1,100", "Owner", "Robin"),
		"names and prices": rows("Coffee", "$4", "Bread", "$3", "Milk", "$2"),
		"dates and words":  rows("Mon", "closed", "Tue", "open", "Wed", "open"),
	} {
		if _, _, err := world.worker.PostFeedItem(context.Background(), &agent.FeedPost{AgentID: world.agent.ID, Item: &models.AgentFeedItem{Title: "Rows " + name, FactRows: kept}}); err != nil {
			t.Errorf("%s: refused with %v", name, err)
		}
	}
}

// A spaced dash becomes a comma and an unspaced one a hyphen, in a card's
// title, body, fact rows, choice labels, chart text and layout text.
func TestDashesAreRewrittenInEveryText(t *testing.T) {
	world := newSpeakFirstWorld(t)
	chart := seriesChart()
	chart.ChartTitle = "Balance — this week"
	chart.XLabels = []string{"Mon–Tue", "Wed", "Thu", "Fri"}
	chart.Markers = []models.FeedChartMarker{{MarkerAt: "Mon–Tue", MarkerLabel: "Rent – paid"}}
	posted := world.post(t, &models.AgentFeedItem{
		Title: "Rent — paid", Body: "Paid early — thanks.\n— first\nA range of 5–10 and an aside—here.",
		FactRows:       []models.FeedFactRow{{FactLabel: "Due – date", FactValue: "1–15"}},
		Choices:        []models.FeedChoice{{ChoiceKey: "ok", ChoiceLabel: "Fine — thanks", ChoiceRole: models.FeedChoiceDismiss}},
		ChartBlocks:    []models.FeedChartBlock{chart},
		FeedCardLayout: models.FeedLayoutList, LayoutContent: `{"listRows":[{"rowTitle":"A — B","rowDetail":"C – D","rowMeta":"E—F"}]}`,
	})
	stored := readFeedItem(t, world, posted.ID)
	for name, got := range map[string]string{
		"title": stored.Title, "body": stored.Body, "factLabel": stored.FactRows[0].FactLabel, "factValue": stored.FactRows[0].FactValue,
		"choiceLabel": stored.Choices[0].ChoiceLabel, "chartTitle": stored.ChartBlocks[0].ChartTitle, "xLabel": stored.ChartBlocks[0].XLabels[0],
		"markerLabel": stored.ChartBlocks[0].Markers[0].MarkerLabel, "layout": stored.LayoutContent,
	} {
		if strings.ContainsAny(got, "\u2014\u2013") {
			t.Errorf("%s still has a dash: %q", name, got)
		}
	}
	if stored.Title != "Rent, paid" || stored.Body != "Paid early, thanks.\n- first\nA range of 5-10 and an aside-here." || stored.FactRows[0].FactLabel != "Due, date" || stored.FactRows[0].FactValue != "1-15" {
		t.Errorf("the text is %q %q %+v", stored.Title, stored.Body, stored.FactRows)
	}
	if stored.ChartBlocks[0].ChartTitle != "Balance, this week" || stored.ChartBlocks[0].XLabels[0] != "Mon-Tue" || stored.ChartBlocks[0].Markers[0].MarkerAt != "Mon-Tue" || stored.ChartBlocks[0].Markers[0].MarkerLabel != "Rent, paid" {
		t.Errorf("the chart is %+v", stored.ChartBlocks[0])
	}
	var layout struct {
		ListRows []struct{ RowTitle, RowDetail, RowMeta string } `json:"listRows"`
	}
	if err := json.Unmarshal([]byte(stored.LayoutContent), &layout); err != nil || len(layout.ListRows) != 1 || layout.ListRows[0].RowTitle != "A, B" || layout.ListRows[0].RowDetail != "C, D" || layout.ListRows[0].RowMeta != "E-F" {
		t.Errorf("the layout is %s %v", stored.LayoutContent, err)
	}
	// A dash at the edge of a text goes.
	if edge := world.post(t, &models.AgentFeedItem{Title: "— Edge —"}); edge.Title != "Edge" {
		t.Errorf("the title is %q", edge.Title)
	}
}

// A card's title is kept whole however long; a label or caption a little too
// long is cut with an ellipsis and listed in the post's notes instead of
// refusing the card; a card that is changed does not list again what an
// earlier post cut.
func TestATextThatIsTooLongIsCutAndListedNotRefused(t *testing.T) {
	world := newSpeakFirstWorld(t)
	long := func(characters int) string { return strings.Repeat("w", characters) }
	item := &models.AgentFeedItem{
		Title: long(230), Body: "Short.", FactRows: []models.FeedFactRow{{FactLabel: long(210), FactValue: "ok"}},
		Choices:     []models.FeedChoice{{ChoiceKey: "ok", ChoiceLabel: long(100), ChoiceRole: models.FeedChoiceDismiss}},
		ChartBlocks: []models.FeedChartBlock{{ChartKind: models.FeedChartKindBars, ChartTitle: long(100), Provenance: long(130), Bars: []models.FeedChartBar{{BarLabel: "A", BarValue: 1}}}},
		FeedMedia:   []models.FeedMedia{},
	}
	posted, _, err := world.worker.PostFeedItem(context.Background(), &agent.FeedPost{AgentID: world.agent.ID, Item: item})
	if err != nil {
		t.Fatal(err)
	}
	if posted.Title != long(230) || len([]rune(posted.ChartBlocks[0].Provenance)) != 121 {
		t.Fatalf("the title was not kept whole, or the provenance was not cut: %d %d", len([]rune(posted.Title)), len([]rune(posted.ChartBlocks[0].Provenance)))
	}
	notes := strings.Join(posted.PostNotes, "; ")
	if strings.Contains(strings.Join(posted.PostNotes, "; "), "title was cut") {
		t.Errorf("the notes say the title was cut: %v", posted.PostNotes)
	}
	for _, expected := range []string{"factRows[0].factLabel was cut to 200", "choices[0].choiceLabel was cut to 80", "chartBlocks[0].chartTitle was cut to 80", "chartBlocks[0].provenance was cut to 120"} {
		if !strings.Contains(notes, expected) {
			t.Errorf("the notes %q do not say %q", notes, expected)
		}
	}
	updated, err := world.worker.UpdateFeedItem(context.Background(), world.agent.ID, posted.ID, "", func(item *models.AgentFeedItem) error {
		item.Body = "Still short."
		return nil
	})
	if err != nil || len(updated.PostNotes) != 0 {
		t.Fatalf("a change listed the cuts of the post before it: %v %v", updated.PostNotes, err)
	}
}

// An update keeps every field it leaves out: title, kind, body, choices,
// media, charts and layout.
func TestAnUpdateKeepsEveryFieldItLeavesOut(t *testing.T) {
	world := newSpeakFirstWorld(t)
	attachment := snapshotAttachment(t, world, "image/png", stillPicture(t))
	posted, _, err := world.worker.PostFeedItem(context.Background(), &agent.FeedPost{AgentID: world.agent.ID, Item: &models.AgentFeedItem{
		Title: "Money this week", Body: "Down a little.", PictureSymbol: "chart.line.uptrend.xyaxis", FeedItemKind: models.FeedItemReport, FeedUrgency: models.FeedUrgencyWhenever,
		FactRows:    []models.FeedFactRow{{FactLabel: "Rent", FactValue: "paid"}},
		Choices:     []models.FeedChoice{{ChoiceKey: "ok", ChoiceLabel: "Fine", ChoiceRole: models.FeedChoiceDismiss}},
		ChartBlocks: []models.FeedChartBlock{seriesChart()}, CoalesceKey: "money",
		FeedMedia: []models.FeedMedia{cameraStillEntry(attachment.ID)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := world.worker.UpdateFeedItem(context.Background(), world.agent.ID, posted.ID, "", func(item *models.AgentFeedItem) error {
		item.Body = "Down a little more."
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Body != "Down a little more." || updated.Title != posted.Title || updated.FeedItemKind != models.FeedItemReport || updated.FeedUrgency != models.FeedUrgencyWhenever ||
		updated.PictureSymbol != posted.PictureSymbol || updated.CoalesceKey != "money" || len(updated.FactRows) != 1 || len(updated.Choices) != 1 ||
		len(updated.ChartBlocks) != 1 || len(updated.FeedMedia) != 1 || updated.FeedMedia[0].MediaKey != posted.FeedMedia[0].MediaKey {
		t.Fatalf("the update dropped what it left out: %+v", updated)
	}
}

// stillPicture is a camera's snapshot, a picture of a size worth scaling.
func stillPicture(t *testing.T) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	for row := 0; row < 1080; row++ {
		for column := 0; column < 1920; column++ {
			canvas.Set(column, row, color.RGBA{R: uint8(column), G: uint8(row), B: 40, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

// snapshotAttachment is what a camera skill's picture step leaves in a run: a
// file of the conversation, with its bytes in the store.
func snapshotAttachment(t *testing.T, world *speakFirstWorld, contentType string, content []byte) *models.AgentAttachment {
	t.Helper()
	return snapshotAttachmentInConversation(t, world, "", contentType, content)
}

// snapshotAttachmentInConversation is a picture a tool left in the named
// conversation.
func snapshotAttachmentInConversation(t *testing.T, world *speakFirstWorld, conversationId, contentType string, content []byte) *models.AgentAttachment {
	t.Helper()
	var attachment *models.AgentAttachment
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		var err error
		if attachment, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: world.agent.ID, ConversationID: conversationId, MessageID: "shared", Name: "protect-get-snapshot.png", ContentType: contentType, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
	})
	if err := world.fileStore.PutFile(context.Background(), attachment.ID, content); err != nil {
		t.Fatal(err)
	}
	return attachment
}

func cameraStillEntry(attachmentId string) models.FeedMedia {
	return models.FeedMedia{
		FeedMediaKind: models.FeedMediaCameraStill, AttachmentID: attachmentId, CameraName: "Front door", CapturedAt: "2026-11-19T14:02:11-05:00",
		StillReason: "The camera at the front door, where the parcel was left.", PlaybackURL: "https://nvr.example.com/playback/front-door", LiveURL: "teanode://app/cameras/front-door",
		MediaCaption: "A parcel by the door",
	}
}

// A camera still is read from the file the camera tool left in the run,
// scaled and kept as a page's picture is, shown on the card with the camera's
// own time, and kept by an update only while its camera and moment are the
// same.
func TestACameraStillIsKeptFromARunsFileAndAnUpdateKeepsItOnlyWhenUnchanged(t *testing.T) {
	world := newSpeakFirstWorld(t)
	attachment := snapshotAttachment(t, world, "image/png", stillPicture(t))
	posted, _, err := world.worker.PostFeedItem(context.Background(), &agent.FeedPost{AgentID: world.agent.ID, Item: &models.AgentFeedItem{
		Title: "A parcel was left", FeedMedia: []models.FeedMedia{cameraStillEntry(attachment.ID)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(posted.PostNotes) != 0 || len(posted.FeedMedia) != 1 {
		t.Fatalf("the still was not kept quietly: %v %+v", posted.PostNotes, posted.FeedMedia)
	}
	still := posted.FeedMedia[0]
	if still.FeedMediaKind != models.FeedMediaCameraStill || !still.IsShown() || still.ImageFileID == "" || still.AttachmentID != "" || still.WidthPixels != 1200 || still.HeightPixels != 675 {
		t.Fatalf("the still is %+v", still)
	}
	if still.CapturedAt != "2026-11-19T14:02:11-05:00" || still.CameraName != "Front door" || still.PlaybackURL == "" || still.LiveURL == "" {
		t.Fatalf("the still lost its words: %+v", still)
	}
	content, err := world.fileStore.GetFile(context.Background(), still.ImageFileID)
	if err != nil || len(content) == 0 {
		t.Fatalf("the picture is not in the store: %v", err)
	}
	if stored := readFeedItem(t, world, posted.ID); len(stored.FeedMedia) != 1 || stored.FeedMedia[0].ImageFileID != still.ImageFileID || stored.FeedMedia[0].AttachmentID != "" {
		t.Fatalf("the stored still is %+v", stored.FeedMedia)
	}

	// The same camera and moment, with no picture of its own, keeps the one it has.
	unchanged := cameraStillEntry("")
	unchanged.StillReason = "The camera at the front door."
	updated, err := world.worker.UpdateFeedItem(context.Background(), world.agent.ID, posted.ID, "", func(item *models.AgentFeedItem) error {
		item.FeedMedia = []models.FeedMedia{unchanged}
		return nil
	})
	if err != nil || len(updated.FeedMedia) != 1 || updated.FeedMedia[0].ImageFileID != still.ImageFileID || updated.FeedMedia[0].StillReason != "The camera at the front door." || len(updated.PostNotes) != 0 {
		t.Fatalf("an unchanged still did not keep its picture: %+v %v", updated, err)
	}
	if _, err := world.fileStore.GetFile(context.Background(), still.ImageFileID); err != nil {
		t.Fatalf("the picture of a kept still was deleted: %v", err)
	}

	// A new moment needs a new still: with no picture it is left off, with a note.
	moved := cameraStillEntry("")
	moved.CapturedAt = "2026-11-19T14:07:40-05:00"
	updated, err = world.worker.UpdateFeedItem(context.Background(), world.agent.ID, posted.ID, "", func(item *models.AgentFeedItem) error {
		item.FeedMedia = []models.FeedMedia{moved}
		return nil
	})
	if err != nil || len(updated.FeedMedia) != 0 || len(updated.PostNotes) != 1 || !strings.Contains(updated.PostNotes[0], "Front door") || !strings.Contains(updated.PostNotes[0], "new moment needs a new") {
		t.Fatalf("a still of a new moment with no picture: %+v %v", updated, err)
	}
	if _, err := world.fileStore.GetFile(context.Background(), still.ImageFileID); err == nil {
		t.Fatal("the picture of a still that left the card was kept")
	}
}

// A still whose picture cannot be read is left off with a note in the answer,
// and the card still posts; so are the refusals of its own words.
func TestACameraStillWithNoReadablePictureIsLeftOffAndTheCardPosts(t *testing.T) {
	world := newSpeakFirstWorld(t)
	notAPicture := snapshotAttachment(t, world, "application/pdf", []byte("%PDF-1.4"))
	brokenPicture := snapshotAttachment(t, world, "image/jpeg", []byte("not really a picture"))
	for name, attachmentId := range map[string]string{"missing": "no-such-attachment", "not a picture": notAPicture.ID, "undecodable": brokenPicture.ID, "none": ""} {
		posted, _, err := world.worker.PostFeedItem(context.Background(), &agent.FeedPost{AgentID: world.agent.ID, Item: &models.AgentFeedItem{
			Title: "A parcel was left (" + name + ")", FeedMedia: []models.FeedMedia{cameraStillEntry(attachmentId)},
		}})
		if err != nil {
			t.Fatalf("%s: the card was refused: %s", name, err)
		}
		if len(posted.FeedMedia) != 0 || len(posted.PostNotes) != 1 || !strings.Contains(posted.PostNotes[0], "the still from Front door was left off the card") {
			t.Errorf("%s: media %+v notes %v", name, posted.FeedMedia, posted.PostNotes)
		}
	}
	for name, change := range map[string]func(entry *models.FeedMedia){
		"no camera":         func(entry *models.FeedMedia) { entry.CameraName = "" },
		"no reason":         func(entry *models.FeedMedia) { entry.StillReason = "" },
		"time without zone": func(entry *models.FeedMedia) { entry.CapturedAt = "2026-11-19 14:02:11" },
		"plain link":        func(entry *models.FeedMedia) { entry.PlaybackURL = "http://nvr.example.com/playback" },
		"both pictures":     func(entry *models.FeedMedia) { entry.PhotoID = "photo-1" },
		"a page too":        func(entry *models.FeedMedia) { entry.PageURL = "https://news.example.com/story" },
	} {
		entry := cameraStillEntry("some-attachment")
		change(&entry)
		if _, _, err := world.worker.PostFeedItem(context.Background(), &agent.FeedPost{AgentID: world.agent.ID, Item: &models.AgentFeedItem{Title: "Bad still " + name, FeedMedia: []models.FeedMedia{entry}}}); err == nil || !strings.Contains(err.Error(), "media[0]") {
			t.Errorf("%s: refused with %v, expected the field named", name, err)
		}
	}
}

// A still is read only from a picture of the conversation of the run that
// posts it: an image the agent fetched in another conversation is left off
// with a note, and one of this run's own is kept.
func TestACameraStillFromAnotherConversationIsLeftOff(t *testing.T) {
	world := newSpeakFirstWorld(t)
	posted, _, err := world.worker.PostFeedItem(context.Background(), &agent.FeedPost{AgentID: world.agent.ID, Item: &models.AgentFeedItem{Title: "A parcel was left"}})
	if err != nil {
		t.Fatal(err)
	}
	elsewhere := snapshotAttachmentInConversation(t, world, "another-conversation", "image/png", stillPicture(t))
	here := snapshotAttachmentInConversation(t, world, "this-conversation", "image/png", stillPicture(t))

	updated, err := world.worker.UpdateFeedItem(context.Background(), world.agent.ID, posted.ID, "this-conversation", func(item *models.AgentFeedItem) error {
		item.FeedMedia = []models.FeedMedia{cameraStillEntry(elsewhere.ID)}
		return nil
	})
	if err != nil || len(updated.FeedMedia) != 0 || len(updated.PostNotes) != 1 || !strings.Contains(updated.PostNotes[0], "not a picture of this conversation") {
		t.Fatalf("a picture of another conversation: %+v %v", updated, err)
	}
	updated, err = world.worker.UpdateFeedItem(context.Background(), world.agent.ID, posted.ID, "this-conversation", func(item *models.AgentFeedItem) error {
		item.FeedMedia = []models.FeedMedia{cameraStillEntry(here.ID)}
		return nil
	})
	if err != nil || len(updated.FeedMedia) != 1 || updated.FeedMedia[0].ImageFileID == "" || len(updated.PostNotes) != 0 {
		t.Fatalf("a picture of this conversation: %+v %v", updated, err)
	}
}
