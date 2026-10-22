package db_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// recordingShareWorld is one finished recording of the fixture's owner, who
// shares the circle "Family" with sam; sam and kim share "Club"; lee is in no
// circle. The owner, sam and lee may use the agent, which is what the
// recording operations require, and kim may not.
type recordingShareWorld struct {
	fixture       *transcriptFixture
	ownerUserId   string
	recordingId   string
	sam, kim, lee *models.User
	family, club  *models.Circle
}

func newRecordingShareWorld(test *testing.T) *recordingShareWorld {
	test.Helper()
	fixture := newTranscriptFixture(test)
	attempt := completeFixtureTranscript(test, fixture)
	world := &recordingShareWorld{fixture: fixture, ownerUserId: fixture.userId, recordingId: fixture.media.RecordingID}
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		for username, target := range map[string]**models.User{"sam": &world.sam, "kim": &world.kim, "lee": &world.lee} {
			made, err := tx.CreateUser(&models.User{Username: username})
			if err != nil {
				test.Fatalf("CreateUser %s: %s", username, err)
			}
			*target = made
		}
		for _, person := range []string{world.ownerUserId, world.sam.ID, world.lee.ID} {
			dbtest.GrantPermission(test, tx, person, models.PermissionAgentUse)
		}
		var err error
		if world.family, err = tx.CreateCircle(&models.Circle{Name: "Family", CircleKind: models.CircleKindFamily}, world.ownerUserId); err != nil {
			test.Fatalf("CreateCircle: %s", err)
		}
		if _, err = tx.AddCircleMember(world.family.ID, world.sam.ID, models.MemberRoleMember, world.ownerUserId); err != nil {
			test.Fatalf("AddCircleMember: %s", err)
		}
		if world.club, err = tx.CreateCircle(&models.Circle{Name: "Club", CircleKind: models.CircleKindFriends}, world.sam.ID); err != nil {
			test.Fatalf("CreateCircle: %s", err)
		}
		if _, err = tx.AddCircleMember(world.club.ID, world.kim.ID, models.MemberRoleMember, world.sam.ID); err != nil {
			test.Fatalf("AddCircleMember: %s", err)
		}
		// The first speaker is named by the owner and the second is not, the
		// first passage's words are corrected, and the recording has a summary.
		source, err := tx.GetRecording(world.ownerUserId, world.recordingId)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := tx.CorrectRecordingSpeaker(world.ownerUserId, db.RecordingSpeakerCorrectionInput{
			RecordingID: source.ID, RecordingTranscriptionAttemptID: attempt.ID, LocalSpeakerID: "speaker-one", ConfirmedName: "Alex Example",
			ClientCorrectionID: "name-one", PassageStartMS: -1, PassageEndMS: -1, ExpectedSourceGeneration: source.SourceGeneration,
		}); err != nil {
			test.Fatal(err)
		}
		if err := tx.StoreGeneratedRecordingSummary(world.ownerUserId, world.recordingId, "Release planning", "They agreed to ship on Friday."); err != nil {
			test.Fatal(err)
		}
	})
	return world
}

func (self *recordingShareWorld) share(test *testing.T, tx db.Transaction, granteeUserId, granteeCircleId string, level models.RecordingShareLevel) *models.RecordingShare {
	test.Helper()
	share, err := tx.ShareRecording(self.ownerUserId, &models.RecordingShare{
		RecordingID: self.recordingId, GranteeUserID: granteeUserId, GranteeCircleID: granteeCircleId, RecordingShareLevel: level,
	})
	if err != nil {
		test.Fatalf("ShareRecording: %s", err)
	}
	return share
}

func levelOf(test *testing.T, tx db.Transaction, readerUserId, recordingId string) models.RecordingShareLevel {
	test.Helper()
	level, err := tx.GetRecordingShareLevel(readerUserId, recordingId)
	if err != nil {
		test.Fatalf("GetRecordingShareLevel: %s", err)
	}
	return level
}

// Sharing a recording is the owner's alone, needs a circle in common with a
// person or membership of a circle, says not found rather than why, and
// sharing again changes the level of the same share.
func TestARecordingShareIsTheOwnersAndNeedsACircleInCommon(test *testing.T) {
	world := newRecordingShareWorld(test)
	dbtest.RunTransactionOn(test, world.fixture.database, func(tx db.Transaction) {
		share := world.share(test, tx, world.sam.ID, "", models.RecordingShareLevelSummary)
		if share.ID == "" || share.RecordingShareLevel != models.RecordingShareLevelSummary || share.CreatedByUserID != world.ownerUserId ||
			share.RecordingTitle != "Release planning" || share.GranteeUsername != "sam" || share.OwnerUserID != world.ownerUserId {
			test.Fatalf("a share with someone in a shared circle: %+v", share)
		}
		again := world.share(test, tx, world.sam.ID, "", models.RecordingShareLevelTranscript)
		if again.ID != share.ID || again.RecordingShareLevel != models.RecordingShareLevelTranscript {
			test.Fatalf("sharing again changes the level of the same share: %+v", again)
		}

		for _, stranger := range []*models.User{world.kim, world.lee} {
			if _, err := tx.ShareRecording(world.ownerUserId, &models.RecordingShare{
				RecordingID: world.recordingId, GranteeUserID: stranger.ID, RecordingShareLevel: models.RecordingShareLevelSummary,
			}); !errors.Is(err, db.ErrNotFound) {
				test.Fatalf("a share with %s needs a circle in common: %v", stranger.Username, err)
			}
		}
		world.share(test, tx, "", world.family.ID, models.RecordingShareLevelSummary)
		if _, err := tx.ShareRecording(world.ownerUserId, &models.RecordingShare{
			RecordingID: world.recordingId, GranteeCircleID: world.club.ID, RecordingShareLevel: models.RecordingShareLevelSummary,
		}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a share with a circle the owner is not in: %v", err)
		}

		// Somebody else's recording is not theirs to share.
		if _, err := tx.ShareRecording(world.sam.ID, &models.RecordingShare{
			RecordingID: world.recordingId, GranteeCircleID: world.club.ID, RecordingShareLevel: models.RecordingShareLevelTranscript,
		}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a recording that is somebody else's: %v", err)
		}
		if _, err := tx.ShareRecording(world.ownerUserId, &models.RecordingShare{
			RecordingID: "nobody", GranteeUserID: world.sam.ID, RecordingShareLevel: models.RecordingShareLevelSummary,
		}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a recording that does not exist: %v", err)
		}

		for name, bad := range map[string]*models.RecordingShare{
			"nolevel":  {RecordingID: world.recordingId, GranteeUserID: world.sam.ID},
			"audio":    {RecordingID: world.recordingId, GranteeUserID: world.sam.ID, RecordingShareLevel: "audio"},
			"both":     {RecordingID: world.recordingId, GranteeUserID: world.sam.ID, GranteeCircleID: world.family.ID, RecordingShareLevel: models.RecordingShareLevelSummary},
			"neither":  {RecordingID: world.recordingId, RecordingShareLevel: models.RecordingShareLevelSummary},
			"yourself": {RecordingID: world.recordingId, GranteeUserID: world.ownerUserId, RecordingShareLevel: models.RecordingShareLevelSummary},
		} {
			if _, err := tx.ShareRecording(world.ownerUserId, bad); !errors.Is(err, db.ErrInvalidArguments) {
				test.Errorf("%s: %v", name, err)
			}
		}
		if _, err := tx.ShareRecording(world.ownerUserId, nil); !errors.Is(err, db.ErrInvalidArguments) {
			test.Errorf("no share: %v", err)
		}
	})
}

// A reader sees what their level shows and nothing the other level adds.
func TestAReaderSeesOnlyWhatTheirLevelShows(test *testing.T) {
	world := newRecordingShareWorld(test)
	dbtest.RunTransactionOn(test, world.fixture.database, func(tx db.Transaction) {
		if _, err := tx.ReadSharedRecording(world.sam.ID, world.recordingId, 0, 50); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("before any share: %v", err)
		}
		world.share(test, tx, world.sam.ID, "", models.RecordingShareLevelSummary)

		summary, err := tx.ReadSharedRecording(world.sam.ID, world.recordingId, 0, 50)
		if err != nil {
			test.Fatal(err)
		}
		if summary.RecordingTitle != "Release planning" || summary.RecordingSummary != "They agreed to ship on Friday." ||
			summary.RecordingShareLevel != models.RecordingShareLevelSummary || summary.OwnerUsername != "transcript-owner" ||
			len(summary.SpeakerNames) != 1 || summary.SpeakerNames[0] != "Alex Example" {
			test.Fatalf("the summary level shows the title, the summary, the owner and the speaker the owner named: %+v", summary)
		}
		if len(summary.Passages) != 0 || summary.PassageCount != 0 || summary.HasMorePassages {
			test.Fatalf("the summary level shows no transcript: %+v", summary)
		}

		world.share(test, tx, world.sam.ID, "", models.RecordingShareLevelTranscript)
		transcript, err := tx.ReadSharedRecording(world.sam.ID, world.recordingId, 0, 1)
		if err != nil {
			test.Fatal(err)
		}
		if transcript.RecordingShareLevel != models.RecordingShareLevelTranscript || transcript.PassageCount != 2 || len(transcript.Passages) != 1 || !transcript.HasMorePassages {
			test.Fatalf("the transcript level shows a page of the transcript: %+v", transcript)
		}
		rest, err := tx.ReadSharedRecording(world.sam.ID, world.recordingId, 1, 1)
		if err != nil || len(rest.Passages) != 1 || rest.HasMorePassages {
			test.Fatalf("the next page: %+v %v", rest, err)
		}
		if len(transcript.SpeakerNames) != 1 || transcript.SpeakerNames[0] != "Alex Example" || len(rest.SpeakerNames) != 0 {
			test.Fatalf("the speaker names come with the first page only: %+v %+v", transcript.SpeakerNames, rest.SpeakerNames)
		}
		all := append(transcript.Passages, rest.Passages...)
		texts := map[string]string{}
		for _, passage := range all {
			texts[passage.SpeakerName] = passage.TranscriptText
		}
		if texts["Alex Example"] != "We should ship on Friday." || texts["speaker-two"] != "Agreed, I will write the notes." {
			test.Fatalf("the passages carry the owner's speaker names and their words: %+v", texts)
		}

		if _, err := tx.ReadSharedRecording(world.sam.ID, world.recordingId, -1, 10); !errors.Is(err, db.ErrInvalidArguments) {
			test.Errorf("a negative offset: %v", err)
		}
		if _, err := tx.ReadSharedRecording(world.sam.ID, world.recordingId, 0, 0); !errors.Is(err, db.ErrInvalidArguments) {
			test.Errorf("an empty page: %v", err)
		}
		// The owner reads their own recording without a share, so the shared
		// read does not answer for it.
		if _, err := tx.ReadSharedRecording(world.ownerUserId, world.recordingId, 0, 10); !errors.Is(err, db.ErrNotFound) {
			test.Errorf("the owner through a share: %v", err)
		}
	})
}

// Redaction names what leaves: nothing about the audio, a voice sample, a
// contact or the processing of the recording is in what a reader is given, at
// either level.
func TestASharedRecordingCarriesNoAudioVoiceOrContact(test *testing.T) {
	world := newRecordingShareWorld(test)
	dbtest.RunTransactionOn(test, world.fixture.database, func(tx db.Transaction) {
		world.share(test, tx, world.sam.ID, "", models.RecordingShareLevelTranscript)
		shared, err := tx.ReadSharedRecording(world.sam.ID, world.recordingId, 0, 50)
		if err != nil {
			test.Fatal(err)
		}
		encoded, err := json.Marshal(shared)
		if err != nil {
			test.Fatal(err)
		}
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &keys); err != nil {
			test.Fatal(err)
		}
		for key := range keys {
			switch key {
			case "recordingId", "ownerUserId", "ownerUsername", "ownerName", "recordingTitle", "createdAt", "recordedAt", "timelineDurationMS",
				"recordingSummary", "recordingShareLevel", "speakerNames", "passages", "passageCount", "hasMorePassages":
			default:
				test.Errorf("a field nobody decided to share: %s", key)
			}
		}
		for _, word := range []string{"media", "sha256", "voice", "contact", "blob", "attempt", "cost", "sourceGeneration", "isAccountOwner", "captureGaps", "clientRecordingId"} {
			if strings.Contains(strings.ToLower(string(encoded)), strings.ToLower(word)) {
				test.Errorf("%q is in what a reader is given: %s", word, encoded)
			}
		}
	})
}

// A share to a person reaches them while the two share a circle, a share to a
// circle reaches its members while the owner is in it, the highest level wins,
// and a disabled owner, a reader or an owner without the permission, a deleted
// recording and a recording with no owner reach nobody.
func TestARecordingShareReachesOnlyWhoItShouldAndTheHighestLevelWins(test *testing.T) {
	world := newRecordingShareWorld(test)
	dbtest.RunTransactionOn(test, world.fixture.database, func(tx db.Transaction) {
		if level := levelOf(test, tx, world.sam.ID, world.recordingId); level != "" {
			test.Fatalf("before any share: %q", level)
		}
		world.share(test, tx, "", world.family.ID, models.RecordingShareLevelSummary)
		if level := levelOf(test, tx, world.sam.ID, world.recordingId); level != models.RecordingShareLevelSummary {
			test.Fatalf("through the circle: %q", level)
		}
		world.share(test, tx, world.sam.ID, "", models.RecordingShareLevelTranscript)
		if level := levelOf(test, tx, world.sam.ID, world.recordingId); level != models.RecordingShareLevelTranscript {
			test.Fatalf("the direct share and the circle's together: %q", level)
		}
		// A share is one way, and says nothing about the owner's own.
		if level := levelOf(test, tx, world.ownerUserId, world.recordingId); level != "" {
			test.Fatalf("the owner: %q", level)
		}
		// Kim shares Club with sam but is not in Family, and lee is in nothing.
		for _, other := range []*models.User{world.kim, world.lee} {
			if level := levelOf(test, tx, other.ID, world.recordingId); level != "" {
				test.Fatalf("%s: %q", other.Username, level)
			}
		}

	})

	// The reader needs the permission the recording operations require.
	dbtest.Exec(test, world.fixture.database, `DELETE FROM "user_group" WHERE "user_id" = '`+world.sam.ID+`'`)
	dbtest.RunTransactionOn(test, world.fixture.database, func(tx db.Transaction) {
		if level := levelOf(test, tx, world.sam.ID, world.recordingId); level != "" {
			test.Fatalf("a reader without agent:use: %q", level)
		}
	})
}

func TestARecordingShareDoesNotReachWhenTheOwnerOrTheRecordingIsGone(test *testing.T) {
	disable := func(world *recordingShareWorld, userId string) {
		dbtest.RunTransactionOn(test, world.fixture.database, func(tx db.Transaction) {
			if _, err := tx.UpdateUser(userId, func(account *models.User) error {
				now := time.Now()
				account.DisabledAt = &now
				return nil
			}); err != nil {
				test.Fatalf("UpdateUser: %s", err)
			}
		})
	}
	for name, change := range map[string]func(world *recordingShareWorld){
		"disabled owner":  func(world *recordingShareWorld) { disable(world, world.ownerUserId) },
		"disabled reader": func(world *recordingShareWorld) { disable(world, world.sam.ID) },
		"owner without agent:use": func(world *recordingShareWorld) {
			dbtest.Exec(test, world.fixture.database, `DELETE FROM "user_group" WHERE "user_id" = '`+world.ownerUserId+`'`)
		},
		"no owner": func(world *recordingShareWorld) {
			dbtest.Exec(test, world.fixture.database, `UPDATE "recording" SET "user_id" = NULL WHERE "id" = '`+world.recordingId+`'`)
		},
		"deleted recording": func(world *recordingShareWorld) {
			dbtest.Exec(test, world.fixture.database, `UPDATE "recording" SET "deleted_at" = now() WHERE "id" = '`+world.recordingId+`'`)
		},
	} {
		test.Run(name, func(test *testing.T) {
			world := newRecordingShareWorld(test)
			dbtest.RunTransactionOn(test, world.fixture.database, func(tx db.Transaction) {
				world.share(test, tx, world.sam.ID, "", models.RecordingShareLevelTranscript)
			})
			dbtest.RunTransactionOn(test, world.fixture.database, func(tx db.Transaction) {
				if level := levelOf(test, tx, world.sam.ID, world.recordingId); level != models.RecordingShareLevelTranscript {
					test.Fatalf("before the change: %q", level)
				}
			})
			change(world)
			dbtest.RunTransactionOn(test, world.fixture.database, func(tx db.Transaction) {
				if level := levelOf(test, tx, world.sam.ID, world.recordingId); level != "" {
					test.Errorf("after the change: %q", level)
				}
				if _, err := tx.ReadSharedRecording(world.sam.ID, world.recordingId, 0, 10); !errors.Is(err, db.ErrNotFound) {
					test.Errorf("a read after the change: %v", err)
				}
				if listed, err := tx.ListSharedRecordings(world.sam.ID, "", "", 10); err != nil || len(listed) != 0 {
					test.Errorf("a listing after the change: %+v %v", listed, err)
				}
				if reaching, err := tx.ListRecordingSharesReaching(world.sam.ID); err != nil || len(reaching) != 0 {
					test.Errorf("shares reaching after the change: %+v %v", reaching, err)
				}
			})
		})
	}
}

// A share to a person ends when the two no longer share a circle, and a share
// to a circle ends when the owner leaves it; one to a circle somebody else
// left stays and simply stops reaching them.
func TestLeavingACircleEndsTheRecordingSharesItMadePossible(test *testing.T) {
	world := newRecordingShareWorld(test)
	dbtest.RunTransactionOn(test, world.fixture.database, func(tx db.Transaction) {
		world.share(test, tx, world.sam.ID, "", models.RecordingShareLevelSummary)
		world.share(test, tx, "", world.family.ID, models.RecordingShareLevelTranscript)

		// Sam leaves Family: the direct share goes, and the circle's share
		// stays but no longer reaches him.
		if _, err := tx.RemoveCircleMember(world.family.ID, world.sam.ID); err != nil {
			test.Fatalf("RemoveCircleMember: %s", err)
		}
		if level := levelOf(test, tx, world.sam.ID, world.recordingId); level != "" {
			test.Errorf("after sam left: %q", level)
		}
		shares, err := tx.ListRecordingShares(world.recordingId)
		if err != nil || len(shares) != 1 || shares[0].GranteeCircleID != world.family.ID {
			test.Errorf("the direct share is gone, not just silent, and the circle's stays: %+v %v", shares, err)
		}

		// The owner leaves Family: the share to it goes.
		other, err := tx.CreateUser(&models.User{Username: "pat"})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := tx.AddCircleMember(world.family.ID, other.ID, models.MemberRoleAdmin, world.ownerUserId); err != nil {
			test.Fatalf("AddCircleMember: %s", err)
		}
		if _, err := tx.RemoveCircleMember(world.family.ID, world.ownerUserId); err != nil {
			test.Fatalf("RemoveCircleMember: %s", err)
		}
		if shares, err := tx.ListRecordingShares(world.recordingId); err != nil || len(shares) != 0 {
			test.Errorf("the owner's share to a circle they left: %+v %v", shares, err)
		}
	})
}

// Deleting a recording takes its shares with it.
func TestDeletingARecordingEndsItsShares(test *testing.T) {
	world := newRecordingShareWorld(test)
	dbtest.RunTransactionOn(test, world.fixture.database, func(tx db.Transaction) {
		world.share(test, tx, world.sam.ID, "", models.RecordingShareLevelSummary)
		if err := tx.DeleteRecording(world.ownerUserId, world.recordingId); err != nil {
			test.Fatal(err)
		}
		if shares, err := tx.ListRecordingShares(world.recordingId); err != nil || len(shares) != 0 {
			test.Errorf("shares of a deleted recording: %+v %v", shares, err)
		}
		if _, err := tx.ShareRecording(world.ownerUserId, &models.RecordingShare{
			RecordingID: world.recordingId, GranteeUserID: world.sam.ID, RecordingShareLevel: models.RecordingShareLevelSummary,
		}); !errors.Is(err, db.ErrNotFound) {
			test.Errorf("sharing a deleted recording: %v", err)
		}
	})
}

// The listings an overview uses: what the owner has shared, what reaches a
// reader, and a reader's recordings newest first without a transcript, one
// entry for a recording that is reached twice. Taking a share back is the
// owner's alone.
func TestTheRecordingShareListings(test *testing.T) {
	world := newRecordingShareWorld(test)
	dbtest.RunTransactionOn(test, world.fixture.database, func(tx db.Transaction) {
		direct := world.share(test, tx, world.sam.ID, "", models.RecordingShareLevelSummary)
		world.share(test, tx, "", world.family.ID, models.RecordingShareLevelTranscript)

		by, err := tx.ListRecordingSharesBy(world.ownerUserId)
		if err != nil || len(by) != 2 || by[0].GranteeUsername != "sam" || by[1].GranteeCircleName != "Family" {
			test.Fatalf("the shares the owner made: %+v %v", by, err)
		}
		if none, _ := tx.ListRecordingSharesBy(world.sam.ID); len(none) != 0 {
			test.Errorf("sam has shared nothing: %+v", none)
		}
		reaching, err := tx.ListRecordingSharesReaching(world.sam.ID)
		if err != nil || len(reaching) != 2 {
			test.Fatalf("the shares that reach sam: %+v %v", reaching, err)
		}
		for _, each := range reaching {
			if each.OwnerUserID != world.ownerUserId || each.OwnerUsername != "transcript-owner" || each.RecordingTitle != "Release planning" {
				test.Errorf("a share says whose recording it is: %+v", each)
			}
		}
		if none, _ := tx.ListRecordingSharesReaching(world.ownerUserId); len(none) != 0 {
			test.Errorf("nothing reaches the owner: %+v", none)
		}

		listed, err := tx.ListSharedRecordings(world.sam.ID, "", "", 10)
		if err != nil || len(listed) != 1 || listed[0].RecordingID != world.recordingId || listed[0].RecordingShareLevel != models.RecordingShareLevelTranscript ||
			len(listed[0].Passages) != 0 || len(listed[0].SpeakerNames) != 0 || listed[0].RecordingSummary == "" {
			test.Fatalf("a recording reached twice is listed once at the higher level, without its transcript: %+v %v", listed, err)
		}
		if listed, err := tx.ListSharedRecordings(world.sam.ID, "someone-else", "", 10); err != nil || len(listed) != 0 {
			test.Errorf("narrowed to another owner: %+v %v", listed, err)
		}
		if listed, err := tx.ListSharedRecordings(world.sam.ID, "", world.recordingId, 10); err != nil || len(listed) != 0 {
			test.Errorf("continued after the only recording: %+v %v", listed, err)
		}
		if _, err := tx.ListSharedRecordings(world.sam.ID, "", "", 0); !errors.Is(err, db.ErrInvalidArguments) {
			test.Errorf("an empty page: %v", err)
		}

		// Taking a share back is the owner's alone.
		if err := tx.DeleteRecordingShare(world.sam.ID, direct.ID); !errors.Is(err, db.ErrNotFound) {
			test.Errorf("a reader taking a share back: %v", err)
		}
		if err := tx.DeleteRecordingShare(world.ownerUserId, direct.ID); err != nil {
			test.Errorf("the owner taking a share back: %s", err)
		}
		if err := tx.DeleteRecordingShare(world.ownerUserId, direct.ID); !errors.Is(err, db.ErrNotFound) {
			test.Errorf("taking it back twice: %v", err)
		}
	})
}
