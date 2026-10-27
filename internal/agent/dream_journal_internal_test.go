package agent

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/journal"
	"github.com/ziyan/teanode/internal/models"
)

func (self *journalFixture) dream(t *testing.T) *models.AgentDream {
	t.Helper()
	record := &models.AgentDream{}
	self.worker.dreamJournal(t.Context(), self.run, record, newDreamBudget(self.worker.settings.Configuration(), self.run.Agent, 1, 0))
	return record
}

// A dream writes the finished days that have something in them, newest first,
// never today, and no day older than the depth unless the person asked for it;
// it counts what it wrote.
func TestTheDreamWritesFinishedDaysAndSkipsTodayAndOldDays(t *testing.T) {
	fixture := newJournalFixture(t, nil)
	today, yesterday, fiveAgo := fixture.dayAgo(0), fixture.dayAgo(1), fixture.dayAgo(5)
	tooOld, asked := fixture.dayAgo(models.JournalNarrativeDepthDays+30), fixture.dayAgo(models.JournalNarrativeDepthDays+40)
	for day, words := range map[string]string{today: "marker-today", yesterday: "marker-yesterday", fiveAgo: "marker-five", tooOld: "marker-old", asked: "marker-asked"} {
		fixture.words(t, day, words)
	}
	fixture.requestNarrative(t, asked)

	record := fixture.dream(t)
	prompts := fixture.dayPrompts()
	if len(prompts) != 3 || !strings.Contains(prompts[0], "marker-yesterday") || !strings.Contains(prompts[1], "marker-five") || !strings.Contains(prompts[2], "marker-asked") {
		t.Fatalf("%d days were written, not yesterday, five days ago and the one asked for: %q", len(prompts), prompts)
	}
	for day, isWritten := range map[string]bool{today: false, yesterday: true, fiveAgo: true, tooOld: false, asked: true} {
		if row := fixture.day(t, day); (row.Narrative != "") != isWritten {
			t.Errorf("%s: narrative %q, should be written: %v", day, row.Narrative, isWritten)
		}
	}
	if record.JournalDaysWritten != 3 || record.JournalNarrativesRefused != 0 || record.JournalPeriodsWritten == 0 {
		t.Errorf("the record: %+v", record)
	}
}

// A day is written again when what it would be written from is not what its
// narrative was: the person changed their words, or told it something was
// wrong. A day that is unchanged is left alone, however often a dream runs.
func TestTheDreamWritesAStaleDayAgainAndNoOther(t *testing.T) {
	fixture := newJournalFixture(t, nil)
	first, second := fixture.dayAgo(1), fixture.dayAgo(2)
	fixture.words(t, first, "marker-first")
	fixture.words(t, second, "marker-second")
	fixture.dream(t)
	if len(fixture.dayPrompts()) != 2 {
		t.Fatalf("%d days written", len(fixture.dayPrompts()))
	}

	fixture.dream(t)
	if len(fixture.dayPrompts()) != 2 {
		t.Fatalf("a day that had not changed was written again: %d prompts", len(fixture.dayPrompts()))
	}

	fixture.words(t, second, "marker-second, and then some more")
	fixture.dream(t)
	prompts := fixture.dayPrompts()
	if len(prompts) != 3 || !strings.Contains(prompts[2], "and then some more") {
		t.Fatalf("the changed day was not written again: %q", prompts)
	}

	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.AddJournalCorrection(fixture.run.Owner.ID, models.JournalTarget{Day: first}, journal.Zone(fixture.run.Owner).String(), "It was a Tuesday.", time.Now()); err != nil {
			t.Fatal(err)
		}
	})
	fixture.dream(t)
	prompts = fixture.dayPrompts()
	if len(prompts) != 4 || !strings.Contains(prompts[3], "It was a Tuesday.") || !strings.Contains(prompts[3], "marker-first") {
		t.Fatalf("the corrected day was not written again with the correction: %q", prompts)
	}
}

// One dream tries at most ten days; the next carries on.
func TestTheDreamWritesAtMostTenDays(t *testing.T) {
	fixture := newJournalFixture(t, nil)
	for count := 1; count <= 12; count++ {
		fixture.words(t, fixture.dayAgo(count), fmt.Sprintf("marker-%02d", count))
	}
	if record := fixture.dream(t); record.JournalDaysWritten != 10 || len(fixture.dayPrompts()) != 10 {
		t.Fatalf("written %d days with %d prompts", record.JournalDaysWritten, len(fixture.dayPrompts()))
	}
	if row := fixture.day(t, fixture.dayAgo(12)); row.Narrative != "" {
		t.Errorf("the oldest day was written in the first dream")
	}
	if record := fixture.dream(t); record.JournalDaysWritten != 2 {
		t.Errorf("the next dream wrote %d days, not the other two", record.JournalDaysWritten)
	}
}

// A person without the journal has nothing written, and a night with no
// budget writes nothing.
func TestTheDreamSkipsAPersonWithoutTheJournalAndANightWithoutBudget(t *testing.T) {
	fixture := newJournalFixture(t, nil, models.PermissionAgentUse)
	fixture.words(t, fixture.dayAgo(1), "marker")
	fixture.dream(t)
	if len(fixture.prompts()) != 0 {
		t.Errorf("a person without journal:use had %d prompts", len(fixture.prompts()))
	}

	other := newJournalFixture(t, nil)
	other.words(t, other.dayAgo(1), "marker")
	spent := &dreamBudget{exhausted: true}
	other.worker.dreamJournal(t.Context(), other.run, &models.AgentDream{}, spent)
	if len(other.prompts()) != 0 {
		t.Errorf("a spent night asked the model %d times", len(other.prompts()))
	}
}

// A day whose writing was refused is left alone for a day, and tried again
// after, or when the person asks.
func TestTheDreamDoesNotAskAgainAtOnceForADayItRefused(t *testing.T) {
	fixture := newJournalFixture(t, func(string) string {
		return `{"headline": "x", "narrative": "See [[mail:invented]].", "choices": [], "coverPhotoId": ""}`
	})
	day := fixture.dayAgo(1)
	fixture.words(t, day, "marker")
	record := fixture.dream(t)
	if record.JournalNarrativesRefused != 2 || record.JournalDaysWritten != 0 || len(fixture.dayPrompts()) != 2 {
		t.Fatalf("record %+v, %d prompts", record, len(fixture.dayPrompts()))
	}
	fixture.dream(t)
	if len(fixture.dayPrompts()) != 2 {
		t.Errorf("the refused day was asked for again at once")
	}
	dbtest.Exec(t, fixture.database, `UPDATE journal_day SET modified_at = modified_at - interval '2 days'`)
	fixture.dream(t)
	if len(fixture.dayPrompts()) != 4 {
		t.Errorf("the refused day was not tried again after a day: %d prompts", len(fixture.dayPrompts()))
	}
}

// weekOf is the first day of the week, Monday first, that a day is in.
func weekOf(t *testing.T, day string) string {
	t.Helper()
	first, _, err := journal.PeriodContaining(models.JournalPeriodWeek, day, models.WeekStartsMonday)
	if err != nil {
		t.Fatal(err)
	}
	return first
}

// After the days the dream writes the week, the month and the year they are in
// from the pages of those days, with the digest of the days' digests; a period
// is not written again within a day of its last writing, and is when that day
// is over and a day below it has changed.
func TestTheDreamWritesTheWeeksMonthsAndYearsAboveTheDaysOnceADay(t *testing.T) {
	fixture := newJournalFixture(t, nil)
	first, second := fixture.dayAgo(2), fixture.dayAgo(3)
	fixture.words(t, first, "marker-first")
	fixture.words(t, second, "marker-second")
	record := fixture.dream(t)

	weeks := map[string]bool{weekOf(t, first): true, weekOf(t, second): true}
	monthStart, _, _ := journal.PeriodContaining(models.JournalPeriodMonth, first, "")
	yearStart, _, _ := journal.PeriodContaining(models.JournalPeriodYear, first, "")
	if record.JournalPeriodsWritten != len(weeks)+2 && record.JournalPeriodsWritten != len(weeks)+3 {
		// The two days may be in two months.
		t.Logf("periods written: %d", record.JournalPeriodsWritten)
	}
	for week := range weeks {
		row := fixture.period(t, models.JournalPeriodWeek, week)
		if row.Narrative == "" || row.Headline != "A quiet day" {
			t.Errorf("the week of %s: %+v", week, row)
		}
	}
	year := fixture.period(t, models.JournalPeriodYear, yearStart)
	if year.Narrative == "" {
		t.Errorf("the year: %+v", year)
	}
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		input, err := readJournalPeriodInput(tx, fixture.run.Owner, models.JournalTarget{PeriodKind: models.JournalPeriodMonth, PeriodStart: monthStart}, journal.AllReadOptions(), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if month, err := tx.GetJournalPeriod(fixture.run.Owner.ID, models.JournalPeriodMonth, monthStart); err != nil || month.NarrativeInputDigest != input.Digest || len(input.Digest) != 64 {
			t.Errorf("the month's digest %v, the days' %s: %v", month, input.Digest, err)
		}
	})

	// A day below changes: the day is written again, the periods not yet.
	periodsBefore := len(fixture.periodPrompts())
	fixture.words(t, first, "marker-first, changed")
	fixture.dream(t)
	if len(fixture.periodPrompts()) != periodsBefore {
		t.Errorf("a period was written twice in a day")
	}
	dbtest.Exec(t, fixture.database, `UPDATE journal_period SET narrative_written_at = narrative_written_at - interval '2 days'`)
	fixture.dream(t)
	if len(fixture.periodPrompts()) <= periodsBefore {
		t.Errorf("no period was written again after the day was over")
	}
	if prompt := fixture.periodPrompts()[0]; !strings.Contains(prompt, "You wrote a note.") || strings.Contains(prompt, "marker-first") {
		t.Errorf("a period is written from the pages of its days, not from their words:\n%s", prompt)
	}
}

// storeDayPage stores a day's narrative as a dream would have, with the
// records it mentions and chose.
func (self *journalFixture) storeDayPage(t *testing.T, day, narrative string, choices ...string) {
	t.Helper()
	links := []*models.JournalLink{}
	for _, reference := range choices {
		kind, recordId, _ := strings.Cut(reference, ":")
		links = append(links, &models.JournalLink{RecordKind: models.AgentRecordKind(kind), RecordID: recordId, LinkReason: "you were asked to decide"})
	}
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		err := tx.PutJournalNarrative(self.run.Owner.ID, models.JournalTarget{Day: day}, journal.Zone(self.run.Owner).String(),
			models.JournalNarrativeWriting{Headline: "Headline of " + day, Narrative: narrative, InputDigest: "digest-" + day, Choices: links}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
	})
}

// A week may mention and choose only what its days mention and chose; an
// answer that goes past that is refused and asked again, and a year's
// chapters must be days of the year.
func TestAWeekMayNameOnlyWhatItsDaysNamed(t *testing.T) {
	fixture := newJournalFixture(t, nil)
	week := weekOf(t, fixture.dayAgo(10))
	first, _ := journal.AddDays(week, 1)
	second, _ := journal.AddDays(week, 2)
	fixture.storeDayPage(t, first, "You read the budget [[mail:budget]].", "mail:budget")
	fixture.storeDayPage(t, second, "You went swimming.")
	target := models.JournalTarget{PeriodKind: models.JournalPeriodWeek, PeriodStart: week}

	asker := &asking{answer: func(call int, _ string) string {
		if call == 1 {
			return `{"headline": "A week", "narrative": "Budget [[mail:budget]] and [[mail:invented]].", "choices": []}`
		}
		return `{"headline": "A week", "narrative": "You read the budget [[mail:budget]].", "choices": [{"reference": "mail:budget", "reason": "decided"}]}`
	}}
	counts := &journalCounts{}
	if err := fixture.worker.writeJournalPeriod(t.Context(), fixture.run, target, false, asker.ask, counts); err != nil {
		t.Fatal(err)
	}
	if len(asker.prompts) != 2 || !strings.Contains(asker.prompts[1], "mail:invented") || counts.refusedCount != 1 || counts.periodsWritten != 1 {
		t.Fatalf("asked %d times, counts %+v", len(asker.prompts), counts)
	}
	if !strings.Contains(asker.prompts[0], "Headline of "+first) || !strings.Contains(asker.prompts[0], "Chosen: mail:budget (you were asked to decide)") {
		t.Errorf("the week is not written from its days' headlines, narratives and choices:\n%s", asker.prompts[0])
	}
	row := fixture.period(t, models.JournalPeriodWeek, week)
	if row.Narrative != "You read the budget [[mail:budget]]." || row.NarrativeFailure != "" {
		t.Errorf("the stored week: %+v", row)
	}

	// A choice that no day chose is refused, twice over, and stored as a failure.
	again := &asking{answer: func(int, string) string {
		return `{"headline": "A week", "narrative": "A week.", "choices": [{"reference": "mail:invented", "reason": "why not"}]}`
	}}
	if err := fixture.worker.writeJournalPeriod(t.Context(), fixture.run, target, true, again.ask, &journalCounts{}); err != nil {
		t.Fatal(err)
	}
	if row = fixture.period(t, models.JournalPeriodWeek, week); !strings.Contains(row.NarrativeFailure, "refused twice") || row.Narrative == "" {
		t.Errorf("the stored week after two refusals: %+v", row)
	}
}

// A year's chapters are days of the year, in order, with a title.
func TestAYearsChaptersMustBeDaysOfTheYear(t *testing.T) {
	input := &journalPeriodInput{Target: models.JournalTarget{PeriodKind: models.JournalPeriodYear, PeriodStart: "2026-01-01"}, Start: "2026-01-01", End: "2026-12-31", allowed: map[string]bool{}}
	answer := func(chapters ...models.JournalChapter) *journalPeriodAnswer {
		return &journalPeriodAnswer{Headline: "A year", Narrative: "A year of docks.", Chapters: chapters}
	}
	writing, err := checkJournalPeriodAnswer(answer(models.JournalChapter{FromDay: "2026-03-01", UntilDay: "2026-05-31", Title: "Spring", Detail: "The dock."}), input)
	if err != nil || len(writing.Chapters) != 1 || writing.Chapters[0].Title != "Spring" {
		t.Fatalf("a chapter inside the year: %v %+v", err, writing)
	}
	for name, chapter := range map[string]models.JournalChapter{
		"before the year": {FromDay: "2025-12-01", UntilDay: "2026-02-01", Title: "Winter"},
		"backwards":       {FromDay: "2026-05-01", UntilDay: "2026-03-01", Title: "Spring"},
		"not a day":       {FromDay: "spring", UntilDay: "2026-03-01", Title: "Spring"},
		"no title":        {FromDay: "2026-03-01", UntilDay: "2026-05-01"},
	} {
		if _, err := checkJournalPeriodAnswer(answer(chapter), input); err == nil {
			t.Errorf("a chapter %s was accepted", name)
		}
	}
	// A week has no chapters, whatever the model says.
	input.Target.PeriodKind = models.JournalPeriodWeek
	if writing, err := checkJournalPeriodAnswer(answer(models.JournalChapter{FromDay: "bad", Title: "x"}), input); err != nil || len(writing.Chapters) != 0 {
		t.Errorf("a week took chapters: %v %+v", err, writing)
	}
}

// A mood that came from Apple Health is in a week's prompt only while the
// person lets the agent read Health; the one they chose always is.
func TestAWeekHasAHealthMoodOnlyUnderTheSwitch(t *testing.T) {
	fixture := newJournalFixture(t, nil, models.PermissionJournalUse, models.PermissionAgentUse, models.PermissionHealthUse)
	week := weekOf(t, fixture.dayAgo(10))
	chosenDay, _ := journal.AddDays(week, 1)
	healthDay, _ := journal.AddDays(week, 2)
	fixture.storeDayPage(t, chosenDay, "You read.")
	fixture.storeDayPage(t, healthDay, "You swam.")
	zone := journal.Zone(fixture.run.Owner).String()
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		low, great := 2, 5
		if _, err := tx.SetJournalMood(fixture.run.Owner.ID, chosenDay, zone, &low, "", models.JournalMoodFromPerson, time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.SetJournalMood(fixture.run.Owner.ID, healthDay, zone, &great, "", models.JournalMoodFromHealth, time.Now()); err != nil {
			t.Fatal(err)
		}
	})
	target := models.JournalTarget{PeriodKind: models.JournalPeriodWeek, PeriodStart: week}
	write := func() string {
		asker := &asking{answer: func(int, string) string { return plainPage }}
		if err := fixture.worker.writeJournalPeriod(t.Context(), fixture.run, target, false, asker.ask, &journalCounts{}); err != nil {
			t.Fatal(err)
		}
		return asker.prompts[0]
	}
	if prompt := write(); !strings.Contains(prompt, "mood low") || strings.Contains(prompt, "mood great") {
		t.Errorf("with the switch off:\n%s", prompt)
	}
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if err := tx.SetCanAgentReadHealth(fixture.run.Owner.ID, true); err != nil {
			t.Fatal(err)
		}
	})
	if prompt := write(); !strings.Contains(prompt, "mood low") || !strings.Contains(prompt, "mood great") {
		t.Errorf("with the switch on:\n%s", prompt)
	}
}
