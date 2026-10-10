import AVKit
import SwiftUI
import TeaNodeKit
import UIKit
import WebKit

extension EnvironmentValues {
    /// The conversation an answer belongs to, through which the pictures it
    /// shows are read.
    @Entry var pictureConversationId: String?
}

/// RemotePicture is a picture the server keeps, read with the app's sign-in;
/// tapping it opens it in Quick Look, to zoom and share.
struct RemotePicture: View {
    let path: String
    var name = "picture.jpg"
    var maxHeight: CGFloat = 360
    @Environment(AppModel.self) private var app
    @State private var picture: UIImage?
    @State private var isMissing = false

    var body: some View {
        Group {
            if let picture {
                Button {
                    Task { await app.preview(path, name: name) }
                } label: {
                    Image(uiImage: picture)
                        .resizable()
                        .scaledToFit()
                        .frame(maxHeight: maxHeight)
                        .clipShape(.rect(cornerRadius: 12, style: .continuous))
                }
                .buttonStyle(.plain)
                .accessibilityLabel(name)
            } else if isMissing {
                Label(name, systemImage: "photo")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            } else {
                ProgressView()
                    .frame(width: 120, height: 90)
            }
        }
        .task(id: path) {
            picture = await app.picture(path)
            isMissing = picture == nil
        }
    }
}

/// AttachmentView is a file the person sent with a message: a picture as a
/// thumbnail, anything else as its name and size; a tap opens it.
struct AttachmentView: View {
    let attachment: AgentAttachment
    @Environment(AppModel.self) private var app

    private var path: String { "/api/v1/agent/attachments/\(attachment.id)" }

    var body: some View {
        if attachment.contentType.hasPrefix("image/") {
            RemotePicture(path: path, name: attachment.name, maxHeight: 160)
                .frame(maxWidth: 200)
        } else {
            Button {
                Task { await app.preview(path, name: attachment.name) }
            } label: {
                Label {
                    Text(verbatim: "\(attachment.name) · \(attachment.size.formatted(.byteCount(style: .file)))")
                } icon: {
                    Image(systemName: "paperclip")
                }
                .font(.footnote)
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
                .background(Color(.tertiarySystemFill), in: .capsule)
            }
            .buttonStyle(.plain)
        }
    }
}

/// FileCard is a file the agent handed over: a picture, a video or a sound to
/// play, or any other file to open.
struct FileCard: View {
    let file: AgentFile
    @Environment(AppModel.self) private var app
    @State private var shareAddress: URL?

    var body: some View {
        AgentBubble {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Label {
                        Text(verbatim: file.size > 0 ? "\(file.name) · \(file.size.formatted(.byteCount(style: .file)))" : file.name)
                    } icon: {
                        Image(systemName: "paperclip")
                    }
                    .font(.subheadline)
                    .lineLimit(1)
                    Spacer(minLength: 8)
                    Button("Open") {
                        Task { await app.preview(file.path, name: file.name) }
                    }
                    .buttonStyle(.glass)
                }
                if file.isImage {
                    RemotePicture(path: file.path, name: file.name)
                } else if file.isVideo || file.isAudio {
                    if let shareAddress {
                        VideoPlayer(player: AVPlayer(url: shareAddress))
                            .frame(height: file.isVideo ? 220 : 60)
                            .clipShape(.rect(cornerRadius: 12, style: .continuous))
                    } else {
                        ProgressView()
                    }
                }
                if !file.caption.isEmpty {
                    Text(file.caption)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .task(id: file.attachmentId) {
            // A player cannot send the app's sign-in, so it is given an
            // address that opens without one, for a few hours.
            if file.isVideo || file.isAudio, let connection = app.connection {
                shareAddress = try? await connection.shareAddress(attachmentId: file.attachmentId)
            }
        }
    }
}

/// ArtifactCard is something the agent made: a page, a picture drawn in SVG,
/// or a Markdown document, shown in place and opened whole on a tap.
struct ArtifactCard: View {
    let file: AgentFile
    @Environment(AppModel.self) private var app
    @Environment(\.colorScheme) private var colorScheme
    @State private var address: URL?
    @State private var markdown: String?
    @State private var isOpen = false

    var body: some View {
        AgentBubble {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Label(file.name.isEmpty ? String(localized: "Untitled") : file.name, systemImage: "sparkles")
                        .font(.subheadline.bold())
                        .lineLimit(1)
                    Spacer(minLength: 8)
                    Button("Open") { isOpen = true }
                        .buttonStyle(.glass)
                        .disabled(address == nil && markdown == nil)
                }
                Group {
                    if let markdown {
                        ScrollView { MarkdownView(text: markdown) }
                    } else if let address {
                        WebView(url: address)
                    } else {
                        ProgressView().frame(maxWidth: .infinity)
                    }
                }
                .frame(height: 320)
                .clipShape(.rect(cornerRadius: 12, style: .continuous))
            }
        }
        .sheet(isPresented: $isOpen) {
            NavigationStack {
                Group {
                    if let markdown {
                        ScrollView { MarkdownView(text: markdown).padding() }
                    } else if let address {
                        WebView(url: address)
                    }
                }
                .navigationTitle(file.name)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { isOpen = false }
                    }
                }
            }
        }
        .task(id: "\(file.attachmentId)-\(colorScheme == .dark)") {
            guard let connection = app.connection else { return }
            if file.artifactKind == "markdown" {
                if let data = try? await connection.download(file.path) {
                    markdown = String(decoding: data, as: UTF8.self)
                }
                return
            }
            // The page runs in a web view that has no sign-in: it is given an
            // address that opens without one, and the theme to draw in.
            if let shared = try? await connection.shareAddress(attachmentId: file.attachmentId),
                var components = URLComponents(url: shared, resolvingAgainstBaseURL: false)
            {
                components.fragment = "theme=\(colorScheme == .dark ? "dark" : "light")"
                address = components.url
            }
        }
    }
}

/// ToolFiles are the files and artifacts a tool line handed over, shown
/// whether or not tool calls are.
struct ToolFiles: View {
    let line: TranscriptLine

    var body: some View {
        ForEach(AgentFile.of(toolResult: line.text), id: \.attachmentId) { file in
            if file.isArtifact {
                ArtifactCard(file: file)
            } else {
                FileCard(file: file)
            }
        }
    }
}

/// PendingFileChip is a file waiting in the composer: the picture, or the
/// file's name, with a way to take it back out.
struct PendingFileChip: View {
    let file: PendingFile
    let onRemove: () -> Void

    var body: some View {
        Group {
            if let preview = file.preview {
                Image(uiImage: preview)
                    .resizable()
                    .scaledToFill()
                    .frame(width: 56, height: 56)
                    .clipShape(.rect(cornerRadius: 10, style: .continuous))
            } else {
                Label(file.name, systemImage: "paperclip")
                    .font(.caption)
                    .lineLimit(1)
                    .frame(maxWidth: 160, minHeight: 56)
                    .padding(.horizontal, 8)
                    .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 10, style: .continuous))
            }
        }
        .overlay(alignment: .topTrailing) {
            Button(action: onRemove) {
                Image(systemName: "xmark.circle.fill")
                    .symbolRenderingMode(.hierarchical)
            }
            .accessibilityLabel("Remove")
        }
    }
}

/// CameraPicker takes a picture with the camera. SwiftUI has no camera of
/// its own, so this wraps UIKit's.
struct CameraPicker: UIViewControllerRepresentable {
    let onPicture: (UIImage) -> Void
    @Environment(\.dismiss) private var dismiss

    func makeUIViewController(context: Context) -> UIImagePickerController {
        let picker = UIImagePickerController()
        picker.sourceType = .camera
        picker.delegate = context.coordinator
        return picker
    }

    func updateUIViewController(_ picker: UIImagePickerController, context: Context) {}

    func makeCoordinator() -> Coordinator {
        Coordinator(onPicture: onPicture, dismiss: { dismiss() })
    }

    @MainActor
    final class Coordinator: NSObject, @preconcurrency UIImagePickerControllerDelegate, @preconcurrency UINavigationControllerDelegate {
        let onPicture: (UIImage) -> Void
        let dismiss: () -> Void

        init(onPicture: @escaping (UIImage) -> Void, dismiss: @escaping () -> Void) {
            self.onPicture = onPicture
            self.dismiss = dismiss
        }

        func imagePickerController(_ picker: UIImagePickerController, didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]) {
            if let picture = info[.originalImage] as? UIImage {
                onPicture(picture)
            }
            dismiss()
        }

        func imagePickerControllerDidCancel(_ picker: UIImagePickerController) {
            dismiss()
        }
    }
}

/// AnswerPicture is a picture an answer shows. It is read through the
/// server, which fetches only what a tool showed the agent in this
/// conversation; one that cannot be read is shown as its link.
struct AnswerPicture: View {
    let alt: String
    let address: String
    let link: String?
    @Environment(\.pictureConversationId) private var conversationId
    @Environment(\.openURL) private var openURL

    var body: some View {
        if let conversationId, address.lowercased().hasPrefix("http") {
            VStack(alignment: .leading, spacing: 4) {
                RemotePicture(path: Connection.picturePath(conversationId: conversationId, address: address), name: alt.isEmpty ? "picture" : alt, maxHeight: 240)
                if let link, let target = URL(string: link) {
                    Button(alt.isEmpty ? link : alt) { openURL(target) }
                        .font(.footnote)
                }
            }
        } else if let target = URL(string: link ?? address) {
            Button(alt.isEmpty ? address : alt) { openURL(target) }
        }
    }
}
