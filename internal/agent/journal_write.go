package agent

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/journal"
	"github.com/ziyan/teanode/internal/models"
)

// Writing a journal day: the narrative and the headline the agent makes for
// one day, from what journal.ReadDay found of it. A dream writes the days
// that have none or have changed (dream_journal.go) and the journal job writes
// the ones the person asked for (journal_job.go); both come through here.
//
// What the model is given is a list of records, one line each, named by the
// kind:id that a link and a narrative name the record by, and the person's own
// words, mood and weather. What it answers is checked against that list: a
// narrative that mentions a record that was not in it, a choice that was not
// a candidate, or a cover that was not one of the day's photos is refused and
// asked for again once, and then left unwritten with the reason stored, so a
// page never opens a record that is not the day's.

// journalAsk puts a prompt to a model and answers what it said. A dream's is
// held to the night's share of the day and the job's to the person's budget.
type journalAsk func(ctx context.Context, title, prompt string) (string, error)

const (
	// journalLineCharacters is how long the description of one record is
	// when it is put in front of the model.
	journalLineCharacters = 160

	// The most lines of one kind of record that are shown. A day can hold
	// hundreds of messages and photos; the totals are given beside the
	// lists, and the page lists the rest live.
	journalDocumentLinesMost    = 80
	journalMailLinesMost        = 60
	journalPhotoLinesMost       = 40
	journalTransactionLinesMost = 40

	// journalChoicesMost is how many records a day's narrative may choose.
	journalChoicesMost = 8

	// journalHeadlineCharacters is the longest a headline is kept.
	journalHeadlineCharacters = 120

	// journalReasonCharacters is the longest a choice's reason is kept.
	journalReasonCharacters = 200
)

// journalReferencePattern is a mention of a record in a narrative, the same
// one the API and the command line read: [[kind:id]].
var journalReferencePattern = regexp.MustCompile(`\[\[([a-z_]+):([^\[\]]+)\]\]`)

// journalCounts is what a writing pass did, for the dream's record.
type journalCounts struct {
	daysWritten    int
	periodsWritten int
	refusedCount   int
}

// journalAccess is what the person may read of their own journal day on the
// agent's behalf: their permissions, and whether they let the agent read
// their Health.
type journalAccess struct {
	permissions        *models.EffectivePermissions
	canAgentReadHealth bool
}

func readJournalAccess(transaction db.Transaction, userId string) (*journalAccess, error) {
	permissions, err := transaction.EffectivePermissions(userId)
	if err != nil {
		return nil, err
	}
	access := &journalAccess{permissions: permissions}
	choices, err := transaction.GetHealthChoices(userId)
	switch {
	case err == nil:
		access.canAgentReadHealth = choices != nil && choices.CanAgentReadHealth
	case errors.Is(err, db.ErrNotFound):
	default:
		return nil, err
	}
	return access, nil
}

func (self *journalAccess) options() journal.ReadOptions {
	return journal.OptionsFor(self.permissions, self.canAgentReadHealth)
}

// journalFactsToStore is the numbers to keep after a day was read without
// Health: the ones just worked out, with the Health numbers the row already
// had, since a read that left Health out knows nothing of them.
func journalFactsToStore(computed models.JournalFacts, read *journal.Day, canAgentReadHealth bool) models.JournalFacts {
	if canAgentReadHealth || read.Row == nil {
		return computed
	}
	stored := read.Row.Facts
	computed.StepCount, computed.SleepMinutes, computed.WalkedMeters = stored.StepCount, stored.SleepMinutes, stored.WalkedMeters
	computed.WorkoutMinutes, computed.HealthMoodLevel = stored.WorkoutMinutes, stored.HealthMoodLevel
	computed.HasActivity = computed.HasActivity || stored.HasActivity
	return computed
}

var journalMoodNames = map[int]string{
	models.JournalMoodAwful: "awful", models.JournalMoodLow: "low", models.JournalMoodOkay: "okay",
	models.JournalMoodGood: "good", models.JournalMoodGreat: "great",
}

// journalWeatherWords says a day's weather in a sentence fragment, with the
// temperatures in the units the person reads, so that the page the model
// writes says "18°C" or "64°F" the way they would.
func journalWeatherWords(weather *models.JournalWeather, isUSUnits bool) string {
	if weather == nil {
		return ""
	}
	var kind string
	switch code := weather.WeatherCode; {
	case code == 0:
		kind = "clear"
	case code == 1 || code == 2:
		kind = "partly cloudy"
	case code == 3:
		kind = "overcast"
	case code == 45 || code == 48:
		kind = "fog"
	case code >= 51 && code <= 57:
		kind = "drizzle"
	case code >= 61 && code <= 67:
		kind = "rain"
	case code >= 71 && code <= 77:
		kind = "snow"
	case code >= 80 && code <= 82:
		kind = "showers"
	case code == 85 || code == 86:
		kind = "snow showers"
	case code >= 95:
		kind = "thunderstorms"
	default:
		kind = "mixed"
	}
	high, low, unit := weather.HighCelsius, weather.LowCelsius, "C"
	if isUSUnits {
		high, low, unit = models.FahrenheitFromCelsius(high), models.FahrenheitFromCelsius(low), "F"
	}
	words := fmt.Sprintf("%s, a high of %.0f°%s and a low of %.0f°%s", kind, high, unit, low, unit)
	if weather.PrecipitationMM >= 0.5 {
		words += fmt.Sprintf(", %.1f mm of rain or snow", weather.PrecipitationMM)
	}
	if place := oneLineForModel(weather.PlaceName, 80); place != "" {
		words += ", near " + place
	}
	return words
}

// oneLineForModel is text from outside made safe to put in the middle of a
// line the model reads as the record: whitespace runs become one space,
// control and invisible formatting characters and angle brackets are dropped,
// a double square bracket (how a narrative mentions a record) is made single,
// and it is cut to characters.
func oneLineForModel(text string, characters int) string {
	cleaned := strings.Map(func(character rune) rune {
		switch {
		case unicode.IsSpace(character):
			return ' '
		case unicode.IsControl(character), unicode.Is(unicode.Cf, character), character == '<', character == '>':
			return -1
		}
		return character
	}, text)
	cleaned = strings.NewReplacer("[[", "[", "]]", "]").Replace(strings.Join(strings.Fields(cleaned), " "))
	return strings.TrimSpace(cutRunes(cleaned, characters))
}

// journalRecordLine is one record as the model reads it.
type journalRecordLine struct {
	Reference   string
	At          time.Time
	Text        string
	IsCandidate bool
}

// journalDayInput is what a day is written from, and the sets an answer is
// checked against.
type journalDayInput struct {
	Day  *journal.Day
	Zone *time.Location

	Records []journalRecordLine

	// allowed is every reference the narrative may mention, candidates is
	// the references a choice may name, and photos the ids a cover may be.
	allowed    map[string]bool
	candidates map[string]bool
	photos     map[string]bool
}

func (self *journalDayInput) add(recordKind models.AgentRecordKind, recordId string, at time.Time, text string, isCandidate bool) {
	if recordId == "" {
		return
	}
	reference := string(recordKind) + ":" + recordId
	if self.allowed[reference] {
		return
	}
	self.allowed[reference] = true
	if isCandidate {
		self.candidates[reference] = true
	}
	if recordKind == models.RecordPhoto {
		self.photos[recordId] = true
	}
	self.Records = append(self.Records, journalRecordLine{Reference: reference, At: at, Text: oneLineForModel(text, journalLineCharacters), IsCandidate: isCandidate})
}

// described is a sentence fragment of two parts, either of which may be
// empty.
func described(first, second string) string {
	first, second = strings.TrimSpace(first), strings.TrimSpace(second)
	switch {
	case first == "":
		return second
	case second == "":
		return first
	}
	return first + ", " + second
}

// newJournalDayInput makes a day's records into lines. Photos are the first
// photo of each stretch of them, and the photos offered to stand for the day.
func newJournalDayInput(day *journal.Day) *journalDayInput {
	input := &journalDayInput{
		Day: day, allowed: map[string]bool{}, candidates: map[string]bool{}, photos: map[string]bool{},
		Zone: time.UTC,
	}
	if location, err := time.LoadLocation(day.TimeZone); err == nil && day.TimeZone != "" {
		input.Zone = location
	}

	// Moments have the details already worded (how long a stay was, what an
	// amount is in its currency, how long a session ran), so they are used
	// for what they say and the sections for what they do not.
	allMoments := journal.Moments(day)
	moments := map[string]journal.Moment{}
	for _, moment := range allMoments {
		moments[string(moment.RecordKind)+":"+moment.RecordID] = moment
	}
	detailOf := func(recordKind models.AgentRecordKind, recordId string) string {
		return moments[string(recordKind)+":"+recordId].Detail
	}

	for _, session := range day.CodingSessions {
		input.add(models.RecordAgentDocument, session.DocumentID, session.StartedAt,
			"Coding session: "+described(session.Title, detailOf(models.RecordAgentDocument, session.DocumentID)), false)
	}
	for index, document := range day.Documents {
		if index == journalDocumentLinesMost {
			break
		}
		kind := "Work document or message"
		if document.SourceName != "" {
			kind += " (" + document.SourceName + ")"
		}
		input.add(models.RecordAgentDocument, document.DocumentID, document.HappenedAt, kind+": "+document.Title, true)
	}
	for _, note := range day.JournalNotes {
		input.add(models.RecordAgentDocument, note.DocumentID, day.From, "Note from their notes folder: "+described(note.Title, note.Text), false)
	}
	for _, conversation := range day.Conversations {
		input.add(models.RecordConversationMessage, conversation.FirstMessageID, conversation.FirstAt,
			fmt.Sprintf("Conversation with the agent: %s (%d messages)", conversation.Title, conversation.MessageCount), false)
	}
	for _, chat := range day.Chats {
		input.add(models.RecordChatMessage, chat.FirstMessageID, chat.FirstAt, fmt.Sprintf("Chat %s (%d messages)", chat.ChatName, chat.MessageCount), false)
	}

	// The important mail first, then the rest, up to the most shown.
	shownMail := 0
	for _, wantImportant := range []bool{true, false} {
		for _, mail := range day.Mail {
			if mail.IsImportant != wantImportant || shownMail == journalMailLinesMost {
				continue
			}
			shownMail++
			text := "Mail from " + described(mail.Counterpart, mail.Subject)
			if mail.IsSent {
				text = "Mail they sent to " + described(mail.Counterpart, mail.Subject)
			}
			if mail.IsImportant {
				text += " (important: high priority, needs a reply or was flagged)"
			}
			input.add(models.RecordMail, mail.ItemID, mail.ReceivedAt, text, true)
		}
	}

	for _, recording := range day.Recordings {
		input.add(models.RecordRecording, recording.RecordingID, recording.RecordedAt, "Recording: "+described(recording.Title, recording.Summary), false)
	}

	shownPhotos := 0
	for _, moment := range allMoments {
		if moment.RecordKind != models.RecordPhoto || shownPhotos == journalPhotoLinesMost {
			continue
		}
		shownPhotos++
		input.add(models.RecordPhoto, moment.RecordID, moment.At, "Photos: "+described(moment.Title, moment.Detail), false)
	}
	photoByID := map[string]journal.Photo{}
	for _, photo := range day.Photos {
		photoByID[photo.PhotoID] = photo
	}
	for _, photoId := range day.PhotoCandidateIDs {
		if photo, isThere := photoByID[photoId]; isThere {
			input.add(models.RecordPhoto, photoId, photo.TakenAt, described("A photo that could stand for the day", photo.PlaceName), false)
		}
	}

	for _, visit := range day.Visits {
		title := "A place"
		if strings.TrimSpace(visit.PlaceName) != "" {
			title = visit.PlaceName
		}
		moment := moments[string(models.RecordPlaceVisit)+":"+visit.VisitID]
		at := moment.At
		if at.IsZero() {
			continue
		}
		input.add(models.RecordPlaceVisit, visit.VisitID, at, "Visit to "+described(title, moment.Detail), false)
	}
	for _, moment := range allMoments {
		switch moment.RecordKind {
		case models.RecordCalendarOccurrence:
			input.add(moment.RecordKind, moment.RecordID, moment.At, "Calendar event: "+described(moment.Title, moment.Detail), false)
		case models.RecordReminder:
			input.add(moment.RecordKind, moment.RecordID, moment.At, "Reminder ("+moment.Detail+"): "+moment.Title, false)
		case models.RecordNote:
			input.add(moment.RecordKind, moment.RecordID, moment.At, "Note they wrote: "+moment.Title, false)
		case models.RecordHealthDay:
			input.add(moment.RecordKind, moment.RecordID, moment.At, "Workout: "+described(moment.Title, moment.Detail), false)
		}
	}
	shownTransactions := 0
	for _, transaction := range day.Transactions {
		if shownTransactions == journalTransactionLinesMost {
			break
		}
		shownTransactions++
		input.add(models.RecordFinanceTransaction, transaction.TransactionID, momentAt(moments, models.RecordFinanceTransaction, transaction.TransactionID, day.From),
			"Transaction: "+described(transaction.Description, detailOf(models.RecordFinanceTransaction, transaction.TransactionID)), false)
	}

	// Earliest first; ties keep a stable order by reference.
	sort.SliceStable(input.Records, func(left, right int) bool {
		if !input.Records[left].At.Equal(input.Records[right].At) {
			return input.Records[left].At.Before(input.Records[right].At)
		}
		return input.Records[left].Reference < input.Records[right].Reference
	})
	return input
}

func momentAt(moments map[string]journal.Moment, recordKind models.AgentRecordKind, recordId string, fallback time.Time) time.Time {
	if moment, isThere := moments[string(recordKind)+":"+recordId]; isThere {
		return moment.At
	}
	return fallback
}

// lines writes the records that are candidates or are not, as the model
// reads them.
func (self *journalDayInput) lines(isCandidate bool) string {
	var lines []string
	for _, record := range self.Records {
		if record.IsCandidate != isCandidate {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s | %s | %s", record.Reference, record.At.In(self.Zone).Format("15:04"), record.Text))
	}
	return strings.Join(lines, "\n")
}

// journalHealthLines are the day's Health numbers, which exist only when the
// day was read with Health, that is only while the person lets the agent. The
// distance is in the units the person reads.
func journalHealthLines(day *journal.Day, isUSUnits bool) string {
	health := day.Health
	if health == nil {
		return ""
	}
	var lines []string
	if health.StepCount > 0 {
		lines = append(lines, fmt.Sprintf("Steps: %d", health.StepCount))
	}
	if health.WalkedMeters > 0 {
		if isUSUnits {
			lines = append(lines, fmt.Sprintf("Walked: %.1f miles", models.MilesFromMeters(health.WalkedMeters)))
		} else {
			lines = append(lines, fmt.Sprintf("Walked: %.1f km", health.WalkedMeters/1000))
		}
	}
	if health.SleepMinutes > 0 {
		lines = append(lines, fmt.Sprintf("Slept the night before: %d h %d min", health.SleepMinutes/60, health.SleepMinutes%60))
	}
	if health.RestingHeartRate > 0 {
		lines = append(lines, fmt.Sprintf("Resting heart rate: %.0f bpm", health.RestingHeartRate))
	}
	return strings.Join(lines, "\n")
}

func (self *journalDayInput) totals() string {
	day := self.Day
	var parts []string
	for _, entry := range []struct {
		count int
		noun  string
	}{
		{day.PhotoTotal, "photos"}, {day.DocumentTotal, "work documents and messages"},
		{day.MailReceivedTotal, "mail received"}, {day.MailSentTotal, "mail sent"},
	} {
		if entry.count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", entry.count, entry.noun))
		}
	}
	return strings.Join(parts, ", ")
}

// prompt renders the day for the model. The person's original words are used,
// never the touched-up version, and the corrections are given as facts.
func (self *journalDayInput) prompt(owner *models.User, agent *models.Agent, options journal.ReadOptions) (string, error) {
	day := self.Day
	data := map[string]any{
		"PersonName":   personName(owner),
		"Language":     languageName(KnowledgeLanguage(agent, owner)),
		"Day":          day.Day,
		"Instructions": "",
		"Records":      fenced(self.lines(false)),
		"Totals":       self.totals(),
		"Health":       "",
		"Words":        "",
		"Mood":         "",
		"Weather":      "",
		"Candidates":   "",
	}
	if agent != nil {
		data["Instructions"] = strings.TrimSpace(agent.Instructions)
	}
	weekday, err := journal.Weekday(day.Day)
	if err != nil {
		return "", err
	}
	data["Weekday"] = strings.ToUpper(weekday[:1]) + weekday[1:]
	if candidates := self.lines(true); candidates != "" {
		data["Candidates"] = fenced(candidates)
	}
	if self.lines(false) == "" {
		data["Records"] = "(none)"
	}
	isUSUnits := owner.UsesUSUnits()
	if health := journalHealthLines(day, isUSUnits); health != "" {
		data["Health"] = fenced(health)
	}
	if row := day.Row; row != nil {
		if words := strings.TrimSpace(row.Words); words != "" {
			data["Words"] = fenced(words)
		}
		// The stored row keeps copies of Health (a mood from Apple Health);
		// what the model may see of it is what journal.ShownDay lets through.
		shown := journal.ShownDay(row, options)
		if shown.MoodLevel != nil {
			mood := journalMoodNames[*shown.MoodLevel]
			if moodWords := oneLineForModel(shown.MoodWords, 200); moodWords != "" {
				mood += `, in their words "` + moodWords + `"`
			}
			data["Mood"] = mood
		}
		data["Weather"] = journalWeatherWords(shown.Weather, isUSUnits)
		if len(row.NarrativeCorrections) > 0 {
			corrections := make([]string, 0, len(row.NarrativeCorrections))
			for _, correction := range row.NarrativeCorrections {
				corrections = append(corrections, "- "+strings.TrimSpace(correction.CorrectionText))
			}
			data["Corrections"] = fenced(strings.Join(corrections, "\n"))
		}
	}
	return render("journal_day.txt", data)
}

// journalDayAnswer is what the model is asked to answer for a day.
type journalDayAnswer struct {
	Headline     string                `json:"headline"`
	Narrative    string                `json:"narrative"`
	Choices      []journalChoiceAnswer `json:"choices"`
	CoverPhotoID string                `json:"coverPhotoId"`
}

// journalChoiceAnswer is one record the model chose, and why.
type journalChoiceAnswer struct {
	Reference string `json:"reference"`
	Reason    string `json:"reason"`
}

// journalRefusal is an answer that was read and not accepted, with the reason
// it is told and stored in.
type journalRefusal struct {
	reason string
}

func (self *journalRefusal) Error() string { return self.reason }

// journalMentions are the references a narrative mentions, in order, once
// each.
func journalMentions(narrative string) []string {
	var mentions []string
	seen := map[string]bool{}
	for _, match := range journalReferencePattern.FindAllStringSubmatch(narrative, -1) {
		reference := match[1] + ":" + match[2]
		if !seen[reference] {
			seen[reference] = true
			mentions = append(mentions, reference)
		}
	}
	return mentions
}

// checkJournalMentions refuses a narrative that mentions a record that is not
// in allowed.
func checkJournalMentions(narrative string, allowed map[string]bool) error {
	var unknown []string
	for _, reference := range journalMentions(narrative) {
		if !allowed[reference] {
			unknown = append(unknown, reference)
		}
	}
	if len(unknown) > 0 {
		return &journalRefusal{reason: "the narrative mentions " + strings.Join(unknown, ", ") + ", which " + pluralWord(len(unknown), "is", "are") + " not in the list of records"}
	}
	return nil
}

func pluralWord(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}

// checkJournalChoices turns the model's choices into links, refusing one that
// is not in allowed, or has no reason. The same record named twice is kept
// once, and no more than most are kept.
func checkJournalChoices(choices []journalChoiceAnswer, allowed map[string]bool, most int) ([]*models.JournalLink, error) {
	var links []*models.JournalLink
	seen := map[string]bool{}
	for _, choice := range choices {
		reference := strings.TrimSpace(choice.Reference)
		reference = strings.TrimSuffix(strings.TrimPrefix(reference, "[["), "]]")
		if !allowed[reference] {
			return nil, &journalRefusal{reason: fmt.Sprintf("the choice %q is not one of the records you may choose from", reference)}
		}
		reason := oneLineForModel(choice.Reason, journalReasonCharacters)
		if reason == "" {
			return nil, &journalRefusal{reason: fmt.Sprintf("the choice %s has no reason", reference)}
		}
		if seen[reference] || len(links) == most {
			continue
		}
		seen[reference] = true
		kind, recordId, _ := strings.Cut(reference, ":")
		links = append(links, &models.JournalLink{RecordKind: models.AgentRecordKind(kind), RecordID: recordId, LinkReason: reason})
	}
	return links, nil
}

// checkJournalHeadline keeps a headline to one line, and refuses an empty
// headline or narrative.
func checkJournalHeadline(headline, narrative string) (string, error) {
	headline = oneLineForModel(headline, journalHeadlineCharacters)
	if headline == "" || strings.TrimSpace(narrative) == "" {
		return "", &journalRefusal{reason: "the headline and the narrative must not be empty"}
	}
	return headline, nil
}

// checkJournalDayAnswer accepts an answer for a day or refuses it with a
// reason. The writing it returns has no digest yet.
func checkJournalDayAnswer(answer *journalDayAnswer, input *journalDayInput) (*models.JournalNarrativeWriting, error) {
	headline, err := checkJournalHeadline(answer.Headline, answer.Narrative)
	if err != nil {
		return nil, err
	}
	narrative := strings.TrimSpace(answer.Narrative)
	if err := checkJournalMentions(narrative, input.allowed); err != nil {
		return nil, err
	}
	links, err := checkJournalChoices(answer.Choices, input.candidates, journalChoicesMost)
	if err != nil {
		return nil, err
	}
	cover := strings.TrimPrefix(strings.TrimSpace(answer.CoverPhotoID), string(models.RecordPhoto)+":")
	if cover != "" && !input.photos[cover] {
		return nil, &journalRefusal{reason: fmt.Sprintf("the cover photo %q is not one of the photos in the list", cover)}
	}
	return &models.JournalNarrativeWriting{Headline: headline, Narrative: narrative, CoverPhotoID: cover, Choices: links}, nil
}

// askJournalAnswer asks for a JSON answer and checks it. An answer that cannot
// be read or is refused by check is asked for once more, told why; a second
// refusal comes back as a *journalRefusal. refusedCount counts the answers
// thrown away. A failure to get an answer at all is an error that is not a
// refusal and leaves the target as it was.
func askJournalAnswer[Answer any, Result any](ctx context.Context, ask journalAsk, title, prompt string, required []string, check func(*Answer) (Result, error)) (Result, int, error) {
	var none, result Result
	refusedCount := 0
	for attempt := 0; ; attempt++ {
		said, err := ask(ctx, title, prompt)
		if err != nil {
			return none, refusedCount, err
		}
		var refusal error
		answer := readModelAnswer[Answer](said, required...)
		if answer.IsValid {
			result, refusal = check(&answer.Value)
			if refusal == nil {
				return result, refusedCount, nil
			}
			var isRefusal *journalRefusal
			if !errors.As(refusal, &isRefusal) {
				return none, refusedCount, refusal
			}
		} else {
			refusal = &journalRefusal{reason: "the answer is not a JSON object that can be read (" + answer.Problem + ")"}
		}
		refusedCount++
		if attempt == 1 {
			return none, refusedCount, refusal
		}
		prompt += "\n\nYour earlier answer was refused: " + refusal.Error() + ". Answer again with the whole JSON object, and mention or choose only what the lists above give."
	}
}

// writeJournalDay writes the narrative of one day. It reads the day, asks,
// checks, and stores the narrative with the digest of what it was read from,
// and the day's numbers again, so that late data updates them. A day with
// nothing on it, and no words, is not written about; when the person asked for
// it (isRequested) the reason is stored, which also ends the wait.
func (self *Agent) writeJournalDay(ctx context.Context, run *Run, day string, isRequested bool, ask journalAsk, counts *journalCounts) error {
	var read *journal.Day
	var access *journalAccess
	if err := run.Database().TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		if access, err = readJournalAccess(transaction, run.Owner.ID); err != nil {
			return err
		}
		read, err = journal.ReadDay(transaction, run.Owner, run.Agent, day, time.Now(), access.options())
		return err
	}); err != nil {
		return err
	}
	return self.writeReadJournalDay(ctx, run, read, access, isRequested, ask, counts)
}

// writeReadJournalDay is writeJournalDay for a day already read.
func (self *Agent) writeReadJournalDay(ctx context.Context, run *Run, read *journal.Day, access *journalAccess, isRequested bool, ask journalAsk, counts *journalCounts) error {
	target := models.JournalTarget{Day: read.Day}
	hasWords := read.Row != nil && strings.TrimSpace(read.Row.Words) != ""
	if read.RecordCount() == 0 && !hasWords {
		if !isRequested {
			return nil
		}
		return self.storeJournalFailure(ctx, run, target, read.TimeZone, "there is nothing on this day to write about yet")
	}
	input := newJournalDayInput(read)
	prompt, err := input.prompt(run.Owner, run.Agent, access.options())
	if err != nil {
		return err
	}
	writing, refusedCount, err := askJournalAnswer(ctx, ask, "Wrote the journal of "+read.Day, prompt, []string{"headline", "narrative"},
		func(answer *journalDayAnswer) (*models.JournalNarrativeWriting, error) {
			return checkJournalDayAnswer(answer, input)
		})
	counts.refusedCount += refusedCount
	var refusal *journalRefusal
	if errors.As(err, &refusal) {
		return self.storeJournalFailure(ctx, run, target, read.TimeZone, "the writing was refused twice: "+refusal.reason)
	}
	if err != nil {
		return err
	}
	writing.InputDigest = journal.InputDigest(read)
	now := time.Now()
	if err := run.Database().TransactionContext(ctx, func(transaction db.Transaction) error {
		if err := transaction.PutJournalNarrative(run.Owner.ID, target, read.TimeZone, *writing, now); err != nil {
			return err
		}
		return transaction.PutJournalFacts(run.Owner.ID, read.Day, read.TimeZone, journalFactsToStore(journal.ComputeFacts(read), read, access.canAgentReadHealth), now)
	}); err != nil {
		return err
	}
	counts.daysWritten++
	return nil
}

// storeJournalFailure keeps why a writing was refused, which also clears the
// wait for it.
func (self *Agent) storeJournalFailure(ctx context.Context, run *Run, target models.JournalTarget, timeZone, failure string) error {
	return run.Database().TransactionContext(ctx, func(transaction db.Transaction) error {
		return transaction.PutJournalNarrativeFailure(run.Owner.ID, target, timeZone, failure, time.Now())
	})
}
