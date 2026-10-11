import PhotosUI
import SwiftUI
import TeaNodeKit

/// ComposeStart is what a message is begun from: nothing, a message to
/// answer or pass on, or a draft to go on with.
struct ComposeStart: Identifiable {
    enum Kind {
        case new, reply, replyAll, forward, draft
    }

    let id = UUID()
    let kind: Kind
    var itemId: String?
    var to: String?
}

/// ComposeModel is a message being written, as the dashboard's compose page
/// keeps it: filled from what it answers, saved as a draft every half
/// minute and on leaving, and sent once even when the answer is lost.
@MainActor
@Observable
final class ComposeModel {
    let mail: MailModel
    let start: ComposeStart
    var mailboxId = ""
    var from = ""
    var to = ""
    var cc = ""
    var bcc = ""
    var subject = ""
    var body = "" {
        didSet { isChanged = true }
    }
    var isShowingCopies = false
    /// The quoted or forwarded message, kept below what is written.
    private(set) var quoteHTML = ""
    private(set) var quoteText = ""
    /// Attachments already on the server: carried from a forwarded message,
    /// or kept on the draft.
    private(set) var carried: [MailContent.Attachment] = []
    private(set) var inlineKept: [Int] = []
    private(set) var isForwardCarry = false
    private(set) var uploadingCount = 0
    private(set) var isLoaded = false
    private(set) var isSaving = false
    private(set) var isSending = false
    private(set) var savedAt: Date?
    var problemText: String?
    var warningText: String?
    private(set) var draftItemId: String?
    private var replyToItemId = ""
    private var forwardItemId = ""
    private(set) var isChanged = false
    /// A send whose answer did not come back, kept on the phone.
    private(set) var pending: MailSending?
    var agentInstructions = ""
    private(set) var isDrafting = false

    private let pendingKey: String

    init(mail: MailModel, start: ComposeStart, username: String) {
        self.mail = mail
        self.start = start
        pendingKey = "mail-pending-send.\(username)"
        if let data = UserDefaults.standard.data(forKey: pendingKey), let kept = try? JSONDecoder().decode(MailSending.self, from: data) {
            pending = kept
        }
    }

    var mailbox: Mailbox? {
        mail.entries.first { $0.mailbox?.id == mailboxId }?.mailbox ?? mail.mailbox
    }

    var isReply: Bool { start.kind == .reply || start.kind == .replyAll }

    var canSend: Bool {
        !from.isEmpty && !recipients.isEmpty && uploadingCount == 0 && !isSending && pending == nil
            && (!body.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !carried.isEmpty)
    }

    private var recipients: [String] { MailWords.addresses(to) + MailWords.addresses(cc) + MailWords.addresses(bcc) }

    // MARK: Filling in

    func load() async {
        guard !isLoaded else { return }
        defer {
            isLoaded = true
            isChanged = false
        }
        mailboxId = mail.mailbox?.id ?? ""
        do {
            switch start.kind {
            case .new:
                to = start.to ?? ""
                addSignature()
            case .draft:
                guard let itemId = start.itemId else { return }
                try await fillDraft(itemId)
            case .reply, .replyAll, .forward:
                guard let itemId = start.itemId else { return }
                try await fillAnswer(itemId)
                addSignature()
            }
        } catch {
            problemText = error.localizedDescription
        }
        if from.isEmpty { from = mailbox?.sendingAddresses.first ?? "" }
        if pending != nil { warningText = String(localized: "This message may already have been sent. Try again to find out, or go back to editing.") }
    }

    private func owningMailbox(of item: MailItem) {
        if let owner = mail.folder(item.folderId)?.mailboxId { mailboxId = owner }
    }

    private func fillDraft(_ itemId: String) async throws {
        let draft = try await mail.connection.mailDraft(itemId: itemId)
        if let item = try? await mail.connection.mailItem(itemId) { owningMailbox(of: item) }
        draftItemId = draft.itemId
        from = draft.from
        to = draft.to.joined(separator: ", ")
        cc = draft.cc.joined(separator: ", ")
        bcc = draft.bcc.joined(separator: ", ")
        isShowingCopies = !draft.cc.isEmpty || !draft.bcc.isEmpty
        subject = draft.subject
        replyToItemId = draft.replyToItemId
        forwardItemId = draft.forwardItemId
        let attachments = draft.attachments.compactMap { $0 }
        carried = attachments.filter { !$0.inline }
        inlineKept = attachments.filter(\.inline).map(\.index)
        // What was written is kept apart from the quote below it.
        if let range = draft.html.range(of: #"<div class="teanode-quote""#) {
            quoteHTML = String(draft.html[range.lowerBound...])
        }
        if let split = draft.text.range(of: #"\n\n(On .+ wrote:\n> |---------- Forwarded message ----------)"#, options: .regularExpression) {
            body = String(draft.text[..<split.lowerBound])
            quoteText = String(draft.text[split.lowerBound...]).trimmingCharacters(in: .newlines)
        } else {
            body = draft.text
        }
    }

    private func fillAnswer(_ itemId: String) async throws {
        let item = try await mail.connection.mailItem(itemId)
        owningMailbox(of: item)
        guard let message = item.mail else {
            warningText = String(localized: "The message answered is no longer stored, so the reply goes without a quote.")
            return
        }
        let content = try? await mail.connection.mailContent(mailId: message.id)
        let original = content.map { $0.html.isEmpty ? MailWords.html(ofText: $0.text) : $0.html } ?? ""
        let originalText = content?.text ?? ""
        if content?.available == false {
            warningText = String(localized: "The message answered is no longer stored, so the reply goes without a quote.")
        }
        let when = ServerTime.date(message.receivedAt)?.formatted(date: .abbreviated, time: .shortened) ?? message.receivedAt
        let sender = message.fromName.isEmpty ? message.from : "\(message.fromName) <\(message.from)>"
        if start.kind == .forward {
            forwardItemId = item.id
            isForwardCarry = true
            subject = MailWords.forwardSubject(message.subject)
            carried = content?.presentAttachments.filter { !$0.inline } ?? []
            let header = [
                "---------- Forwarded message ----------", "From: \(sender)", "Date: \(when)",
                "To: \(message.recipients.joined(separator: ", "))", "Subject: \(message.subject)",
            ]
            quoteHTML = "<div class=\"teanode-quote\"><p>" + header.map(MailWords.escaped).joined(separator: "<br>") + "</p>" + original + "</div>"
            quoteText = header.joined(separator: "\n") + "\n\n" + originalText
        } else {
            replyToItemId = item.id
            let own = mail.entries.flatMap { $0.mailbox?.sendingAddresses ?? [] }
            let reply = MailWords.reply(to: message, replyTo: content?.header("Reply-To"), own: own, isReplyAll: start.kind == .replyAll)
            to = reply.to.joined(separator: ", ")
            cc = reply.cc.joined(separator: ", ")
            isShowingCopies = !reply.cc.isEmpty
            if let address = reply.from, mailbox?.sendingAddresses.contains(where: { $0.lowercased() == address.lowercased() }) == true {
                from = mailbox?.sendingAddresses.first { $0.lowercased() == address.lowercased() } ?? address
            }
            subject = MailWords.replySubject(message.subject)
            let said = String(localized: "On \(when), \(sender) wrote:")
            quoteHTML = "<div class=\"teanode-quote\"><p>\(MailWords.escaped(said))</p><blockquote>\(original)</blockquote></div>"
            quoteText = said + "\n" + MailWords.quoted(originalText)
        }
    }

    /// The signature, once, on a new message: above any quote.
    private func addSignature() {
        guard let signature = mailbox?.signatureText, !signature.isEmpty else { return }
        body = "\n\n-- \n" + signature
    }

    // MARK: Writing

    func uploadFiles(_ files: [PendingFile]) async {
        let limit = mail.entries.first { $0.mailbox?.id == mailboxId }?.maxMessageSize ?? 0
        let adding = files.reduce(0) { $0 + $1.data.count } + carried.reduce(0) { $0 + $1.size }
        if limit > 0, adding > limit {
            problemText = String(localized: "Those files come to \(ByteCountFormatter.string(fromByteCount: Int64(adding), countStyle: .file)), more than a message may be (\(ByteCountFormatter.string(fromByteCount: limit, countStyle: .file))).")
            return
        }
        uploadingCount += files.count
        for file in files {
            defer { uploadingCount -= 1 }
            do {
                if draftItemId == nil { await save() }
                let uploaded = try await mail.connection.attachToDraft(
                    mailboxId: mailboxId, draftItemId: draftItemId, name: file.name, contentType: file.contentType, data: file.data)
                draftItemId = uploaded.itemId
                carried = uploaded.attachments.filter { !$0.inline }
                inlineKept = uploaded.attachments.filter(\.inline).map(\.index)
                isForwardCarry = false
                isChanged = true
            } catch {
                problemText = error.localizedDescription
            }
        }
    }

    func remove(_ attachment: MailContent.Attachment) {
        carried.removeAll { $0.index == attachment.index }
        isChanged = true
    }

    func draftWithAgent() async {
        guard !replyToItemId.isEmpty else { return }
        isDrafting = true
        defer { isDrafting = false }
        do {
            let drafted = try await mail.connection.draftReply(itemId: replyToItemId, instructions: agentInstructions)
            body = drafted + (body.isEmpty ? "" : "\n\n" + body)
            agentInstructions = ""
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// The message as the server takes it: what is written, then the quote.
    private var outgoing: MailOutgoing {
        var message = MailOutgoing()
        message.from = from
        message.to = MailWords.addresses(to)
        message.cc = MailWords.addresses(cc)
        message.bcc = MailWords.addresses(bcc)
        message.subject = subject
        message.htmlContent = MailWords.html(ofText: body) + quoteHTML
        message.textContent = quoteText.isEmpty ? body : body + "\n\n" + quoteText
        message.replyToItemId = replyToItemId
        message.forwardItemId = forwardItemId
        if isForwardCarry {
            message.forwardAttachments = carried.map(\.index)
        }
        if let draftItemId {
            message.draftItemId = draftItemId
            message.keepAttachments = carried.map(\.index) + inlineKept
        }
        return message
    }

    /// Saves the draft, unless a send is in doubt or nothing changed.
    func save(isForced: Bool = false) async {
        guard isLoaded, !isSaving, !isSending, pending == nil, isForced || isChanged || draftItemId == nil else { return }
        isSaving = true
        defer { isSaving = false }
        do {
            draftItemId = try await mail.connection.saveMailDraft(mailboxId: mailboxId, message: outgoing)
            savedAt = Date()
            isChanged = false
            // The server numbers the attachments again on each save.
            if let draftItemId, let draft = try? await mail.connection.mailDraft(itemId: draftItemId) {
                let attachments = draft.attachments.compactMap { $0 }
                carried = attachments.filter { !$0.inline }
                inlineKept = attachments.filter(\.inline).map(\.index)
                isForwardCarry = false
            }
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// Sends, keeping the send on the phone first, so a send whose answer
    /// is lost is asked about before it is made again.
    func send() async -> Bool {
        let sending = pending ?? MailSending(mailboxId: mailboxId, message: outgoing)
        let isRetry = pending != nil
        keep(sending)
        isSending = true
        defer { isSending = false }
        do {
            _ = try await mail.connection.send(sending, isRetry: isRetry)
            keep(nil)
            mail.doneText = String(localized: "Sent")
            await mail.load()
            return true
        } catch {
            warningText = String(localized: "This message may already have been sent. Try again to find out, or go back to editing.")
            problemText = error.localizedDescription
            return false
        }
    }

    /// Stops a send in doubt; if the server had taken it, it was sent.
    func returnToEditing() async -> Bool {
        guard let pending else { return false }
        do {
            let wasSent = try await mail.connection.cancel(pending)
            keep(nil)
            warningText = nil
            if wasSent {
                mail.doneText = String(localized: "Sent")
                return true
            }
        } catch {
            problemText = error.localizedDescription
        }
        return false
    }

    func discard() async {
        guard let draftItemId else { return }
        _ = await mail.act(saying: String(localized: "Draft discarded")) {
            try await mail.connection.deleteMail([draftItemId])
        }
    }

    private func keep(_ sending: MailSending?) {
        pending = sending
        if let sending, let data = try? JSONEncoder().encode(sending) {
            UserDefaults.standard.set(data, forKey: pendingKey)
        } else {
            UserDefaults.standard.removeObject(forKey: pendingKey)
        }
    }
}

/// ComposeView writes a message: from, to, copies, subject, what to say,
/// files, and for a reply, a draft from the agent.
struct ComposeView: View {
    @State private var model: ComposeModel
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase
    @Environment(AppModel.self) private var app
    @State private var isConfirmingDiscard = false
    @State private var isConfirmingClose = false

    init(mail: MailModel, start: ComposeStart) {
        _model = State(initialValue: ComposeModel(mail: mail, start: start, username: mail.username))
    }

    var body: some View {
        @Bindable var model = model
        NavigationStack {
            Form {
                if let warningText = model.warningText {
                    Section {
                        Label(warningText, systemImage: "exclamationmark.triangle")
                        if model.pending != nil {
                            HStack {
                                Button("Try again") { send() }
                                    .buttonStyle(.glassProminent)
                                Button("Back to editing") {
                                    Task {
                                        if await model.returnToEditing() { dismiss() }
                                    }
                                }
                                .buttonStyle(.glass)
                            }
                        }
                    }
                }
                Section {
                    if (model.mailbox?.sendingAddresses.count ?? 0) > 1 {
                        Picker("From", selection: $model.from) {
                            ForEach(model.mailbox?.sendingAddresses ?? [], id: \.self) { address in
                                Text(address).tag(address)
                            }
                        }
                    } else {
                        LabeledContent("From", value: model.from.isEmpty ? String(localized: "No address to send from") : model.from)
                    }
                    AddressField(title: String(localized: "To"), text: $model.to, mail: model.mail)
                    if model.isShowingCopies {
                        AddressField(title: String(localized: "Cc"), text: $model.cc, mail: model.mail)
                        AddressField(title: String(localized: "Bcc"), text: $model.bcc, mail: model.mail)
                    } else {
                        Button("Add Cc and Bcc") { model.isShowingCopies = true }
                    }
                    TextField("Subject", text: $model.subject)
                }
                .disabled(model.pending != nil)
                if model.isReply, model.mailbox?.agent?.granted == true, model.mailbox?.agent?.draftReplies == true {
                    Section {
                        HStack {
                            TextField("What should the reply say? (optional)", text: $model.agentInstructions)
                                .onSubmit { Task { await model.draftWithAgent() } }
                            if model.isDrafting {
                                ProgressView()
                            } else {
                                Button("Draft") { Task { await model.draftWithAgent() } }
                            }
                        }
                    } footer: {
                        Text("Your agent writes a reply above what is already here.")
                    }
                }
                Section {
                    TextField("Message", text: $model.body, axis: .vertical)
                        .lineLimit(8...)
                }
                .disabled(model.pending != nil)
                // What is quoted or forwarded, shown whole under the message,
                // as Mail shows it.
                if !model.quoteText.isEmpty {
                    Section(model.start.kind == .forward ? LocalizedStringKey("Forwarded message") : "Quoted text") {
                        Text(model.quoteText)
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                    }
                }
                Section {
                    ForEach(model.carried, id: \.index) { file in
                        HStack {
                            Label(file.filename.isEmpty ? String(localized: "Attachment") : file.filename, systemImage: "paperclip")
                            Spacer()
                            Text(ByteCountFormatter.string(fromByteCount: Int64(file.size), countStyle: .file))
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                        .swipeActions {
                            Button("Remove", systemImage: "trash", role: .destructive) { model.remove(file) }
                        }
                    }
                    if model.uploadingCount > 0 {
                        HStack {
                            ProgressView()
                            Text("Uploading \(model.uploadingCount) files…").foregroundStyle(.secondary)
                        }
                    }
                    AttachButton { files in
                        Task { await model.uploadFiles(files) }
                    }
                }
                .disabled(model.pending != nil)
                if let problemText = model.problemText {
                    Section {
                        Label(problemText, systemImage: "exclamationmark.circle").foregroundStyle(.red)
                    }
                }
                if model.isLoaded, model.mailbox?.sendingAddresses.isEmpty == true {
                    Section {
                        Text("This mailbox has no address to send from yet.").foregroundStyle(.secondary)
                    }
                }
            }
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close") { isConfirmingClose = true }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button {
                        send()
                    } label: {
                        Label("Send", systemImage: "paperplane.fill")
                    }
                    .disabled(!model.canSend)
                }
                ToolbarItem(placement: .status) {
                    if model.isSaving {
                        Text("Saving…").font(.caption).foregroundStyle(.secondary)
                    } else if let savedAt = model.savedAt {
                        Text("Draft saved \(savedAt.formatted(date: .omitted, time: .shortened))").font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
            .confirmationDialog("Close this message?", isPresented: $isConfirmingClose, titleVisibility: .visible) {
                Button("Save draft") {
                    Task {
                        await model.save(isForced: true)
                        dismiss()
                    }
                }
                Button("Discard", role: .destructive) { isConfirmingDiscard = true }
            }
            .confirmationDialog("Discard this draft?", isPresented: $isConfirmingDiscard, titleVisibility: .visible) {
                Button("Discard", role: .destructive) {
                    Task {
                        await model.discard()
                        dismiss()
                    }
                }
            } message: {
                Text("What was written is gone.")
            }
            .interactiveDismissDisabled(true)
            .task { await model.load() }
            .task {
                // Saved every half minute while it changes.
                while !Task.isCancelled {
                    try? await Task.sleep(for: .seconds(30))
                    await model.save()
                }
            }
            .onChange(of: scenePhase) { _, phase in
                if phase != .active { Task { await model.save() } }
            }
        }
    }

    private var title: String {
        switch model.start.kind {
        case .new: String(localized: "New message")
        case .reply, .replyAll: String(localized: "Reply")
        case .forward: String(localized: "Forward")
        case .draft: String(localized: "Draft")
        }
    }

    private func send() {
        Task {
            if await model.send() { dismiss() }
        }
    }
}

/// AddressField is a recipients field with suggestions from the address
/// book for what is being typed after the last comma.
struct AddressField: View {
    let title: String
    @Binding var text: String
    let mail: MailModel
    @State private var suggestions: [String] = []
    @FocusState private var isFocused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            LabeledContent(title) {
                TextField(title, text: $text, axis: .vertical)
                    .keyboardType(.emailAddress)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .focused($isFocused)
            }
            if isFocused, !suggestions.isEmpty {
                ScrollView(.horizontal) {
                    HStack {
                        ForEach(suggestions, id: \.self) { suggestion in
                            Button(suggestion) { choose(suggestion) }
                                .buttonStyle(.bordered)
                                .buttonBorderShape(.capsule)
                        }
                    }
                }
                .scrollIndicators(.hidden)
            }
        }
        .task(id: text) {
            let typed = typing
            guard isFocused, typed.count >= 2 else {
                suggestions = []
                return
            }
            try? await Task.sleep(for: .milliseconds(150))
            guard !Task.isCancelled, let book = try? await mail.connection.addressBooks().first,
                let contacts = try? await mail.connection.contacts(addressBookId: book.id, matching: typed, first: 10)
            else { return }
            suggestions = contacts.flatMap { contact in
                (contact.emails ?? []).map { email in
                    let name = contact.name ?? ""
                    return name.isEmpty ? email : "\(name) <\(email)>"
                }
            }
        }
    }

    /// What is being typed: the part after the last comma or semicolon.
    private var typing: String {
        let cut = text.lastIndex(where: { $0 == "," || $0 == ";" }).map { text.index(after: $0) } ?? text.startIndex
        return text[cut...].trimmingCharacters(in: .whitespaces)
    }

    private func choose(_ suggestion: String) {
        let cut = text.lastIndex(where: { $0 == "," || $0 == ";" }).map { text.index(after: $0) } ?? text.startIndex
        let before = text[..<cut].trimmingCharacters(in: .whitespaces)
        text = (before.isEmpty ? "" : before + " ") + suggestion + ", "
        suggestions = []
    }
}

/// AttachButton adds files: pictures from the library, a picture taken now,
/// or anything from Files.
struct AttachButton: View {
    let onAdd: ([PendingFile]) -> Void
    @State private var pickedItems: [PhotosPickerItem] = []
    @State private var isPickingPhotos = false
    @State private var isTakingPicture = false
    @State private var isPickingFiles = false

    var body: some View {
        Menu {
            Button("Photo library", systemImage: "photo.on.rectangle") { isPickingPhotos = true }
            if UIImagePickerController.isSourceTypeAvailable(.camera) {
                Button("Camera", systemImage: "camera") { isTakingPicture = true }
            }
            Button("Files", systemImage: "folder") { isPickingFiles = true }
        } label: {
            Label("Attach", systemImage: "paperclip")
        }
        .photosPicker(isPresented: $isPickingPhotos, selection: $pickedItems, maxSelectionCount: 8, matching: .images)
        .cameraPicker(isPresented: $isTakingPicture) { picture in
            if let file = PendingFile(picture: picture, index: 0) { onAdd([file]) }
        }
        .fileImporter(isPresented: $isPickingFiles, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
            guard case .success(let addresses) = result else { return }
            onAdd(addresses.compactMap { PendingFile(fileAt: $0) })
        }
        .onChange(of: pickedItems) { _, items in
            guard !items.isEmpty else { return }
            pickedItems = []
            Task {
                var files: [PendingFile] = []
                for (index, item) in items.enumerated() {
                    if let data = try? await item.loadTransferable(type: Data.self), let file = PendingFile(pictureData: data, index: index) {
                        files.append(file)
                    }
                }
                onAdd(files)
            }
        }
    }
}
