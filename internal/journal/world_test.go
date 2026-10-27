package journal_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// The invented person every test of reading a day is about: Ada, who lives
// in New York, where on 22 October 2026 the clock was four hours behind
// Greenwich. The day has every kind of record in it, and just outside it on
// both sides there are records that are not on it.
const worldDay = "2026-10-22"

var worldZone = mustZone("America/New_York")

func mustZone(name string) *time.Location {
	location, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return location
}

// local is a time on Ada's clock on the day, and later is the same on the
// next day.
func local(hour, minute int) time.Time { return time.Date(2026, 10, 22, hour, minute, 0, 0, worldZone) }
func later(hour, minute int) time.Time { return time.Date(2026, 10, 23, hour, minute, 0, 0, worldZone) }
func earlier(hour, minute int) time.Time {
	return time.Date(2026, 10, 21, hour, minute, 0, 0, worldZone)
}

type world struct {
	database db.Database

	user  *models.User
	agent *models.Agent
	ben   *models.User

	codingId, chatUnitId, wikiId, journalNoteId string
	conversationId, firstConversationMessageId  string
	chatId, firstChatMessageId                  string
	importantItemId, plainItemId, sentItemId    string
	recordingId                                 string
	photoIds                                    map[string]string
	visitIds                                    map[string]string
	harborId, officeId                          string
	meetingId, holidayId                        string
	dueReminderId, doneReminderId               string
	transactionIds                              map[string]string
	noteIdentifier                              string
	memoryNodeId                                string
}

func photoDigest(label string) string {
	digest := sha256.Sum256([]byte(label))
	return hex.EncodeToString(digest[:])
}

const reminderTemplate = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//journal test//EN
BEGIN:VTODO
UID:%s
DTSTAMP:20261001T000000Z
SUMMARY:%s
%s
END:VTODO
END:VCALENDAR
`

// newWorld stores Ada's sources. Times a store sets for itself are put right
// afterward.
func newWorld(t *testing.T) (*world, func()) {
	t.Helper()
	database, release := dbtest.AcquireDatabase(t)
	self := &world{database: database, photoIds: map[string]string{}, visitIds: map[string]string{}, transactionIds: map[string]string{}}
	var updates []string
	update := func(format string, arguments ...any) { updates = append(updates, fmt.Sprintf(format, arguments...)) }
	stamp := func(at time.Time) string { return at.UTC().Format(time.RFC3339Nano) }
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		self.user, err = tx.CreateUser(&models.User{Username: "ada", Name: "Ada Harbor", Timezone: "America/New_York"})
		check(err)
		self.ben, err = tx.CreateUser(&models.User{Username: "ben", Name: "Ben Quay"})
		check(err)
		self.agent, err = tx.CreateAgent(&models.Agent{UserID: self.user.ID, Enabled: true, Name: "Bertie"})
		check(err)
		_, err = tx.CreateAgent(&models.Agent{UserID: self.ben.ID, Enabled: true, Name: "Basil"})
		check(err)

		// Documents: a coding session, work chat she took part in and work
		// chat she did not, a wiki page she wrote and one she did not, and a
		// note from her notes folder. A location day and a recording passage
		// are filed too and belong to other sections.
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: self.agent.ID, Kind: models.SourceArchive, Name: "Harbor wiki and chat", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{Computer: "laptop", Path: "~/records", Format: models.FormatRecords},
		})
		check(err)
		locationSource, err := tx.EnsureLocationSource(self.agent.ID)
		check(err)
		put := func(sourceId, externalId string, kind models.AgentDocumentKind, title string, at time.Time, metadata map[string]any, text string) *models.AgentDocument {
			happenedAt := at
			modifiedAt := at.Add(time.Minute)
			document, err := tx.PutAgentDocument(&models.AgentDocument{
				AgentID: self.agent.ID, SourceID: sourceId, ExternalID: externalId, Kind: kind, Title: title,
				Hash: "hash-" + externalId, HappenedAt: &happenedAt, ModifiedAt: &modifiedAt, Metadata: metadata,
			})
			check(err)
			if text != "" {
				check(tx.ReplaceAgentChunks(document, []*models.AgentChunk{{Text: text}}))
			}
			return document
		}
		self.codingId = put(source.ID, "claude#1", models.DocumentChat, "Claude Code in harbor", local(9, 0),
			map[string]any{"assistant": "Claude Code", "directory": "~/src/harbor", "channel": "Fix the tide chart", "participants": []any{"ada", "Claude Code"}, "posts": 4},
			// A coding session's clock is UTC: 9:00 in New York is 13:00.
			"13:00 ada: the tide chart is off by an hour\n13:20 Claude Code: found it, fixing\n13:45 ada: thanks").ID
		self.chatUnitId = put(source.ID, "team#1", models.DocumentChat, "harbor-team: 22 Oct", local(10, 30),
			map[string]any{"channel": "harbor-team", "participants": []any{"ada", "ben"}, "posts": 3}, "14:30 ada: lunch?").ID
		put(source.ID, "team#2", models.DocumentChat, "other-team: 22 Oct", local(10, 45),
			map[string]any{"channel": "other-team", "participants": []any{"zed", "yan"}, "posts": 5}, "14:45 zed: hello")
		put(source.ID, "team#3", models.DocumentChat, "harbor-team: a remark", local(10, 50),
			map[string]any{"channel": "harbor-team", "participants": []any{"ada"}, "posts": 1}, "14:50 ada: ok")
		self.wikiId = put(source.ID, "wiki#1", models.DocumentPage, "Tide tables", local(11, 0),
			map[string]any{"author": "Ada Harbor"}, "The tide tables.").ID
		put(source.ID, "wiki#2", models.DocumentPage, "Mooring rules", local(11, 5), map[string]any{"author": "Zed Mooring"}, "The rules.")
		put(source.ID, "wiki#3", models.DocumentPage, "Anonymous page", local(11, 6), nil, "Nobody signed it.")
		put(source.ID, "yesterday#1", models.DocumentChat, "harbor-team: yesterday", earlier(23, 50),
			map[string]any{"channel": "harbor-team", "participants": []any{"ada", "ben"}, "posts": 3}, "03:50 ada: late")
		put(locationSource.ID, "location:2026-10-22", models.DocumentJournal, "Where they went", local(0, 0), nil, "Harbor Cafe, Office.")
		// A note from her notes folder is dated at noon UTC of its date.
		self.journalNoteId = put(source.ID, "notes/2026-10-22.md", models.DocumentJournal, "22 October", time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC),
			nil, "Windy on the quay. Fixed the tide chart.").ID

		// Conversations with the agent.
		conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: self.agent.ID, Kind: models.AgentConversationNamed, Title: "Trip plans", LastAt: time.Now()})
		check(err)
		self.conversationId = conversation.ID
		var messageIds []string
		for _, content := range []string{"Where should we stay?", "Near the harbor.", "Book it."} {
			role := "user"
			if content == "Near the harbor." {
				role = "assistant"
			}
			message, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: role, Content: content})
			check(err)
			messageIds = append(messageIds, message.ID)
		}
		self.firstConversationMessageId = messageIds[0]
		for index, at := range []time.Time{local(13, 0), local(13, 1), later(0, 5)} {
			update(`UPDATE "agent_message" SET "created_at" = '%s' WHERE "id" = '%s'`, stamp(at), messageIds[index])
		}

		// A chat with Ben, in which two things were said that day.
		chat, err := tx.OpenDirectChat(self.user.ID, self.ben.ID)
		check(err)
		self.chatId = chat.ID
		chatTimes := map[string]time.Time{}
		for index, body := range []string{"Lunch?", "Noon at the quay."} {
			author, name := self.user, "Ada Harbor"
			if index == 1 {
				author, name = self.ben, "Ben Quay"
			}
			message, err := tx.AppendChatMessage(&models.ChatMessage{ChatID: chat.ID, AuthorUserID: author.ID, AuthorName: name, Body: body})
			check(err)
			chatTimes[message.ID] = local(11+index, 30)
			if index == 0 {
				self.firstChatMessageId = message.ID
			}
		}
		update(`UPDATE "chat_member" SET "joined_at" = '%s' WHERE "chat_id" = '%s'`, stamp(earlier(0, 0)), chat.ID)
		for messageId, at := range chatTimes {
			update(`UPDATE "chat_message" SET "created_at" = '%s' WHERE "id" = '%s'`, stamp(at), messageId)
		}

		// Mail: two that arrived, one of them marked important, one in the
		// trash, one sent, a note, and one that arrived the day before.
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: self.user.ID, Name: "Personal"})
		check(err)
		folder := func(kind models.MailboxFolderKind) *models.MailboxFolder {
			found, err := tx.GetFolderByKind(mailbox.ID, kind)
			check(err)
			if found == nil {
				t.Fatalf("no %s folder", kind)
			}
			return found
		}
		deliver := func(kind models.MailboxFolderKind, mail *models.Mail) (string, string) {
			stored, err := tx.CreateMail(mail, nil)
			check(err)
			item, err := tx.AddItem(folder(kind).ID, stored.ID, "", models.MailboxItemFlags{})
			check(err)
			return item.ID, stored.ID
		}
		var importantMailId string
		self.importantItemId, importantMailId = deliver(models.MailboxFolderKindInbox, &models.Mail{
			From: "Sam Reed <sam@example.test>", FromName: "Sam Reed", Subject: "Contract needs a reply", Kind: models.MailKindIncoming, ReceivedAt: local(9, 30),
		})
		check(tx.PutMailInsight(&models.MailInsight{MailID: importantMailId, MailboxID: mailbox.ID, AgentID: self.agent.ID, Category: "work", Priority: "high", NeedsReply: true}))
		self.plainItemId, _ = deliver(models.MailboxFolderKindInbox, &models.Mail{
			From: "news@example.test", Subject: "Weekly news", Kind: models.MailKindIncoming, ReceivedAt: local(14, 30),
		})
		deliver(models.MailboxFolderKindTrash, &models.Mail{From: "spam@example.test", Subject: "Thrown away", Kind: models.MailKindIncoming, ReceivedAt: local(8, 0)})
		deliver(models.MailboxFolderKindInbox, &models.Mail{From: "old@example.test", Subject: "Yesterday's mail", Kind: models.MailKindIncoming, ReceivedAt: earlier(23, 59)})
		self.sentItemId, _ = deliver(models.MailboxFolderKindSent, &models.Mail{
			From: "ada@example.test", Recipients: []string{"Sam Reed <sam@example.test>"}, Subject: "Re: Contract needs a reply", Kind: models.MailKindOutgoing, ReceivedAt: local(10, 0),
		})
		self.noteIdentifier = "note-packing"
		noteItemId, _ := deliver(models.MailboxFolderKindInbox, &models.Mail{Subject: "Packing list", Kind: models.MailKindNote, NoteIdentifier: self.noteIdentifier, ReceivedAt: local(13, 0)})
		update(`UPDATE "mailbox_item" SET "added_at" = '%s' WHERE "id" = '%s'`, stamp(local(13, 0)), noteItemId)

		// A recording, with its summary.
		recording, err := tx.CreateRecording(&models.Recording{UserID: self.user.ID, ClientRecordingID: "standup", RecordingTitle: "Harbor standup", RecordingOrigin: models.RecordingOriginAudioImport})
		check(err)
		self.recordingId = recording.ID
		update(`UPDATE "recording" SET "recorded_at" = '%s', "recording_summary" = 'Tides and lunch.', "timeline_duration_ms" = 900000 WHERE "id" = '%s'`, stamp(local(14, 0)), recording.ID)

		// Photos: two on the day (one of them at 23:30, which is already the
		// next day in Greenwich), and one just outside it on each side.
		for _, photo := range []struct {
			label string
			at    time.Time
		}{
			{"morning", local(7, 15)}, {"late", local(23, 30)}, {"after-midnight", later(0, 10)}, {"before", earlier(23, 50)},
		} {
			takenAt := photo.at
			made, _, err := tx.CreatePhoto(&models.Photo{UserID: self.user.ID, ClientPhotoID: photo.label, PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceCommandLine, TakenAt: &takenAt, PlaceName: "Quay " + photo.label})
			check(err)
			digest := photoDigest("blob of " + photo.label)
			_, err = tx.InsertPhotoBlobIfAbsent(&models.PhotoBlob{BlobSHA256: digest, BlobSizeBytes: 100})
			check(err)
			_, err = tx.SetPhotoFile(self.user.ID, made.ID, &models.PhotoFile{PhotoFileRole: models.PhotoFileRoleOriginal, BlobSHA256: digest, FileSizeBytes: 100, ContentType: "image/jpeg"}, 0)
			check(err)
			self.photoIds[photo.label] = made.ID
		}

		// Visits: three hours at the cafe, then five and a half at the office,
		// and a night at home that ended in the small hours of the day.
		harbor, err := tx.CreatePlace(self.user.ID, &models.Place{PlaceName: "Harbor Cafe", LatitudeDegrees: 40.7128, LongitudeDegrees: -74.0060})
		check(err)
		office, err := tx.CreatePlace(self.user.ID, &models.Place{PlaceName: "Harbor Office", LatitudeDegrees: 40.7484, LongitudeDegrees: -73.9857})
		check(err)
		home, err := tx.CreatePlace(self.user.ID, &models.Place{PlaceName: "Home", LatitudeDegrees: 40.65, LongitudeDegrees: -73.95})
		check(err)
		self.harborId, self.officeId = harbor.ID, office.ID
		for _, visit := range []struct {
			key     string
			place   *models.Place
			arrived time.Time
			left    time.Time
		}{
			{"cafe", harbor, local(9, 0), local(12, 0)},
			{"office", office, local(12, 30), local(18, 0)},
			{"home-night", home, earlier(20, 0), local(5, 0)},
		} {
			arrived, left := visit.arrived, visit.left
			_, err := tx.PutPlaceVisit(self.user.ID, &models.PlaceVisit{
				PlaceID: visit.place.ID, ClientVisitKey: visit.key, VisitSource: models.VisitSourceVisit,
				ArrivedAt: &arrived, DepartedAt: &left, LatitudeDegrees: visit.place.LatitudeDegrees, LongitudeDegrees: visit.place.LongitudeDegrees, AccuracyMeters: 20,
			})
			check(err)
			found, err := tx.GetPlaceVisitByClientKey(self.user.ID, visit.key)
			check(err)
			self.visitIds[visit.key] = found.ID
		}

		// Calendar: a meeting, an all-day holiday that is a date, a holiday on
		// the next date, and a meeting that was called off.
		events, err := tx.CreateCalendar(&models.Calendar{UserID: self.user.ID, Name: "Work", CalendarKind: models.CalendarEvents})
		check(err)
		event := func(uid, summary, status string, starts, ends time.Time, isAllDay bool) string {
			object, err := tx.PutCalendarObject(&models.CalendarObject{
				CalendarID: events.ID, UID: uid, ETag: uid, Data: "x", Summary: summary, Location: "Dock 4", StartsAt: starts, EndsAt: ends, AllDay: isAllDay, Status: status,
			}, []models.Occurrence{{StartsAt: starts, EndsAt: ends, AllDay: isAllDay}})
			check(err)
			return object.ID
		}
		self.meetingId = event("meeting", "Tide review", "CONFIRMED", local(15, 0), local(16, 0), false)
		self.holidayId = event("holiday", "Harbor day", "", time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC), true)
		event("next-holiday", "Next day", "", time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC), true)
		event("cancelled", "Called off", "CANCELLED", local(17, 0), local(18, 0), false)

		// Reminders: one due on the day and not done, one done on the day,
		// and one due the next day.
		list, err := tx.EnsureCalendar(self.user.ID, models.CalendarReminders, nil)
		check(err)
		reminder := func(uid, title, extra string, due time.Time, status string) string {
			object, err := tx.PutCalendarObject(&models.CalendarObject{
				CalendarID: list.ID, UID: uid, ETag: uid, Data: fmt.Sprintf(reminderTemplate, uid, title, extra),
				Summary: title, StartsAt: due, EndsAt: due, Status: status,
			}, nil)
			check(err)
			return object.ID
		}
		self.dueReminderId = reminder("call-bank", "Call the bank", "DUE:"+local(17, 0).UTC().Format("20060102T150405Z"), local(17, 0), "NEEDS-ACTION")
		self.doneReminderId = reminder("water-plants", "Water the plants", "DUE:"+local(8, 0).UTC().Format("20060102T150405Z")+"\nSTATUS:COMPLETED\nCOMPLETED:"+local(18, 30).UTC().Format("20060102T150405Z"), local(8, 0), "COMPLETED")
		reminder("tomorrow", "Tomorrow's chore", "DUE:"+later(9, 0).UTC().Format("20060102T150405Z"), later(9, 0), "NEEDS-ACTION")
		update(`UPDATE "calendar_object" SET "modified_at" = '%s' WHERE "id" = '%s'`, stamp(local(18, 30)), self.doneReminderId)

		// Money: groceries and a refund that day, and a coffee that posted the
		// next day but was bought on the day.
		financeSource, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: self.agent.ID, Kind: models.SourceWeb, Name: "bank", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{Start: "https://bank.example.test/"},
		})
		check(err)
		boughtAt := local(8, 15)
		_, err = tx.ApplyFinanceSync(self.agent.ID, financeSource.ID, &finance.SyncResult{
			Accounts: []finance.Account{{ProviderAccountID: "everyday", AccountName: "Everyday", AccountKind: finance.AccountKindDepository, CurrencyCode: "USD", CurrentBalance: "10", ProviderMetadata: json.RawMessage(`{}`)}},
			Added: []finance.Transaction{
				{ProviderTransactionID: "groceries", ProviderAccountID: "everyday", PostedOn: worldDay, Amount: "-42.17", CurrencyCode: "USD", Description: "CORNER GROCER", MerchantName: "Corner Grocer"},
				{ProviderTransactionID: "refund", ProviderAccountID: "everyday", PostedOn: worldDay, Amount: "10.00", CurrencyCode: "USD", Description: "REFUND"},
				{ProviderTransactionID: "coffee", ProviderAccountID: "everyday", PostedOn: "2026-10-23", TransactedAt: &boughtAt, Amount: "-3.50", CurrencyCode: "USD", Description: "QUAY COFFEE"},
				{ProviderTransactionID: "older", ProviderAccountID: "everyday", PostedOn: "2026-10-21", Amount: "-9.99", CurrencyCode: "USD", Description: "YESTERDAY"},
			},
		}, worldDay)
		check(err)
		page, err := tx.ListFinanceTransactions(self.agent.ID, &db.FinanceTransactionFilter{Limit: 50})
		check(err)
		for _, found := range page.FinanceTransactions {
			self.transactionIds[found.Description] = found.ID
		}

		// Health: the phone's summaries for the day, a run, two State of Mind
		// entries and two step readings.
		number := func(value float64) *float64 { return &value }
		_, err = tx.UpsertHealthDaySummaries(self.user.ID, []*models.HealthDaySummary{
			{HealthType: "stepCount", SummaryDay: worldDay, QuantityUnit: "count", SumValue: number(8200), SampleCount: 2, WorkedOutAt: time.Now()},
			{HealthType: "distanceWalkingRunning", SummaryDay: worldDay, QuantityUnit: "m", SumValue: number(5400), SampleCount: 2, WorkedOutAt: time.Now()},
			{HealthType: "restingHeartRate", SummaryDay: worldDay, QuantityUnit: "count/min", AverageValue: number(58), SampleCount: 1, WorkedOutAt: time.Now()},
			{
				HealthType: "sleepAnalysis", SummaryDay: worldDay, SampleCount: 5, WorkedOutAt: time.Now(),
				CategorySecondsJSON: `{"asleep":25200,"asleepDeep":5400,"asleepCore":14400,"asleepREM":5400,"awake":1200}`,
			},
		})
		check(err)
		_, err = tx.InsertHealthSamples(self.user.ID, []*models.HealthSample{
			{HealthSampleID: "run", HealthType: "workout", StartedAt: local(7, 0), EndedAt: local(7, 40), WorkoutActivityType: "running", SampleMetadataJSON: `{"totalDistanceMeters":5200}`},
			{HealthSampleID: "mood-one", HealthType: "stateOfMind", StartedAt: local(12, 0), EndedAt: local(12, 0), SampleMetadataJSON: `{"stateOfMindValence":0.4}`},
			{HealthSampleID: "mood-two", HealthType: "stateOfMind", StartedAt: local(20, 0), EndedAt: local(20, 0), SampleMetadataJSON: `{"stateOfMindValence":0.6}`},
			{HealthSampleID: "steps-one", HealthType: "stepCount", StartedAt: local(8, 10), EndedAt: local(8, 40), QuantityValue: ptrOf(1200.0), QuantityUnit: "count"},
			{HealthSampleID: "steps-two", HealthType: "stepCount", StartedAt: local(18, 5), EndedAt: local(18, 30), QuantityValue: ptrOf(900.0), QuantityUnit: "count"},
		})
		check(err)

		// Memory: a page with two facts from the day and one that is only
		// known to the month.
		node, err := tx.PutAgentNode(&models.AgentNode{AgentID: self.agent.ID, Path: "places/harbor-cafe", Kind: models.NodePlace, Name: "Harbor Cafe"})
		check(err)
		self.memoryNodeId = node.ID
		for index, fact := range []struct {
			text      string
			at        time.Time
			precision string
		}{
			{"Met Sam at the cafe.", local(10, 0), ""}, {"Tried the fish soup.", local(11, 0), models.HappenedDay},
			{"Went in October.", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), models.HappenedMonth},
		} {
			happenedAt := fact.at
			_, err := tx.AddAgentFact(&models.AgentFact{AgentID: self.agent.ID, NodeID: node.ID, Kind: models.FactEvent, Text: fact.text, HappenedAt: &happenedAt, HappenedPrecision: fact.precision})
			check(err)
			_ = index
		}
	})

	for _, statement := range updates {
		dbtest.Exec(t, database, statement)
	}
	dbtest.MarkPhotoReady(t, database, self.photoIds["morning"])
	dbtest.MarkPhotoReady(t, database, self.photoIds["late"])
	dbtest.MarkPhotoReady(t, database, self.photoIds["after-midnight"])
	dbtest.MarkPhotoReady(t, database, self.photoIds["before"])
	return self, release
}

func ptrOf[Value any](value Value) *Value { return &value }
