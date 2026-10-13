import SwiftUI
import TeaNodeKit

struct RecordingAttachmentImportView: View {
    let connection: Connection
    let attachment: AgentAttachment
    @Environment(\.dismiss) private var dismiss
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @State private var recordingTitle = ""
    @State private var draft: RecordingAttachmentImportDraft?
    @State private var importedRecording: Recording?
    @State private var problemText: String?
    @State private var isImporting = false

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    let filenameLayout =
                        dynamicTypeSize.isAccessibilitySize
                        ? AnyLayout(VStackLayout(alignment: .leading, spacing: 4))
                        : AnyLayout(HStackLayout(alignment: .firstTextBaseline))
                    filenameLayout {
                        Image(systemName: "paperclip").accessibilityHidden(true)
                        Text(attachment.name)
                    }.accessibilityElement(children: .combine)
                    TextField("Meeting title (optional)", text: $recordingTitle).disabled(draft != nil || isImporting)
                } footer: {
                    Text(
                        "Copy this audio or video attachment into your account's recording library. The attachment remains in the conversation. Transcription is a separate action."
                    )
                }
                if let importedRecording {
                    NavigationLink {
                        RecordingDetailScreen(connection: connection, recording: importedRecording)
                    } label: {
                        Label("Open recording", systemImage: "waveform")
                    }
                } else {
                    Button(draft == nil ? "Import recording" : "Retry import") { Task { await importAttachment() } }.disabled(isImporting)
                }
                if let problemText { Text(problemText).foregroundStyle(.secondary) }
            }
            .navigationTitle("Import recording")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() }.disabled(isImporting) } }
            .onAppear {
                draft = RecordingAttachmentImportDrafts.read(accountKey: connection.accountKey, attachmentId: attachment.id)
                let filenameTitle = (attachment.name as NSString).deletingPathExtension
                recordingTitle = draft?.recordingTitle ?? (filenameTitle.utf8.count <= 255 ? filenameTitle : String(localized: "Recording"))
            }
        }
    }

    private func importAttachment() async {
        guard !isImporting else { return }
        isImporting = true
        defer { isImporting = false }
        do {
            guard recordingTitle.utf8.count <= 255 else {
                throw TeaNodeError.refused(String(localized: "Use a recording title of at most 255 UTF-8 bytes."))
            }
            let submitted =
                draft
                ?? RecordingAttachmentImportDraft(accountKey: connection.accountKey, agentAttachmentId: attachment.id, recordingTitle: recordingTitle)
            try RecordingAttachmentImportDrafts.save(submitted)
            draft = submitted
            let copied = try await connection.importRecordingAttachment(attachment, draft: submitted)
            RecordingAttachmentImportDrafts.finish(submitted)
            importedRecording = copied.recording
            problemText = nil
        } catch { problemText = error.localizedDescription }
    }
}
