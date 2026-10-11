import SwiftUI
import TeaNodeKit
import WebKit

/// MailThreadScreen is one conversation: its messages newest first, the
/// unread and newest open, the agent's summary and held reply above them,
/// and the toolbar acting on the conversation in the folder it was opened
/// from.
struct MailThreadScreen: View {
    let mail: MailModel
    let opened: OpenedThread
    @Binding var composing: ComposeStart?
    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var app
    @State private var thread: MailThreadView?
    @State private var problemText: String?
    @State private var expanded = Set<String>()
    // Closed at first and remembered, as the dashboard keeps it.
    @AppStorage("mail-summary-open") private var isSummaryOpen = false
    @State private var isSorting = false

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 12) {
                if let thread {
                    Text(thread.subject.isEmpty ? String(localized: "(no subject)") : thread.subject)
                        .font(.title3.weight(.semibold))
                        .textSelection(.enabled)
                    if thread.entries.count > 1 {
                        Text(thread.truncated ? String(localized: "Only the newest \(thread.entries.count) messages are shown") : String(localized: "\(thread.entries.count) messages"))
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                    if let reply = thread.heldReply, reply.status == "held" {
                        HeldReplyBanner(mail: mail, reply: reply, composing: $composing) { Task { await load() } }
                    }
                    if let summary = thread.summary, !summary.summary.isEmpty || summary.pending {
                        SummaryStrip(summary: summary, isOpen: $isSummaryOpen)
                    }
                    ForEach(thread.entries, id: \.item?.id) { entry in
                        if let item = entry.item {
                            MessageCard(
                                mail: mail, entry: entry, item: item, isOpen: expanded.contains(item.id),
                                isOtherFolder: opened.folderId != nil && entry.folderId != opened.folderId
                            ) {
                                toggle(item)
                            }
                        }
                    }
                } else if let problemText {
                    ContentUnavailableView("This conversation could not be read", systemImage: "envelope.badge", description: Text(problemText))
                } else {
                    ProgressView().frame(maxWidth: .infinity, minHeight: 200)
                }
            }
            .padding()
        }
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { toolbar }
        .task { await load() }
        .task(id: thread?.summary?.pending) {
            // A summary being written is read again every five seconds, six
            // times at most.
            guard thread?.summary?.pending == true else { return }
            for _ in 0..<6 {
                try? await Task.sleep(for: .seconds(5))
                guard !Task.isCancelled else { return }
                if let read = try? await mail.connection.mailThread(itemId: opened.itemId) {
                    thread = read
                    if read.summary?.pending != true { return }
                }
            }
        }
        .sheet(isPresented: $isSorting) {
            SortSheet(mail: mail, count: targetItems.count) { category, priority, needsReply in
                Task {
                    let itemIds = targetItems.map(\.id)
                    _ = await mail.act(saying: String(localized: "Sorted")) {
                        try await mail.connection.sortMail(itemIds, category: category, priority: priority, needsReply: needsReply)
                    }
                    await load()
                }
            }
        }
    }

    // MARK: Loading

    private func load() async {
        do {
            let read = try await mail.connection.mailThread(itemId: opened.itemId)
            let isFirst = thread == nil
            thread = read
            if isFirst { openFirst(read) }
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// Opens the newest message that is not a draft, every unread one and
    /// the one tapped, and marks those read: the ones in the folder being
    /// read, or the one tapped.
    private func openFirst(_ read: MailThreadView) {
        let items = read.entries.compactMap(\.item)
        var open = Set(items.filter { !$0.seen }.map(\.id))
        if let newest = items.first(where: { !$0.draft }) { open.insert(newest.id) }
        open.insert(opened.itemId)
        expanded = open
        let marking = read.entries.filter { entry in
            guard let item = entry.item, open.contains(item.id), !item.seen, !item.draft else { return false }
            return item.id == opened.itemId || opened.folderId == nil || entry.folderId == opened.folderId
        }.compactMap(\.item?.id)
        guard !marking.isEmpty else { return }
        Task {
            try? await mail.connection.setMailFlags(marking, isSeen: true)
            await mail.load()
        }
        if opened.itemId == items.first?.id, items.first?.draft == true {
            composing = ComposeStart(kind: .draft, itemId: opened.itemId)
        }
    }

    private func toggle(_ item: MailItem) {
        if item.draft {
            composing = ComposeStart(kind: .draft, itemId: item.id)
            return
        }
        if expanded.contains(item.id) {
            expanded.remove(item.id)
        } else {
            expanded.insert(item.id)
            if !item.seen {
                Task { try? await mail.connection.setMailFlags([item.id], isSeen: true) }
            }
        }
    }

    // MARK: Toolbar

    /// What the toolbar acts on: the conversation's messages in the folder
    /// it was opened in, or the one message opened.
    private var targetItems: [MailItem] {
        let entries = thread?.entries ?? []
        let inFolder = entries.filter { opened.folderId != nil && $0.folderId == opened.folderId }.compactMap(\.item)
        if !inFolder.isEmpty { return inFolder }
        return entries.compactMap(\.item).filter { $0.id == opened.itemId }
    }

    private var newest: MailItem? { thread?.entries.compactMap(\.item).first }

    private var currentFolder: MailFolder? { opened.folderId.flatMap { mail.folder($0) } }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        let items = targetItems
        let isAnyUnread = items.contains { !$0.seen }
        let isAnyUnstarred = items.contains { !$0.flagged }
        ToolbarItem(placement: .topBarTrailing) {
            Button {
                Task {
                    let itemIds = items.map(\.id)
                    _ = await mail.act(saying: isAnyUnstarred ? String(localized: "Starred") : String(localized: "Unstarred")) {
                        try await mail.connection.setMailFlags(itemIds, isFlagged: isAnyUnstarred)
                    }
                    await load()
                }
            } label: {
                Label(isAnyUnstarred ? LocalizedStringKey("Star") : "Unstar", systemImage: isAnyUnstarred ? "star" : "star.fill")
            }
            .disabled(items.isEmpty)
        }
        ToolbarItem(placement: .topBarTrailing) {
            Menu {
                Button(isAnyUnread ? LocalizedStringKey("Mark read") : "Mark unread", systemImage: isAnyUnread ? "envelope.open" : "envelope.badge") {
                    Task {
                        let itemIds = items.map(\.id)
                        _ = await mail.act(saying: isAnyUnread ? String(localized: "Marked read") : String(localized: "Marked unread")) {
                            try await mail.connection.setMailFlags(itemIds, isSeen: isAnyUnread)
                        }
                        await load()
                    }
                }
                Button("Ask \(app.agentTitle)", systemImage: "bubble.left.and.text.bubble.right") {
                    askAgent()
                }
                if mail.mailbox?.agent?.isSorting == true {
                    Button("Sort as…", systemImage: "tag") { isSorting = true }
                }
                if currentFolder?.kind == "junk" {
                    Button("Not junk", systemImage: "hand.thumbsup") { junk(isNotJunk: true) }
                } else {
                    Button("Report junk", systemImage: "xmark.bin") { junk(isNotJunk: false) }
                }
            } label: {
                Label("More", systemImage: "ellipsis.circle")
            }
            .disabled(items.isEmpty)
        }
        ToolbarItemGroup(placement: .bottomBar) {
            if let archive = mail.folder(kind: "archive"), currentFolder?.id != archive.id {
                Button {
                    move(to: archive, saying: String(localized: "Conversation archived"))
                } label: {
                    Label("Archive", systemImage: "archivebox")
                }
            }
            Spacer()
            Menu {
                ForEach(MailFolders.rows(mail.folders).filter { $0.folder.id != currentFolder?.id }, id: \.folder.id) { row in
                    Button(String(repeating: "  ", count: row.depth) + MailNames.folder(row.folder)) {
                        move(to: row.folder, saying: String(localized: "Moved to \(MailNames.folder(row.folder))"))
                    }
                }
            } label: {
                Label("Move to…", systemImage: "folder")
            }
            Spacer()
            Button(role: .destructive) {
                delete()
            } label: {
                Label(currentFolder?.kind == "trash" ? LocalizedStringKey("Delete forever") : "Delete", systemImage: "trash")
            }
            Spacer()
            if let newest, !newest.draft {
                Menu {
                    Button("Reply", systemImage: "arrowshape.turn.up.left") {
                        composing = ComposeStart(kind: .reply, itemId: newest.id)
                    }
                    Button("Reply all", systemImage: "arrowshape.turn.up.left.2") {
                        composing = ComposeStart(kind: .replyAll, itemId: newest.id)
                    }
                    Button("Forward", systemImage: "arrowshape.turn.up.right") {
                        composing = ComposeStart(kind: .forward, itemId: newest.id)
                    }
                } label: {
                    Label("Reply", systemImage: "arrowshape.turn.up.left")
                } primaryAction: {
                    composing = ComposeStart(kind: .reply, itemId: newest.id)
                }
            }
        }
    }

    private func move(to folder: MailFolder, saying said: String) {
        let target = MailTarget(items: targetItems)
        Task {
            if await mail.move(target, to: folder.id, saying: said) { dismiss() }
        }
    }

    private func delete() {
        let target = MailTarget(items: targetItems)
        let isForever = currentFolder?.kind == "trash"
        Task {
            let said = isForever ? String(localized: "Deleted for good") : String(localized: "Moved to Trash")
            if await mail.delete(target, isForever: isForever, saying: said) { dismiss() }
        }
    }

    private func junk(isNotJunk: Bool) {
        let target = MailTarget(items: targetItems)
        Task {
            let said = isNotJunk ? String(localized: "Said this is not junk") : String(localized: "Reported as junk")
            if await mail.reportJunk(target, isNotJunk: isNotJunk, saying: said) { dismiss() }
        }
    }

    /// Opens the agent with this conversation quoted, as the dashboard's
    /// Ask agent does.
    private func askAgent() {
        guard let newest, let message = newest.mail else { return }
        app.askingAbout = AgentReference(itemId: newest.id, threadId: thread?.threadId, subject: message.subject, from: message.shownSender)
    }
}

/// HeldReplyBanner is a reply the agent is holding to send: what it says,
/// and editing or cancelling it.
struct HeldReplyBanner: View {
    let mail: MailModel
    let reply: MailThreadView.HeldReply
    @Binding var composing: ComposeStart?
    let onChange: () -> Void
    @State private var isShowingText = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Label {
                if let sendAfter = ServerTime.date(reply.sendAfter) {
                    Text("Your agent will reply to \(reply.to) at \(sendAfter.formatted(date: .omitted, time: .shortened))")
                } else {
                    Text("Your agent will reply to \(reply.to)")
                }
            } icon: {
                Image(systemName: "clock.arrow.circlepath")
            }
            .font(.callout)
            if isShowingText {
                Text(reply.text).font(.footnote).textSelection(.enabled)
            }
            HStack {
                Button(isShowingText ? LocalizedStringKey("Hide") : "Show") { isShowingText.toggle() }
                if !reply.draftItemId.isEmpty {
                    Button("Edit") { composing = ComposeStart(kind: .draft, itemId: reply.draftItemId) }
                }
                Button("Cancel reply", role: .destructive) {
                    Task {
                        _ = await mail.act(saying: String(localized: "The agent's reply was cancelled")) {
                            try await mail.connection.cancelAgentReply(reply.id)
                        }
                        onChange()
                    }
                }
            }
            .buttonStyle(.bordered)
            .font(.footnote)
        }
        .padding()
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 12))
    }
}

/// SummaryStrip is the agent's summary of the conversation, folded on a
/// tap.
struct SummaryStrip: View {
    let summary: MailThreadView.Summary
    @Binding var isOpen: Bool

    var body: some View {
        DisclosureGroup(isExpanded: $isOpen) {
            Text(summary.summary)
                .font(.callout)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.top, 4)
        } label: {
            HStack {
                Label("Summary", systemImage: "text.alignleft")
                if summary.pending {
                    Text(summary.summary.isEmpty ? LocalizedStringKey("writing…") : "catching up…").foregroundStyle(.secondary)
                } else if summary.stale {
                    Text("from before the newest message").foregroundStyle(.secondary)
                }
            }
            .font(.subheadline)
        }
        .padding()
        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 12))
    }
}

/// MessageCard is one message: a line with who and when while folded, and
/// open, its details, invitation, attachments and content.
struct MessageCard: View {
    let mail: MailModel
    let entry: MailThreadView.Entry
    let item: MailItem
    let isOpen: Bool
    let isOtherFolder: Bool
    let onToggle: () -> Void
    @Environment(AppModel.self) private var app
    @State private var content: MailContent?
    @State private var problemText: String?
    @State private var isShowingDetails = false
    @State private var isShowingHeaders = false
    @State private var isLoadingPictures = false
    @State private var isOriginalLook = false

    var body: some View {
        let message = item.mail
        VStack(alignment: .leading, spacing: 10) {
            Button(action: onToggle) {
                HStack(alignment: .top, spacing: 10) {
                    SenderLogo(domain: message?.logoDomain ?? "", name: message?.shownSender ?? "")
                    VStack(alignment: .leading, spacing: 2) {
                        HStack {
                            Text(message?.shownSender ?? String(localized: "Unknown sender"))
                                .font(.headline.weight(item.seen ? .regular : .semibold))
                                .lineLimit(1)
                            if item.draft {
                                Text("Draft").font(.caption.weight(.semibold)).foregroundStyle(.red)
                            }
                            Spacer()
                            VerdictShield(results: message?.authenticationResults)
                            Text(MailTime.short(message?.receivedAt ?? item.addedAt))
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        if isOtherFolder {
                            Text(entry.folderName).font(.caption).foregroundStyle(.secondary)
                        }
                        if !isOpen {
                            Text(item.insight?.summary.isEmpty == false ? item.insight?.summary ?? "" : message?.subject ?? "")
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                                .lineLimit(2)
                        } else if let to = message?.recipients, !to.isEmpty {
                            Text("To \(to.joined(separator: ", "))")
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                                .lineLimit(isShowingDetails ? nil : 1)
                        }
                    }
                }
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            if isOpen {
                opened(message)
            }
        }
        .padding()
        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 14))
        .task(id: isOpen) {
            guard isOpen, content == nil, let mailId = message?.id else { return }
            do {
                content = try await mail.connection.mailContent(mailId: mailId)
            } catch {
                problemText = error.localizedDescription
            }
        }
    }

    @ViewBuilder
    private func opened(_ message: MailMessage?) -> some View {
        HStack {
            Spacer()
            Menu {
                Button(isShowingDetails ? LocalizedStringKey("Hide details") : "Show details", systemImage: "info.circle") { isShowingDetails.toggle() }
                Button(isShowingHeaders ? LocalizedStringKey("Hide headers") : "Show headers", systemImage: "list.bullet.rectangle") { isShowingHeaders.toggle() }
                    .disabled((content?.rawHeaders ?? "").isEmpty)
                if let message {
                    Button("Download original", systemImage: "arrow.down.doc") {
                        Task { await app.preview("/api/v1/mail/\(message.id)/raw", name: "\(message.id).eml") }
                    }
                }
                Button(isOriginalLook ? LocalizedStringKey("Show dark") : "Show original look", systemImage: "circle.lefthalf.filled") { isOriginalLook.toggle() }
            } label: {
                Label("Message actions", systemImage: "ellipsis")
                    .labelStyle(.iconOnly)
                    .frame(minWidth: 44, minHeight: 32)
            }
        }
        if isShowingDetails, let message {
            MessageDetails(message: message, insight: item.insight)
        }
        if isShowingHeaders, let raw = content?.rawHeaders, !raw.isEmpty {
            ScrollView(.horizontal) {
                Text(raw).font(.caption.monospaced()).textSelection(.enabled)
            }
            .frame(maxHeight: 240)
        }
        InvitationCard(mail: mail, itemId: item.id)
        if let content {
            if !content.available {
                Label("This message is no longer stored.", systemImage: "exclamationmark.triangle")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            let files = content.presentAttachments.filter { !$0.inline || !content.html.contains("/attachment/\($0.index)") }
            ForEach(files, id: \.index) { file in
                Button {
                    Task { await app.preview("/api/v1/mail/\(content.mailId)/attachment/\(file.index)", name: file.filename) }
                } label: {
                    Label {
                        Text("\(file.filename.isEmpty ? String(localized: "Attachment") : file.filename) · \(ByteCountFormatter.string(fromByteCount: Int64(file.size), countStyle: .file))")
                    } icon: {
                        Image(systemName: "paperclip")
                    }
                    .font(.footnote)
                }
            }
            if content.hasRemoteContent == true, content.imagesAllowed != true, !isLoadingPictures {
                HStack {
                    Label("Pictures from the sender are held back.", systemImage: "photo.badge.exclamationmark")
                        .font(.footnote)
                    Spacer()
                    Button("Load them") {
                        isLoadingPictures = true
                        Task { try? await mail.connection.showMailImages([item.id]) }
                    }
                    .font(.footnote)
                }
            }
            if !content.html.isEmpty {
                MailBody(
                    html: content.html, mailId: content.mailId,
                    isShowingPictures: isLoadingPictures || content.imagesAllowed == true, isOriginalLook: isOriginalLook)
            } else {
                Text(content.text)
                    .font(.body)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        } else if let problemText {
            Text(problemText).font(.footnote).foregroundStyle(.red)
        } else {
            ProgressView().frame(maxWidth: .infinity)
        }
    }
}

/// MessageDetails are who it is from and to, when it came, how it was
/// checked, and what the agent made of it.
struct MessageDetails: View {
    let message: MailMessage
    let insight: MailInsight?

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            LabeledContent("From", value: message.fromName.isEmpty ? message.from : "\(message.fromName) <\(message.from)>")
            LabeledContent("To", value: message.recipients.joined(separator: ", "))
            if let received = ServerTime.date(message.receivedAt) {
                LabeledContent("Received", value: received.formatted(date: .abbreviated, time: .shortened))
            }
            let checks = MailVerdict.detail(message.authenticationResults)
            if !checks.isEmpty {
                LabeledContent("Checks", value: checks)
            }
            if let insight {
                LabeledContent("Category", value: insight.category)
                LabeledContent("Priority", value: insight.priority)
                LabeledContent("Needs reply", value: insight.needsReply ? String(localized: "Yes") : String(localized: "No"))
                if !insight.summary.isEmpty {
                    Text(insight.summary)
                }
                ForEach(insight.actionItems, id: \.self) { action in
                    Label(action, systemImage: "checkmark.circle")
                }
                if let notes = insight.notes, !notes.isEmpty {
                    Text(notes).foregroundStyle(.secondary)
                }
            }
        }
        .font(.footnote)
        .textSelection(.enabled)
    }
}

/// InvitationCard is a calendar invitation a message carries: what, when,
/// where, who, and answering it, once, even if the answer's reply is lost.
struct InvitationCard: View {
    let mail: MailModel
    let itemId: String
    @State private var invitation: MailInvitation?
    @State private var isAnswering = false

    var body: some View {
        Group {
            if let invitation {
                if invitation.method != "REQUEST" {
                    Label(invitation.cancelled ? String(localized: "“\(invitation.summary)” was cancelled") : String(localized: "“\(invitation.summary)” was answered"), systemImage: "calendar")
                        .font(.footnote)
                } else {
                    VStack(alignment: .leading, spacing: 6) {
                        HStack {
                            Label(invitation.summary, systemImage: "calendar").font(.headline)
                            if invitation.cancelled {
                                Text("Cancelled").font(.caption).foregroundStyle(.red)
                            }
                        }
                        Text(when(invitation)).font(.callout)
                        if !invitation.location.isEmpty {
                            Label(invitation.location, systemImage: "mappin").font(.footnote)
                        }
                        if !invitation.organizer.isEmpty {
                            Text("Organized by \(invitation.organizer)").font(.footnote).foregroundStyle(.secondary)
                        }
                        if !invitation.cancelled {
                            HStack {
                                answerButton("ACCEPTED", title: String(localized: "Accept"))
                                answerButton("TENTATIVE", title: String(localized: "Maybe"))
                                answerButton("DECLINED", title: String(localized: "Decline"))
                            }
                            .disabled(isAnswering)
                        }
                    }
                    .padding()
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Color(.tertiarySystemBackground), in: .rect(cornerRadius: 12))
                }
            }
        }
        .task {
            invitation = try? await mail.connection.mailInvitation(itemId: itemId)
        }
    }

    private func answerButton(_ answer: String, title: String) -> some View {
        Button(title) {
            Task { await send(answer) }
        }
        .buttonStyle(invitation?.participation.uppercased() == answer ? AnyPrimitiveButtonStyle(BorderedProminentButtonStyle()) : AnyPrimitiveButtonStyle(BorderedButtonStyle()))
    }

    /// Sends the answer with an id kept on the phone first, so a retry asks
    /// whether the first one landed instead of answering twice.
    private func send(_ answer: String) async {
        let key = "invitation-answer.\(itemId)"
        let isRetry = UserDefaults.standard.string(forKey: key) != nil
        let requestId = UserDefaults.standard.string(forKey: key)
            ?? (0..<16).map { _ in String(format: "%02x", UInt8.random(in: 0...255)) }.joined()
        UserDefaults.standard.set(requestId, forKey: key)
        isAnswering = true
        defer { isAnswering = false }
        let isAnswered = await mail.act(saying: String(localized: "Answer sent")) {
            try await mail.connection.answerInvitation(itemId: itemId, answer: answer, requestId: requestId, isRetry: isRetry)
        }
        if isAnswered {
            UserDefaults.standard.removeObject(forKey: key)
            invitation = try? await mail.connection.mailInvitation(itemId: itemId)
        }
    }

    private func when(_ invitation: MailInvitation) -> String {
        if invitation.allDay, let day = CalendarDates.day(ofAllDay: invitation.startsAt), let date = Calendar.current.date(from: day) {
            return date.formatted(date: .complete, time: .omitted)
        }
        guard let start = ServerTime.date(invitation.startsAt) else { return "" }
        let end = ServerTime.date(invitation.endsAt) ?? start
        return "\(start.formatted(date: .abbreviated, time: .shortened)) – \(end.formatted(date: .omitted, time: .shortened))"
    }
}

/// AnyPrimitiveButtonStyle lets a button's style be chosen at run time.
struct AnyPrimitiveButtonStyle: PrimitiveButtonStyle {
    private let make: (Configuration) -> AnyView

    init<Style: PrimitiveButtonStyle>(_ style: Style) {
        make = { AnyView(style.makeBody(configuration: $0)) }
    }

    func makeBody(configuration: Configuration) -> some View {
        make(configuration)
    }
}

/// MailBody is a message's cleaned HTML in a web view with scripts off. It
/// may load only from this server, through the app with its token; links
/// open outside it; it is as tall as what it holds.
struct MailBody: View {
    let html: String
    let mailId: String
    let isShowingPictures: Bool
    let isOriginalLook: Bool
    @Environment(\.colorScheme) private var colorScheme
    @Environment(AppModel.self) private var app
    @State private var height: CGFloat = 60

    var body: some View {
        MailWebView(document: document, connection: app.connection, height: $height)
            .frame(height: height)
    }

    private var document: String {
        let shown = isShowingPictures ? MailWords.showingRemoteImages(html, mailId: mailId) : html
        // Dark mode turns the message's colors around, and its pictures back,
        // unless the person asks for the original look.
        let isDarkened = colorScheme == .dark && !isOriginalLook
        let darkening = isDarkened
            ? "html{filter:invert(1) hue-rotate(180deg)} img,video,picture,[style*=\"background-image\"]{filter:invert(1) hue-rotate(180deg)}"
            : ""
        return """
            <!doctype html><html><head><meta charset="utf-8">
            <meta name="viewport" content="width=device-width, initial-scale=1">
            <meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data: teanode-mail:; style-src 'unsafe-inline'; font-src data:">
            <style>
            html,body{margin:0;padding:0;background:#fff;color:#111}
            #teanode-content{font:-apple-system-body;font-family:-apple-system,sans-serif;line-height:1.45;word-wrap:break-word;overflow-x:auto}
            img{max-width:100%;height:auto} table{max-width:100%} pre{white-space:pre-wrap}
            \(darkening)
            </style></head><body><div id="teanode-content">\(shown)</div></body></html>
            """
    }
}

/// MailWebView is the WKWebView behind MailBody.
struct MailWebView: UIViewRepresentable {
    let document: String
    let connection: Connection?
    @Binding var height: CGFloat

    static let scheme = "teanode-mail"

    func makeCoordinator() -> Coordinator {
        Coordinator(height: $height)
    }

    func makeUIView(context: Context) -> WKWebView {
        let configuration = WKWebViewConfiguration()
        configuration.defaultWebpagePreferences.allowsContentJavaScript = false
        configuration.websiteDataStore = .nonPersistent()
        configuration.setURLSchemeHandler(context.coordinator.loader, forURLScheme: Self.scheme)
        context.coordinator.loader.connection = connection
        let webView = WKWebView(frame: .zero, configuration: configuration)
        webView.navigationDelegate = context.coordinator
        webView.isOpaque = false
        webView.backgroundColor = .clear
        webView.scrollView.backgroundColor = .clear
        webView.scrollView.alwaysBounceVertical = false
        webView.scrollView.bounces = false
        context.coordinator.watch(webView)
        return webView
    }

    func updateUIView(_ webView: WKWebView, context: Context) {
        guard context.coordinator.document != document else { return }
        context.coordinator.document = document
        webView.loadHTMLString(document, baseURL: URL(string: "\(Self.scheme)://mail/"))
    }

    @MainActor
    final class Coordinator: NSObject, @preconcurrency WKNavigationDelegate {
        let loader = MailLoader()
        var document = ""
        private var height: Binding<CGFloat>
        private var watching: NSKeyValueObservation?

        init(height: Binding<CGFloat>) {
            self.height = height
        }

        /// Follows the content's height, so the view is as tall as the
        /// message and only scrolls sideways.
        func watch(_ webView: WKWebView) {
            watching = webView.scrollView.observe(\.contentSize, options: [.new]) { [weak self] _, change in
                guard let tall = change.newValue?.height, tall > 0 else { return }
                // UIKit reports a view's size on the main thread.
                MainActor.assumeIsolated {
                    guard let self, abs(self.height.wrappedValue - tall) > 1 else { return }
                    self.height.wrappedValue = tall
                }
            }
        }

        func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction) async -> WKNavigationActionPolicy {
            guard action.navigationType == .linkActivated, let address = action.request.url else {
                return action.request.url?.scheme == MailWebView.scheme || action.request.url?.scheme == "about" ? .allow : .cancel
            }
            if ["http", "https", "mailto", "tel"].contains(address.scheme?.lowercased() ?? "") {
                _ = await UIApplication.shared.open(address)
            }
            return .cancel
        }
    }
}

/// MailLoader answers the message's requests for this server's addresses
/// (its attachments, and the pictures proxy) with the phone's token; any
/// other address is refused.
@MainActor
final class MailLoader: NSObject, @preconcurrency WKURLSchemeHandler {
    var connection: Connection?
    private var stopped = Set<ObjectIdentifier>()

    func webView(_ webView: WKWebView, start task: any WKURLSchemeTask) {
        guard let address = task.request.url, let connection, address.path.hasPrefix("/api/v1/") else {
            task.didFailWithError(URLError(.unsupportedURL))
            return
        }
        let path = address.path + (address.query.map { "?\($0)" } ?? "")
        let id = ObjectIdentifier(task)
        Task {
            do {
                let data = try await connection.download(path)
                guard !stopped.contains(id) else { return }
                task.didReceive(URLResponse(url: address, mimeType: nil, expectedContentLength: data.count, textEncodingName: nil))
                task.didReceive(data)
                task.didFinish()
            } catch {
                guard !stopped.contains(id) else { return }
                task.didFailWithError(error)
            }
            stopped.remove(id)
        }
    }

    func webView(_ webView: WKWebView, stop task: any WKURLSchemeTask) {
        stopped.insert(ObjectIdentifier(task))
    }
}
