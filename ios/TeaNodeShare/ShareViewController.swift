import ImageIO
import SwiftUI
import TeaNodeKit
import UIKit
import UniformTypeIdentifiers

/// ShareViewController is TeaNode in the share sheet: what another app
/// shares (pictures, files, a link, words) goes to the person's agent, in
/// the main conversation, with a note they can write. Shared files can go
/// straight to finance instead: PDFs and pictures read as tax documents,
/// handed in as tax documents so the server keeps no text of them, and OFX
/// statements imported; the sheet then says what came of it.
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

    /// Whether it can be read as a paystub or a W-2: a PDF or a picture.
    var isTaxDocument: Bool { contentType == "application/pdf" || contentType.hasPrefix("image/") }

    /// Whether it is an OFX statement: named .ofx, .qfx or .qbo, or OFX
    /// inside whatever it is called, as a wallet's export often is.
    var isStatement: Bool {
        if ["ofx", "qfx", "qbo"].contains((name as NSString).pathExtension.lowercased()) { return true }
        let start = String(decoding: data.prefix(1024), as: UTF8.self).uppercased()
        return start.contains("OFXHEADER") || start.contains("<OFX>")
    }
}

/// ShareFileDestination is where shared files go: to the agent with the
/// note, to be read as tax documents, or to be imported as statements.
enum ShareFileDestination: Hashable {
    case agent, taxDocuments, statements
}

@MainActor
final class ShareModel: ObservableObject {
    @Published var note = ""
    @Published var files: [SharedFile] = []
    @Published var recordingFiles: [SharedRecordingFile] = []
    @Published var isRecordingDestination = false
    @Published var fileDestination = ShareFileDestination.agent
    /// What came of files sent to finance, shown before the sheet closes.
    @Published var resultText: String?
    /// The files finance has taken already, so trying again after a
    /// failure sends only the rest, and what each import said.
    private var handledFileIds = Set<UUID>()
    private var statementSummaries: [String] = []
    @Published var isReading = true
    @Published var isSending = false
    @Published var problemText: String?
    @Published var agentName = ""
    let context: NSExtensionContext?
    private let connection: Connection?
    private let recordingUserId: String?

    /// The most a message's files may come to, as the app's composer allows.
    static let bytesMost = 25 << 20

    init(context: NSExtensionContext?) {
        self.context = context
        if let account = try? KeychainAccountStore().load() {
            connection = Connection(account: account, transport: URLSessionTransport(), store: KeychainAccountStore())
            recordingUserId = account.cachedRecordingUserId
        } else {
            connection = nil
            recordingUserId = nil
        }
    }

    var isConnected: Bool { connection != nil }

    var canSend: Bool {
        guard isConnected, !isSending else { return false }
        switch effectiveFileDestination {
        case .taxDocuments: return files.contains(where: \.isTaxDocument)
        case .statements: return files.contains(where: \.isStatement)
        case .agent: return !note.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !files.isEmpty || !recordingFiles.isEmpty
        }
    }

    /// Whether the files can go to finance at all, as paystubs and W-2s or
    /// as statements.
    var canReadTaxDocuments: Bool { files.contains(where: \.isTaxDocument) }
    var canImportStatements: Bool { files.contains(where: \.isStatement) }

    /// Where the files go: the agent while audio or video is saved as
    /// recordings, or when finance cannot take them.
    var effectiveFileDestination: ShareFileDestination {
        if isRecordingDestination { return .agent }
        switch fileDestination {
        case .taxDocuments where canReadTaxDocuments: return .taxDocuments
        case .statements where canImportStatements: return .statements
        default: return .agent
        }
    }

    /// Reads what was shared: links and words into the note, pictures and
    /// files as attachments.
    func read() async {
        defer { isReading = false }
        let providers = (context?.inputItems as? [NSExtensionItem] ?? []).flatMap { $0.attachments ?? [] }
        var lines: [String] = []
        for (index, provider) in providers.enumerated() {
            if provider.hasItemConformingToTypeIdentifier(UTType.audio.identifier)
                || provider.hasItemConformingToTypeIdentifier(UTType.movie.identifier)
            {
                do { if let file = try await ShareRecordingImport.stage(provider: provider) { recordingFiles.append(file) } } catch {
                    problemText = error.localizedDescription
                }
            } else if provider.hasItemConformingToTypeIdentifier(UTType.image.identifier) {
                if let data = await Self.data(of: provider, type: UTType.image), let file = Self.picture(data, index: index) {
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
        // Statement files and nothing else were shared to be imported: a
        // wallet's export goes nowhere else.
        if !files.isEmpty, files.allSatisfy(\.isStatement), recordingFiles.isEmpty, lines.isEmpty {
            fileDestination = .statements
        }
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
                guard let recordingUserId else { throw TeaNodeError.refused(String(localized: "Open TeaNode online once to verify this account before recording offline.")) }
                for file in recordingFiles {
                    try await ShareRecordingImport.save(
                        file, accountKey: connection.accountKey, recordingUserId: recordingUserId, title: note.trimmingCharacters(in: .whitespacesAndNewlines))
                }
                cleanupStagedRecordings()
                context?.completeRequest(returningItems: nil)
                return
            }
            switch effectiveFileDestination {
            case .taxDocuments:
                try await readTaxDocuments(connection)
                return
            case .statements:
                try await importStatements(connection)
                return
            case .agent:
                break
            }
            let recordingBytes = try recordingFiles.reduce(0) { total, file in
                total + (try file.originalURL.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0)
            }
            guard recordingBytes + files.reduce(0, { $0 + $1.data.count }) <= Self.bytesMost else {
                throw TeaNodeError.refused(
                    String(localized: "These files exceed the chat attachment limit. Save audio or video as recordings instead."))
            }
            var attachmentIds: [String] = []
            for file in recordingFiles {
                attachmentIds.append(
                    try await connection.upload(
                        name: file.originalFilename, contentType: file.originalContentType, data: Data(contentsOf: file.originalURL)))
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

    /// Hands each PDF and picture in as a tax document, so the server keeps
    /// no text of it, and has it read as a paystub or a W-2. A failure says
    /// how many went before it; trying again sends only the rest.
    private func readTaxDocuments(_ connection: Connection) async throws {
        let taxDocuments = files.filter(\.isTaxDocument)
        for file in taxDocuments where !handledFileIds.contains(file.id) {
            do {
                let attachmentId = try await connection.upload(name: file.name, contentType: file.contentType, data: file.data, isTaxDocument: true)
                _ = try await connection.readTaxDocument(attachmentId: attachmentId)
                handledFileIds.insert(file.id)
            } catch {
                throw partialFailure(error, handledCount: handledFileIds.count, totalCount: taxDocuments.count, fileName: file.name)
            }
        }
        var said = taxDocuments.count == 1
            ? String(localized: "Reading 1 tax document. It shows under Finance, Taxes once read.")
            : String(localized: "Reading \(taxDocuments.count) tax documents. They show under Finance, Taxes once read.")
        if taxDocuments.count < files.count {
            said += " " + String(localized: "Files that are not a PDF or a picture were left out.")
        }
        resultText = said
    }

    /// Uploads each OFX statement and imports it, saying what each import
    /// did.
    private func importStatements(_ connection: Connection) async throws {
        let statements = files.filter(\.isStatement)
        for file in statements where !handledFileIds.contains(file.id) {
            do {
                let attachmentId = try await connection.upload(name: file.name, contentType: file.contentType, data: file.data)
                let imported = try await connection.importStatement(attachmentId: attachmentId)
                handledFileIds.insert(file.id)
                statementSummaries.append(StatementImportWords.summary(imported))
            } catch {
                throw partialFailure(error, handledCount: handledFileIds.count, totalCount: statements.count, fileName: file.name)
            }
        }
        var summaries = statementSummaries
        if files.contains(where: { !$0.isStatement }) {
            summaries.append(String(localized: "Files that are not OFX statements were left out."))
        }
        resultText = summaries.joined(separator: "\n")
    }

    /// A failure partway through, saying what went before it, so trying
    /// again is known to send only the rest.
    private func partialFailure(_ error: Error, handledCount: Int, totalCount: Int, fileName: String) -> Error {
        guard handledCount > 0 else { return error }
        return TeaNodeError.refused(String(localized: "\(handledCount) of \(totalCount) files went; \(fileName) did not: \(error.localizedDescription) Send again for the rest."))
    }

    func finish() {
        cleanupStagedRecordings()
        context?.completeRequest(returningItems: nil)
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

    /// A picture as a JPEG at most 2048 pixels on its longest side, as the
    /// app sends one, with a small copy to show in the sheet.
    ///
    /// Read scaled down straight from the file, never decoded whole: a
    /// share extension is killed past about 120 MB, and a photo from the
    /// camera decodes to more than that, so the sheet closed as it opened.
    private static func picture(_ data: Data, index: Int) -> SharedFile? {
        guard let source = CGImageSourceCreateWithData(data as CFData, [kCGImageSourceShouldCache: false] as CFDictionary),
            let sent = scaled(source, longestPixels: 2048), let jpeg = UIImage(cgImage: sent).jpegData(compressionQuality: 0.8)
        else { return nil }
        let preview = scaled(source, longestPixels: 264).map { UIImage(cgImage: $0) }
        return SharedFile(name: "picture-\(index + 1).jpg", contentType: "image/jpeg", data: jpeg, preview: preview)
    }

    /// The picture at most longestPixels on its longest side, turned the
    /// way it was taken.
    private static func scaled(_ source: CGImageSource, longestPixels: Int) -> CGImage? {
        let options: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceShouldCacheImmediately: true,
            kCGImageSourceThumbnailMaxPixelSize: longestPixels,
        ]
        return CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary)
    }
}

/// ShareView is the share sheet's page: what goes, where the files go, a
/// note for the agent, and Send; after files went to finance, what came of
/// it and Done.
struct ShareView: View {
    @ObservedObject var model: ShareModel

    private var sendLabel: String {
        if model.isRecordingDestination { return String(localized: "Save recording") }
        switch model.effectiveFileDestination {
        case .taxDocuments: return String(localized: "Read")
        case .statements: return String(localized: "Import")
        case .agent: return String(localized: "Send")
        }
    }

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
                        } footer: {
                            Text(
                                "Recordings are kept locally until verified by the server. Open TeaNode to resume their background uploads. Recording speech is not sent as an instruction to your agent."
                            )
                            if model.isRecordingDestination && !model.files.isEmpty {
                                Text("Only audio and video are saved as recordings. The other shared attachments are not included.")
                            }
                        }
                    }
                    if !model.isRecordingDestination, model.canReadTaxDocuments || model.canImportStatements {
                        Section {
                            Picker("Send the files", selection: $model.fileDestination) {
                                Text("To your agent").tag(ShareFileDestination.agent)
                                if model.canReadTaxDocuments {
                                    Text("Read as tax documents").tag(ShareFileDestination.taxDocuments)
                                }
                                if model.canImportStatements {
                                    Text("Import as statements").tag(ShareFileDestination.statements)
                                }
                            }
                            .pickerStyle(.inline)
                            .labelsHidden()
                            .disabled(model.resultText != nil)
                        } header: {
                            Text("Send the files")
                        } footer: {
                            switch model.effectiveFileDestination {
                            case .taxDocuments:
                                Text("Each PDF or picture is read as a paystub, W-2, 1099 or 1098 on its own, and the server keeps no text of it. No note goes with them.")
                            case .statements:
                                Text("Each OFX, QFX or QBO file is imported into your accounts; a transaction imported twice is kept once. No note goes with them.")
                            case .agent:
                                EmptyView()
                            }
                        }
                    }
                    if let resultText = model.resultText {
                        Section {
                            Label(resultText, systemImage: "checkmark.circle")
                        }
                    }
                    if model.effectiveFileDestination == .agent {
                        Section {
                            TextField(
                                model.isRecordingDestination
                                    ? String(localized: "Meeting title (optional)") : String(localized: "Add a note for your agent"), text: $model.note,
                                axis: .vertical
                            )
                            .lineLimit(3...10)
                        } footer: {
                            Text(
                                model.isRecordingDestination
                                    ? String(localized: "Saved recordings will appear in the recording library.")
                                    : String(localized: "It goes to your main conversation."))
                        }
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
                    if model.resultText == nil {
                        Button("Cancel") { model.cancel() }
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if model.resultText != nil {
                        Button("Done") { model.finish() }
                    } else if model.isSending || model.isReading {
                        ProgressView()
                    } else {
                        Button(sendLabel) {
                            Task { await model.send() }
                        }
                        .disabled(!model.canSend)
                    }
                }
            }
        }
    }
}
