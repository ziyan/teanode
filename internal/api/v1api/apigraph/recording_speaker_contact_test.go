package apigraph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/graphql-go/graphql"
	gqlparser "github.com/graphql-go/graphql/language/parser"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The tie of a speaker to a contact, and the contact fields of the answers,
// are in the schema the server builds.
func TestRecordingSpeakerContactDocumentsMatchTheBuiltSchema(test *testing.T) {
	schema := buildSchemaForValidation(test)
	for _, source := range []string{
		`mutation { SetRecordingSpeakerContact(recordingId: "a", expectedSourceGeneration: 1, speakerIdentityId: "b", addressBookId: "c", contactId: "d") { sourceGeneration speakerIdentities { id contactAddressBookId contactId contactName } } }`,
		`mutation { SetRecordingSpeakerContact(recordingId: "a", expectedSourceGeneration: 1, speakerIdentityId: "b") { sourceGeneration } }`,
		`mutation { CorrectRecordingSpeaker(recordingId: "a", expectedSourceGeneration: 1, clientCorrectionId: "k", confirmedName: "Participant One", recordingTranscriptionAttemptId: "t", localSpeakerId: "s", passageStartMS: -1, passageEndMS: -1, contactAddressBookId: "c", contactId: "d") { sourceGeneration } }`,
		`mutation { NameRecordingTemporarySpeaker(recordingId: "a", speakerLabel: "Speaker 1", confirmedName: "Participant One", contactAddressBookId: "c", contactId: "d") }`,
		`{ SuggestRecordingSpeakerNames { confirmedName speakerNameSource addressBookId contactId } }`,
	} {
		document, err := gqlparser.Parse(gqlparser.ParseParams{Source: source})
		if err != nil {
			test.Fatal(err)
		}
		if validated := graphql.ValidateDocument(&schema, document, nil); !validated.IsValid {
			test.Errorf("%s does not match the built schema: %v", source, validated.Errors)
		}
	}
}

type speakerContactWorld struct {
	database     db.Database
	resolver     *graph
	ownerId      string
	strangerId   string
	recordingId  string
	attemptId    string
	grantedBook  string
	grantedCard  string
	privateBook  string
	privateCard  string
	strangerBook string
	strangerCard string
}

// aSpeakerContactWorld is a person with a recording whose one speaker is
// ready to be named, a granted and a private address book with a contact in
// each, and a stranger with a contact of their own.
func aSpeakerContactWorld(test *testing.T) *speakerContactWorld {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	world := &speakerContactWorld{database: database, resolver: &graph{database: database}}
	world.ownerId = dbtest.CreateUser(test, database, "speaker-contact-owner")
	world.strangerId = dbtest.CreateUser(test, database, "speaker-contact-stranger")
	content := []byte("fixture-audio")
	digest := sha256.Sum256(content)
	digestText := hex.EncodeToString(digest[:])
	var claim db.RecordingWorkerClaim
	var runId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		saveContact := func(userId, bookName string, isGranted bool) (string, string) {
			book, err := transaction.CreateAddressBook(&models.AddressBook{UserID: userId, Name: bookName})
			if err != nil {
				test.Fatal(err)
			}
			if isGranted {
				book.AgentGranted = true
				if _, err := transaction.UpdateAddressBook(book); err != nil {
					test.Fatal(err)
				}
			}
			contact, err := transaction.PutContact(&models.Contact{AddressBookID: book.ID, UID: bookName, ETag: "e1", Card: "BEGIN:VCARD\r\nEND:VCARD\r\n", Name: bookName + " Contact"})
			if err != nil {
				test.Fatal(err)
			}
			return book.ID, contact.ID
		}
		world.grantedBook, world.grantedCard = saveContact(world.ownerId, "Shared", true)
		world.privateBook, world.privateCard = saveContact(world.ownerId, "Private", false)
		world.strangerBook, world.strangerCard = saveContact(world.strangerId, "Stranger", false)
		agent, err := transaction.CreateAgent(&models.Agent{UserID: world.ownerId, Name: "Fixture assistant", Enabled: true})
		if err != nil {
			test.Fatal(err)
		}
		recording, err := transaction.CreateRecording(&models.Recording{UserID: world.ownerId, ClientRecordingID: "speaker-contact-source", RecordingOrigin: models.RecordingOriginAudioImport})
		if err != nil {
			test.Fatal(err)
		}
		world.recordingId = recording.ID
		media, err := transaction.CreateRecordingMedia(world.ownerId, &models.RecordingMedia{RecordingID: recording.ID, ClientMediaID: "speaker-contact-media", OriginalFilename: "fixture.wav", OriginalContentType: "audio/wav", OriginalSizeBytes: int64(len(content)), OriginalSHA256: digestText, TimelineStartMS: 2000}, 1024, 4096)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.PutRecordingMediaPart(world.ownerId, &models.RecordingMediaPart{RecordingMediaID: media.ID, PartIndex: 0, PartSizeBytes: int64(len(content)), PartSHA256: digestText}); err != nil {
			test.Fatal(err)
		}
		if _, _, err := transaction.CompleteRecordingMedia(world.ownerId, media.ID, digestText); err != nil {
			test.Fatal(err)
		}
		run, err := transaction.CreateRecordingTranscriptRun(world.ownerId, media.ID, agent.ID, "initial", "fixture:diarize", false)
		if err != nil {
			test.Fatal(err)
		}
		runId = run.ID
		if _, err := transaction.EnqueueAgentJob(&models.AgentJob{AgentID: agent.ID, Kind: models.AgentJobRecording, SubjectID: run.ID}); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		jobs, err := transaction.ClaimAgentJobs("fixture-instance", 1, time.Now())
		if err != nil || len(jobs) != 1 {
			test.Fatal("recording job was not claimed", err)
		}
		claim = db.RecordingWorkerClaim{JobID: jobs[0].ID, ClaimID: jobs[0].ClaimID}
		if err := transaction.UpdateRecordingTranscriptProgress(world.ownerId, runId, claim, models.RecordingProcessingPreparing, "", 0); err != nil {
			test.Fatal(err)
		}
		digest := sha256.Sum256([]byte("derived-wave-bytes"))
		blob := &models.RecordingBlob{ID: db.RecordingAudioFileID(runId, 0), BlobSizeBytes: 128, BlobSHA256: hex.EncodeToString(digest[:])}
		chunk, err := transaction.CreateRecordingAudioChunk(world.ownerId, runId, claim, &models.RecordingAudioChunk{ChunkIndex: 0, TimelineStartMS: 2000, AudioDurationMS: 1000}, blob, 4096)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.VerifyRecordingBlob(world.ownerId, blob.ID, blob.BlobSHA256); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(world.ownerId, runId, claim, models.RecordingProcessingTranscribing, "", 1000); err != nil {
			test.Fatal(err)
		}
		attempt, err := transaction.CreateRecordingTranscriptionAttempt(world.ownerId, runId, chunk.ID, claim, "USD", nil)
		if err != nil {
			test.Fatal(err)
		}
		world.attemptId = attempt.ID
		if err := transaction.SubmitRecordingTranscriptionAttempt(world.ownerId, attempt.ID, claim); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.FinishRecordingTranscriptionAttempt(world.ownerId, attempt.ID, claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "The fixture participant spoke."}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(world.ownerId, runId, claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
	})
	return world
}

// run runs the function as the person, or as their agent, in one transaction.
func (self *speakerContactWorld) run(test *testing.T, asAgent bool, run func(ctx context.Context, transaction db.Transaction)) {
	test.Helper()
	dbtest.RunTransactionOn(test, self.database, func(transaction db.Transaction) {
		owner, err := transaction.GetUser(self.ownerId)
		if err != nil {
			test.Fatal(err)
		}
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), &api.Principal{User: owner, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}), transaction)
		if asAgent {
			ctx = db.ContextWithAuditPrincipal(ctx, db.AuditPrincipal{ActorKind: models.AuditActorAgent, UserID: self.ownerId})
		}
		run(ctx, transaction)
	})
}

// Naming a speaker with a contact ties the name to it, tying again with
// another contact moves the tie, and no contact removes it. Only the person's
// own address books count, and their agent may only use the ones it was given.
func TestSettingASpeakerContactIsTheOwnersAndTheAgentsGrantedBooksOnly(test *testing.T) {
	world := aSpeakerContactWorld(test)
	var identityId string
	var generation int64
	world.run(test, false, func(ctx context.Context, transaction db.Transaction) {
		source, err := transaction.GetRecording(world.ownerId, world.recordingId)
		if err != nil {
			test.Fatal(err)
		}
		view, err := world.resolver.CorrectRecordingSpeaker(ctx, CorrectRecordingSpeakerArguments{
			RecordingID: world.recordingId, ExpectedSourceGeneration: source.SourceGeneration, ClientCorrectionID: "contact-naming",
			ConfirmedName: "Participant One", RecordingTranscriptionAttemptID: world.attemptId, LocalSpeakerID: "speaker-one", PassageStartMS: -1, PassageEndMS: -1,
			ContactAddressBookID: world.grantedBook, ContactID: world.grantedCard,
		})
		if err != nil {
			test.Fatal(err)
		}
		if len(view.SpeakerIdentities) != 1 || view.SpeakerIdentities[0].ContactID != world.grantedCard || view.SpeakerIdentities[0].ContactName != "Shared Contact" {
			test.Fatalf("naming with a contact did not tie the name: %+v", view.SpeakerIdentities)
		}
		identityId, generation = view.SpeakerIdentities[0].ID, view.SourceGeneration
	})
	set := func(ctx context.Context, addressBookId, contactId string) (*RecordingSpeakersView, error) {
		return world.resolver.SetRecordingSpeakerContact(ctx, SetRecordingSpeakerContactArguments{RecordingID: world.recordingId, ExpectedSourceGeneration: generation, SpeakerIdentityID: identityId, AddressBookID: addressBookId, ContactID: contactId})
	}

	world.run(test, false, func(ctx context.Context, transaction db.Transaction) {
		if _, err := set(ctx, world.strangerBook, world.strangerCard); !errors.Is(err, api.ErrNotFound) {
			test.Errorf("another person's contact: want not found, got %v", err)
		}
		view, err := set(ctx, world.privateBook, world.privateCard)
		if err != nil {
			test.Fatal(err)
		}
		if view.SpeakerIdentities[0].ContactID != world.privateCard || view.SpeakerIdentities[0].ContactName != "Private Contact" {
			test.Fatalf("the person could not tie to their own private book: %+v", view.SpeakerIdentities)
		}
	})
	world.run(test, true, func(ctx context.Context, transaction db.Transaction) {
		if _, err := set(ctx, world.privateBook, world.privateCard); !errors.Is(err, api.ErrNotFound) {
			test.Errorf("the agent tied a speaker to a book it was not given: %v", err)
		}
		// What the person tied to a private book is not shown to the agent.
		view, err := world.resolver.ReadRecordingSpeakers(ctx, RecordingArguments{RecordingID: world.recordingId})
		if err != nil {
			test.Fatal(err)
		}
		if identity := view.SpeakerIdentities[0]; identity.ContactID != "" || identity.ContactAddressBookID != "" || identity.ContactName != "" {
			test.Errorf("the agent was shown a tie to a book it was not given: %+v", identity)
		}
		// Nor may it take that tie off or move it to another contact.
		if _, err := set(ctx, world.grantedBook, world.grantedCard); !errors.Is(err, api.ErrNotFound) {
			test.Errorf("the agent moved a tie it cannot see: %v", err)
		}
		if _, err := set(ctx, "", ""); !errors.Is(err, api.ErrNotFound) {
			test.Errorf("the agent took off a tie it cannot see: %v", err)
		}
	})
	world.run(test, false, func(ctx context.Context, transaction db.Transaction) {
		if _, err := set(ctx, "", ""); err != nil {
			test.Fatal(err)
		}
	})
	world.run(test, true, func(ctx context.Context, transaction db.Transaction) {
		view, err := set(ctx, world.grantedBook, world.grantedCard)
		if err != nil {
			test.Fatal(err)
		}
		if identity := view.SpeakerIdentities[0]; identity.ContactID != world.grantedCard || identity.ContactName != "Shared Contact" {
			test.Errorf("the agent could not tie to a book it was given: %+v", identity)
		}
		if view, err = set(ctx, "", ""); err != nil {
			test.Fatal(err)
		}
		if identity := view.SpeakerIdentities[0]; identity.ContactID != "" || identity.ContactName != "" {
			test.Errorf("the tie was not taken off: %+v", identity)
		}
	})
}

// A contact is offered with the book and the ID that name it, and a voice
// already tied to one is offered with the same, but the agent is only shown
// the tie of a voice to a book it was given.
func TestSpeakerNameSuggestionsSayWhichContactTheyAre(test *testing.T) {
	world := aSpeakerContactWorld(test)
	world.run(test, false, func(ctx context.Context, transaction db.Transaction) {
		source, err := transaction.GetRecording(world.ownerId, world.recordingId)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CorrectRecordingSpeaker(world.ownerId, db.RecordingSpeakerCorrectionInput{
			RecordingID: world.recordingId, ExpectedSourceGeneration: source.SourceGeneration, ClientCorrectionID: "contact-suggestion",
			ConfirmedName: "Participant One", RecordingTranscriptionAttemptID: world.attemptId, LocalSpeakerID: "speaker-one", PassageStartMS: -1, PassageEndMS: -1,
			ContactAddressBookID: world.privateBook, ContactID: world.privateCard,
		}); err != nil {
			test.Fatal(err)
		}
	})
	suggest := func(asAgent bool) map[string]*RecordingSpeakerNameSuggestion {
		offered := map[string]*RecordingSpeakerNameSuggestion{}
		world.run(test, asAgent, func(ctx context.Context, transaction db.Transaction) {
			suggestions, err := world.resolver.SuggestRecordingSpeakerNames(ctx, SuggestRecordingSpeakerNamesArguments{})
			if err != nil {
				test.Fatal(err)
			}
			for _, suggestion := range suggestions {
				offered[suggestion.ConfirmedName] = suggestion
			}
		})
		return offered
	}
	person := suggest(false)
	if tied := person["Participant One"]; tied == nil || tied.SpeakerNameSource != "speaker" || tied.AddressBookID != world.privateBook || tied.ContactID != world.privateCard {
		test.Errorf("a voice tied to a contact is not offered with it: %+v", tied)
	}
	if contact := person["Shared Contact"]; contact == nil || contact.SpeakerNameSource != "contact" || contact.AddressBookID != world.grantedBook || contact.ContactID != world.grantedCard {
		test.Errorf("a contact is not offered with its book and ID: %+v", contact)
	}
	agent := suggest(true)
	if tied := agent["Participant One"]; tied == nil || tied.AddressBookID != "" || tied.ContactID != "" {
		test.Errorf("the agent was shown the tie of a voice to a book it was not given: %+v", tied)
	}
	if contact := agent["Shared Contact"]; contact == nil || contact.ContactID != world.grantedCard {
		test.Errorf("the agent was not offered a contact of a book it was given: %+v", contact)
	}
	if agent["Private Contact"] != nil {
		test.Errorf("the agent was offered a contact of a book it was not given")
	}
}
