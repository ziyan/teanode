#if DEBUG
    import SwiftUI
    import TeaNodeKit

    extension DemoServer {
        static let recordingFixture =
            #"{"id":"recording-a","userId":"account-a","createdAt":"2030-01-02T03:04:05Z","updatedAt":"2030-01-02T03:04:05Z","recordedAt":"2030-01-02T02:04:05Z","clientRecordingId":"local-a","recordingTitle":"Design discussion","recordingOrigin":"capture","recordingCaptureState":"stopped","timelineDurationMS":3600000,"sourceGeneration":4,"captureGaps":[{"gapId":"gap-a","gapStartMS":1200000,"gapEndMS":1220000,"recordingGapReason":"paused"}]}"#
        static let recordingRunFixture =
            #"{"id":"run-a","recordingId":"recording-a","recordingMediaId":"media-a","versionNumber":2,"recordingProcessingState":"complete","providerModelName":"demo:transcription","processingErrorMessage":"","createdAt":"2030-01-02T03:04:05Z"}"#
        static let recordingSegmentFixture =
            #"{"id":"segment-a","recordingTranscriptRunId":"run-a","recordingTranscriptionAttemptId":"attempt-a","localSpeakerId":"speaker-a","segmentStartMS":30000,"segmentEndMS":44000,"transcriptText":"We should keep the original media and preserve corrections when another transcript version is generated.","effectiveTranscriptText":"We should keep the original media and preserve manual corrections when another transcript version is generated.","appliedTextCorrectionId":"text-correction-a"}"#
        static let recordingTextCorrectionFixture =
            #"{"id":"text-correction-a","recordingId":"recording-a","recordingMediaId":"media-a","recordingTranscriptRunId":"run-a","recordingTranscriptSegmentId":"segment-a","clientCorrectionId":"text-request-a","passageStartMS":30000,"passageEndMS":44000,"originalTranscriptText":"We should keep the original media and preserve corrections when another transcript version is generated.","correctedTranscriptText":"We should keep the original media and preserve manual corrections when another transcript version is generated.","sourceGeneration":4,"createdAt":"2030-01-02T03:04:05Z","isRemoved":false}"#
        static let recordingSpeakersFixture =
            #"{"sourceGeneration":4,"speakerIdentities":[{"id":"identity-a","confirmedName":"Speaker A"}],"speakerCorrections":[{"id":"speaker-correction-a","recordingMediaId":"media-a","recordingTranscriptionAttemptId":"attempt-a","speakerIdentityId":"identity-a","localSpeakerId":"speaker-a","passageStartMS":30000,"passageEndMS":44000,"sourceGeneration":3,"isRemoved":false}],"voiceReferences":[{"id":"reference-a","speakerIdentityId":"identity-a","recordingMediaId":"media-a","recordingTranscriptRunId":"run-a","passageStartMS":30000,"passageEndMS":36000,"sourceGeneration":3,"isRevoked":false}]}"#
        static let recordingMediaFixture =
            #"{"id":"media-a","recordingId":"recording-a","clientMediaId":"original-a","originalFilename":"meeting.caf","originalContentType":"audio/x-caf","originalSizeBytes":19200000,"originalSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","timelineStartMS":0,"mediaDurationMS":600000,"partCount":3,"recordingUploadState":"complete","verifiedAt":"2030-01-02T03:04:05Z"}"#

        static func recordingAnswer(query: String) -> String? {
            if query.contains("ReadRecordingTranscriptTimeline") {
                return
                    "{\"data\":{\"ReadRecordingTranscriptTimeline\":{\"sourceGeneration\":4,\"segments\":[\(recordingSegmentFixture)],\"transcriptRuns\":[\(recordingRunFixture)],\"segmentCount\":1,\"hasMoreSegments\":false}}}"
            }
            if query.contains("ReadRecordingTranscriptTextCorrections") {
                return "{\"data\":{\"ReadRecordingTranscriptTextCorrections\":[\(recordingTextCorrectionFixture)]}}"
            }
            if query.contains("ReadRecordingTranscript") {
                return
                    "{\"data\":{\"ReadRecordingTranscript\":{\"transcriptRun\":\(recordingRunFixture),\"segments\":[\(recordingSegmentFixture)],\"attempts\":[{\"id\":\"attempt-a\",\"recordingAttemptState\":\"complete\",\"providerModelName\":\"demo:transcription\",\"hasProviderUsage\":false,\"estimatedCostAmount\":null,\"actualCostAmount\":null,\"currencyCode\":\"USD\",\"attemptErrorMessage\":\"\"}],\"segmentCount\":1,\"hasMoreSegments\":false}}}"
            }
            if query.contains("ReadRecordingSpeakers") { return "{\"data\":{\"ReadRecordingSpeakers\":\(recordingSpeakersFixture)}}" }
            if query.contains("ReadRecordingReferenceInvalidations") { return "{\"data\":{\"ReadRecordingReferenceInvalidations\":[]}}" }
            if query.contains("ReadRecordingLearning") {
                return
                    #"{"data":{"ReadRecordingLearning":{"sourceGeneration":4,"hasAgent":true,"knowledgeSource":null,"pendingDocumentCount":2,"digestedDocumentCount":5,"lastPublishedSourceGeneration":4,"isCurationPending":true}}}"#
            }
            if query.contains("ReadRecording(") {
                return
                    "{\"data\":{\"ReadRecording\":{\"recording\":\(recordingFixture),\"mediaFiles\":[\(recordingMediaFixture)],\"uploadParts\":[],\"transcriptRuns\":[\(recordingRunFixture)]}}}"
            }
            return nil
        }

        static func recordingObject<Object: Decodable>(_ json: String, as: Object.Type) -> Object {
            try! JSONDecoder().decode(Object.self, from: Data(json.utf8))
        }
    }

    struct RecordingDemoScreen: View {
        let connection: Connection
        let screen: String
        private var recording: Recording { DemoServer.recordingObject(DemoServer.recordingFixture, as: Recording.self) }
        private var segment: RecordingTranscriptSegment {
            DemoServer.recordingObject(DemoServer.recordingSegmentFixture, as: RecordingTranscriptSegment.self)
        }

        var body: some View {
            NavigationStack {
                switch screen {
                case "recording-detail": RecordingDetailScreen(connection: connection, recording: recording)
                case "recording-transcript": RecordingTranscriptScreen(connection: connection, recording: recording)
                case "recording-version":
                    RecordingTranscriptScreen(
                        connection: connection, recording: recording,
                        transcriptRun: DemoServer.recordingObject(DemoServer.recordingRunFixture, as: RecordingTranscriptRun.self))
                case "recording-speakers":
                    RecordingSpeakersScreen(
                        connection: connection, recording: recording,
                        initial: DemoServer.recordingObject(DemoServer.recordingSpeakersFixture, as: RecordingSpeakersView.self))
                case "recording-text-history": RecordingTextHistoryScreen(connection: connection, recordingId: recording.id)
                case "recording-text-editor": RecordingTextEditor(connection: connection, recordingId: recording.id, segment: segment, didSave: {})
                default: RecordingsScreen(connection: connection)
                }
            }
        }
    }
#endif
