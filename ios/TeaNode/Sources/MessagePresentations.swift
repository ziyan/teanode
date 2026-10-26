import SwiftUI
import TeaNodeKit

/// MessagePresentation is what a card in a conversation opens: a file kept as
/// a recording, a photo the agent showed, or an artifact. The conversation
/// shows it, not the card: the list of messages keeps only the cards near the
/// screen, and a sheet on a card that an answer streaming in pushes away is
/// closed with it.
enum MessagePresentation: Identifiable {
    case recordingImport(AgentAttachment)
    case agentPhoto(photoId: String)
    case artifact(name: String, markdown: String?, address: URL?)
    case savingProblem(String)

    var id: String {
        switch self {
        case .recordingImport(let attachment): "recording-\(attachment.id)"
        case .agentPhoto(let photoId): "photo-\(photoId)"
        case .artifact(let name, _, let address): "artifact-\(name)-\(address?.absoluteString ?? "")"
        case .savingProblem(let text): "problem-\(text)"
        }
    }
}

/// MessagePresenter is how a card asks the screen around it to show what it
/// opens.
struct MessagePresenter {
    let present: @MainActor (MessagePresentation) -> Void
}

extension EnvironmentValues {
    /// Set by `messagePresentations()`; nil outside a screen that shows what
    /// cards open, where a card's tap opens nothing.
    @Entry var messagePresenter: MessagePresenter?
}

/// MessagePresentations shows what the cards inside it open, on the view it
/// is applied to: the conversation, or any screen that lists the cards.
struct MessagePresentations: ViewModifier {
    @Environment(AppModel.self) private var app
    @State private var sheetItem: MessagePresentation?
    @State private var coverItem: MessagePresentation?
    @State private var problemText: String?

    func body(content: Content) -> some View {
        content
            .environment(
                \.messagePresenter,
                MessagePresenter { item in
                    switch item {
                    case .agentPhoto: coverItem = item
                    case .savingProblem(let text): problemText = text
                    case .recordingImport, .artifact: sheetItem = item
                    }
                }
            )
            .sheet(item: $sheetItem) { item in
                switch item {
                case .recordingImport(let attachment):
                    if let connection = app.connection { RecordingAttachmentImportView(connection: connection, attachment: attachment) }
                case .artifact(let name, let markdown, let address):
                    ArtifactSheet(name: name, markdown: markdown, address: address)
                case .agentPhoto, .savingProblem:
                    EmptyView()
                }
            }
            .fullScreenCover(item: $coverItem) { item in
                if case .agentPhoto(let photoId) = item, let connection = app.connection {
                    AgentPhotoViewer(photoId: photoId, connection: connection)
                }
            }
            .alert(
                "Cannot save to my library", isPresented: Binding(get: { problemText != nil }, set: { if !$0 { problemText = nil } }),
                presenting: problemText
            ) { _ in
                Button("OK", role: .cancel) {}
            } message: { text in
                Text(text)
            }
    }
}

extension View {
    /// Shows what the message cards inside open (MessagePresentations).
    func messagePresentations() -> some View {
        modifier(MessagePresentations())
    }
}

/// ArtifactSheet is an artifact the agent made, opened: its Markdown, or its
/// page in a web view.
struct ArtifactSheet: View {
    let name: String
    let markdown: String?
    let address: URL?
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Group {
                if let markdown {
                    ScrollView { MarkdownView(text: markdown).padding() }
                } else if let address {
                    ArtifactWebView(address: address)
                }
            }
            .navigationTitle(name)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
    }
}
