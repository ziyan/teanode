import SwiftUI
import TeaNodeKit

struct RecordingAttachmentImportView: View {
    let connection: Connection
    let attachment: AgentAttachment
    @Environment(\.dismiss) private var dismiss
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
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
                } footer: {
                    Text(
                        "Copy this audio or video attachment into your account's recording library. The attachment remains in the conversation. Your agent names the recording once it is transcribed."
                    )
                }
                if let importedRecording {
                    NavigationLink {
                        RecordingDetailScreen(connection: connection, recording: importedRecording)
                    } label: {
                        Label("Open recording", systemImage: "waveform")
                            .foregroundStyle(.primary)
                    }
                } else {
                    Button(draft == nil ? "Import recording" : "Retry import") { Task { await importAttachment() } }.disabled(isImporting)
                }
            }
            .toast(done: .constant(nil), problem: $problemText)
            .navigationTitle("Import recording")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() }.disabled(isImporting) } }
            .onAppear {
                draft = RecordingAttachmentImportDrafts.read(accountKey: connection.accountKey, attachmentId: attachment.id)
            }
        }
    }

    private func importAttachment() async {
        guard !isImporting else { return }
        isImporting = true
        defer { isImporting = false }
        do {
            let submitted =
                draft
                ?? RecordingAttachmentImportDraft(accountKey: connection.accountKey, agentAttachmentId: attachment.id, recordingTitle: "")
            try RecordingAttachmentImportDrafts.save(submitted)
            draft = submitted
            let copied = try await connection.importRecordingAttachment(attachment, draft: submitted)
            RecordingAttachmentImportDrafts.finish(submitted)
            importedRecording = copied.recording
            problemText = nil
        } catch { problemText = error.localizedDescription }
    }
}
