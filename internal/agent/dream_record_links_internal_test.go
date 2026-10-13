package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// The pass that links pages to the person's own records states only what is
// certain, proposes what is a guess, honors every grant, writes each link
// once, and does not put back what the person took away. One test per source,
// with invented people.

func recordLinkPage(t *testing.T, tx db.Transaction, run *Run, path string, kind models.AgentNodeKind, name string, aliases ...string) *models.AgentNode {
	t.Helper()
	page, err := tx.PutAgentNode(&models.AgentNode{AgentID: run.Agent.ID, Path: path, Kind: kind, Name: name, Aliases: aliases})
	if err != nil {
		t.Fatalf("PutAgentNode %s: %s", path, err)
	}
	return page
}

// recordLinksOf are a page's links as relation, status and record, whatever
// order they were written in.
func recordLinksOf(t *testing.T, database db.Database, run *Run, page *models.AgentNode) []*models.AgentRecordLink {
	t.Helper()
	var links []*models.AgentRecordLink
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if links, err = tx.ListAgentRecordLinks(run.Agent.ID, page.ID); err != nil {
			t.Fatalf("ListAgentRecordLinks: %s", err)
		}
	})
	return links
}

func theLinkTo(links []*models.AgentRecordLink, recordId string) *models.AgentRecordLink {
	for _, link := range links {
		if link.RecordID == recordId {
			return link
		}
	}
	return nil
}

func aBookWithCards(t *testing.T, tx db.Transaction, run *Run, name string, isGranted bool, cards ...*models.Contact) []*models.Contact {
	t.Helper()
	book, err := tx.CreateAddressBook(&models.AddressBook{UserID: run.Owner.ID, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	if isGranted {
		book.AgentGranted = true
		if _, err = tx.UpdateAddressBook(book); err != nil {
			t.Fatal(err)
		}
	}
	stored := make([]*models.Contact, 0, len(cards))
	for index, card := range cards {
		card.AddressBookID, card.UID, card.ETag, card.Card = book.ID, fmt.Sprintf("%s-%d", name, index), "e", "x"
		saved, err := tx.PutContact(card)
		if err != nil {
			t.Fatal(err)
		}
		stored = append(stored, saved)
	}
	return stored
}

func TestContactsAreLinkedByAddressWhenSureAndByNameWhenGuessing(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	worker, run := digestSplitWorld(t, database, "http://127.0.0.1:1")

	var dana, eli, frankOne, frankTwo, gail, hana *models.AgentNode
	var cards []*models.Contact
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		cards = aBookWithCards(t, tx, run, "Shared", true,
			&models.Contact{Name: "Dana Fixture", Emails: []string{"dana@example.com"}},
			&models.Contact{Name: "Eli Sample"},
			&models.Contact{Name: "Frank Twin"},
			&models.Contact{Name: "Hana Shared", Emails: []string{"twin@example.com"}},
			&models.Contact{Name: "Hana Copy", Emails: []string{"twin@example.com"}})
		hidden := aBookWithCards(t, tx, run, "Private", false, &models.Contact{Name: "Gail Hidden", Emails: []string{"gail@example.com"}})
		_ = hidden
		dana = recordLinkPage(t, tx, run, "people/dana-fixture", models.NodePerson, "Dana F.", "Dana@Example.com")
		eli = recordLinkPage(t, tx, run, "people/eli-sample", models.NodePerson, "Eli Sample")
		frankOne = recordLinkPage(t, tx, run, "people/frank-one", models.NodePerson, "Frank Twin")
		frankTwo = recordLinkPage(t, tx, run, "people/frank-two", models.NodePerson, "Frank Twin")
		gail = recordLinkPage(t, tx, run, "people/gail-hidden", models.NodePerson, "Gail Hidden", "gail@example.com")
		hana = recordLinkPage(t, tx, run, "people/hana", models.NodePerson, "Hana", "twin@example.com")
	})

	counts := worker.dreamRecordLinks(context.Background(), run)
	if counts.Stated != 1 || counts.Proposed != 1 {
		t.Errorf("one address and one name: %+v", counts)
	}

	danaLinks := recordLinksOf(t, database, run, dana)
	if len(danaLinks) != 1 || danaLinks[0].RecordID != cards[0].ID || danaLinks[0].LinkStatus != models.EdgeStated ||
		danaLinks[0].RecordRelation != models.RecordRelationIsContact {
		t.Errorf("an address on exactly one card states the link: %+v", danaLinks)
	}
	eliLinks := recordLinksOf(t, database, run, eli)
	if len(eliLinks) != 1 || eliLinks[0].RecordID != cards[1].ID || eliLinks[0].LinkStatus != models.EdgeProposed {
		t.Errorf("a full name alone proposes: %+v", eliLinks)
	}
	for name, page := range map[string]*models.AgentNode{"two pages with one name": frankOne, "the same": frankTwo,
		"a card in a book that was not given": gail, "an address on two cards": hana} {
		if links := recordLinksOf(t, database, run, page); len(links) != 0 {
			t.Errorf("%s makes no link: %+v", name, links)
		}
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found, err := tx.GetAgentNodeByID(run.Agent.ID, dana.ID)
		if err != nil || found.ContactID != cards[0].ID {
			t.Errorf("a stated link names the card on the page: %+v %v", found, err)
		}
		found, _ = tx.GetAgentNodeByID(run.Agent.ID, eli.ID)
		if found.ContactID != "" {
			t.Errorf("a guess does not name the card on the page: %q", found.ContactID)
		}
	})

	// Once written, left alone; and a proposal made again does not touch a
	// statement.
	if again := worker.dreamRecordLinks(context.Background(), run); again.Stated+again.Proposed != 0 {
		t.Errorf("nothing new the second time: %+v", again)
	}

	// The person takes the guess away, and it is not made again until they
	// make it themselves.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if removed, err := tx.DeleteAgentRecordLink(run.Agent.ID, eli.ID, models.RecordContact, cards[1].ID, "", 0, 0); err != nil || removed != 1 {
			t.Fatalf("remove: %d %v", removed, err)
		}
	})
	if again := worker.dreamRecordLinks(context.Background(), run); again.Stated+again.Proposed != 0 {
		t.Errorf("a link the person removed is not made again: %+v", again)
	}
	if links := recordLinksOf(t, database, run, eli); len(links) != 0 {
		t.Errorf("still removed: %+v", links)
	}
}

func TestSentAndReceivedMailIsLinkedByExactAddressFromGrantedMailboxes(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	worker, run := digestSplitWorld(t, database, "http://127.0.0.1:1")

	var iris, jules, kai *models.AgentNode
	var sentItem, receivedItem, privateItem string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		iris = recordLinkPage(t, tx, run, "people/iris", models.NodePerson, "Iris Fixture", "iris@example.com")
		jules = recordLinkPage(t, tx, run, "people/jules", models.NodePerson, "Jules Fixture", "jules@example.com")
		kai = recordLinkPage(t, tx, run, "people/kai", models.NodePerson, "Kai Fixture", "kai@example.com")
		itemIn := func(grant *models.AgentMailbox, kind models.MailboxFolderKind, mail *models.Mail) string {
			mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: run.Owner.ID, Name: fmt.Sprintf("Box %d", time.Now().UnixNano()), Agent: grant})
			if err != nil {
				t.Fatal(err)
			}
			folder, err := tx.GetFolderByKind(mailbox.ID, kind)
			if err != nil || folder == nil {
				t.Fatalf("no folder %s: %v", kind, err)
			}
			stored, err := tx.CreateMail(mail, nil)
			if err != nil {
				t.Fatal(err)
			}
			item, err := tx.AddItem(folder.ID, stored.ID, "", models.MailboxItemFlags{})
			if err != nil {
				t.Fatal(err)
			}
			return item.ID
		}
		granted := &models.AgentMailbox{Granted: true}
		sentItem = itemIn(granted, models.MailboxFolderKindSent, &models.Mail{
			Subject: "Fixture note", Kind: models.MailKindOutgoing, ReceivedAt: time.Now(), From: "owner@example.com",
			Recipients: []string{"Iris@Example.com", "stranger@example.net"}})
		receivedItem = itemIn(granted, models.MailboxFolderKindInbox, &models.Mail{
			Subject: "Fixture reply", Kind: models.MailKindIncoming, ReceivedAt: time.Now(), From: "jules@example.com"})
		privateItem = itemIn(nil, models.MailboxFolderKindInbox, &models.Mail{
			Subject: "Fixture private", Kind: models.MailKindIncoming, ReceivedAt: time.Now(), From: "kai@example.com"})
	})

	counts := worker.dreamRecordLinks(context.Background(), run)
	if counts.Stated != 2 || counts.Proposed != 0 {
		t.Errorf("two stated links: %+v", counts)
	}
	irisLinks := recordLinksOf(t, database, run, iris)
	if len(irisLinks) != 1 || irisLinks[0].RecordID != sentItem || irisLinks[0].RecordRelation != models.RecordRelationReceived ||
		irisLinks[0].LinkStatus != models.EdgeStated || irisLinks[0].RecordKind != models.RecordMail {
		t.Errorf("a recipient received the sent message: %+v", irisLinks)
	}
	julesLinks := recordLinksOf(t, database, run, jules)
	if len(julesLinks) != 1 || julesLinks[0].RecordID != receivedItem || julesLinks[0].RecordRelation != models.RecordRelationWrote {
		t.Errorf("a sender wrote the message: %+v", julesLinks)
	}
	if links := recordLinksOf(t, database, run, kai); len(links) != 0 || privateItem == "" {
		t.Errorf("a mailbox the agent was not given is not read: %+v", links)
	}
	if again := worker.dreamRecordLinks(context.Background(), run); again.Stated != 0 {
		t.Errorf("idempotent: %+v", again)
	}
}

const recordLinkEvent = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Fixture//EN\r\nBEGIN:VEVENT\r\nUID:%s\r\nDTSTAMP:20260101T000000Z\r\n" +
	"DTSTART:%s\r\nDTEND:%s\r\nSUMMARY:%s\r\nORGANIZER:mailto:%s\r\nATTENDEE;PARTSTAT=ACCEPTED:mailto:%s\r\nATTENDEE:mailto:%s\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func TestEventAttendeesAreLinkedByAddressAndTitlesProposeProjects(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	worker, run := digestSplitWorld(t, database, "http://127.0.0.1:1")

	var lena, milo, nora, project *models.AgentNode
	var granted, ungranted, reminder *models.CalendarObject
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		lena = recordLinkPage(t, tx, run, "people/lena", models.NodePerson, "Lena Fixture", "lena@example.com")
		milo = recordLinkPage(t, tx, run, "people/milo", models.NodePerson, "Milo Fixture", "milo@example.com")
		nora = recordLinkPage(t, tx, run, "people/nora", models.NodePerson, "Nora Fixture", "nora@example.com")
		project = recordLinkPage(t, tx, run, "projects/lantern", models.NodeProject, "Lantern")
		startsAt := time.Now().Add(48 * time.Hour).UTC()
		format := "20060102T150405Z"
		put := func(name string, kind models.CalendarKind, isGranted bool, uid, summary string) *models.CalendarObject {
			calendar, err := tx.CreateCalendar(&models.Calendar{UserID: run.Owner.ID, Name: name, CalendarKind: kind})
			if err != nil {
				t.Fatal(err)
			}
			if isGranted {
				calendar.AgentGranted = true
				if calendar, err = tx.UpdateCalendar(calendar); err != nil {
					t.Fatal(err)
				}
			}
			data := fmt.Sprintf(recordLinkEvent, uid, startsAt.Format(format), startsAt.Add(time.Hour).Format(format), summary,
				"lena@example.com", "milo@example.com", "stranger@example.net")
			object, err := tx.PutCalendarObject(&models.CalendarObject{
				CalendarID: calendar.ID, UID: uid, ETag: "e", Data: data, Summary: summary,
				StartsAt: startsAt, EndsAt: startsAt.Add(time.Hour),
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			return object
		}
		granted = put("Work", models.CalendarEvents, true, "granted", "Lantern planning")
		ungranted = put("Home", models.CalendarEvents, false, "ungranted", "Lantern at home")
		reminder = put("Tasks", models.CalendarReminders, true, "reminder", "Send the Lantern notes")
	})

	counts := worker.dreamRecordLinks(context.Background(), run)
	if counts.Stated != 2 || counts.Proposed != 2 {
		t.Errorf("an organizer and a guest stated, the project proposed for an event and a reminder: %+v", counts)
	}
	lenaLinks := recordLinksOf(t, database, run, lena)
	if len(lenaLinks) != 1 || lenaLinks[0].RecordID != granted.ID || lenaLinks[0].RecordRelation != models.RecordRelationOrganized || lenaLinks[0].LinkStatus != models.EdgeStated {
		t.Errorf("the organizer organized it: %+v", lenaLinks)
	}
	miloLinks := recordLinksOf(t, database, run, milo)
	if len(miloLinks) != 1 || miloLinks[0].RecordRelation != models.RecordRelationAttended {
		t.Errorf("a guest attended it: %+v", miloLinks)
	}
	if links := recordLinksOf(t, database, run, nora); len(links) != 0 {
		t.Errorf("a page not on the list has no link: %+v", links)
	}
	projectLinks := recordLinksOf(t, database, run, project)
	if len(projectLinks) != 2 {
		t.Fatalf("the project is proposed for the event and the reminder: %+v", projectLinks)
	}
	for _, link := range projectLinks {
		if link.LinkStatus != models.EdgeProposed || link.RecordRelation != models.RecordRelationAbout {
			t.Errorf("a title naming a project is a guess: %+v", link)
		}
	}
	if theLinkTo(projectLinks, ungranted.ID) != nil || theLinkTo(projectLinks, granted.ID) == nil || theLinkTo(projectLinks, reminder.ID) == nil {
		t.Errorf("only granted calendars are read: %+v", projectLinks)
	}
}

func TestMerchantsProposeOrganizationsAndMatchedReceiptsFollowTheirTransaction(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	worker, run := digestSplitWorld(t, database, "http://127.0.0.1:1")

	var grocer, other *models.AgentNode
	var sourceId string
	var transactionId, receiptId, unmatchedReceiptId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		grocer = recordLinkPage(t, tx, run, "organizations/corner-grocer", models.NodeOrganization, "Corner Grocer")
		other = recordLinkPage(t, tx, run, "organizations/other", models.NodeOrganization, "Elsewhere")
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: run.Agent.ID, Kind: models.SourceFinance, Name: "institution", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{Type: "fixture"},
		})
		if err != nil {
			t.Fatal(err)
		}
		sourceId = source.ID
		if _, err := tx.ApplyFinanceSync(run.Agent.ID, source.ID, &finance.SyncResult{
			Accounts: []finance.Account{{ProviderAccountID: "account-1", AccountName: "Everyday Checking", AccountKind: finance.AccountKindDepository, CurrencyCode: "USD", CurrentBalance: "10", ProviderMetadata: json.RawMessage(`{}`)}},
			Added: []finance.Transaction{
				{ProviderTransactionID: "transaction-1", ProviderAccountID: "account-1", PostedOn: "2026-09-10", Amount: "-42.17", CurrencyCode: "USD", Description: "CORNER GROCER", MerchantName: "Corner Grocer"},
				{ProviderTransactionID: "transaction-2", ProviderAccountID: "account-1", PostedOn: "2026-09-11", Amount: "-5.00", CurrencyCode: "USD", Description: "UNKNOWN", MerchantName: "No Such Page"},
			},
		}, "2026-09-11"); err != nil {
			t.Fatal(err)
		}
		page, err := tx.ListFinanceTransactions(run.Agent.ID, &db.FinanceTransactionFilter{Limit: 10})
		if err != nil || len(page.FinanceTransactions) != 2 {
			t.Fatalf("transactions: %v", err)
		}
		for _, found := range page.FinanceTransactions {
			if found.MerchantName == "Corner Grocer" {
				transactionId = found.ID
			}
		}
		receipt, err := tx.PutFinanceReceipt(&models.FinanceReceipt{
			AgentID: run.Agent.ID, ReceiptSourceKind: models.ReceiptSourceKindMail, MailID: "mail-receipt-one",
			MerchantName: "Corner Grocer", PurchasedOn: "2026-09-10", CurrencyCode: "usd", SubtotalAmount: "40.17", TotalAmount: "42.17",
			ReceiptCheckState: models.ReceiptCheckStateBalanced, CheckDifferenceAmount: "0",
			ReceiptLines: []*models.FinanceReceiptLine{{LineNumber: 1, ReceiptLineKind: models.ReceiptLineKindItem, Description: "OAT FLAKES", LineAmount: "40.17"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		receiptId = receipt.ID
		unmatched, err := tx.PutFinanceReceipt(&models.FinanceReceipt{
			AgentID: run.Agent.ID, ReceiptSourceKind: models.ReceiptSourceKindMail, MailID: "mail-receipt-two",
			MerchantName: "Corner Grocer", PurchasedOn: "2026-09-12", CurrencyCode: "usd", SubtotalAmount: "1.00", TotalAmount: "1.00",
			ReceiptCheckState: models.ReceiptCheckStateBalanced, CheckDifferenceAmount: "0",
			ReceiptLines: []*models.FinanceReceiptLine{{LineNumber: 1, ReceiptLineKind: models.ReceiptLineKindItem, Description: "GUM", LineAmount: "1.00"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		unmatchedReceiptId = unmatched.ID
		if _, err := tx.PutFinanceReceiptMatch(run.Agent.ID, &models.FinanceReceiptMatch{
			ReceiptID: receiptId, FinanceTransactionID: transactionId, MatchedAmount: "42.17", ReceiptMatchSource: models.ReceiptMatchSourcePerson,
		}); err != nil {
			t.Fatal(err)
		}
	})

	counts := worker.dreamRecordLinks(context.Background(), run)
	if counts.Stated != 0 || counts.Proposed != 2 {
		t.Errorf("the merchant and, with it, the matched receipt are proposed: %+v", counts)
	}
	links := recordLinksOf(t, database, run, grocer)
	paid, receipt := theLinkTo(links, transactionId), theLinkTo(links, receiptId)
	if paid == nil || paid.RecordRelation != models.RecordRelationPaid || paid.LinkStatus != models.EdgeProposed {
		t.Errorf("a merchant with the name of an organization is a guess: %+v", links)
	}
	if receipt == nil || receipt.RecordRelation != models.RecordRelationReceiptFor || receipt.LinkStatus != models.EdgeProposed {
		t.Errorf("a receipt follows its transaction's link, and its status: %+v", links)
	}
	if theLinkTo(links, unmatchedReceiptId) != nil || len(recordLinksOf(t, database, run, other)) != 0 {
		t.Errorf("a receipt matched to nothing, and a page with another name, have no links: %+v", links)
	}

	// The person states the merchant link; the receipt then follows as a
	// statement, and a proposal made again does not weaken it.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.PutAgentRecordLink(&models.AgentRecordLink{
			AgentID: run.Agent.ID, NodeID: grocer.ID, RecordKind: models.RecordFinanceTransaction, RecordID: transactionId,
			RecordRelation: models.RecordRelationPaid, LinkStatus: models.EdgeStated,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.DeleteAgentRecordLink(run.Agent.ID, grocer.ID, models.RecordFinanceReceipt, receiptId, "", 0, 0); err != nil {
			t.Fatal(err)
		}
	})
	worker.dreamRecordLinks(context.Background(), run)
	links = recordLinksOf(t, database, run, grocer)
	if paid = theLinkTo(links, transactionId); paid == nil || paid.LinkStatus != models.EdgeStated {
		t.Errorf("a statement stays one: %+v", links)
	}
	if receipt = theLinkTo(links, receiptId); receipt != nil {
		t.Errorf("a receipt link the person removed is not made again: %+v", receipt)
	}

	// With the institution switched off, nothing of it is linked.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		source, _ := tx.GetAgentSource(run.Agent.ID, sourceId)
		source.Enabled = false
		if _, err := tx.PutAgentSource(source); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.DeleteAgentRecordLink(run.Agent.ID, grocer.ID, models.RecordFinanceTransaction, transactionId, "", 0, 0); err != nil {
			t.Fatal(err)
		}
	})
	if counts = worker.dreamRecordLinks(context.Background(), run); counts.Stated+counts.Proposed != 0 {
		t.Errorf("a finance source that is switched off is not read: %+v", counts)
	}
}

// recordingWithSpeakers makes a transcribed recording of the person's with
// the given local speakers, one segment each, and has the person confirm the
// given names. It returns the recording and the transcript run.
func recordingWithSpeakers(t *testing.T, database db.Database, run *Run, confirmed map[string]string, ownerSpeaker string) (*models.Recording, string) {
	t.Helper()
	original := []byte("fixture-audio")
	digest := sha256.Sum256(original)
	digestText := hex.EncodeToString(digest[:])
	userId, agentId := run.Owner.ID, run.Agent.ID
	var recording *models.Recording
	var media *models.RecordingMedia
	var transcriptRun *models.RecordingTranscriptRun
	var claim db.RecordingWorkerClaim
	var attempt *models.RecordingTranscriptionAttempt
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if recording, err = tx.CreateRecording(&models.Recording{UserID: userId, ClientRecordingID: "links-fixture", RecordingTitle: "Fixture review", RecordingOrigin: models.RecordingOriginAudioImport}); err != nil {
			t.Fatal(err)
		}
		if media, err = tx.CreateRecordingMedia(userId, &models.RecordingMedia{RecordingID: recording.ID, ClientMediaID: "links-media", OriginalFilename: "fixture.wav",
			OriginalContentType: "audio/wav", OriginalSizeBytes: int64(len(original)), OriginalSHA256: digestText, TimelineStartMS: 2000}, 1024, 4096); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.PutRecordingMediaPart(userId, &models.RecordingMediaPart{RecordingMediaID: media.ID, PartIndex: 0, PartSizeBytes: int64(len(original)), PartSHA256: digestText}); err != nil {
			t.Fatal(err)
		}
		if _, _, err = tx.CompleteRecordingMedia(userId, media.ID, digestText); err != nil {
			t.Fatal(err)
		}
		if transcriptRun, err = tx.CreateRecordingTranscriptRun(userId, media.ID, agentId, "initial", "fixture:diarize", false); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.EnqueueAgentJob(&models.AgentJob{AgentID: agentId, Kind: models.AgentJobRecording, SubjectID: transcriptRun.ID}); err != nil {
			t.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		jobs, err := tx.ClaimAgentJobs("links-fixture-worker", 1, time.Now())
		if err != nil || len(jobs) != 1 {
			t.Fatalf("claim: %v", err)
		}
		claim = db.RecordingWorkerClaim{JobID: jobs[0].ID, ClaimID: jobs[0].ClaimID}
		if err = tx.UpdateRecordingTranscriptProgress(userId, transcriptRun.ID, claim, models.RecordingProcessingPreparing, "", 0); err != nil {
			t.Fatal(err)
		}
		blobDigest := sha256.Sum256([]byte("derived-wave-bytes"))
		blob := &models.RecordingBlob{ID: db.RecordingAudioFileID(transcriptRun.ID, 0), BlobSizeBytes: 128, BlobSHA256: hex.EncodeToString(blobDigest[:])}
		chunk, err := tx.CreateRecordingAudioChunk(userId, transcriptRun.ID, claim, &models.RecordingAudioChunk{ChunkIndex: 0, TimelineStartMS: 2000, AudioDurationMS: 60000}, blob, 4096)
		if err != nil {
			t.Fatal(err)
		}
		if err = tx.VerifyRecordingBlob(userId, blob.ID, blob.BlobSHA256); err != nil {
			t.Fatal(err)
		}
		if err = tx.UpdateRecordingTranscriptProgress(userId, transcriptRun.ID, claim, models.RecordingProcessingTranscribing, "", 60000); err != nil {
			t.Fatal(err)
		}
		if attempt, err = tx.CreateRecordingTranscriptionAttempt(userId, transcriptRun.ID, chunk.ID, claim, "USD", nil); err != nil {
			t.Fatal(err)
		}
		if err = tx.SubmitRecordingTranscriptionAttempt(userId, attempt.ID, claim); err != nil {
			t.Fatal(err)
		}
		// Two turns of the first speaker with a short silence between them,
		// which are one turn; the second speaker; then the first again after
		// a long silence, which is another.
		segments := []*models.RecordingTranscriptSegment{
			{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 5000, TranscriptText: "One."},
			{LocalSpeakerID: "speaker-one", SegmentStartMS: 5500, SegmentEndMS: 8000, TranscriptText: "Two."},
			{LocalSpeakerID: "speaker-two", SegmentStartMS: 8000, SegmentEndMS: 11000, TranscriptText: "Three."},
			{LocalSpeakerID: "speaker-one", SegmentStartMS: 30000, SegmentEndMS: 33000, TranscriptText: "Four."},
			{LocalSpeakerID: "speaker-three", SegmentStartMS: 34000, SegmentEndMS: 36000, TranscriptText: "Five."},
		}
		if _, err = tx.FinishRecordingTranscriptionAttempt(userId, attempt.ID, claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, segments); err != nil {
			t.Fatal(err)
		}
		if err = tx.UpdateRecordingTranscriptProgress(userId, transcriptRun.ID, claim, models.RecordingProcessingComplete, "", 60000); err != nil {
			t.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		current, err := tx.GetRecording(userId, recording.ID)
		if err != nil {
			t.Fatal(err)
		}
		generation := current.SourceGeneration
		for speaker, name := range confirmed {
			corrections, err := tx.CorrectRecordingSpeaker(userId, db.RecordingSpeakerCorrectionInput{
				RecordingID: recording.ID, RecordingTranscriptionAttemptID: attempt.ID, LocalSpeakerID: speaker, ConfirmedName: name,
				ClientCorrectionID: "confirm-" + speaker, PassageStartMS: -1, PassageEndMS: -1, ExpectedSourceGeneration: generation,
				IsAccountOwner: speaker == ownerSpeaker,
			})
			if err != nil || len(corrections) == 0 {
				t.Fatalf("CorrectRecordingSpeaker %s: %v", speaker, err)
			}
			if current, err = tx.GetRecording(userId, recording.ID); err != nil {
				t.Fatal(err)
			}
			generation = current.SourceGeneration
		}
	})
	return recording, transcriptRun.ID
}

func TestConfirmedSpeakersLinkTheirPagesToTheirTurns(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	worker, run := digestSplitWorld(t, database, "http://127.0.0.1:1")

	var pia, stranger, selfPage *models.AgentNode
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		pia = recordLinkPage(t, tx, run, "people/pia", models.NodePerson, "Pia Fixture", "Pia")
		stranger = recordLinkPage(t, tx, run, "people/ravi", models.NodePerson, "Ravi Fixture")
		selfPage = recordLinkPage(t, tx, run, "self", models.NodeSelf, "Alice Example")
	})
	// Pia is confirmed under her alias; the owner under their own name; the
	// third speaker is named by somebody who has no page.
	recording, runId := recordingWithSpeakers(t, database, run,
		map[string]string{"speaker-one": "pia", "speaker-two": "Alice Example", "speaker-three": "Someone Else"}, "speaker-two")

	counts := worker.dreamRecordLinks(context.Background(), run)
	if counts.Stated != 3 || counts.Proposed != 0 {
		t.Errorf("two turns of Pia and one of the owner: %+v", counts)
	}
	piaLinks := recordLinksOf(t, database, run, pia)
	if len(piaLinks) != 2 {
		t.Fatalf("the two turns of the speaker: %+v", piaLinks)
	}
	ranges := map[string]bool{}
	for _, link := range piaLinks {
		ranges[fmt.Sprintf("%d-%d", link.PassageStartMS, link.PassageEndMS)] = true
		if link.RecordKind != models.RecordRecordingPassage || link.RecordID != recording.ID || link.RecordRelation != models.RecordRelationSpokeIn ||
			link.LinkStatus != models.EdgeStated || link.RecordDetail.TranscriptRunID != runId {
			t.Errorf("a stated spoke_in link naming the transcript: %+v", link)
		}
	}
	if !ranges["2000-8000"] || !ranges["30000-33000"] {
		t.Errorf("segments with a short silence between them are one turn: %v", ranges)
	}
	selfLinks := recordLinksOf(t, database, run, selfPage)
	if len(selfLinks) != 1 || selfLinks[0].PassageStartMS != 8000 || selfLinks[0].RecordRelation != models.RecordRelationSpokeIn {
		t.Errorf("the identity that is the person links to the self page: %+v", selfLinks)
	}
	if links := recordLinksOf(t, database, run, stranger); len(links) != 0 {
		t.Errorf("a page nobody confirmed has no passages: %+v", links)
	}
	if strings.Contains(piaLinks[0].Evidence[0].Quote, "One.") {
		t.Errorf("the words of the recording are not copied into a link: %+v", piaLinks[0].Evidence)
	}
	if again := worker.dreamRecordLinks(context.Background(), run); again.Stated != 0 {
		t.Errorf("idempotent: %+v", again)
	}
}

func TestOnePassMakesAtMostTheBoundOfNewLinks(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	worker, run := digestSplitWorld(t, database, "http://127.0.0.1:1")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var cards []*models.Contact
		for index := 0; index < recordLinksPerDream+20; index++ {
			cards = append(cards, &models.Contact{Name: fmt.Sprintf("Person Number%03d", index), Emails: []string{fmt.Sprintf("p%03d@example.com", index)}})
		}
		aBookWithCards(t, tx, run, "Shared", true, cards...)
		for index := 0; index < recordLinksPerDream+20; index++ {
			recordLinkPage(t, tx, run, fmt.Sprintf("people/p%03d", index), models.NodePerson, fmt.Sprintf("P %03d", index), fmt.Sprintf("p%03d@example.com", index))
		}
	})
	if counts := worker.dreamRecordLinks(context.Background(), run); counts.Stated != recordLinksPerDream {
		t.Errorf("bounded: %+v", counts)
	}
	if counts := worker.dreamRecordLinks(context.Background(), run); counts.Stated != 20 {
		t.Errorf("the rest on the next dream: %+v", counts)
	}
}
