import SwiftUI
import TeaNodeKit
import UIKit
import UniformTypeIdentifiers

/// ShareViewController is TeaNode in the share sheet: what another app
/// shares (pictures, files, a link, words) goes to the person's agent, in
/// the main conversation, with a note they can write.
///
/// This target builds in the Swift 5 language mode: NSItemProvider and the
/// extension context are not yet marked for Swift 6's checking.
final class ShareViewController: UIViewController {
    override func viewDidLoad() {
        super.viewDidLoad()
        let model = ShareModel(context: extensionContext)
        let host = UIHostingController(rootView: ShareView(model: model))
        addChild(host)
        host.view.frame = view.bounds
        host.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(host.view)
        host.didMove(toParent: self)
        Task { await model.read() }
    }
}

/// SharedFile is a file shared, ready to upload.
struct SharedFile: Identifiable {
    let id = UUID()
    let name: String
    let contentType: String
    let data: Data
    let preview: UIImage?
}

@MainActor
final class ShareModel: ObservableObject {
    @Published var note = ""
    @Published var files: [SharedFile] = []
    @Published var recordingFiles: [SharedRecordingFile] = []
    @Published var isRecordingDestination = false
    @Published var isReading = true
    @Published var isSending = false
    @Published var problemText: String?
    @Published var agentName = ""
    let context: NSExtensionContext?
    private let connection: Connection?

    /// The most a message's files may come to, as the app's composer allows.
    static let bytesMost = 25 << 20

    init(context: NSExtensionContext?) {
        self.context = context
        if let account = try? KeychainAccountStore().load() {
            connection = Connection(account: account, transport: URLSessionTransport(), store: KeychainAccountStore())
        } else {
            connection = nil
        }
    }

    var isConnected: Bool { connection != nil }

    var canSend: Bool {
        isConnected && !isSending && (!note.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !files.isEmpty || !recordingFiles.isEmpty)
    }

    /// Reads what was shared: links and words into the note, pictures and
    /// files as attachments.
    func read() async {
        defer { isReading = false }
        let providers = (context?.inputItems as? [NSExtensionItem] ?? []).flatMap { $0.attachments ?? [] }
        var lines: [String] = []
        for (index, provider) in providers.enumerated() {
            if provider.hasItemConformingToTypeIdentifier(UTType.audio.identifier) || provider.hasItemConformingToTypeIdentifier(UTType.movie.identifier) {
                do { if let file = try await ShareRecordingImport.stage(provider: provider) { recordingFiles.append(file) } }
                catch { problemText = error.localizedDescription }
            } else if provider.hasItemConformingToTypeIdentifier(UTType.image.identifier) {
                if let data = await Self.data(of: provider, type: UTType.image), let image = UIImage(data: data),
                    let file = Self.picture(image, index: index)
                {
                    files.append(file)
                }
            } else if provider.hasItemConformingToTypeIdentifier(UTType.url.identifier),
                let url = await Self.url(of: provider), !url.isFileURL
            {
                lines.append(url.absoluteString)
            } else if provider.hasItemConformingToTypeIdentifier(UTType.plainText.identifier),
                let text = await Self.text(of: provider)
            {
                lines.append(text)
            } else if let (data, name, contentType) = await Self.file(of: provider) {
                files.append(SharedFile(name: name, contentType: contentType, data: data, preview: nil))
            }
        }
        note = lines.joined(separator: "\n")
        if let connection { Task { if let name = try? await connection.agentName() { agentName = name } } }
        if files.reduce(0, { $0 + $1.data.count }) > Self.bytesMost {
            problemText = String(localized: "The files come to more than 25 MB, more than the server takes in one message.")
        }
    }

    func send() async {
        guard let connection, canSend else { return }
        isSending = true
        defer { isSending = false }
        do {
            if isRecordingDestination {
                for file in recordingFiles { try await ShareRecordingImport.save(file, accountKey: connection.accountKey, title: note.trimmingCharacters(in: .whitespacesAndNewlines)) }
                cleanupStagedRecordings()
                context?.completeRequest(returningItems: nil)
                return
            }
            let recordingBytes = try recordingFiles.reduce(0) { total, file in total + (try file.originalURL.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0) }
            guard recordingBytes + files.reduce(0, { $0 + $1.data.count }) <= Self.bytesMost else {
                throw TeaNodeError.refused(String(localized: "These files exceed the chat attachment limit. Save audio or video as recordings instead."))
            }
            var attachmentIds: [String] = []
            for file in recordingFiles {
                attachmentIds.append(try await connection.upload(name: file.originalFilename, contentType: file.originalContentType, data: Data(contentsOf: file.originalURL)))
            }
            for file in files {
                attachmentIds.append(try await connection.upload(name: file.name, contentType: file.contentType, data: file.data))
            }
            let text = note.trimmingCharacters(in: .whitespacesAndNewlines)
            _ = try await connection.ask(
                text.isEmpty ? String(localized: "(shared files)") : text, in: nil, attachmentIds: attachmentIds,
                drawerId: "share-\(UUID().uuidString)")
            cleanupStagedRecordings()
            context?.completeRequest(returningItems: nil)
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func cleanupStagedRecordings() {
        for file in recordingFiles { try? FileManager.default.removeItem(at: file.originalURL) }
    }

    func cancel() {
        cleanupStagedRecordings()
        context?.cancelRequest(withError: NSError(domain: "com.teanode.app.share", code: 0))
    }

    // MARK: Reading the providers

    private static func data(of provider: NSItemProvider, type: UTType) async -> Data? {
        await withCheckedContinuation { continuation in
            provider.loadDataRepresentation(forTypeIdentifier: type.identifier) { data, _ in continuation.resume(returning: data) }
        }
    }

    private static func url(of provider: NSItemProvider) async -> URL? {
        await withCheckedContinuation { continuation in
            _ = provider.loadObject(ofClass: URL.self) { url, _ in continuation.resume(returning: url) }
        }
    }

    private static func text(of provider: NSItemProvider) async -> String? {
        await withCheckedContinuation { continuation in
            _ = provider.loadObject(ofClass: String.self) { text, _ in continuation.resume(returning: text) }
        }
    }

    private static func file(of provider: NSItemProvider) async -> (Data, String, String)? {
        guard let type = provider.registeredTypeIdentifiers.first.flatMap(UTType.init) else { return nil }
        let name = provider.suggestedName ?? "file"
        guard let data = await data(of: provider, type: type) else { return nil }
        let fileName = type.preferredFilenameExtension.map { name.hasSuffix(".\($0)") ? name : "\(name).\($0)" } ?? name
        return (data, fileName, type.preferredMIMEType ?? "application/octet-stream")
    }

    /// A picture as a JPEG at most 2048 points on its longest side, as the
    /// app sends one.
    private static func picture(_ image: UIImage, index: Int) -> SharedFile? {
        let longest: CGFloat = 2048
        let scale = min(1, longest / max(image.size.width, image.size.height))
        let size = CGSize(width: (image.size.width * scale).rounded(), height: (image.size.height * scale).rounded())
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        let resized = UIGraphicsImageRenderer(size: size, format: format).image { _ in image.draw(in: CGRect(origin: .zero, size: size)) }
        guard let jpeg = resized.jpegData(compressionQuality: 0.8) else { return nil }
        return SharedFile(name: "picture-\(index + 1).jpg", contentType: "image/jpeg", data: jpeg, preview: resized)
    }
}

/// ShareView is the share sheet's page: what goes, a note, and Send.
struct ShareView: View {
    @ObservedObject var model: ShareModel

    var body: some View {
        NavigationStack {
            Form {
                if !model.isConnected {
                    Section {
                        Label("Open TeaNode and connect it to your server first.", systemImage: "link")
                    }
                } else {
                    if !model.files.isEmpty {
                        Section {
                            ScrollView(.horizontal) {
                                HStack(spacing: 10) {
                                    ForEach(model.files) { file in
                                        if let preview = file.preview {
                                            Image(uiImage: preview)
                                                .resizable()
                                                .scaledToFill()
                                                .frame(width: 88, height: 88)
                                                .clipShape(.rect(cornerRadius: 12, style: .continuous))
                                        } else {
                                            Label(file.name, systemImage: "doc")
                                                .font(.footnote)
                                                .padding(10)
                                                .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 12, style: .continuous))
                                        }
                                    }
                                }
                            }
                            .scrollIndicators(.hidden)
                        }
                    }
                    if !model.recordingFiles.isEmpty {
                        Section {
                            Toggle("Save audio or video as recordings", isOn: $model.isRecordingDestination)
                            ForEach(model.recordingFiles) { file in Label(file.originalFilename, systemImage: file.isVideo ? "video" : "waveform") }
                        } footer: { Text("Recordings are kept locally until verified by the server. Open TeaNode to resume their background uploads. Recording speech is not sent as an instruction to your agent.") }
                    }
                    Section {
                        TextField(model.isRecordingDestination ? String(localized: "Meeting title (optional)") : String(localized: "Add a note for your agent"), text: $model.note, axis: .vertical)
                            .lineLimit(3...10)
                    } footer: {
                        Text(model.isRecordingDestination ? String(localized: "Saved recordings will appear in the recording library.") : String(localized: "It goes to your main conversation."))
                    }
                    if let problemText = model.problemText {
                        Section {
                            Label(problemText, systemImage: "exclamationmark.triangle").foregroundStyle(.red)
                        }
                    }
                }
            }
            .navigationTitle(model.agentName.isEmpty ? String(localized: "Send to your agent") : String(localized: "Send to \(model.agentName)"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { model.cancel() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if model.isSending || model.isReading {
                        ProgressView()
                    } else {
                        Button(model.isRecordingDestination ? String(localized: "Save recording") : String(localized: "Send")) { Task { await model.send() } }
                            .disabled(!model.canSend)
                    }
                }
            }
        }
    }
}
