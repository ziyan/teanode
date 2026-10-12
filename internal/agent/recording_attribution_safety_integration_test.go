package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestRecordingSplitMergedSpeakerRetiresWholeAttribution(test *testing.T) {
	fixture := newRecordingCurationFixture(test, 1)
	var fact *models.AgentFact
	var ownerAgentId string
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		source, err := transaction.GetRecording(fixture.userId, fixture.source.ID)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CorrectRecordingSpeaker(fixture.userId, db.RecordingSpeakerCorrectionInput{RecordingID: source.ID, RecordingTranscriptionAttemptID: fixture.attempt.ID, LocalSpeakerID: "speaker-one", ConfirmedName: "Original merged participant", ClientCorrectionID: "merged-original", PassageStartMS: -1, PassageEndMS: -1, ExpectedSourceGeneration: source.SourceGeneration}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, source.ID); err != nil {
			test.Fatal(err)
		}
		ownerAgent, err := transaction.GetAgentByUser(fixture.userId)
		if err != nil {
			test.Fatal(err)
		}
		ownerAgentId = ownerAgent.ID
		documents, _, err := transaction.ListAgentDocumentsToDigest(ownerAgent.ID, nil, 10)
		if err != nil || len(documents) != 1 {
			test.Fatal(err)
		}
		page, err := transaction.PutAgentNode(&models.AgentNode{AgentID: ownerAgent.ID, Path: "projects/split-attribution", Kind: models.NodeProject, Name: "Split attribution fixture"})
		if err != nil {
			test.Fatal(err)
		}
		fact, err = transaction.AddAgentFact(&models.AgentFact{AgentID: ownerAgent.ID, NodeID: page.ID, Text: "The entire synthetic statement was attributed to one participant.", Evidence: []models.Evidence{{Kind: models.EvidenceDocument, ID: documents[0].ID, Quote: "synthetic fixture statement"}}})
		if err != nil {
			test.Fatal(err)
		}
	})
	for passageIndex, confirmedName := range []string{"Split participant one", "Split participant two"} {
		dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
			source, err := transaction.GetRecording(fixture.userId, fixture.source.ID)
			if err != nil {
				test.Fatal(err)
			}
			clientCorrectionId := "split-first"
			if passageIndex == 1 {
				clientCorrectionId = "split-second"
			}
			if _, err := transaction.CorrectRecordingSpeaker(fixture.userId, db.RecordingSpeakerCorrectionInput{RecordingID: source.ID, RecordingTranscriptionAttemptID: fixture.attempt.ID, LocalSpeakerID: "speaker-one", ConfirmedName: confirmedName, ClientCorrectionID: clientCorrectionId, PassageStartMS: int64(passageIndex * 3000), PassageEndMS: int64((passageIndex + 1) * 3000), ExpectedSourceGeneration: source.SourceGeneration}); err != nil {
				test.Fatal(err)
			}
			if err := transaction.SyncRecordingKnowledge(fixture.userId, source.ID); err != nil {
				test.Fatal(err)
			}
		})
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		segments, err := transaction.ListRecordingTranscriptSegments(fixture.userId, fixture.attempt.RecordingTranscriptRunID)
		if err != nil || len(segments) != 1 {
			test.Fatal(err)
		}
		if err := transaction.ProjectRecordingSpeakerCorrections(fixture.userId, segments); err != nil {
			test.Fatal(err)
		}
		if segments[0].HasUserConfirmedSpeaker || !segments[0].HasUnalignedSpeakerCorrection || segments[0].TranscriptText != "A synthetic fixture statement." {
			test.Fatal("split confirmation rewrote or confirmed the whole merged output")
		}
		retired, err := transaction.GetAgentFacts(ownerAgentId, []string{fact.ID})
		if err != nil || len(retired) != 1 || !retired[0].Dormant {
			test.Fatal("old whole-speaker attribution remained active", err)
		}
		documents, _, err := transaction.ListAgentDocumentsToDigest(ownerAgentId, nil, 10)
		if err != nil || len(documents) != 1 {
			test.Fatal(err)
		}
		chunks, err := transaction.ListAgentChunks(ownerAgentId, documents[0].ID)
		if err != nil || len(chunks) != 1 || !strings.Contains(chunks[0].Text, "Split participant one") || !strings.Contains(chunks[0].Text, "Split participant two") || !strings.Contains(chunks[0].Text, "do not assign the whole statement") {
			test.Fatal("learning lost split intervals or wording uncertainty", err)
		}
	})
	if err := CurateRecordingSpeakersLocally(test.Context(), &Settings{Database: fixture.database, Storage: fixture.store, Configuration: func() *config.Configuration {
		configuration := config.Default()
		configuration.Agent.Enabled = false
		return configuration
	}}, fixture.userId, fixture.source.ID); err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		references, err := transaction.ListRecordingVoiceReferences(fixture.userId, fixture.source.ID)
		if err != nil || len(references) != 2 || references[0].SpeakerIdentityID == references[1].SpeakerIdentityID {
			test.Fatal("selected user-confirmed split intervals were not kept separate", err)
		}
		for _, reference := range references {
			if reference.PassageStartMS < 3000 && reference.PassageEndMS > 3000 {
				test.Fatal("clip crossed the explicit speaker boundary")
			}
		}
	})
}

func TestRecordingCurationRejectsOverlappingAndUncertainSourcePassages(test *testing.T) {
	for _, sourceCondition := range []string{"different-speaker-overlap", "same-label-overlap", "cross-request-overlap", "uncertain-source"} {
		test.Run(sourceCondition, func(test *testing.T) {
			passages := []*models.RecordingTranscriptSegment{{LocalSpeakerID: "speaker-one", SegmentStartMS: 0, SegmentEndMS: 6000, TranscriptText: "A synthetic merged statement."}}
			if sourceCondition != "uncertain-source" {
				otherLocalId := "speaker-two"
				if sourceCondition == "same-label-overlap" {
					otherLocalId = "speaker-one"
				}
				passages = append(passages, &models.RecordingTranscriptSegment{LocalSpeakerID: otherLocalId, SegmentStartMS: 2000, SegmentEndMS: 5000, TranscriptText: "Overlapping synthetic evidence."})
			}
			fixture := newRecordingCurationFixtureWithPassages(test, 1, passages)
			if sourceCondition == "cross-request-overlap" {
				dbtest.Exec(test, fixture.database, `INSERT INTO recording_transcription_attempt (id,recording_transcript_run_id,recording_audio_chunk_id,attempt_index,created_at,recording_attempt_state,provider_model_name,currency_code) SELECT 'synthetic-other-attempt',recording_transcript_run_id,recording_audio_chunk_id,2,now(),'complete',provider_model_name,currency_code FROM recording_transcription_attempt WHERE id='`+fixture.attempt.ID+`'`)
				dbtest.Exec(test, fixture.database, `UPDATE recording_transcript_segment SET recording_transcription_attempt_id='synthetic-other-attempt' WHERE recording_transcript_run_id='`+fixture.attempt.RecordingTranscriptRunID+`' AND local_speaker_id='speaker-two'`)
			}
			var correction *models.RecordingSpeakerCorrection
			dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
				source, err := transaction.GetRecording(fixture.userId, fixture.source.ID)
				if err != nil {
					test.Fatal(err)
				}
				corrections, err := transaction.CorrectRecordingSpeaker(fixture.userId, db.RecordingSpeakerCorrectionInput{RecordingID: source.ID, RecordingTranscriptionAttemptID: fixture.attempt.ID, LocalSpeakerID: "speaker-one", ConfirmedName: "Confirmed synthetic participant", ClientCorrectionID: "ambiguous-confirmation", PassageStartMS: -1, PassageEndMS: -1, ExpectedSourceGeneration: source.SourceGeneration})
				if err != nil {
					test.Fatal(err)
				}
				correction = corrections[0]
			})
			if sourceCondition == "uncertain-source" {
				dbtest.Exec(test, fixture.database, `UPDATE recording_transcript_run SET recording_processing_state='uncertain' WHERE id='`+fixture.attempt.RecordingTranscriptRunID+`'`)
			}
			if err := CurateRecordingSpeakersLocally(test.Context(), &Settings{Database: fixture.database, Storage: fixture.store, Configuration: func() *config.Configuration {
				configuration := config.Default()
				configuration.Agent.Enabled = false
				return configuration
			}}, fixture.userId, fixture.source.ID); err != nil {
				test.Fatal(err)
			}
			dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
				references, err := transaction.ListRecordingVoiceReferences(fixture.userId, fixture.source.ID)
				if err != nil || len(references) != 0 {
					test.Fatal("ambiguous source became an automatic voice reference", err)
				}
			})
			err := fixture.database.Transaction(func(transaction db.Transaction) error {
				source, err := transaction.GetRecording(fixture.userId, fixture.source.ID)
				if err != nil {
					return err
				}
				_, err = transaction.CreateRecordingVoiceReference(fixture.userId, &models.RecordingVoiceReference{RecordingID: source.ID, RecordingMediaID: correction.RecordingMediaID, RecordingTranscriptRunID: fixture.attempt.RecordingTranscriptRunID, RecordingSpeakerCorrectionID: correction.ID, SpeakerIdentityID: correction.SpeakerIdentityID, PassageStartMS: 2200, PassageEndMS: 4800, SourceGeneration: source.SourceGeneration}, &models.RecordingBlob{BlobSizeBytes: 83244, BlobSHA256: strings.Repeat("a", 64)}, 1<<20)
				return err
			})
			if !errors.Is(err, db.ErrInvalidArguments) {
				test.Fatal("durable reference admission accepted ambiguous provenance", err)
			}
			if sourceCondition == "uncertain-source" {
				dbtest.Exec(test, fixture.database, `UPDATE recording_transcript_run SET recording_processing_state='complete' WHERE id='`+fixture.attempt.RecordingTranscriptRunID+`'`)
				dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
					if err := transaction.QueueRecordingSpeakerCuration(fixture.userId, fixture.source.ID); err != nil {
						test.Fatal(err)
					}
				})
				if err := CurateRecordingSpeakersLocally(test.Context(), &Settings{Database: fixture.database, Storage: fixture.store, Configuration: func() *config.Configuration {
					configuration := config.Default()
					configuration.Agent.Enabled = false
					return configuration
				}}, fixture.userId, fixture.source.ID); err != nil {
					test.Fatal(err)
				}
				dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
					references, err := transaction.ListRecordingVoiceReferences(fixture.userId, fixture.source.ID)
					if err != nil || len(references) != 1 {
						test.Fatal("completed evidence did not recover curation", err)
					}
				})
			}
		})
	}
}
