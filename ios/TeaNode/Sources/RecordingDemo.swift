#if DEBUG
    import SwiftUI
    import TeaNodeKit

    extension DemoServer {
        static let recordingAgentSettingsFixture =
            #"{"enabled":true,"instructions":"","allowPrivateAddresses":[],"skipCertificateCheck":[],"providers":[],"models":{"default":"","fast":"","quick":"","embedding":"","triage":"","research":"","summarize":"","reply":"","ask":"","schedule":"","compact":"","scan":"","synthesize":"","decide":"","categorize":"","transcription":"demo:transcription","embeddingDimensions":0,"choices":[]},"features":{},"limits":{"maxBodyCharacters":0,"dailyTokensPerAgent":0,"monthlyTokensPerServer":0,"dailyCostPerAgent":0,"monthlyCostPerServer":0,"maxRoundsPerAsk":0,"maxRoundsPerResearch":0,"maxRoundsPerReply":0,"maxRoundsPerDream":0,"maxToolCallsPerRun":0,"concurrency":0,"scanConcurrency":0,"rewriteConcurrency":0,"dreamShare":0,"ingestChunksPerRun":0,"embeddingTokensPerDay":0,"maxRoundsPerSubagent":0,"goalTurnsPerDay":0,"goalTurnsAlone":0,"goalsInProgress":0,"backgroundWakesAlone":0,"requestTimeout":"2m","maxRecordingMediaBytes":"2GB","maxRecordingAccountBytes":"64GB"},"retention":{"runs":"720h","corrections":"2160h"},"currency":"USD","search":{"kind":"","hasApiKey":false},"tools":{"disabled":[],"confirm":[],"catalog":[]},"voice":{"enabled":false,"provider":"","transcriptionModel":"","silenceMS":0,"speechModel":"","speechVoice":"","askModel":""},"browser":{"enabled":false,"cdpEndpoint":"","attachTabs":true,"allowPrivateAddresses":[],"idleTimeout":"5m","maxContexts":4},"mcpServers":[],"finance":{"offeredProviders":[],"plaid":{"environment":"","clientId":"","hasSecret":false,"countryCodes":[],"products":[]}},"works":[],"families":[],"kinds":[]}"#
        static let recordingSourceFixture =
            #"{"id":"source-a","kind":"recording","name":"recording:recording-a","enabled":true,"documentCount":5,"chunkCount":7,"more":false,"lastError":"","cron":"17 3 * * *","specification":{"settings":{"recordingId":"recording-a","recordingTitle":"Design discussion"}}}"#
        static let recordingFixture =
            #"{"id":"recording-a","userId":"account-a","createdAt":"2030-01-02T03:04:05Z","updatedAt":"2030-01-02T03:04:05Z","recordedAt":"2030-01-02T02:04:05Z","clientRecordingId":"local-a","recordingTitle":"Design discussion","recordingOrigin":"capture","recordingCaptureState":"stopped","timelineDurationMS":3600000,"sourceGeneration":4,"captureGaps":[{"gapId":"gap-a","gapStartMS":1200000,"gapEndMS":1220000,"recordingGapReason":"paused"}]}"#
        static let recordingRunFixture =
            #"{"id":"run-a","recordingId":"recording-a","recordingMediaId":"media-a","versionNumber":2,"recordingProcessingState":"complete","providerModelName":"demo:transcription","processingErrorMessage":"","createdAt":"2030-01-02T03:04:05Z"}"#
        static let recordingSegmentFixture =
            #"{"id":"segment-a","recordingTranscriptRunId":"run-a","recordingTranscriptionAttemptId":"attempt-a","localSpeakerId":"speaker-a","segmentStartMS":30000,"segmentEndMS":44000,"transcriptText":"We should keep the original media and preserve corrections when another transcript version is generated.","effectiveTranscriptText":"We should keep the original media and preserve manual corrections when another transcript version is generated.","appliedTextCorrectionId":"text-correction-a","hasUserConfirmedSpeaker":true,"effectiveSpeakerIdentityId":"identity-a","effectiveSpeakerName":"Speaker A","hasUnalignedSpeakerCorrection":false}"#
        static let recordingTextCorrectionFixture =
            #"{"id":"text-correction-a","recordingId":"recording-a","recordingMediaId":"media-a","recordingTranscriptRunId":"run-a","recordingTranscriptSegmentId":"segment-a","clientCorrectionId":"text-request-a","passageStartMS":30000,"passageEndMS":44000,"originalTranscriptText":"We should keep the original media and preserve corrections when another transcript version is generated.","correctedTranscriptText":"We should keep the original media and preserve manual corrections when another transcript version is generated.","sourceGeneration":4,"createdAt":"2030-01-02T03:04:05Z","isRemoved":false}"#
        static let recordingSpeakersFixture =
            #"{"sourceGeneration":4,"speakerIdentities":[{"id":"identity-a","confirmedName":"Speaker A"}],"speakerCorrections":[{"id":"speaker-correction-a","recordingMediaId":"media-a","recordingTranscriptionAttemptId":"attempt-a","speakerIdentityId":"identity-a","localSpeakerId":"speaker-a","passageStartMS":30000,"passageEndMS":44000,"sourceGeneration":3,"isRemoved":false}],"voiceReferences":[{"id":"reference-a","speakerIdentityId":"identity-a","recordingMediaId":"media-a","recordingTranscriptRunId":"run-a","passageStartMS":30000,"passageEndMS":36000,"sourceGeneration":3,"isRevoked":false}]}"#
        static let recordingMediaFixture =
            #"{"id":"media-a","recordingId":"recording-a","clientMediaId":"original-a","originalFilename":"meeting.caf","originalContentType":"audio/x-caf","originalSizeBytes":19200000,"originalSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","timelineStartMS":0,"mediaDurationMS":600000,"partCount":3,"recordingUploadState":"complete","verifiedAt":"2030-01-02T03:04:05Z"}"#

        static func recordingPreviewURL() throws -> URL {
            let sampleRateHz = 16000
            let sampleByteCount = sampleRateHz * 2
            var waveBytes = Data()
            func appendWord(_ wordNumber: UInt32) {
                var littleEndianNumber = wordNumber.littleEndian
                withUnsafeBytes(of: &littleEndianNumber) { waveBytes.append(contentsOf: $0) }
            }
            waveBytes.append(Data("RIFF".utf8))
            appendWord(UInt32(sampleByteCount + 36))
            waveBytes.append(Data("WAVEfmt ".utf8))
            appendWord(16)
            waveBytes.append(contentsOf: [1, 0, 1, 0])
            appendWord(UInt32(sampleRateHz))
            appendWord(UInt32(sampleRateHz * 2))
            waveBytes.append(contentsOf: [2, 0, 16, 0])
            waveBytes.append(Data("data".utf8))
            appendWord(UInt32(sampleByteCount))
            waveBytes.append(Data(repeating: 0, count: sampleByteCount))
            let destination = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).appendingPathExtension("wav")
            try waveBytes.write(to: destination, options: .atomic)
            return destination
        }

        static func recordingAnswer(query: String) -> String? {
            if query.contains("GetSettings"), query.contains("agent {") {
                return "{\"data\":{\"GetSettings\":{\"agent\":\(recordingAgentSettingsFixture)}}}"
            }
            if query.contains("ListAgentModels") {
                return
                    #"{"data":{"ListAgentModels":[{"name":"demo:transcription","canTranscribe":true,"hasSpeakerDiarization":true,"hasSegmentTimestamps":true,"hasSpeakerReferences":true,"maximumAudioBytes":24000000,"maximumSpeakerReferenceCount":3}]}}"#
            }

            if query.contains("ReadRecordingTranscriptTimeline") {
                return
                    "{\"data\":{\"ReadRecordingTranscriptTimeline\":{\"sourceGeneration\":4,\"segments\":[\(recordingSegmentFixture)],\"transcriptRuns\":[\(recordingRunFixture)],\"segmentCount\":1,\"hasMoreSegments\":false}}}"
            }
            if query.contains("ReadRecordingTranscriptTextCorrections") {
                return "{\"data\":{\"ReadRecordingTranscriptTextCorrections\":[\(recordingTextCorrectionFixture)]}}"
            }
            if query.contains("ReadRecordingTranscriptPassageOffset") {
                return #"{"data":{"ReadRecordingTranscriptPassageOffset":0}}"#
            }
            if query.contains("ReadRecordingTranscript") {
                return
                    "{\"data\":{\"ReadRecordingTranscript\":{\"transcriptRun\":\(recordingRunFixture),\"segments\":[\(recordingSegmentFixture)],\"attempts\":[{\"id\":\"attempt-a\",\"recordingAttemptState\":\"complete\",\"providerModelName\":\"demo:transcription\",\"hasProviderUsage\":false,\"estimatedCostAmount\":null,\"actualCostAmount\":null,\"currencyCode\":\"USD\",\"attemptErrorMessage\":\"\"}],\"segmentCount\":1,\"hasMoreSegments\":false}}}"
            }
            if query.contains("ReadRecordingSpeakers") { return "{\"data\":{\"ReadRecordingSpeakers\":\(recordingSpeakersFixture)}}" }
            if query.contains("ReadRecordingReferenceInvalidations") { return "{\"data\":{\"ReadRecordingReferenceInvalidations\":[]}}" }
            if query.contains("ReadRecordingLearning"), Self.screen == "recording-learning-stopped" {
                return
                    #"{"data":{"ReadRecordingLearning":{"sourceGeneration":4,"hasAgent":true,"hasStoppedReading":true,"knowledgeSource":null,"pendingDocumentCount":0,"digestedDocumentCount":0,"lastPublishedSourceGeneration":4,"isCurationPending":false}}}"#
            }
            if query.contains("ReadRecordingLearning") {
                return
                    "{\"data\":{\"ReadRecordingLearning\":{\"sourceGeneration\":4,\"hasAgent\":true,\"knowledgeSource\":\(recordingSourceFixture),\"pendingDocumentCount\":2,\"digestedDocumentCount\":5,\"lastPublishedSourceGeneration\":4,\"isCurationPending\":true}}}"
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
                case "recording-model-settings", "recording-quota-settings": RecordingOperatorDemoScreen(connection: connection, screen: screen)
                case "recording-citation":
                    RecordingCitationScreen(
                        connection: connection,
                        citation: RecordingLink(
                            URL(string: "/recordings/recording-a?version=run-a&startMS=30000&endMS=44000")!, server: DemoServer.account.server)!)
                case "recording-source-settings":
                    KnowledgeSourceEditor(
                        connection: connection,
                        source: DemoServer.recordingObject(DemoServer.recordingSourceFixture, as: KnowledgeSource.self),
                        sourceTypes: [], mailboxes: [], onSaved: { _ in })
                case "recording-attachment-import":
                    RecordingAttachmentImportView(
                        connection: connection,
                        attachment: AgentAttachment(id: "attachment-a", name: "meeting.wav", contentType: "audio/wav", size: 120000))
                case "recording-learning-stopped": RecordingDetailScreen(connection: connection, recording: recording, shouldFocusLearning: true)
                case "recording-compatible-playback", "recording-original-playback":
                    RecordingPlaybackScreen(
                        connection: connection, media: DemoServer.recordingObject(DemoServer.recordingMediaFixture, as: RecordingMedia.self))
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
