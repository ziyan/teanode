package recordlink_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recordlink"
)

// person is one account with an agent and a reader of each sort.
type person struct {
	user   *models.User
	agent  *models.Agent
	person recordlink.Reader
	robot  recordlink.Reader
}

func aPerson(t *testing.T, tx db.Transaction, username string) *person {
	t.Helper()
	user, err := tx.CreateUser(&models.User{Username: username, Name: "Fixture Person"})
	if err != nil {
		t.Fatalf("CreateUser: %s", err)
	}
	agent, err := tx.CreateAgent(&models.Agent{UserID: user.ID, Enabled: true, Name: "Bertie"})
	if err != nil {
		t.Fatalf("CreateAgent: %s", err)
	}
	return &person{
		user: user, agent: agent,
		person: recordlink.Reader{UserID: user.ID, AgentID: agent.ID},
		robot:  recordlink.Reader{UserID: user.ID, AgentID: agent.ID, IsAgent: true},
	}
}

// aBook makes an address book and, where asked, gives it to the agent: a
// book is made closed and opened by an update, as the person does it.
func aBook(t *testing.T, tx db.Transaction, user *models.User, name string, isGranted bool) *models.AddressBook {
	t.Helper()
	book, err := tx.CreateAddressBook(&models.AddressBook{UserID: user.ID, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	if isGranted {
		book.AgentGranted = true
		if book, err = tx.UpdateAddressBook(book); err != nil {
			t.Fatal(err)
		}
	}
	return book
}

func aCalendar(t *testing.T, tx db.Transaction, user *models.User, name string, calendarKind models.CalendarKind, isGranted bool) *models.Calendar {
	t.Helper()
	calendar, err := tx.CreateCalendar(&models.Calendar{UserID: user.ID, Name: name, CalendarKind: calendarKind})
	if err != nil {
		t.Fatal(err)
	}
	if isGranted {
		calendar.AgentGranted = true
		if calendar, err = tx.UpdateCalendar(calendar); err != nil {
			t.Fatal(err)
		}
	}
	return calendar
}

func resolve(t *testing.T, tx db.Transaction, reader recordlink.Reader, recordKind models.AgentRecordKind, recordId string) *recordlink.Resolved {
	t.Helper()
	resolved, err := recordlink.ResolveRecord(tx, reader, recordKind, recordId, models.AgentRecordDetail{}, 0, 0)
	if err != nil {
		t.Fatalf("ResolveRecord(%s %s): %s", recordKind, recordId, err)
	}
	return resolved
}

// The person reads a card whatever the agent was given; the agent reads it
// only from a book it was given; a stranger's card and a missing one read
// as unavailable for both.
func TestAContactIsReadOnlyWhereTheAgentWasGivenTheBook(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice, bob := aPerson(t, tx, "fixture-alice"), aPerson(t, tx, "fixture-bob")
		granted := aBook(t, tx, alice.user, "Shared", true)
		kept := aBook(t, tx, alice.user, "Private", false)
		bobs := aBook(t, tx, bob.user, "Bobs", true)
		open, err := tx.PutContact(&models.Contact{AddressBookID: granted.ID, UID: "one", ETag: "e", Card: "x", Name: "Open Contact", Emails: []string{"open@example.com"}})
		if err != nil {
			t.Fatal(err)
		}
		closed, _ := tx.PutContact(&models.Contact{AddressBookID: kept.ID, UID: "two", ETag: "e", Card: "x", Name: "Closed Contact"})
		stranger, _ := tx.PutContact(&models.Contact{AddressBookID: bobs.ID, UID: "three", ETag: "e", Card: "x", Name: "Stranger Contact"})

		if resolved := resolve(t, tx, alice.robot, models.RecordContact, open.ID); !resolved.IsReadable() || resolved.Title != "Open Contact" || resolved.Contact == nil {
			t.Errorf("the agent reads a card in a book it was given: %+v", resolved)
		}
		withheld := resolve(t, tx, alice.robot, models.RecordContact, closed.ID)
		if !withheld.IsWithheld || withheld.Title != "" || withheld.Contact != nil {
			t.Errorf("the agent reads nothing of a card in a book it was not given: %+v", withheld)
		}
		if resolved := resolve(t, tx, alice.person, models.RecordContact, closed.ID); !resolved.IsReadable() || resolved.Title != "Closed Contact" {
			t.Errorf("the person reads their own card whatever the agent was given: %+v", resolved)
		}
		for _, reader := range []recordlink.Reader{alice.person, alice.robot} {
			if resolved := resolve(t, tx, reader, models.RecordContact, stranger.ID); !resolved.IsUnavailable || resolved.Title != "" {
				t.Errorf("another person's card is not found: %+v", resolved)
			}
			if resolved := resolve(t, tx, reader, models.RecordContact, "missing"); !resolved.IsUnavailable {
				t.Errorf("a card that is gone is unavailable: %+v", resolved)
			}
		}
	})
}

// A mail message is read by the agent only from a mailbox it is granted,
// and never from somebody else's.
func TestAMailMessageIsReadOnlyFromAGrantedMailbox(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice, bob := aPerson(t, tx, "fixture-alice"), aPerson(t, tx, "fixture-bob")
		mail, err := tx.CreateMail(&models.Mail{Subject: "Fixture subject", Kind: models.MailKindIncoming, ReceivedAt: time.Now()}, nil)
		if err != nil {
			t.Fatal(err)
		}
		itemIn := func(user *models.User, grant *models.AgentMailbox) string {
			mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Personal", Agent: grant})
			if err != nil {
				t.Fatal(err)
			}
			inbox, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
			if err != nil || inbox == nil {
				t.Fatalf("no Inbox: %v", err)
			}
			item, err := tx.AddItem(inbox.ID, mail.ID, "", models.MailboxItemFlags{})
			if err != nil {
				t.Fatal(err)
			}
			return item.ID
		}
		granted := itemIn(alice.user, &models.AgentMailbox{Granted: true})
		ungranted := itemIn(alice.user, nil)
		strangers := itemIn(bob.user, &models.AgentMailbox{Granted: true})

		if resolved := resolve(t, tx, alice.robot, models.RecordMail, granted); !resolved.IsReadable() || resolved.Title != "Fixture subject" || resolved.DashboardPath != "/mailbox/starred/"+granted {
			t.Errorf("a message in a granted mailbox: %+v", resolved)
		}
		if resolved := resolve(t, tx, alice.robot, models.RecordMail, ungranted); !resolved.IsWithheld || resolved.Title != "" {
			t.Errorf("a message in a mailbox that was not granted shows no subject: %+v", resolved)
		}
		if resolved := resolve(t, tx, alice.person, models.RecordMail, ungranted); !resolved.IsReadable() || resolved.Title != "Fixture subject" {
			t.Errorf("the person reads their own mailbox: %+v", resolved)
		}
		for _, reader := range []recordlink.Reader{alice.person, alice.robot} {
			if resolved := resolve(t, tx, reader, models.RecordMail, strangers); !resolved.IsUnavailable || resolved.Title != "" {
				t.Errorf("another person's message is not found: %+v", resolved)
			}
		}
	})
}

// An event is read by the agent only from a calendar it was given, and an
// event named as a reminder is not one.
func TestAnEventIsReadOnlyFromAGrantedCalendar(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice := aPerson(t, tx, "fixture-alice")
		put := func(calendar *models.Calendar, uid string) string {
			calendar = aCalendar(t, tx, alice.user, calendar.Name, calendar.CalendarKind, calendar.AgentGranted)
			object, err := tx.PutCalendarObject(&models.CalendarObject{
				CalendarID: calendar.ID, UID: uid, ETag: "e", Data: "x", Summary: "Fixture meeting " + uid,
				StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour),
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			return object.ID
		}
		granted := put(&models.Calendar{UserID: alice.user.ID, Name: "Work", AgentGranted: true}, "granted")
		ungranted := put(&models.Calendar{UserID: alice.user.ID, Name: "Home"}, "ungranted")
		reminder := put(&models.Calendar{UserID: alice.user.ID, Name: "Tasks", CalendarKind: models.CalendarReminders, AgentGranted: true}, "reminder")

		if resolved := resolve(t, tx, alice.robot, models.RecordCalendarEvent, granted); !resolved.IsReadable() || resolved.Title != "Fixture meeting granted" {
			t.Errorf("an event in a granted calendar: %+v", resolved)
		}
		if resolved := resolve(t, tx, alice.robot, models.RecordCalendarEvent, ungranted); !resolved.IsWithheld || resolved.Title != "" {
			t.Errorf("an event in a calendar that was not given: %+v", resolved)
		}
		if resolved := resolve(t, tx, alice.person, models.RecordCalendarEvent, ungranted); !resolved.IsReadable() {
			t.Errorf("the person reads their own calendar: %+v", resolved)
		}
		if resolved := resolve(t, tx, alice.person, models.RecordCalendarEvent, reminder); !resolved.IsUnavailable {
			t.Errorf("a reminder is not an event: %+v", resolved)
		}
		if resolved := resolve(t, tx, alice.robot, models.RecordReminder, reminder); !resolved.IsReadable() {
			t.Errorf("a reminder in a granted list is a reminder: %+v", resolved)
		}
	})
}

// A recording is the person's own: found for them and their agent, never
// for another person, and unavailable once deleted.
func TestARecordingBelongsToItsOwner(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice, bob := aPerson(t, tx, "fixture-alice"), aPerson(t, tx, "fixture-bob")
		recording, err := tx.CreateRecording(&models.Recording{UserID: alice.user.ID, ClientRecordingID: "fixture", RecordingTitle: "Fixture review", RecordingOrigin: models.RecordingOriginCapture})
		if err != nil {
			t.Fatal(err)
		}
		resolved, err := recordlink.ResolveRecord(tx, alice.robot, models.RecordRecordingPassage, recording.ID, models.AgentRecordDetail{TranscriptRunID: "run-1"}, 1000, 2000)
		if err != nil || !resolved.IsReadable() || resolved.Title != "Fixture review" {
			t.Fatalf("the owner's agent reads their recording: %+v %v", resolved, err)
		}
		if resolved.DashboardPath != "/recordings/"+recording.ID+"?endMS=2000&startMS=1000&version=run-1" {
			t.Errorf("the dashboard opens the passage: %q", resolved.DashboardPath)
		}
		for _, reader := range []recordlink.Reader{bob.person, bob.robot} {
			if resolved := resolve(t, tx, reader, models.RecordRecordingPassage, recording.ID); !resolved.IsUnavailable || resolved.Title != "" {
				t.Errorf("another person's recording is not found: %+v", resolved)
			}
		}
	})
}

// A conversation message is the agent's own conversation, and nobody
// else's.
func TestAConversationMessageBelongsToItsAgent(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice, bob := aPerson(t, tx, "fixture-alice"), aPerson(t, tx, "fixture-bob")
		conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: alice.agent.ID, Kind: models.AgentConversationMain, Title: "Fixture talk", LastAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		message, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "user", Content: "hello"})
		if err != nil {
			t.Fatal(err)
		}
		if resolved := resolve(t, tx, alice.robot, models.RecordConversationMessage, message.ID); !resolved.IsReadable() || resolved.Title != "Fixture talk" {
			t.Errorf("its own conversation: %+v", resolved)
		}
		if resolved := resolve(t, tx, bob.person, models.RecordConversationMessage, message.ID); !resolved.IsUnavailable {
			t.Errorf("another agent's conversation: %+v", resolved)
		}
	})
}

// A transaction is read by the agent only while the source it came from is
// switched on.
func TestATransactionIsReadOnlyFromAnEnabledSource(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var alice, bob *person
	var sourceId, transactionId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice, bob = aPerson(t, tx, "fixture-alice"), aPerson(t, tx, "fixture-bob")
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: alice.agent.ID, Kind: models.SourceWeb, Name: "institution", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{Start: "https://example.com/"},
		})
		if err != nil {
			t.Fatal(err)
		}
		sourceId = source.ID
		if _, err := tx.ApplyFinanceSync(alice.agent.ID, source.ID, &finance.SyncResult{
			Accounts: []finance.Account{{ProviderAccountID: "account-1", AccountName: "Everyday Checking", AccountKind: finance.AccountKindDepository, CurrencyCode: "USD", CurrentBalance: "10", ProviderMetadata: json.RawMessage(`{}`)}},
			Added: []finance.Transaction{{
				ProviderTransactionID: "transaction-1", ProviderAccountID: "account-1", PostedOn: "2026-09-10",
				Amount: "-42.17", CurrencyCode: "USD", Description: "CORNER GROCER", MerchantName: "Corner Grocer",
			}},
		}, "2026-09-10"); err != nil {
			t.Fatal(err)
		}
		page, err := tx.ListFinanceTransactions(alice.agent.ID, &db.FinanceTransactionFilter{Limit: 10})
		if err != nil || len(page.FinanceTransactions) != 1 {
			t.Fatalf("no transaction: %v", err)
		}
		transactionId = page.FinanceTransactions[0].ID
	})
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if resolved := resolve(t, tx, alice.robot, models.RecordFinanceTransaction, transactionId); !resolved.IsReadable() || resolved.Title != "Corner Grocer -42.1700 USD" {
			t.Errorf("a transaction of an enabled source: %+v", resolved)
		}
		if resolved := resolve(t, tx, bob.person, models.RecordFinanceTransaction, transactionId); !resolved.IsUnavailable {
			t.Errorf("another agent's transaction: %+v", resolved)
		}
		source, _ := tx.GetAgentSource(alice.agent.ID, sourceId)
		source.Enabled = false
		if _, err := tx.PutAgentSource(source); err != nil {
			t.Fatal(err)
		}
		if resolved := resolve(t, tx, alice.robot, models.RecordFinanceTransaction, transactionId); !resolved.IsWithheld || resolved.Title != "" {
			t.Errorf("a transaction of a source that is switched off: %+v", resolved)
		}
		if resolved := resolve(t, tx, alice.person, models.RecordFinanceTransaction, transactionId); !resolved.IsReadable() {
			t.Errorf("the person still reads it: %+v", resolved)
		}
	})
}

func TestTitlesAreOneShortLine(t *testing.T) {
	got := recordlink.OneLine("first line\n\nsecond  line\t" + string(make([]rune, 0)))
	if got != "first line second line" {
		t.Errorf("OneLine = %q", got)
	}
	long := make([]rune, 400)
	for index := range long {
		long[index] = 'x'
	}
	if length := len([]rune(recordlink.OneLine(string(long)))); length != 120 {
		t.Errorf("a long title is cut to 120 characters, got %d", length)
	}
}
