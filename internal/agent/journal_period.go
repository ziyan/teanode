package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/journal"
	"github.com/ziyan/teanode/internal/models"
)

// Writing a week, a month or a year: from the pages written below it, never
// from the records again. A week is written from its days' headlines,
// narratives and choices, a month from its weeks and days, a year from its
// months. What the model may mention or choose is what those pages mention
// and chose, and an answer that goes past that is refused like a day's.

const (
	// journalChaptersMost is how many chapters of a year are kept.
	journalChaptersMost = 8

	// journalChapterDetailCharacters is the longest a chapter's detail is.
	journalChapterDetailCharacters = 300

	// journalPeriodChoicesMost is how many records a period's narrative may
	// choose.
	journalPeriodChoicesMost = 6

	// journalDayRowsPageSize is how many day rows one read asks for.
	journalDayRowsPageSize = 100
)

// journalPeriodInput is what a period is written from, and the digest of it.
type journalPeriodInput struct {
	Target models.JournalTarget
	Start  string
	End    string
	Today  string

	// Row is what the journal keeps for the period, nil when nothing yet.
	Row *models.JournalPeriod

	// Days are the period's days that have a narrative, oldest first. A
	// year's days are not shown to the model, only counted into its digest.
	Days     []*models.JournalDay
	DayLinks map[string][]*models.JournalLink

	// Weeks are a month's weeks with a narrative, and Months a year's months
	// with one, oldest first.
	Weeks       []*models.JournalPeriod
	Months      []*models.JournalPeriod
	PeriodLinks map[string][]*models.JournalLink

	options journal.ReadOptions

	// allowed is every reference the narrative or a choice may name.
	allowed map[string]bool

	// Digest is the hex SHA-256 of the narrative digests of the days, and of
	// the period's own corrections and looking back.
	Digest string
}

// HasPages says there is anything below the period to write it from.
func (self *journalPeriodInput) HasPages() bool {
	return len(self.Days) > 0
}

// listJournalDayRows reads every day row from one day to another, oldest
// first, whatever the page size of the storage.
func listJournalDayRows(transaction db.Transaction, userId, fromDay, untilDay string) ([]*models.JournalDay, error) {
	var days []*models.JournalDay
	for offset := 0; ; {
		page, total, err := transaction.ListJournalDays(userId, fromDay, untilDay, offset, journalDayRowsPageSize)
		if err != nil {
			return nil, err
		}
		days = append(days, page...)
		offset += len(page)
		if len(page) == 0 || offset >= total {
			break
		}
	}
	sort.SliceStable(days, func(left, right int) bool { return days[left].Day < days[right].Day })
	return days, nil
}

// visibleJournalLinks are a day's or a period's links the page shows: the
// agent's choices the person did not hide, and the person's pins.
func visibleJournalLinks(transaction db.Transaction, userId, journalDayId, journalPeriodId string) ([]*models.JournalLink, error) {
	links, err := transaction.ListJournalLinks(userId, journalDayId, journalPeriodId)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	visible := links[:0:0]
	for _, link := range links {
		if !link.IsHidden {
			visible = append(visible, link)
		}
	}
	return visible, nil
}

// readJournalPeriodInput reads what the period named by target is written
// from. The days' moods are the person's own unless the agent may read
// Health; the days' numbers are not given to the model at all.
func readJournalPeriodInput(transaction db.Transaction, user *models.User, target models.JournalTarget, options journal.ReadOptions, now time.Time) (*journalPeriodInput, error) {
	weekStart := ""
	if target.PeriodKind == models.JournalPeriodWeek {
		var err error
		if weekStart, err = journal.WeekStartOf(transaction, user.ID); err != nil {
			return nil, err
		}
	}
	first, last, err := journal.PeriodContaining(target.PeriodKind, target.PeriodStart, weekStart)
	if err != nil {
		return nil, err
	}
	input := &journalPeriodInput{
		Target: target, Start: first, End: last, Today: journal.Today(user, now), options: options,
		DayLinks: map[string][]*models.JournalLink{}, PeriodLinks: map[string][]*models.JournalLink{}, allowed: map[string]bool{},
	}
	row, err := transaction.GetJournalPeriod(user.ID, target.PeriodKind, first)
	switch {
	case err == nil:
		input.Row = row
	case errors.Is(err, db.ErrNotFound):
	default:
		return nil, err
	}

	until := last
	if input.Today < until {
		until = input.Today
	}
	if first <= until {
		rows, err := listJournalDayRows(transaction, user.ID, first, until)
		if err != nil {
			return nil, err
		}
		for _, day := range rows {
			// Shown as the agent may see it: a mood from Apple Health is not
			// there unless the agent may read Health.
			if strings.TrimSpace(day.Narrative) != "" {
				input.Days = append(input.Days, journal.ShownDay(day, options))
			}
		}
	}
	switch target.PeriodKind {
	case models.JournalPeriodWeek, models.JournalPeriodMonth:
		for _, day := range input.Days {
			links, err := visibleJournalLinks(transaction, user.ID, day.ID, "")
			if err != nil {
				return nil, err
			}
			input.DayLinks[day.ID] = links
			input.takeNarrative(day.Narrative, links)
		}
	}
	switch target.PeriodKind {
	case models.JournalPeriodMonth:
		// A week that starts in the month before still has days in this one.
		earliest, err := journal.AddDays(first, -6)
		if err != nil {
			return nil, err
		}
		weekStartDay, err := journal.WeekStartOf(transaction, user.ID)
		if err != nil {
			return nil, err
		}
		weeks, err := transaction.ListJournalPeriods(user.ID, models.JournalPeriodWeek, earliest, last)
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			return nil, err
		}
		for _, week := range weeks {
			weekFirst, weekLast, err := journal.PeriodContaining(models.JournalPeriodWeek, week.PeriodStart, weekStartDay)
			if err != nil || strings.TrimSpace(week.Narrative) == "" || weekLast < first || weekFirst > last {
				continue
			}
			input.Weeks = append(input.Weeks, week)
		}
		for _, week := range input.Weeks {
			links, err := visibleJournalLinks(transaction, user.ID, "", week.ID)
			if err != nil {
				return nil, err
			}
			input.PeriodLinks[week.ID] = links
			input.takeNarrative(week.Narrative, links)
		}
	case models.JournalPeriodYear:
		months, err := transaction.ListJournalPeriods(user.ID, models.JournalPeriodMonth, first, last)
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			return nil, err
		}
		for _, month := range months {
			if strings.TrimSpace(month.Narrative) == "" {
				continue
			}
			input.Months = append(input.Months, month)
			links, err := visibleJournalLinks(transaction, user.ID, "", month.ID)
			if err != nil {
				return nil, err
			}
			input.PeriodLinks[month.ID] = links
			input.takeNarrative(month.Narrative, links)
		}
	}
	input.Digest = input.digest()
	return input, nil
}

// takeNarrative allows what a page below mentions and chose.
func (self *journalPeriodInput) takeNarrative(narrative string, links []*models.JournalLink) {
	for _, reference := range journalMentions(narrative) {
		self.allowed[reference] = true
	}
	for _, link := range links {
		self.allowed[string(link.RecordKind)+":"+link.RecordID] = true
	}
}

// digest is the fingerprint of what the period is written from: the digests
// the narratives of its days were written from, and the period's own
// corrections and looking back (the person's original, never the touched-up
// text). A day written again with other input changes it; a day whose
// narrative is only written again from the same input does not.
func (self *journalPeriodInput) digest() string {
	lines := make([]string, 0, len(self.Days)+2)
	for _, day := range self.Days {
		lines = append(lines, "day|"+day.Day+"|"+day.NarrativeInputDigest)
	}
	sort.Strings(lines)
	lines = append(lines, "period|"+string(self.Target.PeriodKind)+"|"+self.Start)
	if row := self.Row; row != nil {
		if row.LookingBack != "" {
			lines = append(lines, "lookingBack|"+row.LookingBack)
		}
		for _, correction := range row.NarrativeCorrections {
			lines = append(lines, "correction|"+correction.CorrectionText)
		}
	}
	digest := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(digest[:])
}

// pageOf writes a day or a period's page for the model: when it was, its
// headline and narrative, and what was chosen with why.
func pageOf(heading, headline, narrative string, links []*models.JournalLink) string {
	var builder strings.Builder
	builder.WriteString(heading + "\n")
	if headline != "" {
		builder.WriteString("Headline: " + oneLineForModel(headline, journalHeadlineCharacters) + "\n")
	}
	builder.WriteString(strings.TrimSpace(narrative) + "\n")
	var chosen []string
	for _, link := range links {
		reason := oneLineForModel(link.LinkReason, journalReasonCharacters)
		entry := string(link.RecordKind) + ":" + link.RecordID
		if reason != "" {
			entry += " (" + reason + ")"
		}
		chosen = append(chosen, entry)
	}
	if len(chosen) > 0 {
		builder.WriteString("Chosen: " + strings.Join(chosen, "; ") + "\n")
	}
	return builder.String()
}

// pages are the pages below the period, oldest first, as the model reads
// them.
func (self *journalPeriodInput) pages() string {
	var blocks []string
	switch self.Target.PeriodKind {
	case models.JournalPeriodYear:
		for _, month := range self.Months {
			blocks = append(blocks, pageOf("Month starting "+month.PeriodStart, month.Headline, month.Narrative, self.PeriodLinks[month.ID]))
		}
	case models.JournalPeriodMonth:
		for _, week := range self.Weeks {
			blocks = append(blocks, pageOf("Week starting "+week.PeriodStart, week.Headline, week.Narrative, self.PeriodLinks[week.ID]))
		}
		fallthrough
	default:
		for _, day := range self.Days {
			weekday, _ := journal.Weekday(day.Day)
			heading := fmt.Sprintf("Day %s (%s)", day.Day, weekday)
			if day.MoodLevel != nil {
				heading += ", mood " + journalMoodNames[*day.MoodLevel]
			}
			blocks = append(blocks, pageOf(heading, day.Headline, day.Narrative, self.DayLinks[day.ID]))
		}
	}
	return strings.Join(blocks, "\n")
}

// journalPeriodAnswer is what the model is asked to answer for a period.
type journalPeriodAnswer struct {
	Headline  string                  `json:"headline"`
	Narrative string                  `json:"narrative"`
	Choices   []journalChoiceAnswer   `json:"choices"`
	Chapters  []models.JournalChapter `json:"chapters"`
}

// checkJournalPeriodAnswer accepts an answer for a period or refuses it. The
// narrative and the choices may name only what the pages below name; a year's
// chapters must be days of the year.
func checkJournalPeriodAnswer(answer *journalPeriodAnswer, input *journalPeriodInput) (*models.JournalNarrativeWriting, error) {
	headline, err := checkJournalHeadline(answer.Headline, answer.Narrative)
	if err != nil {
		return nil, err
	}
	narrative := strings.TrimSpace(answer.Narrative)
	if err := checkJournalMentions(narrative, input.allowed); err != nil {
		return nil, err
	}
	links, err := checkJournalChoices(answer.Choices, input.allowed, journalPeriodChoicesMost)
	if err != nil {
		return nil, err
	}
	writing := &models.JournalNarrativeWriting{Headline: headline, Narrative: narrative, Choices: links}
	if input.Target.PeriodKind != models.JournalPeriodYear {
		return writing, nil
	}
	for _, chapter := range answer.Chapters {
		chapter.Title = oneLineForModel(chapter.Title, journalHeadlineCharacters)
		chapter.Detail = oneLineForModel(chapter.Detail, journalChapterDetailCharacters)
		_, fromError := journal.ParseDay(chapter.FromDay)
		_, untilError := journal.ParseDay(chapter.UntilDay)
		switch {
		case chapter.Title == "":
			return nil, &journalRefusal{reason: "a chapter has no title"}
		case fromError != nil || untilError != nil:
			return nil, &journalRefusal{reason: fmt.Sprintf("the chapter %q does not start and end on days written YYYY-MM-DD", chapter.Title)}
		case chapter.FromDay > chapter.UntilDay || chapter.FromDay < input.Start || chapter.UntilDay > input.End:
			return nil, &journalRefusal{reason: fmt.Sprintf("the chapter %q is not inside the year %s to %s", chapter.Title, input.Start, input.End)}
		}
		if len(writing.Chapters) < journalChaptersMost {
			writing.Chapters = append(writing.Chapters, chapter)
		}
	}
	return writing, nil
}

// writeJournalPeriod reads what a period is written from and writes it.
func (self *Agent) writeJournalPeriod(ctx context.Context, run *Run, target models.JournalTarget, isRequested bool, ask journalAsk, counts *journalCounts) error {
	var input *journalPeriodInput
	var access *journalAccess
	if err := run.Database().TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		if access, err = readJournalAccess(transaction, run.Owner.ID); err != nil {
			return err
		}
		input, err = readJournalPeriodInput(transaction, run.Owner, target, access.options(), time.Now())
		return err
	}); err != nil {
		return err
	}
	return self.writeReadJournalPeriod(ctx, run, input, isRequested, ask, counts)
}

// writeReadJournalPeriod is writeJournalPeriod for a period already read.
func (self *Agent) writeReadJournalPeriod(ctx context.Context, run *Run, input *journalPeriodInput, isRequested bool, ask journalAsk, counts *journalCounts) error {
	timeZone := journal.Zone(run.Owner).String()
	if !input.HasPages() {
		if !isRequested {
			return nil
		}
		return self.storeJournalFailure(ctx, run, input.Target, timeZone, "none of the days in it has a page written yet")
	}
	data := map[string]any{
		"PersonName": personName(run.Owner),
		"Language":   languageName(KnowledgeLanguage(run.Agent, run.Owner)),
		"PeriodKind": string(input.Target.PeriodKind),
		"Range":      input.Start + " to " + input.End,
		"IsOpen":     input.Today >= input.Start && input.Today <= input.End,
		"IsYear":     input.Target.PeriodKind == models.JournalPeriodYear,
		"Pages":      fenced(input.pages()),

		"LookingBack":  "",
		"Corrections":  "",
		"Instructions": strings.TrimSpace(run.Agent.Instructions),
	}
	if row := input.Row; row != nil {
		if words := strings.TrimSpace(row.LookingBack); words != "" {
			data["LookingBack"] = fenced(words)
		}
		if len(row.NarrativeCorrections) > 0 {
			corrections := make([]string, 0, len(row.NarrativeCorrections))
			for _, correction := range row.NarrativeCorrections {
				corrections = append(corrections, "- "+strings.TrimSpace(correction.CorrectionText))
			}
			data["Corrections"] = fenced(strings.Join(corrections, "\n"))
		}
	}
	prompt, err := render("journal_period.txt", data)
	if err != nil {
		return err
	}
	title := fmt.Sprintf("Wrote the journal of the %s from %s", input.Target.PeriodKind, input.Start)
	writing, refusedCount, err := askJournalAnswer(ctx, ask, title, prompt, []string{"headline", "narrative"},
		func(answer *journalPeriodAnswer) (*models.JournalNarrativeWriting, error) {
			return checkJournalPeriodAnswer(answer, input)
		})
	counts.refusedCount += refusedCount
	var refusal *journalRefusal
	if errors.As(err, &refusal) {
		return self.storeJournalFailure(ctx, run, input.Target, timeZone, "the writing was refused twice: "+refusal.reason)
	}
	if err != nil {
		return err
	}
	writing.InputDigest = input.Digest
	if err := run.Database().TransactionContext(ctx, func(transaction db.Transaction) error {
		return transaction.PutJournalNarrative(run.Owner.ID, input.Target, timeZone, *writing, time.Now())
	}); err != nil {
		return err
	}
	counts.periodsWritten++
	return nil
}
