#if DEBUG
    import SwiftUI
    import TeaNodeKit

    extension DemoServer {
        static let recordingAgentSettingsFixture =
            #"{"enabled":true,"instructions":"","allowPrivateAddresses":[],"skipCertificateCheck":[],"providers":[],"models":{"default":"","fast":"","quick":"","embedding":"","triage":"","research":"","summarize":"","reply":"","ask":"","schedule":"","compact":"","scan":"","synthesize":"","decide":"","categorize":"","transcription":"demo:transcription","embeddingDimensions":0,"choices":[]},"features":{},"limits":{"maxBodyCharacters":0,"dailyTokensPerAgent":0,"monthlyTokensPerServer":0,"dailyCostPerAgent":0,"monthlyCostPerServer":0,"maxRoundsPerAsk":0,"maxRoundsPerResearch":0,"maxRoundsPerReply":0,"maxRoundsPerDream":0,"maxToolCallsPerRun":0,"concurrency":0,"scanConcurrency":0,"rewriteConcurrency":0,"dreamShare":0,"ingestChunksPerRun":0,"embeddingTokensPerDay":0,"maxRoundsPerSubagent":0,"goalTurnsPerDay":0,"goalTurnsAlone":0,"goalsInProgress":0,"backgroundWakesAlone":0,"requestTimeout":"2m","maxRecordingMediaBytes":"2GB","maxRecordingAccountBytes":"64GB"},"retention":{"runs":"720h","corrections":"2160h"},"currency":"USD","search":{"kind":"","hasApiKey":false},"tools":{"disabled":[],"confirm":[],"catalog":[]},"voice":{"enabled":false,"provider":"","transcriptionModel":"","silenceMS":0,"speechModel":"","speechVoice":"","askModel":""},"browser":{"enabled":false,"cdpEndpoint":"","attachTabs":true,"allowPrivateAddresses":[],"idleTimeout":"5m","maxContexts":4},"mcpServers":[],"finance":{"offeredProviders":[],"plaid":{"environment":"","clientId":"","hasSecret":false,"countryCodes":[],"products":[]}},"works":[],"families":[],"kinds":[]}"#
        static let providerPricesFixture =
            #""providers":[{"name":"openai","kind":"openai","baseUrl":"","hasApiKey":true,"hasRefreshToken":false,"account":"","enabled":true,"allow":[],"deny":[],"pricingInput":2.5,"pricingOutput":10,"pricingCacheRead":1.25,"pricingCacheWrite":0,"pricingAudioInput":6,"modelPricing":[{"model":"gpt-4o-transcribe*","input":2.5,"output":10,"cacheRead":0,"cacheWrite":0,"audioInput":6}],"planUsage":null}]"#
        static let recordingSourceFixture =
            #"{"id":"source-a","kind":"recording","name":"recording:recording-a","enabled":true,"documentCount":5,"chunkCount":7,"more":false,"lastError":"","cron":"17 3 * * *","specification":{"settings":{"recordingId":"recording-a","recordingTitle":"Choosing the second layout"}}}"#
        static let recordingFixture =
            #"{"id":"recording-a","userId":"account-a","createdAt":"2030-01-02T03:04:05Z","updatedAt":"2030-01-02T03:04:05Z","recordedAt":"2030-01-02T02:04:05Z","clientRecordingId":"local-a","recordingTitle":"Choosing the second layout","recordingTitleSource":"generated","recordingSummary":"Priya and Robin compared two home screen layouts. They chose the one that puts search first and agreed to test it with ten people for a week, with a third person asking to join the group.","recordingOrigin":"capture","recordingCaptureState":"stopped","timelineDurationMS":3600000,"sourceGeneration":4,"captureGaps":[{"gapId":"gap-a","gapStartMS":1200000,"gapEndMS":1220000,"recordingGapReason":"paused"}]}"#
        static let recordingRunFixture =
            #"{"id":"run-a","recordingId":"recording-a","recordingMediaId":"media-a","versionNumber":2,"recordingProcessingState":"complete","providerModelName":"demo:transcription","processingErrorMessage":"","createdAt":"2030-01-02T03:04:05Z"}"#
        static let recordingSegmentFixture =
            #"{"id":"segment-a","recordingTranscriptRunId":"run-a","recordingTranscriptionAttemptId":"attempt-a","localSpeakerId":"speaker-a","segmentStartMS":30000,"segmentEndMS":44000,"transcriptText":"Yes. We can run it for a week with ten people and compare how long a task takes them.","effectiveTranscriptText":"Yes. We can run it for a week with ten people and compare how long each task takes them.","appliedTextCorrectionId":"text-correction-a","hasUserConfirmedSpeaker":true,"effectiveSpeakerIdentityId":"identity-b","effectiveSpeakerName":"Priya","isAccountOwner":false,"hasUnalignedSpeakerCorrection":false}"#
        static let recordingTextCorrectionFixture =
            #"{"id":"text-correction-a","recordingId":"recording-a","recordingMediaId":"media-a","recordingTranscriptRunId":"run-a","recordingTranscriptSegmentId":"segment-a","clientCorrectionId":"text-request-a","passageStartMS":30000,"passageEndMS":44000,"originalTranscriptText":"Yes. We can run it for a week with ten people and compare how long a task takes them.","correctedTranscriptText":"Yes. We can run it for a week with ten people and compare how long each task takes them.","sourceGeneration":4,"createdAt":"2030-01-02T03:04:05Z","isRemoved":false}"#
        static let recordingSpeakersFixture =
            #"{"sourceGeneration":4,"speakerIdentities":[{"id":"identity-a","confirmedName":"Robin","isAccountOwner":true},{"id":"identity-b","confirmedName":"Priya","isAccountOwner":false}],"speakerCorrections":[{"id":"speaker-correction-a","recordingMediaId":"media-a","recordingTranscriptionAttemptId":"attempt-a","speakerIdentityId":"identity-b","localSpeakerId":"speaker-a","passageStartMS":-1,"passageEndMS":-1,"sourceGeneration":3,"isRemoved":false},{"id":"speaker-correction-b","recordingMediaId":"media-a","recordingTranscriptionAttemptId":"attempt-a","speakerIdentityId":"identity-a","localSpeakerId":"speaker-b","passageStartMS":-1,"passageEndMS":-1,"sourceGeneration":3,"isRemoved":false}],"voiceReferences":[{"id":"reference-a","speakerIdentityId":"identity-b","recordingMediaId":"media-a","recordingTranscriptRunId":"run-a","passageStartMS":30000,"passageEndMS":36000,"sourceGeneration":3,"isRevoked":false}]}"#
        static let recordingMediaFixture =
            #"{"id":"media-a","recordingId":"recording-a","clientMediaId":"original-a","originalFilename":"meeting.caf","originalContentType":"audio/x-caf","originalSizeBytes":19200000,"originalSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","timelineStartMS":0,"mediaDurationMS":600000,"partCount":3,"recordingUploadState":"complete","verifiedAt":"2030-01-02T03:04:05Z"}"#

        static func conversationSegmentJSON(
            _ id: String, speaker: String, startMS: Int, endMS: Int, text: String, identityId: String? = nil, name: String? = nil,
            isAccountOwner: Bool = false, temporaryLabel: String? = nil, mergedLabel: String? = nil
        ) -> String {
            let speakerFields =
                identityId.map {
                    #","hasUserConfirmedSpeaker":true,"effectiveSpeakerIdentityId":"\#($0)","effectiveSpeakerName":"\#(name ?? "")","isAccountOwner":\#(isAccountOwner)"#
                }
                ?? (#","hasUserConfirmedSpeaker":false,"isAccountOwner":false"#
                    + (temporaryLabel.map { #","effectiveSpeakerName":"\#($0)","temporarySpeakerLabel":"\#($0)""# } ?? "")
                    + (mergedLabel.map { #","mergedTemporarySpeakerLabel":"\#($0)""# } ?? ""))
            return
                #"{"id":"\#(id)","recordingTranscriptRunId":"run-a","recordingTranscriptionAttemptId":"attempt-a","localSpeakerId":"\#(speaker)","segmentStartMS":\#(startMS),"segmentEndMS":\#(endMS),"transcriptText":"\#(text)"\#(speakerFields),"hasUnalignedSpeakerCorrection":false}"#
        }

        /// A conversation of three people: Priya, Robin (the account owner) and a speaker nobody has named.
        static var conversationSegmentFixtures: [String] {
            [
                conversationSegmentJSON(
                    "segment-1", speaker: "speaker-a", startMS: 0, endMS: 8000,
                    text: "Thanks for joining. Today we need to pick one of the two layouts for the new home screen.", identityId: "identity-b", name: "Priya"),
                conversationSegmentJSON(
                    "segment-2", speaker: "speaker-a", startMS: 8000, endMS: 15000,
                    text: "Layout one keeps the list. Layout two puts search first.", identityId: "identity-b", name: "Priya"),
                conversationSegmentJSON(
                    "segment-3", speaker: "speaker-b", startMS: 15000, endMS: 24000,
                    text: "I prefer the second one. People start from search more than they browse.", identityId: "identity-a", name: "Robin", isAccountOwner: true),
                conversationSegmentJSON(
                    "segment-4", speaker: "speaker-b", startMS: 24000, endMS: 30000,
                    text: "Can we test it with a small group before we commit?", identityId: "identity-a", name: "Robin", isAccountOwner: true),
                recordingSegmentFixture,
                conversationSegmentJSON(
                    "segment-5", speaker: "speaker-c", startMS: 44000, endMS: 52000, text: "Could I join the test group too?"),
                conversationSegmentJSON(
                    "segment-6", speaker: "speaker-a", startMS: 52000, endMS: 60000, text: "Of course. I will send the invitation today.",
                    identityId: "identity-b", name: "Priya"),
            ]
        }

        /// The id of the recording the capture screen's screenshots show as under way.
        static let liveCaptureRecordingId = "recording-live"

        /// A capture under way: the first piece has been transcribed with its speakers, one of whom nobody has named yet
        /// ("Speaker 1"), and what was said after it is only live words, heard a moment ago.
        static var liveCaptureSegmentFixtures: [String] {
            [
                conversationSegmentJSON(
                    "live-segment-1", speaker: "speaker-a", startMS: 0, endMS: 8000,
                    text: "Thanks for joining. Today we need to pick one of the two layouts for the new home screen.", identityId: "identity-b", name: "Priya"),
                conversationSegmentJSON(
                    "live-segment-2", speaker: "speaker-b", startMS: 8000, endMS: 17000,
                    text: "I prefer the second one. People start from search more than they browse.", identityId: "identity-a", name: "Robin", isAccountOwner: true),
                conversationSegmentJSON(
                    "live-segment-3", speaker: "speaker-c", startMS: 17000, endMS: 23000, text: "How many people will test it?",
                    temporaryLabel: "Speaker 1"),
                // The voice of "Speaker 1" heard again under a label of its own, which the capture found to be the same speaker.
                conversationSegmentJSON(
                    "live-segment-3b", speaker: "speaker-d", startMS: 23000, endMS: 24500, text: "Is it ten, or more?",
                    temporaryLabel: "Speaker 1", mergedLabel: "Speaker 4"),
                conversationSegmentJSON(
                    "live-segment-4", speaker: "speaker-a", startMS: 24500, endMS: 28000, text: "Ten people, for one week.", identityId: "identity-b",
                    name: "Priya"),
            ]
        }

        static var liveCaptureUtterances: [RecordingLiveUtterance] {
            [
                RecordingLiveUtterance(
                    utteranceId: "live-utterance-1", utteranceStartMS: 28000, utteranceEndMS: 34000,
                    transcriptText: "Ten sounds fine, and we can compare how long a task takes them."),
                RecordingLiveUtterance(
                    utteranceId: "live-utterance-2", utteranceStartMS: 35000, utteranceEndMS: 38500,
                    transcriptText: "Could I join the test group as well?"),
            ]
        }

        /// The conversation of the capture screen: the passages, the live words, and the words being said now.
        static var liveCaptureBubbles: [RecordingConversationBubble] {
            RecordingConversation.bubbles(
                liveCaptureSegmentFixtures.map { recordingObject($0, as: RecordingTranscriptSegment.self) },
                liveUtterances: RecordingLiveWords.shown(
                    serverUtterances: liveCaptureUtterances, heardUtterances: [], partialText: "Of course, I will send the invitation today.",
                    partialStartMS: 40000, segments: []))
        }

        /// A graph of levels as speech makes them: bursts and pauses.
        static var levelBars: [Double] {
            (0..<RecordingLevelHistory.barCount).map { index in
                let burst = abs(sin(Double(index) * 0.55)) * (index % 11 < 7 ? 1 : 0.15)
                return min(1, 0.12 + burst * 0.8)
            }
        }

        static var liveCaptureTimelineAnswer: String {
            let liveWords = liveCaptureUtterances.map {
                #"{"utteranceId":"\#($0.utteranceId)","utteranceStartMS":\#($0.utteranceStartMS),"utteranceEndMS":\#($0.utteranceEndMS),"transcriptText":"\#($0.transcriptText)"}"#
            }.joined(separator: ",")
            return
                "{\"data\":{\"ReadRecordingTranscriptTimeline\":{\"sourceGeneration\":2,\"segments\":[\(liveCaptureSegmentFixtures.joined(separator: ","))],\"transcriptRuns\":[],\"temporarySpeakers\":[{\"speakerLabel\":\"Speaker 1\",\"speakerIdentityId\":\"\"}],\"segmentCount\":\(liveCaptureSegmentFixtures.count),\"hasMoreSegments\":false,\"liveUtterances\":[\(liveWords)]}}}"
        }

        static var conversationSegments: [RecordingTranscriptSegment] {
            conversationSegmentFixtures.map { recordingObject($0, as: RecordingTranscriptSegment.self) }
        }

        static var conversationSegmentsJSON: String { conversationSegmentFixtures.joined(separator: ",") }

        static func libraryRecordingJSON(
            _ id: String, clientRecordingId: String, title: String = "", titleSource: String = "none", summary: String = "", recordedAt: String,
            durationMS: Int
        ) -> String {
            #"{"id":"\#(id)","userId":"account-a","createdAt":"\#(recordedAt)","updatedAt":"\#(recordedAt)","recordedAt":"\#(recordedAt)","clientRecordingId":"\#(clientRecordingId)","recordingTitle":"\#(title)","recordingTitleSource":"\#(titleSource)","recordingSummary":"\#(summary)","recordingOrigin":"capture","recordingCaptureState":"stopped","timelineDurationMS":\#(durationMS),"sourceGeneration":2,"captureGaps":[]}"#
        }

        /// Recordings in every state the library tells apart. The ones without a summary are read one by one, and
        /// `recordingViewJSON` says how each of them is doing.
        static var libraryRecordingFixtures: [String] {
            [
                libraryRecordingJSON(
                    "recording-c", clientRecordingId: "local-c", recordedAt: "2030-01-02T08:15:00Z", durationMS: 372_000),
                libraryRecordingJSON(
                    "recording-d", clientRecordingId: "local-d", recordedAt: "2030-01-02T07:00:00Z", durationMS: 1_020_000),
                libraryRecordingJSON(
                    "recording-e", clientRecordingId: "local-e", recordedAt: "2030-01-02T06:00:00Z", durationMS: 95_000),
                recordingFixture,
                libraryRecordingJSON(
                    "recording-b", clientRecordingId: "local-b", title: "Project planning and follow-up questions", titleSource: "user",
                    summary: "They listed the open questions and who will answer each one by Monday.", recordedAt: "2029-12-30T15:00:00Z",
                    durationMS: 2_400_000),
            ]
        }

        /// The recording the request names, if the request is one that names it.
        static func requestedRecordingId(_ query: String) -> String {
            guard let start = query.range(of: #""recordingId":""#)?.upperBound, let end = query[start...].firstIndex(of: "\"") else {
                return "recording-a"
            }
            return String(query[start..<end])
        }

        static func recordingViewJSON(for id: String) -> String {
            func media(_ mediaId: String, recordingId: String, uploadState: String, partCount: Int) -> String {
                let verifiedAt = uploadState == "complete" ? "\"2030-01-02T03:04:05Z\"" : "null"
                return #"{"id":"\#(mediaId)","recordingId":"\#(recordingId)","clientMediaId":"original-\#(mediaId)","originalFilename":"recording.caf","originalContentType":"audio/x-caf","originalSizeBytes":19200000,"originalSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","timelineStartMS":0,"mediaDurationMS":600000,"partCount":\#(partCount),"recordingUploadState":"\#(uploadState)","verifiedAt":\#(verifiedAt)}"#
            }
            func run(_ runId: String, recordingId: String, mediaId: String, state: String) -> String {
                #"{"id":"\#(runId)","recordingId":"\#(recordingId)","recordingMediaId":"\#(mediaId)","versionNumber":1,"recordingProcessingState":"\#(state)","providerModelName":"demo:transcription","processingErrorMessage":"","createdAt":"2030-01-02T03:04:05Z"}"#
            }
            func view(_ recording: String, _ mediaFiles: String, uploadParts: String = "", runs: String = "") -> String {
                "{\"recording\":\(recording),\"mediaFiles\":[\(mediaFiles)],\"uploadParts\":[\(uploadParts)],\"transcriptRuns\":[\(runs)]}"
            }
            let recording = libraryRecordingFixtures.first { $0.contains("\"id\":\"\(id)\"") }
            switch id {
            case "recording-c":
                return view(
                    recording ?? "", media("media-c", recordingId: id, uploadState: "complete", partCount: 3),
                    runs: run("run-c", recordingId: id, mediaId: "media-c", state: "transcribing"))
            case "recording-d":
                return view(
                    recording ?? "", media("media-d", recordingId: id, uploadState: "pending", partCount: 4),
                    uploadParts:
                        #"{"recordingMediaId":"media-d","partIndex":0,"partSizeBytes":8388608,"partSha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","storedAt":"2030-01-02T07:05:00Z"}"#
                )
            case "recording-e":
                return view(
                    recording ?? "", media("media-e", recordingId: id, uploadState: "complete", partCount: 3),
                    runs: run("run-e", recordingId: id, mediaId: "media-e", state: "failed"))
            case "recording-b":
                return view(
                    recording ?? "", media("media-b", recordingId: id, uploadState: "complete", partCount: 3),
                    runs: run("run-b", recordingId: id, mediaId: "media-b", state: "complete"))
            default:
                return "{\"recording\":\(recordingFixture),\"mediaFiles\":[\(recordingMediaFixture)],\"uploadParts\":[],\"transcriptRuns\":[\(recordingRunFixture)]}"
            }
        }

        /// The phone's own recordings: one being captured, and one stopped whose upload waits for a connection.
        static var localRecordings: [LocalRecording] {
            let formatter = ISO8601DateFormatter()
            var active = LocalRecording(
                accountKey: account.accountKey, recordingTitle: "", recordingOrigin: "capture", recordingUserId: "account-a",
                startedAt: formatter.date(from: "2030-01-02T10:00:00Z") ?? Date())
            active.timelineDurationMS = 42_000
            var waiting = LocalRecording(
                accountKey: account.accountKey, recordingTitle: "", recordingOrigin: "capture", recordingUserId: "account-a",
                startedAt: formatter.date(from: "2030-01-02T09:30:00Z") ?? Date())
            waiting.recordingCaptureState = "stopped"
            waiting.timelineDurationMS = 1_380_000
            var media = LocalRecordingMedia(
                originalFilename: "recording.caf", originalContentType: "audio/x-caf", timelineStartMS: 0, mediaDurationMS: 1_380_000)
            media.originalSizeBytes = 44_160_000
            media.originalSha256 = String(repeating: "c", count: 64)
            media.acceptedParts = [
                recordingObject(
                    #"{"recordingMediaId":"media-w","partIndex":0,"partSizeBytes":8388608,"partSha256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","storedAt":"2030-01-02T09:40:00Z"}"#,
                    as: RecordingMediaPart.self)
            ]
            waiting.localMedia = [media]
            waiting.uploadErrorMessage = "The Internet connection appears to be offline."
            waiting.isUploadWaitingForNetwork = true
            return [active, waiting]
        }

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
                // `-TeaNodeScreen provider-prices`: one provider with a price of its own and a price for particular models.
                let agentSettings =
                    Self.screen == "provider-prices"
                    ? recordingAgentSettingsFixture.replacingOccurrences(of: #""providers":[]"#, with: providerPricesFixture) : recordingAgentSettingsFixture
                return "{\"data\":{\"GetSettings\":{\"agent\":\(agentSettings)}}}"
            }
            if query.contains("ListAgentModels") {
                return
                    #"{"data":{"ListAgentModels":[{"name":"demo:transcription","canTranscribe":true,"hasSpeakerDiarization":true,"hasSegmentTimestamps":true,"hasSpeakerReferences":true,"maximumAudioBytes":24000000,"maximumSpeakerReferenceCount":3}]}}"#
            }

            if query.contains("ReadRecordingTranscriptTimeline"), requestedRecordingId(query) == liveCaptureRecordingId {
                return liveCaptureTimelineAnswer
            }
            if query.contains("ReadRecordingTranscriptTimeline") {
                // Asked for the unnamed speakers alone, the passages nobody has named.
                let segments =
                    query.contains(#""isUnnamedOnly":true"#)
                    ? conversationSegmentFixtures.filter { !$0.contains("effectiveSpeakerIdentityId") } : conversationSegmentFixtures
                return
                    "{\"data\":{\"ReadRecordingTranscriptTimeline\":{\"sourceGeneration\":4,\"segments\":[\(segments.joined(separator: ","))],\"transcriptRuns\":[\(recordingRunFixture)],\"segmentCount\":\(segments.count),\"hasMoreSegments\":false}}}"
            }
            if query.contains("ReadRecordingTranscriptTextCorrections") {
                return "{\"data\":{\"ReadRecordingTranscriptTextCorrections\":[\(recordingTextCorrectionFixture)]}}"
            }
            if query.contains("ReadRecordingTranscriptPassageOffset") {
                return #"{"data":{"ReadRecordingTranscriptPassageOffset":0}}"#
            }
            if query.contains("ReadRecordingTranscript") {
                return
                    "{\"data\":{\"ReadRecordingTranscript\":{\"transcriptRun\":\(recordingRunFixture),\"segments\":[\(conversationSegmentsJSON)],\"attempts\":[{\"id\":\"attempt-a\",\"recordingAttemptState\":\"complete\",\"providerModelName\":\"demo:transcription\",\"hasProviderUsage\":false,\"estimatedCostAmount\":null,\"actualCostAmount\":null,\"currencyCode\":\"USD\",\"attemptErrorMessage\":\"\"}],\"segmentCount\":\(conversationSegmentFixtures.count),\"hasMoreSegments\":false}}}"
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
                return "{\"data\":{\"ReadRecording\":\(recordingViewJSON(for: requestedRecordingId(query)))}}"
            }
            if query.contains("ListRecordings") {
                return "{\"data\":{\"ListRecordings\":[\(libraryRecordingFixtures.joined(separator: ","))]}}"
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
                case "recording-compatible-playback", "recording-original-playback", "recording-original-preview":
                    RecordingPlaybackScreen(
                        connection: connection, media: DemoServer.recordingObject(DemoServer.recordingMediaFixture, as: RecordingMedia.self))
                case "recording-detail": RecordingDetailScreen(connection: connection, recording: recording)
                case "recording-unnamed": RecordingDetailScreen(connection: connection, recording: recording, startsUnnamedOnly: true)
                case "recording-capture":
                    RecordingCaptureView(
                        elapsedMS: 754_000, displayState: .recording, inputLevel: { 0.6 }, onPauseOrResume: {}, onStop: {})
                case "recording-capture-live": RecordingLiveCaptureDemoScreen(connection: connection)
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

    /// The capture screen as it looks a minute and a half into a recording, read through the same model as the real one from
    /// the demo server's answers: passages with speakers, a speaker nobody has named, and live words after them.
    struct RecordingLiveCaptureDemoScreen: View {
        @State private var conversation: RecordingConversationModel

        init(connection: Connection) {
            let conversation = RecordingConversationModel(connection: connection, capturedRecordingId: DemoServer.liveCaptureRecordingId)
            conversation.partialText = "Of course, I will send the invitation today."
            conversation.partialStartMS = 40000
            _conversation = State(initialValue: conversation)
        }

        var body: some View {
            RecordingCaptureView(
                elapsedMS: 94_000, displayState: .recording, inputLevel: { 0.6 }, recentLevels: { DemoServer.levelBars }, bubbles: conversation.bubbles,
                actions: RecordingPassageActions(
                    play: { _ in }, copy: { _ in }, correctText: { _ in }, nameSpeaker: { segment in conversation.beginNaming(segment) },
                    thisIsMe: { segment in Task { await conversation.confirmAsAccountOwner(segment) } },
                    notMe: { segment in conversation.beginNaming(segment, startsBlank: true) }, clearName: { segments in Task { await conversation.clearName(segments) } },
                    separateSpeaker: { segment in Task { await conversation.separateSpeaker(segment) } }, isPlaybackAvailable: false),
                microphoneModeText: String(localized: "Voice Isolation"), onMicrophoneModes: {}, onPauseOrResume: {}, onStop: {}
            )
            .recordingPassageSheets(conversation)
            .task { await conversation.loadCapture() }
        }
    }
#endif
