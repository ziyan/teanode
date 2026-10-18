import SwiftUI
import TeaNodeKit

struct RecordingCitationScreen: View {
    let connection: Connection
    let citation: RecordingLink
    @State private var recordingView: RecordingView?
    @State private var problemText: String?

    var body: some View {
        Group {
            if let recordingView {
                if let transcriptRunId = citation.recordingTranscriptRunId,
                    let run = recordingView.transcriptRuns?.first(where: { $0.id == transcriptRunId })
                {
                    RecordingTranscriptScreen(connection: connection, recording: recordingView.recording, transcriptRun: run, citation: citation)
                } else if citation.recordingTranscriptRunId == nil {
                    RecordingDetailScreen(connection: connection, recording: recordingView.recording)
                } else {
                    Text("The cited transcript version is unavailable. Open the recording library to browse other versions.").foregroundStyle(
                        .secondary)
                }
            } else if let problemText {
                ContentUnavailableView("Cannot open recording", systemImage: "exclamationmark.triangle", description: Text(problemText.asReasonSentence))
            } else {
                ProgressView()
            }
        }
        .task(id: citation.id) {
            do {
                let read = try await connection.recording(citation.recordingId)
                guard read.recording.id == citation.recordingId,
                    read.transcriptRuns?.allSatisfy({ $0.recordingId == citation.recordingId }) ?? true
                else { throw TeaNodeError.unreadableAnswer }
                recordingView = read
            } catch { problemText = error.localizedDescription }
        }
    }
}
