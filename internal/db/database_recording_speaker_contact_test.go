package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// contactFixture is a completed one-speaker transcript, plus two contacts in
// the owner's address book and one in the address book of somebody else.
type contactFixture struct {
	*transcriptFixture
	attemptId           string
	ownBookId           string
	samId               string
	rileyBookId         string
	rileyId             string
	otherUserId         string
	otherBookId         string
	otherContactId      string
	nextCorrectionIndex int
}

func newContactFixture(test *testing.T) *contactFixture {
	test.Helper()
	return newContactFixtureOfOrigin(test, models.RecordingOriginAudioImport)
}

func newContactFixtureOfOrigin(test *testing.T, recordingOrigin models.RecordingOrigin) *contactFixture {
	test.Helper()
	fixture := &contactFixture{transcriptFixture: newTranscriptFixtureOfOrigin(test, recordingOrigin)}
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	fixture.attemptId = attempt.ID
	fixture.otherUserId = dbtest.CreateUser(test, fixture.database, "speaker-contact-other")
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "The fixture participant spoke for a while."}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
		saveContact := func(userId, name string) (string, string) {
			book, err := transaction.CreateAddressBook(&models.AddressBook{UserID: userId})
			if err != nil {
				test.Fatal(err)
			}
			contact, err := transaction.PutContact(&models.Contact{AddressBookID: book.ID, UID: "urn:uuid:" + name, ETag: "e1", Card: "BEGIN:VCARD\r\nEND:VCARD\r\n", Name: name})
			if err != nil {
				test.Fatal(err)
			}
			return book.ID, contact.ID
		}
		fixture.ownBookId, fixture.samId = saveContact(fixture.userId, "Sam Example")
		fixture.rileyBookId, fixture.rileyId = saveContact(fixture.userId, "Riley Example")
		fixture.otherBookId, fixture.otherContactId = saveContact(fixture.otherUserId, "Alex Example")
	})
	return fixture
}

// name confirms the passage as the name, optionally tied to a contact, and
// returns the correction and the identity it is for.
func (self *contactFixture) name(test *testing.T, confirmedName string, passageStartMS, passageEndMS int64, addressBookId, contactId string) (*models.RecordingSpeakerCorrection, error) {
	test.Helper()
	self.nextCorrectionIndex++
	var corrected []*models.RecordingSpeakerCorrection
	err := self.database.Transaction(func(transaction db.Transaction) error {
		source, err := transaction.GetRecording(self.userId, self.media.RecordingID)
		if err != nil {
			return err
		}
		corrected, err = transaction.CorrectRecordingSpeaker(self.userId, db.RecordingSpeakerCorrectionInput{
			RecordingID: source.ID, RecordingTranscriptionAttemptID: self.attemptId, LocalSpeakerID: "speaker-one",
			ConfirmedName: confirmedName, ClientCorrectionID: "contact-naming-" + string(rune('a'+self.nextCorrectionIndex)),
			PassageStartMS: passageStartMS, PassageEndMS: passageEndMS, ExpectedSourceGeneration: source.SourceGeneration,
			ContactAddressBookID: addressBookId, ContactID: contactId,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return corrected[0], nil
}

func (self *contactFixture) spokenBy(test *testing.T, userId, addressBookId, contactId string, limit int) ([]*db.RecordingSpokenByContact, bool) {
	test.Helper()
	var spoken []*db.RecordingSpokenByContact
	var hasMore bool
	dbtest.RunTransactionOn(test, self.database, func(transaction db.Transaction) {
		var err error
		spoken, hasMore, err = transaction.ListRecordingsSpokenByContact(userId, addressBookId, contactId, limit)
		if err != nil {
			test.Fatal(err)
		}
	})
	return spoken, hasMore
}

func (self *contactFixture) setContact(userId, speakerIdentityId, addressBookId, contactId string, expectedSourceGeneration int64) error {
	return self.database.Transaction(func(transaction db.Transaction) error {
		return transaction.SetRecordingSpeakerContact(userId, self.media.RecordingID, speakerIdentityId, addressBookId, contactId, expectedSourceGeneration)
	})
}

func (self *contactFixture) identity(test *testing.T, speakerIdentityId string) *models.RecordingSpeakerIdentity {
	test.Helper()
	var found *models.RecordingSpeakerIdentity
	dbtest.RunTransactionOn(test, self.database, func(transaction db.Transaction) {
		identities, err := transaction.ListRecordingSpeakerIdentities(self.userId)
		if err != nil {
			test.Fatal(err)
		}
		for _, identity := range identities {
			if identity.ID == speakerIdentityId {
				found = identity
			}
		}
	})
	if found == nil {
		test.Fatalf("identity %q is not listed", speakerIdentityId)
	}
	return found
}

func (self *contactFixture) generation(test *testing.T) int64 {
	test.Helper()
	var generation int64
	dbtest.RunTransactionOn(test, self.database, func(transaction db.Transaction) {
		source, err := transaction.GetRecording(self.userId, self.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		generation = source.SourceGeneration
	})
	return generation
}

func TestNamingASpeakerTiesItToAContactOfTheOwnersOnly(test *testing.T) {
	fixture := newContactFixture(test)

	for name, tied := range map[string][2]string{
		"another person's address book":  {fixture.otherBookId, fixture.otherContactId},
		"a contact that is not there":    {fixture.ownBookId, "missing-contact"},
		"a contact of another book":      {fixture.otherBookId, fixture.samId},
		"an address book that is absent": {"missing-book", fixture.samId},
	} {
		if _, err := fixture.name(test, "Participant One", 2000, 2300, tied[0], tied[1]); !errors.Is(err, db.ErrNotFound) {
			test.Errorf("%s: want not found, got %v", name, err)
		}
	}
	if _, err := fixture.name(test, "Participant One", 2000, 2300, fixture.ownBookId, ""); !errors.Is(err, db.ErrInvalidArguments) {
		test.Errorf("an address book with no contact: want invalid arguments, got %v", err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		identities, err := transaction.ListRecordingSpeakerIdentities(fixture.userId)
		if err != nil || len(identities) != 0 {
			test.Fatalf("a refused naming left %d identities: %v", len(identities), err)
		}
	})

	first, err := fixture.name(test, "Participant One", 2000, 2300, fixture.ownBookId, fixture.samId)
	if err != nil {
		test.Fatal(err)
	}
	identity := fixture.identity(test, first.SpeakerIdentityID)
	if identity.ContactAddressBookID != fixture.ownBookId || identity.ContactID != fixture.samId || identity.ContactName != "Sam Example" {
		test.Fatalf("the name is not tied to the contact: %+v", identity)
	}

	// A later naming of the same name without a contact leaves the tie as it is.
	if _, err := fixture.name(test, "Participant One", 2300, 2600, "", ""); err != nil {
		test.Fatal(err)
	}
	if identity = fixture.identity(test, first.SpeakerIdentityID); identity.ContactID != fixture.samId {
		test.Fatalf("naming again without a contact untied the name: %+v", identity)
	}
	// With another contact it is tied to that one instead.
	if _, err := fixture.name(test, "Participant One", 2600, 2900, fixture.rileyBookId, fixture.rileyId); err != nil {
		test.Fatal(err)
	}
	if identity = fixture.identity(test, first.SpeakerIdentityID); identity.ContactID != fixture.rileyId || identity.ContactName != "Riley Example" {
		test.Fatalf("naming again with another contact did not re-tie the name: %+v", identity)
	}
}

func TestSettingASpeakerContactChecksTheOwnerAndTheGeneration(test *testing.T) {
	fixture := newContactFixture(test)
	named, err := fixture.name(test, "Participant One", 2000, 2300, "", "")
	if err != nil {
		test.Fatal(err)
	}
	identityId := named.SpeakerIdentityID
	generation := fixture.generation(test)

	if err := fixture.setContact(fixture.userId, identityId, fixture.ownBookId, fixture.samId, generation-1); !errors.Is(err, db.ErrAlreadyExists) {
		test.Errorf("an older view: want already exists, got %v", err)
	}
	if err := fixture.setContact(fixture.otherUserId, identityId, fixture.otherBookId, fixture.otherContactId, generation); !errors.Is(err, db.ErrNotFound) {
		test.Errorf("another person's recording: want not found, got %v", err)
	}
	if err := fixture.setContact(fixture.userId, identityId, fixture.otherBookId, fixture.otherContactId, generation); !errors.Is(err, db.ErrNotFound) {
		test.Errorf("another person's contact: want not found, got %v", err)
	}
	if err := fixture.setContact(fixture.userId, "missing-identity", fixture.ownBookId, fixture.samId, generation); !errors.Is(err, db.ErrNotFound) {
		test.Errorf("a name that is not there: want not found, got %v", err)
	}
	if err := fixture.setContact(fixture.userId, identityId, fixture.ownBookId, "", generation); !errors.Is(err, db.ErrInvalidArguments) {
		test.Errorf("an address book with no contact: want invalid arguments, got %v", err)
	}
	if identity := fixture.identity(test, identityId); identity.ContactID != "" {
		test.Fatalf("a refused tie was written: %+v", identity)
	}

	if err := fixture.setContact(fixture.userId, identityId, fixture.ownBookId, fixture.samId, generation); err != nil {
		test.Fatal(err)
	}
	if identity := fixture.identity(test, identityId); identity.ContactID != fixture.samId || identity.ContactName != "Sam Example" {
		test.Fatalf("the tie was not written: %+v", identity)
	}
	if after := fixture.generation(test); after != generation {
		test.Errorf("tying changed the generation from %d to %d", generation, after)
	}
	if err := fixture.setContact(fixture.userId, identityId, "", "", generation); err != nil {
		test.Fatal(err)
	}
	if identity := fixture.identity(test, identityId); identity.ContactID != "" || identity.ContactAddressBookID != "" || identity.ContactName != "" {
		test.Fatalf("the tie was not taken off: %+v", identity)
	}
}

func TestDeletingAContactUntiesTheNameButKeepsIt(test *testing.T) {
	fixture := newContactFixture(test)
	named, err := fixture.name(test, "Participant One", 2000, 2300, fixture.ownBookId, fixture.samId)
	if err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.DeleteContact(fixture.ownBookId, fixture.samId); err != nil {
			test.Fatal(err)
		}
	})
	identity := fixture.identity(test, named.SpeakerIdentityID)
	if identity.ConfirmedName != "Participant One" || identity.ContactID != "" || identity.ContactAddressBookID != "" || identity.ContactName != "" {
		test.Fatalf("a deleted contact left the name %+v", identity)
	}
	spoken, _ := fixture.spokenBy(test, fixture.userId, fixture.ownBookId, fixture.samId, 10)
	if len(spoken) != 0 {
		test.Fatalf("a deleted contact still has %d recordings", len(spoken))
	}
	if got := dbtest.QueryString(test, fixture.database, `SELECT count(*) FROM "recording_speaker_correction" WHERE "is_removed" = false`); got != "1" {
		test.Fatalf("a deleted contact left %s passages named, want 1", got)
	}
}

func TestRecordingsSpokenByAContactCountPassagesThatStillStand(test *testing.T) {
	fixture := newContactFixture(test)
	first, err := fixture.name(test, "Participant One", 2000, 2300, fixture.ownBookId, fixture.samId)
	if err != nil {
		test.Fatal(err)
	}
	if _, err := fixture.name(test, "Participant One", 2300, 2600, "", ""); err != nil {
		test.Fatal(err)
	}
	// Somebody else's contact of the same IDs is nobody's business.
	if spoken, _ := fixture.spokenBy(test, fixture.otherUserId, fixture.ownBookId, fixture.samId, 10); len(spoken) != 0 {
		test.Fatalf("another account listed %d recordings", len(spoken))
	}
	if spoken, _ := fixture.spokenBy(test, fixture.userId, fixture.rileyBookId, fixture.rileyId, 10); len(spoken) != 0 {
		test.Fatalf("a contact nobody is tied to listed %d recordings", len(spoken))
	}
	spoken, hasMore := fixture.spokenBy(test, fixture.userId, fixture.ownBookId, fixture.samId, 10)
	if len(spoken) != 1 || hasMore || spoken[0].Recording.ID != fixture.media.RecordingID || spoken[0].SpokenPassageCount != 2 {
		test.Fatalf("want the one recording with 2 passages, got %+v (more: %v)", spoken, hasMore)
	}

	// Another name given to part of a passage replaces that passage.
	if _, err := fixture.name(test, "Participant Two", 2400, 2500, "", ""); err != nil {
		test.Fatal(err)
	}
	spoken, _ = fixture.spokenBy(test, fixture.userId, fixture.ownBookId, fixture.samId, 10)
	if len(spoken) != 1 || spoken[0].SpokenPassageCount != 1 {
		test.Fatalf("a passage named again by somebody else still counts: %+v", spoken)
	}

	// A naming the person took off no longer counts, and the recording drops out.
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.RemoveRecordingSpeakerCorrection(fixture.userId, source.ID, first.ID, source.SourceGeneration); err != nil {
			test.Fatal(err)
		}
	})
	spoken, _ = fixture.spokenBy(test, fixture.userId, fixture.ownBookId, fixture.samId, 10)
	if len(spoken) != 0 {
		test.Fatalf("the earlier naming of the other passage was superseded, but %+v is listed", spoken)
	}
}

func TestRecordingsSpokenByAContactAreNewestFirstAndPaged(test *testing.T) {
	fixture := newContactFixture(test)
	first, err := fixture.name(test, "Participant One", 2000, 2300, fixture.ownBookId, fixture.samId)
	if err != nil {
		test.Fatal(err)
	}
	var secondId string
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		second, err := transaction.CreateRecording(&models.Recording{UserID: fixture.userId, ClientRecordingID: "second-source", RecordingOrigin: models.RecordingOriginAudioImport})
		if err != nil {
			test.Fatal(err)
		}
		secondId = second.ID
	})
	// The second recording was made later, so it is listed first until the
	// first is given a recorded time after it.
	dbtest.Exec(test, fixture.database, `INSERT INTO "recording_speaker_correction" ("id", "recording_id", "recording_media_id", "recording_transcription_attempt_id", "speaker_identity_id", "client_correction_id", "local_speaker_id", "passage_start_ms", "passage_end_ms", "source_generation", "created_at", "is_removed")
		SELECT 'second-correction', '`+secondId+`', "recording_media_id", "recording_transcription_attempt_id", "speaker_identity_id", 'second-naming', "local_speaker_id", 5000, 5500, 1, "created_at", false
		FROM "recording_speaker_correction" WHERE "id" = '`+first.ID+`'`)

	spoken, hasMore := fixture.spokenBy(test, fixture.userId, fixture.ownBookId, fixture.samId, 10)
	if len(spoken) != 2 || hasMore || spoken[0].Recording.ID != secondId || spoken[1].Recording.ID != fixture.media.RecordingID {
		test.Fatalf("want the later recording first, got %+v (more: %v)", spoken, hasMore)
	}
	spoken, hasMore = fixture.spokenBy(test, fixture.userId, fixture.ownBookId, fixture.samId, 1)
	if len(spoken) != 1 || !hasMore || spoken[0].Recording.ID != secondId {
		test.Fatalf("want one recording and more to come, got %+v (more: %v)", spoken, hasMore)
	}

	dbtest.Exec(test, fixture.database, `UPDATE "recording" SET "recorded_at" = '`+time.Now().Add(time.Hour).UTC().Format(time.RFC3339)+`' WHERE "id" = '`+fixture.media.RecordingID+`'`)
	spoken, _ = fixture.spokenBy(test, fixture.userId, fixture.ownBookId, fixture.samId, 10)
	if len(spoken) != 2 || spoken[0].Recording.ID != fixture.media.RecordingID {
		test.Fatalf("want the recording with the later recorded time first, got %+v", spoken)
	}

	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		for _, limit := range []int{0, 101} {
			if _, _, err := transaction.ListRecordingsSpokenByContact(fixture.userId, fixture.ownBookId, fixture.samId, limit); !errors.Is(err, db.ErrInvalidArguments) {
				test.Errorf("limit %d: want invalid arguments, got %v", limit, err)
			}
		}
		if _, _, err := transaction.ListRecordingsSpokenByContact(fixture.userId, "", "", 10); !errors.Is(err, db.ErrNotFound) {
			test.Errorf("no contact: want not found, got %v", err)
		}
	})
}

func TestNamingATemporarySpeakerTiesItToAContact(test *testing.T) {
	fixture := newContactFixtureOfOrigin(test, models.RecordingOriginCapture)
	recordingId := fixture.media.RecordingID
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.CreateRecordingTemporarySpeaker(fixture.userId, &models.RecordingTemporarySpeaker{RecordingID: recordingId, SourceAttemptID: fixture.attemptId, SourceLocalSpeakerID: "speaker-one", PassageStartMS: 2000, PassageEndMS: 4000}, temporarySpeakerSample("sample"), 1<<20); err != nil {
			test.Fatal(err)
		}
	})
	name := func(addressBookId, contactId string) error {
		return fixture.database.Transaction(func(transaction db.Transaction) error {
			return transaction.NameRecordingTemporarySpeakerAsContact(fixture.userId, recordingId, "Speaker 1", "Participant One", false, addressBookId, contactId)
		})
	}
	if err := name(fixture.otherBookId, fixture.otherContactId); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("another person's contact: want not found, got %v", err)
	}
	if err := name(fixture.ownBookId, ""); !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatalf("an address book with no contact: want invalid arguments, got %v", err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		identities, err := transaction.ListRecordingSpeakerIdentities(fixture.userId)
		if err != nil || len(identities) != 0 {
			test.Fatalf("a refused naming left %d identities: %v", len(identities), err)
		}
	})
	if err := name(fixture.ownBookId, fixture.samId); err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		identities, err := transaction.ListRecordingSpeakerIdentities(fixture.userId)
		if err != nil || len(identities) != 1 || identities[0].ContactID != fixture.samId || identities[0].ContactName != "Sam Example" {
			test.Fatalf("the named speaker is not tied to the contact: %+v %v", identities, err)
		}
	})
	spoken, _ := fixture.spokenBy(test, fixture.userId, fixture.ownBookId, fixture.samId, 10)
	if len(spoken) != 1 || spoken[0].SpokenPassageCount < 1 {
		test.Fatalf("want the recording listed, got %+v", spoken)
	}
	// Naming again without a contact leaves the tie.
	if err := name("", ""); err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		identities, err := transaction.ListRecordingSpeakerIdentities(fixture.userId)
		if err != nil || len(identities) != 1 || identities[0].ContactID != fixture.samId {
			test.Fatalf("naming again untied the speaker: %+v %v", identities, err)
		}
	})
}
