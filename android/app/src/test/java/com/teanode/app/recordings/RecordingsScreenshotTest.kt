package com.teanode.app.recordings

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createComposeRule
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import com.teanode.app.ui.theme.AppearanceChoice
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.recordings.LocalRecording
import com.teanode.kit.recordings.LocalRecordingMedia
import com.teanode.kit.recordings.RecordingView
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Pictures of every Recordings screen and state, in the invented demo library: light and dark, the dense ones at the largest text. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class RecordingsScreenshotTest {
    @get:Rule val composeRule = createComposeRule()

    private fun shoot(name: String, appearance: AppearanceChoice = AppearanceChoice.Light, fontScale: Float = 1f, content: @Composable () -> Unit) {
        composeRule.setContent { DemoHost(appearance = appearance) { WithFontScale(fontScale) { content() } } }
        composeRule.mainClock.advanceTimeBy(500)
        composeRule.waitForIdle()
        composeRule.photograph(name)
    }

    private val pendingMedia = LocalRecordingMedia("lm1", null, "voice.m4a", "audio/mp4", 0, 60_000, 2_000_000, "d".repeat(64))

    private val localRecordings = listOf(
        LocalRecording("l1", "account", "user-robin", "client-l1", null, "Shopping list", "capture", System.currentTimeMillis() - 120_000, "stopped", 42_000, localMedia = listOf(pendingMedia), uploadErrorMessage = "offline", isUploadWaitingForNetwork = true),
        LocalRecording("l2", "account", "user-robin", "client-l2", null, "Imported lecture", "imported", System.currentTimeMillis() - 60_000, "imported", 0, localMedia = listOf(pendingMedia.copy(id = "lm2")), uploadErrorMessage = "The server refused the file.", isUploadWaitingForNetwork = false),
        LocalRecording("l3", "account", "user-robin", "client-l3", null, "", "capture", System.currentTimeMillis() - 30_000, "paused", 15_000),
    )

    @Test fun `list light`() = shoot("recordings-list-light") { RecordingsSection(initialLocalRecordings = emptyList()) }

    @Test fun `list dark with local recordings`() = shoot("recordings-list-local-dark", AppearanceChoice.Dark) { RecordingsSection(initialLocalRecordings = localRecordings) }

    @Test fun `list largest text`() = shoot("recordings-list-largest-text", fontScale = 2f) { RecordingsSection(initialLocalRecordings = localRecordings) }

    @Test fun `list search empty`() = shoot("recordings-list-search-empty-light") { RecordingsSection(initialLocalRecordings = emptyList(), initialSearchText = "zzzz") }

    @Test fun `detail transcript light`() = shoot("recordings-detail-light") { RecordingDetailScreen("r1", onBack = {}) }

    @Test fun `detail transcript dark`() = shoot("recordings-detail-dark", AppearanceChoice.Dark) { RecordingDetailScreen("r1", onBack = {}) }

    @Test fun `detail largest text`() = shoot("recordings-detail-largest-text", fontScale = 2f) { RecordingDetailScreen("r1", onBack = {}) }

    @Test fun `detail transcribing light`() = shoot("recordings-detail-transcribing-light") { RecordingDetailScreen("r2", onBack = {}) }

    @Test fun `detail uploading dark`() = shoot("recordings-detail-uploading-dark", AppearanceChoice.Dark) { RecordingDetailScreen("r3", onBack = {}) }

    @Test fun `detail failed light`() = shoot("recordings-detail-failed-light") { RecordingDetailScreen("r4", onBack = {}) }

    @Test fun `detail cited passage light`() = shoot("recordings-detail-citation-light") {
        RecordingDetailScreen("r1", onBack = {}, link = com.teanode.kit.recordings.RecordingLink("r1", "run-r1", 28_000, 36_000))
    }

    @Test fun `details sheet light`() = shoot("recordings-details-sheet-light") {
        val view = TeaNodeJson.decodeFromString(RecordingView.serializer(), """{"recording":{"id":"r1","createdAt":"2026-11-10T15:00:00Z","updatedAt":"2026-11-10T15:00:00Z","clientRecordingId":"c","recordingTitle":"Weekly planning","recordingOrigin":"capture","recordingCaptureState":"stopped","timelineDurationMS":1412000,"captureGaps":[{"gapId":"g","gapStartMS":600000,"gapEndMS":660000,"recordingGapReason":"paused"}]},
            "mediaFiles":[{"id":"m1","recordingId":"r1","clientMediaId":"c1","originalFilename":"piece-1.m4a","originalContentType":"audio/mp4","originalSizeBytes":4200000,"originalSha256":"${"a".repeat(64)}","timelineStartMS":0,"mediaDurationMS":600000,"partCount":1,"recordingUploadState":"complete"}],
            "transcriptRuns":[{"id":"run","recordingId":"r1","recordingMediaId":"m1","versionNumber":1,"recordingProcessingState":"complete","providerModelName":"example-transcribe"}]}""")
        Surface(Modifier.fillMaxSize()) { RecordingDetailsSheet(view) }
    }

    @Test fun `local original light`() = shoot("recordings-local-light") { RecordingLocalScreen("l1", onBack = {}, preview = localRecordings[0]) }

    @Test fun `local failed dark`() = shoot("recordings-local-failed-dark", AppearanceChoice.Dark) { RecordingLocalScreen("l2", onBack = {}, preview = localRecordings[1]) }

    @Test fun `capture recording light`() = shoot("recordings-capture-recording-light") {
        RecordingCaptureScreen(onBack = {}, previewState = RecordingCaptureState("l", isRecording = true, elapsedMS = 754_000, levelBars = List(48) { index -> if (index < 20) 0.0 else (0.2 + (index % 7) * 0.1).coerceAtMost(1.0) }))
    }

    @Test fun `capture paused dark`() = shoot("recordings-capture-paused-dark", AppearanceChoice.Dark) { RecordingCaptureScreen(onBack = {}, previewState = RecordingCaptureState("l", isRecording = true, isPaused = true, elapsedMS = 754_000)) }

    @Test fun `capture problem light`() = shoot("recordings-capture-problem-light") {
        RecordingCaptureScreen(onBack = {}, previewState = RecordingCaptureState(problemMessageId = com.teanode.app.R.string.recordings_cannot_record))
    }

    @Test fun `share light`() = shoot("recordings-share-light") { RecordingShareScreen("r1", onBack = {}) }

    @Test fun `share largest text`() = shoot("recordings-share-largest-text", fontScale = 2f) { RecordingShareScreen("r1", onBack = {}) }

    @Test fun `shared list dark`() = shoot("recordings-shared-list-dark", AppearanceChoice.Dark) { SharedRecordingsScreen(onBack = {}) }

    @Test fun `shared transcript light`() = shoot("recordings-shared-transcript-light") { SharedRecordingScreen("sr1", onBack = {}) }

    @Test fun `shared summary only dark`() = shoot("recordings-shared-summary-dark", AppearanceChoice.Dark) { SharedRecordingScreen("sr2", onBack = {}) }
}
