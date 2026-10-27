package journal_test

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

func readWorldDay(t *testing.T, tx db.Transaction, world *world, options journal.ReadOptions) *journal.Day {
	t.Helper()
	day, err := journal.ReadDay(tx, world.user, world.agent, worldDay, local(22, 0).Add(5*time.Hour), options)
	if err != nil {
		t.Fatalf("ReadDay: %s", err)
	}
	return day
}

// A day holds what happened in the person's clock from midnight to
// midnight, from every source, and nothing from the day on either side.
func TestReadDayHoldsEverythingOfTheDayInThePersonsClock(t *testing.T) {
	world, release := newWorld(t)
	defer release()

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		day := readWorldDay(t, tx, world, journal.AllReadOptions())

		if day.Day != worldDay || day.TimeZone != "America/New_York" || !day.From.Equal(local(0, 0)) || !day.Until.Equal(later(0, 0)) {
			t.Fatalf("the day's edges: %s %s %s %s", day.Day, day.TimeZone, day.From, day.Until)
		}
		if day.Row != nil || len(day.UnavailableSections) != 0 {
			t.Errorf("nothing is kept for the day and every section is open: %+v %v", day.Row, day.UnavailableSections)
		}

		// Coding sessions: the one session, from its start to its last post.
		if len(day.CodingSessions) != 1 {
			t.Fatalf("one coding session: %+v", day.CodingSessions)
		}
		session := day.CodingSessions[0]
		if session.DocumentID != world.codingId || session.Assistant != "claude-code" || session.Directory != "~/src/harbor" || session.Title != "Fix the tide chart" ||
			!session.StartedAt.Equal(local(9, 0)) || !session.EndedAt.Equal(local(9, 45)) {
			t.Errorf("the coding session: %+v", session)
		}

		// Documents: the chat she took part in and the page she wrote, not
		// her coding session, the chat she was not in, a remark, a page of
		// somebody else's, a page nobody signed, the day before, or what the
		// other sections read.
		var titles []string
		for _, document := range day.Documents {
			titles = append(titles, document.Title)
		}
		if fmt.Sprint(titles) != "[harbor-team: 22 Oct Tide tables]" || day.DocumentTotal != 2 {
			t.Errorf("the documents of the day: %v of %d", titles, day.DocumentTotal)
		}
		if day.Documents[0].SourceName != "Harbor wiki and chat" {
			t.Errorf("a document names its source: %+v", day.Documents[0])
		}
		if len(day.JournalNotes) != 1 || day.JournalNotes[0].DocumentID != world.journalNoteId || day.JournalNotes[0].Text != "Windy on the quay. Fixed the tide chart." {
			t.Errorf("her own note: %+v", day.JournalNotes)
		}

		if len(day.Conversations) != 1 || day.Conversations[0].Title != "Trip plans" || day.Conversations[0].MessageCount != 2 ||
			day.Conversations[0].FirstMessageID != world.firstConversationMessageId {
			t.Errorf("the conversation with the agent, not what was said after midnight: %+v", day.Conversations)
		}
		if len(day.Chats) != 1 || day.Chats[0].ChatName != "Ben Quay" || day.Chats[0].MessageCount != 2 || day.Chats[0].FirstMessageID != world.firstChatMessageId || day.Chats[0].ConversationID == "" {
			t.Errorf("the chat with Ben: %+v", day.Chats)
		}

		// Mail: the two that arrived and the one sent; the one in the trash,
		// the note and yesterday's are not there. The important one is marked.
		if day.MailReceivedTotal != 2 || day.MailSentTotal != 1 || len(day.Mail) != 3 {
			t.Fatalf("mail: %+v received %d sent %d", day.Mail, day.MailReceivedTotal, day.MailSentTotal)
		}
		subjects := []string{}
		for _, mail := range day.Mail {
			subjects = append(subjects, mail.Subject)
		}
		if fmt.Sprint(subjects) != "[Contract needs a reply Re: Contract needs a reply Weekly news]" {
			t.Errorf("mail in order: %v", subjects)
		}
		if mail := day.Mail[0]; mail.ItemID != world.importantItemId || !mail.IsImportant || mail.Counterpart != "Sam Reed" || mail.IsSent {
			t.Errorf("the mail that needs a reply: %+v", mail)
		}
		if mail := day.Mail[1]; mail.ItemID != world.sentItemId || !mail.IsSent || mail.IsImportant || !strings.Contains(mail.Counterpart, "Sam Reed") {
			t.Errorf("the mail she sent: %+v", mail)
		}
		if mail := day.Mail[2]; mail.ItemID != world.plainItemId || mail.IsImportant || mail.Counterpart != "news@example.test" {
			t.Errorf("the plain mail: %+v", mail)
		}

		if len(day.Recordings) != 1 || day.Recordings[0].Title != "Harbor standup" || day.Recordings[0].Summary != "Tides and lunch." || day.Recordings[0].DurationMS != 900000 || !day.Recordings[0].RecordedAt.Equal(local(14, 0)) {
			t.Errorf("the recording: %+v", day.Recordings)
		}

		// Photos: the one at 23:30 on her clock is on the day though it is the
		// next day in Greenwich; the one at ten past midnight is not.
		if day.PhotoTotal != 2 || len(day.Photos) != 2 || day.Photos[0].PhotoID != world.photoIds["morning"] || day.Photos[1].PhotoID != world.photoIds["late"] {
			t.Errorf("the photos of the day: %+v of %d", day.Photos, day.PhotoTotal)
		}
		if fmt.Sprint(day.PhotoCandidateIDs) != fmt.Sprint([]string{world.photoIds["morning"], world.photoIds["late"]}) {
			t.Errorf("the photos offered to stand for the day: %v", day.PhotoCandidateIDs)
		}

		// Visits: the night at home that ended at six, the cafe and the office.
		var places []string
		for _, visit := range day.Visits {
			places = append(places, visit.PlaceName)
		}
		if fmt.Sprint(places) != "[Home Harbor Cafe Harbor Office]" {
			t.Errorf("the visits that overlap the day: %v", places)
		}

		// Events: the holiday that is a date, then the meeting; not the next
		// day's holiday, and not the one called off.
		if len(day.Events) != 2 || day.Events[0].Title != "Harbor day" || !day.Events[0].IsAllDay || day.Events[1].Title != "Tide review" || day.Events[1].IsAllDay ||
			day.Events[1].CalendarName != "Work" || day.Events[1].Location != "Dock 4" || !day.Events[1].StartsAt.Equal(local(15, 0)) {
			t.Errorf("the events: %+v", day.Events)
		}
		var reminders []string
		for _, reminder := range day.Reminders {
			reminders = append(reminders, reminder.Title)
		}
		if fmt.Sprint(reminders) != "[Call the bank Water the plants]" {
			t.Errorf("the reminders due or done that day: %v", reminders)
		}
		if reminder := day.Reminders[1]; reminder.DoneAt == nil || !reminder.DoneAt.Equal(local(18, 30)) {
			t.Errorf("a reminder done that day: %+v", reminder)
		}

		// Money: the groceries, the refund and the coffee that was bought that
		// day and posted the next; not what posted the day before.
		var amounts []string
		for _, transaction := range day.Transactions {
			amounts = append(amounts, fmt.Sprintf("%s %d", transaction.Description, transaction.AmountMinorUnits))
		}
		if fmt.Sprint(amounts) != "[Corner Grocer -4217 REFUND 1000 QUAY COFFEE -350]" {
			t.Errorf("the transactions of the day: %v", amounts)
		}
		if day.Transactions[0].AccountName != "Everyday" || day.Transactions[0].Currency != "USD" || day.Transactions[0].TransactionID != world.transactionIds["CORNER GROCER"] {
			t.Errorf("a transaction: %+v", day.Transactions[0])
		}

		health := day.Health
		if health == nil {
			t.Fatal("Health of the day")
		}
		if health.StepCount != 8200 || health.WalkedMeters != 5400 || health.RestingHeartRate != 58 || health.SleepMinutes != 420 || health.MoodLevel != 4 {
			t.Errorf("the numbers of Health: %+v", health)
		}
		if fmt.Sprint(health.SleepStages) != "[{deep 90} {core 240} {rem 90} {awake 20}]" {
			t.Errorf("the night's stages: %v", health.SleepStages)
		}
		if len(health.Workouts) != 1 || health.Workouts[0].ActivityType != "running" || health.Workouts[0].Minutes != 40 || health.Workouts[0].Meters != 5200 {
			t.Errorf("the workout: %+v", health.Workouts)
		}
		if len(health.StepsByHour) != 24 || health.StepsByHour[8] != 1200 || health.StepsByHour[18] != 900 || health.StepsByHour[0] != 0 {
			t.Errorf("steps by the hour: %v", health.StepsByHour)
		}

		if len(day.Notes) != 1 || day.Notes[0].NoteID != world.noteIdentifier || day.Notes[0].Title != "Packing list" {
			t.Errorf("the note written that day: %+v", day.Notes)
		}
		if len(day.MemoryPages) != 1 || day.MemoryPages[0].Path != "places/harbor-cafe" || day.MemoryPages[0].FactCount != 2 {
			t.Errorf("the memory page with facts from the day, not the one known to the month: %+v", day.MemoryPages)
		}
	})
}

// A section the caller may not read is left out and named, and a person
// whose Health the agent may not read has none in the day the dream reads.
func TestReadDayLeavesOutWhatTheCallerMayNotRead(t *testing.T) {
	world, release := newWorld(t)
	defer release()

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		options := journal.AllReadOptions()
		options.CanReadPhotos, options.CanReadMail, options.CanReadAgent = false, false, false
		day := readWorldDay(t, tx, world, options)
		if len(day.Photos) != 0 || day.PhotoTotal != 0 || len(day.Mail) != 0 || len(day.CodingSessions) != 0 || len(day.Documents) != 0 ||
			len(day.Conversations) != 0 || len(day.Recordings) != 0 || len(day.Transactions) != 0 || len(day.Notes) != 0 || len(day.MemoryPages) != 0 {
			t.Errorf("sections the caller may not read are empty: %+v", day)
		}
		want := "[codingSessions documents conversations mail recordings photos transactions notes memoryPages]"
		if fmt.Sprint(day.UnavailableSections) != want {
			t.Errorf("unavailable sections: %v, want %s", day.UnavailableSections, want)
		}
		if len(day.Visits) != 3 || len(day.Events) != 2 || day.Health == nil || len(day.Chats) != 1 {
			t.Errorf("what the caller may read is still there: %d visits %d events %v %d chats", len(day.Visits), len(day.Events), day.Health, len(day.Chats))
		}

		// Health left out by the dream is not a section the person lacks.
		options = journal.AllReadOptions()
		options.ShouldReadHealth = false
		day = readWorldDay(t, tx, world, options)
		if day.Health != nil || len(day.UnavailableSections) != 0 {
			t.Errorf("Health left out for the agent: %+v %v", day.Health, day.UnavailableSections)
		}
		if facts := journal.ComputeFacts(day); facts.StepCount != 0 || facts.SleepMinutes != 0 || facts.HealthMoodLevel != 0 {
			t.Errorf("no Health numbers in the facts: %+v", facts)
		}

		// A person who may not read Health at all lists it.
		options = journal.AllReadOptions()
		options.CanReadHealth = false
		if day = readWorldDay(t, tx, world, options); fmt.Sprint(day.UnavailableSections) != "[health]" {
			t.Errorf("Health the person may not read: %v", day.UnavailableSections)
		}

		// A person with no agent has no agent sections.
		withoutAgent, err := journal.ReadDay(tx, world.user, nil, worldDay, local(22, 0), journal.AllReadOptions())
		if err != nil {
			t.Fatal(err)
		}
		if len(withoutAgent.CodingSessions) != 0 || fmt.Sprint(withoutAgent.UnavailableSections) != "[codingSessions documents conversations recordings transactions memoryPages]" {
			t.Errorf("no agent: %v", withoutAgent.UnavailableSections)
		}
	})
}

// The agent reads only what the person gave it: the mailboxes granted to it
// (their mail and notes), the calendars given to it, the chats it may
// remember, and the transactions of the finance sources that are on. The
// person reads all of it.
func TestReadDayGivesTheAgentOnlyWhatThePersonGaveIt(t *testing.T) {
	world, release := newWorld(t)
	defer release()

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		agentOptions := journal.AllReadOptions()
		agentOptions.IsReadByAgent = true
		day := readWorldDay(t, tx, world, agentOptions)
		if len(day.Mail) != 0 || day.MailReceivedTotal != 0 || len(day.Notes) != 0 || len(day.Events) != 0 || len(day.Reminders) != 0 || len(day.Chats) != 0 {
			t.Errorf("the agent read what it was not given: %d mail, %d notes, %d events, %d reminders, %d chats",
				len(day.Mail), len(day.Notes), len(day.Events), len(day.Reminders), len(day.Chats))
		}
		if len(day.Transactions) == 0 || len(day.Photos) == 0 || len(day.Visits) == 0 {
			t.Errorf("the agent lost what is its own to read: %d transactions, %d photos, %d visits", len(day.Transactions), len(day.Photos), len(day.Visits))
		}
		if len(day.UnavailableSections) != 0 {
			t.Errorf("nothing is unavailable, only not given: %v", day.UnavailableSections)
		}

		mailboxes, err := tx.ListMailboxes(world.user.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, mailbox := range mailboxes {
			if _, err := tx.UpdateMailbox(mailbox.ID, func(updated *models.Mailbox) error {
				updated.Agent = &models.AgentMailbox{Granted: true}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		calendars, err := tx.ListCalendars(world.user.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, calendar := range calendars {
			calendar.AgentGranted = true
			if _, err := tx.UpdateCalendar(calendar); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := tx.SetChatAgentReading(world.chatId, world.user.ID, true); err != nil {
			t.Fatal(err)
		}
		sources, err := tx.ListAgentSources(world.agent.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, source := range sources {
			if source.Name == "bank" {
				source.Enabled = false
				if _, err := tx.PutAgentSource(source); err != nil {
					t.Fatal(err)
				}
			}
		}
		given := readWorldDay(t, tx, world, agentOptions)
		whole := readWorldDay(t, tx, world, journal.AllReadOptions())
		if len(given.Mail) != len(whole.Mail) || len(given.Notes) != len(whole.Notes) || len(given.Events) != len(whole.Events) ||
			len(given.Reminders) != len(whole.Reminders) || len(given.Chats) != len(whole.Chats) || len(whole.Mail) == 0 || len(whole.Chats) == 0 {
			t.Errorf("what was given: %d/%d mail, %d/%d notes, %d/%d events, %d/%d reminders, %d/%d chats", len(given.Mail), len(whole.Mail),
				len(given.Notes), len(whole.Notes), len(given.Events), len(whole.Events), len(given.Reminders), len(whole.Reminders), len(given.Chats), len(whole.Chats))
		}
		if len(given.Transactions) != 0 || len(whole.Transactions) == 0 {
			t.Errorf("a finance source switched off: the agent read %d, the person %d", len(given.Transactions), len(whole.Transactions))
		}
	})
}

// The stored row and the links ride along with the day, and the day is cut
// in the zone it was made in.
func TestReadDayCarriesTheRowAndTheLinks(t *testing.T) {
	world, release := newWorld(t)
	defer release()

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		none := readWorldDay(t, tx, world, journal.AllReadOptions())
		if none.Row != nil || len(none.Links) != 0 {
			t.Errorf("nothing is kept for the day: %+v", none.Row)
		}

		moodLevel := models.JournalMoodGood
		if _, err := tx.SetJournalWords(world.user.ID, worldDay, "Europe/London", "A windy day.", nil, local(22, 0)); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.SetJournalMood(world.user.ID, worldDay, "Europe/London", &moodLevel, "", models.JournalMoodFromPerson, local(22, 0)); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.PinJournalRecord(world.user.ID, worldDay, "Europe/London", models.RecordMail, world.importantItemId, "needed a reply", local(22, 0)); err != nil {
			t.Fatal(err)
		}
		day, err := journal.ReadDay(tx, world.user, world.agent, worldDay, local(22, 0), journal.AllReadOptions())
		if err != nil {
			t.Fatal(err)
		}
		if day.Row == nil || day.Row.Words != "A windy day." || len(day.Links) != 1 || day.Links[0].LinkReason != "needed a reply" {
			t.Errorf("the row and the links: %+v %+v", day.Row, day.Links)
		}
		// The day was cut in the zone it was made in, not the person's zone now.
		if day.TimeZone != "Europe/London" || !day.From.Equal(time.Date(2026, 10, 22, 0, 0, 0, 0, mustZone("Europe/London"))) {
			t.Errorf("the zone the day was made in: %s %s", day.TimeZone, day.From)
		}
		if _, err := journal.ReadDay(tx, world.user, world.agent, "22 October", local(22, 0), journal.AllReadOptions()); err == nil {
			t.Error("a day written another way is refused")
		}
	})
}

func TestComputeFactsFillsEveryNumberOfTheDay(t *testing.T) {
	world, release := newWorld(t)
	defer release()

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		facts := journal.ComputeFacts(readWorldDay(t, tx, world, journal.AllReadOptions()))
		want := models.JournalFacts{
			PhotoCount: 2, PhotoCandidateIDs: []string{world.photoIds["morning"], world.photoIds["late"]},
			PlaceCount: 3, VisitedPlaceIDs: nil,
			WalkedMeters: 5400, StepCount: 8200, SleepMinutes: 420, WorkoutMinutes: 40,
			CodingSessionCount: 1, WorkProjectCount: 1, CodingMinutes: 10, MeetingCount: 1, MeetingMinutes: 60,
			RecordingCount: 1, ConversationCount: 1, ChatMessageCount: 2, MailReceivedCount: 2, MailSentCount: 1, DocumentCount: 2,
			TransactionCount: 3, SpentMinorUnits: 4567, SpentCurrency: "USD", ShoppingTripCount: 0, MemoryPageCount: 1,
			HasActivity: true, MainPlaceID: world.officeId, MainPlaceName: "Harbor Office", MainPlaceLatitude: 40.7, MainPlaceLongitude: -74.0,
			HealthMoodLevel: 4, FactsVersion: models.JournalFactsVersion,
		}
		if len(facts.VisitedPlaceIDs) != 3 || facts.VisitedPlaceIDs[1] != world.harborId || facts.VisitedPlaceIDs[2] != world.officeId {
			t.Errorf("the places visited, in the order reached: %v", facts.VisitedPlaceIDs)
		}
		facts.VisitedPlaceIDs = nil
		if fmt.Sprintf("%+v", facts) != fmt.Sprintf("%+v", want) {
			t.Errorf("facts:\n got %+v\nwant %+v", facts, want)
		}

		// An empty day has no activity, and a day with only words has.
		empty, err := journal.ReadDay(tx, world.user, world.agent, "2026-09-01", local(22, 0), journal.AllReadOptions())
		if err != nil {
			t.Fatal(err)
		}
		if journal.ComputeFacts(empty).HasActivity {
			t.Error("a day with nothing in it has no activity")
		}
		empty.Row = &models.JournalDay{Words: "Stayed in."}
		if !journal.ComputeFacts(empty).HasActivity {
			t.Error("the person's words are something")
		}
	})
}

func TestMomentsAreInTheOrderTheDayHappened(t *testing.T) {
	world, release := newWorld(t)
	defer release()

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		moments := journal.Moments(readWorldDay(t, tx, world, journal.AllReadOptions()))
		var line []string
		for _, moment := range moments {
			line = append(line, moment.At.In(worldZone).Format("15:04")+" "+string(moment.RecordKind))
		}
		// Everything is sorted by time, and the same time keeps the order sources are read in.
		for index := 1; index < len(moments); index++ {
			if moments[index].At.Before(moments[index-1].At) {
				t.Fatalf("out of order at %d: %v", index, line)
			}
		}
		byKind := map[models.AgentRecordKind][]journal.Moment{}
		for _, moment := range moments {
			byKind[moment.RecordKind] = append(byKind[moment.RecordKind], moment)
		}
		if photos := byKind[models.RecordPhoto]; len(photos) != 2 || photos[0].RecordID != world.photoIds["morning"] || photos[0].DashboardPath != "/photos/"+world.photoIds["morning"] {
			t.Errorf("each photo taken far apart is its own moment: %+v", photos)
		}
		// Only the message that mattered is a moment of the day; the others
		// are in the mail section.
		if mail := byKind[models.RecordMail]; len(mail) != 1 || mail[0].RecordID != world.importantItemId || mail[0].Detail != "from Sam Reed" {
			t.Errorf("mail moments: %+v", mail)
		}
		if visits := byKind[models.RecordPlaceVisit]; len(visits) != 3 || visits[0].Title != "Home" || !visits[0].At.Equal(local(0, 0)) || visits[0].Detail != "5 h" || visits[1].Title != "Harbor Cafe" || visits[1].Detail != "3 h" ||
			visits[1].DashboardPath != "/location?day=2026-10-22&place="+world.harborId {
			t.Errorf("visit moments: %+v", visits)
		}
		if events := byKind[models.RecordCalendarOccurrence]; len(events) != 2 || events[0].Title != "Harbor day" || events[0].Detail != "all day, Dock 4" || !events[0].At.Equal(local(0, 0)) ||
			events[1].RecordID != world.meetingId+"@"+local(15, 0).UTC().Format(time.RFC3339) {
			t.Errorf("event moments: %+v", events)
		}
		if transactions := byKind[models.RecordFinanceTransaction]; len(transactions) != 3 || transactions[0].Title != "Corner Grocer" || transactions[0].Detail != "-42.17 USD" || !transactions[0].At.Equal(local(0, 0)) || transactions[2].Title != "QUAY COFFEE" || !transactions[2].At.Equal(local(8, 15)) {
			t.Errorf("transaction moments: %+v", transactions)
		}
		if chats := byKind[models.RecordChatMessage]; len(chats) != 1 || chats[0].RecordID != world.firstChatMessageId || !strings.HasPrefix(chats[0].DashboardPath, "/chat?conversation=") {
			t.Errorf("chat moments: %+v", chats)
		}
		// A project is one moment, named after its folder; a document that
		// nobody chose is not one.
		if coding := byKind[models.RecordAgentDocument]; len(coding) != 1 || coding[0].Title != "harbor" || coding[0].Detail != "10 min" {
			t.Errorf("document moments: %+v", coding)
		}
		if workouts := byKind[models.RecordHealthDay]; len(workouts) != 1 || workouts[0].Title != "Running" || workouts[0].RecordID != "workout@"+worldDay {
			t.Errorf("workout moments: %+v", workouts)
		}
	})
}

// What a narrative is written from is fingerprinted, and the fingerprint
// moves when that changes and holds still when only other things do.
func TestInputDigestMovesWithWhatTheNarrativeIsWrittenFrom(t *testing.T) {
	world, release := newWorld(t)
	defer release()

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		options := journal.AllReadOptions()
		first := journal.InputDigest(readWorldDay(t, tx, world, options))
		if again := journal.InputDigest(readWorldDay(t, tx, world, options)); again != first || len(first) != 64 {
			t.Fatalf("the same day is the same digest: %s %s", first, again)
		}

		// Health left out is a different input.
		withoutHealth := options
		withoutHealth.ShouldReadHealth = false
		if journal.InputDigest(readWorldDay(t, tx, world, withoutHealth)) == first {
			t.Error("a day read without Health is not the day read with it")
		}

		// The person's words, mood and corrections are inputs; the touched-up
		// version, the source of the mood, the weather and the story are not.
		// The day is made in the zone it is read in.
		zone := journal.ZoneName(world.user)
		digestOf := func() string {
			return journal.InputDigest(readWorldDay(t, tx, world, options))
		}
		if _, err := tx.SetJournalWords(world.user.ID, worldDay, zone, "A windy day.", nil, local(22, 0)); err != nil {
			t.Fatal(err)
		}
		moodLevel := models.JournalMoodGood
		if _, err := tx.SetJournalMood(world.user.ID, worldDay, zone, &moodLevel, "", models.JournalMoodFromPerson, local(22, 0)); err != nil {
			t.Fatal(err)
		}
		base := digestOf()
		if base == first {
			t.Error("words and a mood are part of the input")
		}
		if err := tx.RequestJournalTouchUp(world.user.ID, models.JournalTarget{Day: worldDay}, zone, nil, local(22, 0)); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.PutJournalTouchedUp(world.user.ID, models.JournalTarget{Day: worldDay}, "A breezy day.", models.JournalWordsDigest("A windy day.", "")); err != nil {
			t.Fatal(err)
		}
		if err := tx.PutJournalWeather(world.user.ID, worldDay, &models.JournalWeather{WeatherCode: 3, HighCelsius: 14, FetchedAt: local(22, 0)}); err != nil {
			t.Fatal(err)
		}
		if err := tx.PutJournalNarrative(world.user.ID, models.JournalTarget{Day: worldDay}, zone,
			models.JournalNarrativeWriting{Headline: "Windy", Narrative: "A story.", InputDigest: base}, local(22, 0)); err != nil {
			t.Fatal(err)
		}
		if digestOf() != base {
			t.Error("the touched-up words, the weather and the story itself are not inputs")
		}
		saved, err := tx.GetJournalDay(world.user.ID, worldDay)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.SetJournalWords(world.user.ID, worldDay, zone, "A windy day with a long walk.", saved.WordsModifiedAt, local(22, 0)); err != nil {
			t.Fatal(err)
		}
		changed := digestOf()
		if err := tx.AddJournalCorrection(world.user.ID, models.JournalTarget{Day: worldDay}, zone, "It was Sam at lunch.", local(22, 0)); err != nil {
			t.Fatal(err)
		}
		corrected := digestOf()
		if changed == base || corrected == changed {
			t.Error("the words and a correction are inputs")
		}
	})
}
