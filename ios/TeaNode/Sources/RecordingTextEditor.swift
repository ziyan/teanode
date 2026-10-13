import SwiftUI
import TeaNodeKit

struct RecordingTextEditor: View {
    let connection: Connection
    let recordingId: String
    let segment: RecordingTranscriptSegment
    let didSave: () async -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var draft: RecordingTextCorrectionDraft?
    @State private var correctedTranscriptText = ""
    @State private var problemText: String?
    @State private var isSaving = false
    @State private var isLoading = true

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextEditor(text: $correctedTranscriptText)
                        .frame(minHeight: 160)
                        .disabled(isLoading || isSaving || draft?.hasSubmittedRequest == true)
                        .accessibilityLabel("Corrected transcript")
                } header: {
                    Text("Corrected transcript")
                } footer: {
                    Text(
                        "Text edits are free metadata. The original transcript is retained, and learning is reconciled without another provider call."
                    )
                }
                if draft?.hasSubmittedRequest == true {
                    Text("This edit has an unconfirmed response. Retry the same edit before changing its text.").foregroundStyle(.secondary)
                }
                DisclosureGroup("Original transcript") {
                    Text(segment.transcriptText).textSelection(.enabled)
                }
                if let problemText { Text(problemText).foregroundStyle(.secondary) }
            }
            .navigationTitle("Edit transcript")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() }.disabled(isSaving) }
                ToolbarItem(placement: .confirmationAction) {
                    Button(draft?.hasSubmittedRequest == true ? "Retry edit" : "Save") { Task { await save() } }
                        .disabled(isLoading || isSaving || correctedTranscriptText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
            }
            .task { await load() }
            .onChange(of: correctedTranscriptText) { _, updatedText in
                guard var draft, !draft.hasSubmittedRequest else { return }
                draft.correctedTranscriptText = updatedText
                self.draft = draft
                do { try RecordingTextCorrectionDrafts.save(draft) } catch { problemText = error.localizedDescription }
            }
        }
    }

    private func load() async {
        defer { isLoading = false }
        do {
            let source = try await connection.recording(recordingId).recording
            var pending = RecordingTextCorrectionDrafts.read(accountKey: connection.accountKey, segmentId: segment.id)
            if let existing = pending, existing.hasSubmittedRequest {
                let history = try await connection.recordingTranscriptTextCorrections(recordingId: recordingId)
                if history.contains(where: {
                    $0.clientCorrectionId == existing.clientCorrectionId && $0.recordingTranscriptSegmentId == existing.recordingTranscriptSegmentId
                        && $0.correctedTranscriptText == existing.correctedTranscriptText
                }) {
                    RecordingTextCorrectionDrafts.finish(existing)
                    await didSave()
                    dismiss()
                    return
                }
            }
            if pending == nil {
                pending = RecordingTextCorrectionDraft(
                    accountKey: connection.accountKey, recordingId: recordingId, recordingTranscriptSegmentId: segment.id,
                    correctedTranscriptText: segment.displayedTranscriptText, expectedSourceGeneration: source.sourceGeneration)
            } else if pending?.hasSubmittedRequest == false {
                pending?.expectedSourceGeneration = source.sourceGeneration
            }
            guard let pending else { throw TeaNodeError.unreadableAnswer }
            draft = pending
            correctedTranscriptText = pending.correctedTranscriptText
            try RecordingTextCorrectionDrafts.save(pending)
        } catch { problemText = error.localizedDescription }
    }

    private func save() async {
        guard !isSaving, var draft else { return }
        isSaving = true
        defer { isSaving = false }
        do {
            if !draft.hasSubmittedRequest { draft.correctedTranscriptText = correctedTranscriptText }
            draft.hasSubmittedRequest = true
            try RecordingTextCorrectionDrafts.save(draft)
            self.draft = draft
            _ = try await connection.correctRecordingTranscriptText(draft)
            RecordingTextCorrectionDrafts.finish(draft)
            await didSave()
            dismiss()
        } catch {
            problemText = error.localizedDescription
            if let refusal = error as? TeaNodeError, case .refused = refusal {
                // A definitive API refusal permits editing the payload; an unknown outcome does not.
                draft.hasSubmittedRequest = false
                if let source = try? await connection.recording(recordingId).recording { draft.expectedSourceGeneration = source.sourceGeneration }
                self.draft = draft
                try? RecordingTextCorrectionDrafts.save(draft)
            }
        }
    }
}

struct RecordingTextHistoryScreen: View {
    let connection: Connection
    let recordingId: String
    @State private var corrections: [RecordingTranscriptTextCorrection] = []
    @State private var problemText: String?
    @State private var selectedCorrection: RecordingTranscriptTextCorrection?
    @State private var isResetting = false

    var body: some View {
        List {
            Section {
                ForEach(corrections.sorted { $0.sourceGeneration > $1.sourceGeneration }) { correction in
                    VStack(alignment: .leading, spacing: 8) {
                        Text(Duration.milliseconds(correction.passageStartMS).formatted(.time(pattern: .hourMinuteSecond)))
                            .font(.caption).foregroundStyle(.secondary)
                        Text(correction.correctedTranscriptText).textSelection(.enabled)
                        if correction.isRemoved { Label("Correction removed", systemImage: "arrow.uturn.backward").font(.caption) }
                        DisclosureGroup("Original transcript") { Text(correction.originalTranscriptText).textSelection(.enabled) }
                        if !correction.isRemoved {
                            Button("Remove text correction", role: .destructive) { selectedCorrection = correction }.disabled(isResetting)
                        }
                    }.padding(.vertical, 4)
                }
                if corrections.isEmpty { Text("No transcript text corrections").foregroundStyle(.secondary) }
            } footer: {
                Text(
                    "Corrections retain their original passage and version. Changed passage boundaries may keep a correction in history instead of applying it to unrelated text."
                )
            }
            if let problemText { Text(problemText).foregroundStyle(.secondary) }
        }
        .navigationTitle("Transcript edit history")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
        .confirmationDialog(
            "Remove text correction?",
            isPresented: Binding(
                get: { selectedCorrection != nil }, set: { if !$0 { selectedCorrection = nil } }
            ), titleVisibility: .visible
        ) {
            if let correction = selectedCorrection {
                Button("Remove text correction", role: .destructive) { Task { await reset(correction) } }
            }
        } message: {
            Text("The generated transcript remains intact. Learning is reconciled without a paid reprocessing pass.")
        }
    }

    private func load() async {
        do {
            corrections = try await connection.recordingTranscriptTextCorrections(recordingId: recordingId)
            problemText = nil
        } catch { problemText = error.localizedDescription }
    }
    private func reset(_ correction: RecordingTranscriptTextCorrection) async {
        guard !isResetting else { return }
        isResetting = true
        defer { isResetting = false }
        do {
            let source = try await connection.recording(recordingId).recording
            try await connection.removeRecordingTranscriptTextCorrection(
                recordingId: recordingId, recordingTranscriptTextCorrectionId: correction.id, expectedSourceGeneration: source.sourceGeneration)
            await load()
        } catch { problemText = error.localizedDescription }
    }
}
