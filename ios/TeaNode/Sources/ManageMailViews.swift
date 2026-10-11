import QuickLook
import SwiftUI
import TeaNodeKit
import UniformTypeIdentifiers

// The dashboard's operator pages for mail delivery, under /manage: all the
// mail on the server, the delivery queue, the DMARC reports and the domains.
// Each list is a table on the dashboard, and stays one here: a row per
// record with the columns that identify it, and a screen for the rest.

// MARK: - The area

/// ManageOverviewView is the way into the operator's mail delivery pages:
/// the rows of the dashboard's rail this person may open, as /manage sends
/// them to the first of them.
struct ManageOverviewView: View {
    let connection: Connection
    @State private var access: OperatorAccess?
    @State private var isLoading = true
    @State private var problemText: String?

    var body: some View {
        List {
            if let access {
                if access.canOpenMailDelivery {
                    Section {
                        if access.canAuditMail {
                            NavigationLink {
                                AllMailView(connection: connection)
                            } label: {
                                Label("Mail", systemImage: "tray.full")
                            }
                        }
                        if access.canManageQueue {
                            NavigationLink {
                                DeliveryQueueView(connection: connection)
                            } label: {
                                Label("Queue", systemImage: "clock.arrow.circlepath")
                            }
                        }
                        if access.canReadReports {
                            NavigationLink {
                                ReportsView(connection: connection)
                            } label: {
                                Label("Reports", systemImage: "checkmark.shield")
                            }
                        }
                    } footer: {
                        Text("What is arriving, and what is stuck.")
                    }
                    if access.canManageDomains {
                        Section {
                            NavigationLink {
                                DomainsView(connection: connection)
                            } label: {
                                Label("Domains", systemImage: "globe")
                            }
                        } footer: {
                            Text("What it arrives for, and how each domain is set up.")
                        }
                    }
                }
                if access.isOperator {
                    // Each page offers only what the account may change, as
                    // the dashboard's do.
                    Section {
                        NavigationLink {
                            PeopleView(connection: connection)
                        } label: {
                            Label("People", systemImage: "person.2")
                        }
                        NavigationLink {
                            GroupsView(connection: connection)
                        } label: {
                            Label("Groups", systemImage: "person.3")
                        }
                        NavigationLink {
                            RolesView(connection: connection)
                        } label: {
                            Label("Roles", systemImage: "checkmark.seal")
                        }
                        NavigationLink {
                            AuditLogView(connection: connection)
                        } label: {
                            Label("Audit log", systemImage: "list.bullet.clipboard")
                        }
                    } header: {
                        Text("People and access")
                    }
                }
            }
        }
        .navigationTitle("Manage")
        .overlay {
            if access == nil && isLoading {
                ProgressView()
            } else if let access, !access.isOperator {
                ContentUnavailableView(
                    "Nothing to manage", systemImage: "lock",
                    description: Text("This account holds no permission that opens the mail, the queue, the reports or the domains."))
            }
        }
        .task { await load() }
        .refreshable { await load() }
        .toast(done: .constant(nil), problem: $problemText)
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            access = try await connection.operatorAccess()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

// MARK: - Words and pieces the pages share

/// ManageLabels are the server's words in the reader's language: the kinds
/// and statuses of mail, and times and sizes as the dashboard writes them.
/// A word nobody translated is shown as itself rather than left out.
private enum ManageLabels {
    static func mailStatus(_ statusText: String?) -> String {
        switch statusText ?? "" {
        case "": String(localized: "Unknown")
        case "received": String(localized: "Received")
        case "accepted": String(localized: "Accepted")
        case "rejected": String(localized: "Rejected")
        case "queued": String(localized: "Queued")
        case "dropped": String(localized: "Dropped")
        case "delivered": String(localized: "Delivered")
        case "attempted": String(localized: "Attempted")
        case "failed": String(localized: "Failed")
        case "delayed": String(localized: "Delayed")
        case "relayed": String(localized: "Relayed")
        case "expanded": String(localized: "Expanded")
        default: statusText ?? ""
        }
    }

    static func mailKind(_ kindText: String?) -> String {
        switch kindText ?? "" {
        case "incoming": String(localized: "Incoming")
        case "outgoing": String(localized: "Outgoing")
        case "exchange": String(localized: "Exchange")
        case "dsn": String(localized: "Bounce")
        case "rua": String(localized: "DMARC report")
        case "ruf": String(localized: "DMARC failure")
        case "forward": String(localized: "Forward")
        case "internal": String(localized: "Internal")
        case "external": String(localized: "External")
        default: kindText ?? ""
        }
    }

    static func alignment(_ alignment: ReportAlignment) -> String {
        switch alignment {
        case .both: String(localized: "DKIM and SPF")
        case .one: String(localized: "One aligned")
        case .neither: String(localized: "Neither aligned")
        }
    }

    /// relaxed or strict, as a DMARC alignment mode is written r or s.
    static func alignmentMode(_ mode: String?) -> String {
        mode == "s" ? String(localized: "strict") : String(localized: "relaxed")
    }

    static func relativeTime(_ timeText: String?) -> String? {
        ServerTime.date(timeText).map { $0.formatted(.relative(presentation: .named)) }
    }

    /// A moment with its zone: these are compared against logs from a
    /// server elsewhere.
    static func fullTime(_ timeText: String?) -> String? {
        ServerTime.date(timeText).map {
            $0.formatted(.dateTime.year().month(.abbreviated).day().hour().minute().timeZone(.specificName(.short)))
        }
    }

    static func byteText(_ byteCount: Int?) -> String {
        guard let byteCount, byteCount > 0 else { return "" }
        return ByteCountFormatter.string(fromByteCount: Int64(byteCount), countStyle: .file)
    }

    static func domainName(_ domainId: String?, in domains: [ManagedDomain]) -> String {
        domains.first { $0.id == domainId }?.domainName ?? String(localized: "(deleted)")
    }
}

/// ManageTag is a status or a verdict as a small label, with a symbol so
/// that its color never says it alone. Green is kept for what can be
/// pressed, so a good state is plain.
private struct ManageTag: View {
    let text: String
    let tone: ManagedTone

    var body: some View {
        Label(text, systemImage: symbolName)
            .font(.caption)
            .foregroundStyle(color)
            .symbolRenderingMode(.hierarchical)
    }

    private var symbolName: String {
        switch tone {
        case .good: "checkmark.circle"
        case .bad: "xmark.octagon"
        case .warn: "exclamationmark.triangle"
        case .plain: "circle"
        }
    }

    private var color: Color {
        switch tone {
        case .good, .plain: Color(.secondaryLabel)
        case .bad: .red
        case .warn: .orange
        }
    }
}

/// DetailRow is one field of a record: its name above, its value under it,
/// selectable. A field with nothing in it is not shown, as on the
/// dashboard.
private struct DetailRow: View {
    let label: LocalizedStringKey
    let fieldText: String?
    var isMonospaced = false

    var body: some View {
        if let fieldText, !fieldText.isEmpty {
            VStack(alignment: .leading, spacing: 2) {
                Text(label)
                    .font(.caption)
                    .foregroundStyle(Color(.secondaryLabel))
                Text(verbatim: fieldText)
                    .font(isMonospaced ? .callout.monospaced() : .callout)
                    .foregroundStyle(Color(.label))
                    .textSelection(.enabled)
            }
        }
    }
}

/// FilterChoice is one option of a filter menu: the value it narrows to,
/// and what the menu calls it.
private struct FilterChoice: Hashable {
    let filterValue: String
    let title: String
}

/// FilterMenu is a list's filters in the toolbar, one picker per column the
/// dashboard filters by choosing from a list.
private struct FilterMenu: View {
    let isFiltering: Bool
    let pickers: [FilterPicker]

    var body: some View {
        Menu {
            ForEach(pickers) { picker in
                Picker(picker.title, selection: picker.selection) {
                    Text(picker.allTitle).tag(String?.none)
                    ForEach(picker.choices, id: \.self) { choice in
                        Text(verbatim: choice.title).tag(String?.some(choice.filterValue))
                    }
                }
                .pickerStyle(.menu)
            }
        } label: {
            Label(
                "Filter",
                systemImage: isFiltering ? "line.3.horizontal.decrease.circle.fill" : "line.3.horizontal.decrease.circle")
        }
    }
}

/// FilterPicker is one picker of a FilterMenu.
private struct FilterPicker: Identifiable {
    let title: String
    let allTitle: String
    let choices: [FilterChoice]
    let selection: Binding<String?>

    var id: String { title }
}

/// FilePreview downloads one of the server's files and opens it in Quick
/// Look.
@MainActor
private enum FilePreview {
    static func save(_ path: String, name: String, connection: Connection) async throws -> URL {
        let fileData = try await connection.download(path)
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let fileName = name.isEmpty ? "file" : name.replacingOccurrences(of: "/", with: "-")
        let address = folder.appendingPathComponent(fileName)
        try fileData.write(to: address)
        return address
    }
}

/// ListPaging shows a long list fifty rows at a time, as the dashboard's
/// tables page it, and the next fifty as the last row comes into view.
private struct ListPaging: View {
    @Binding var shownCount: Int
    let totalCount: Int

    var body: some View {
        if totalCount > shownCount {
            ProgressView()
                .frame(maxWidth: .infinity)
                .onAppear { shownCount += ManagedWords.pageSize }
        }
    }
}

// MARK: - All mail

/// AllMailView is the dashboard's Mail page: the newest five hundred
/// messages on the server, for every domain, filtered by domain, status and
/// kind, and searched by sender and subject. A message opens on its own
/// screen.
struct AllMailView: View {
    let connection: Connection
    @State private var mails: [AuditedMail]?
    @State private var domains: [ManagedDomain] = []
    @State private var opensByMailId: [String: AuditedMailOpens] = [:]
    @State private var filter: MailListFilter
    @State private var shownCount = ManagedWords.pageSize
    @State private var isLoading = true
    @State private var problemText: String?

    /// A domain, and a status in it, to start narrowed to, as the domain
    /// page links here.
    init(connection: Connection, domainId: String? = nil, mailStatus: String? = nil) {
        self.connection = connection
        _filter = State(initialValue: MailListFilter(domainId: domainId, mailStatus: mailStatus))
    }

    private var filtered: [AuditedMail] { filter.applied(to: mails ?? []) }

    var body: some View {
        List {
            if let mails {
                Section {
                    ForEach(filtered.prefix(shownCount)) { mail in
                        NavigationLink {
                            AuditedMailView(connection: connection, mailId: mail.id)
                        } label: {
                            row(mail)
                        }
                    }
                    ListPaging(shownCount: $shownCount, totalCount: filtered.count)
                } footer: {
                    if !mails.isEmpty { Text(verbatim: countText(shownTotal: filtered.count, total: mails.count)) }
                }
            }
        }
        .navigationTitle("Mail")
        .searchable(text: $filter.searchText, prompt: Text("Sender or subject"))
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                FilterMenu(isFiltering: filter.domainId != nil || filter.mailStatus != nil || filter.mailKind != nil, pickers: pickers)
            }
        }
        .overlay {
            if mails == nil && isLoading {
                ProgressView()
            } else if mails != nil && domains.isEmpty && !isLoading {
                ContentUnavailableView(
                    "No domains", systemImage: "globe", description: Text("No domains are configured yet. Add one under Domains."))
            } else if let mails, mails.isEmpty {
                ContentUnavailableView("No mail received yet.", systemImage: "tray")
            } else if let mails, !mails.isEmpty, filtered.isEmpty {
                ContentUnavailableView.search
            }
        }
        .onChange(of: filter) { shownCount = ManagedWords.pageSize }
        .task { await load() }
        .refreshable { await load() }
        .toast(done: .constant(nil), problem: $problemText)
    }

    private var pickers: [FilterPicker] {
        [
            FilterPicker(
                title: String(localized: "Domain"), allTitle: String(localized: "All domains"),
                choices: domains.map { FilterChoice(filterValue: $0.id, title: $0.domainName) }, selection: $filter.domainId),
            FilterPicker(
                title: String(localized: "Status"), allTitle: String(localized: "Any status"),
                choices: Set((mails ?? []).compactMap(\.mailStatus)).sorted().map { FilterChoice(filterValue: $0, title: ManageLabels.mailStatus($0)) },
                selection: $filter.mailStatus),
            FilterPicker(
                title: String(localized: "Kind"), allTitle: String(localized: "Any kind"),
                choices: Set((mails ?? []).compactMap(\.mailKind)).sorted().map { FilterChoice(filterValue: $0, title: ManageLabels.mailKind($0)) },
                selection: $filter.mailKind),
        ]
    }

    private func row(_ mail: AuditedMail) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: mail.shownSender)
                    .font(.subheadline)
                    .foregroundStyle(Color(.label))
                    .lineLimit(1)
                Spacer()
                if let receivedText = ManageLabels.relativeTime(mail.receivedAt) {
                    Text(verbatim: receivedText)
                        .font(.caption)
                        .foregroundStyle(Color(.secondaryLabel))
                }
            }
            Group {
                if let subject = mail.subject, !subject.isEmpty {
                    Text(verbatim: subject)
                } else {
                    Text("(no subject)")
                }
            }
            .font(.callout)
            .foregroundStyle(Color(.label))
            .lineLimit(1)
            HStack(spacing: 8) {
                ManageTag(text: ManageLabels.mailStatus(mail.mailStatus), tone: ManagedTone.of(mail.mailStatus))
                Text(verbatim: details(mail))
                    .font(.caption)
                    .foregroundStyle(Color(.secondaryLabel))
                    .lineLimit(1)
                if let opens = opensByMailId[mail.id], opens.isTrackable {
                    if opens.isOpened {
                        let openCount = opens.openCount ?? 0
                        ManageTag(
                            text: openCount > 1 ? String(localized: "Opened ×\(openCount)") : String(localized: "Opened"), tone: .good)
                    } else {
                        Text("Not fetched").font(.caption).foregroundStyle(Color(.secondaryLabel))
                    }
                }
            }
        }
    }

    /// The domain, the kind and the size.
    private func details(_ mail: AuditedMail) -> String {
        [ManageLabels.domainName(mail.domainId, in: domains), ManageLabels.mailKind(mail.mailKind), ManageLabels.byteText(mail.byteCount)]
            .filter { !$0.isEmpty }
            .joined(separator: " · ")
    }

    /// As the dashboard counts it: the newest five hundred are not everything
    /// the server holds, and saying so is not optional.
    private func countText(shownTotal: Int, total: Int) -> String {
        if filter.isFiltering {
            return shownTotal == 1 ? String(localized: "1 of \(total) messages") : String(localized: "\(shownTotal) of \(total) messages")
        }
        if total >= ManagedWords.listLimit { return String(localized: "The \(total) most recent messages") }
        return total == 1 ? String(localized: "1 message") : String(localized: "\(total) messages")
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            async let namesLoading = connection.managedDomainNames()
            async let mailsLoading = connection.allMail()
            let (loadedDomains, loadedMails) = try await (namesLoading, mailsLoading)
            domains = loadedDomains
            mails = loadedMails
            // Asked after the list, and only about its rows: most mail
            // carries no picture at all.
            opensByMailId = (try? await connection.mailOpens(mailIds: loadedMails.map(\.id))) ?? [:]
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// AuditedMailView is the dashboard's page for one message: what happened
/// to it, teaching the filter, where it came from, its checks, its pictures,
/// its deliveries and the message itself.
struct AuditedMailView: View {
    let connection: Connection
    let mailId: String
    @State private var mailDetail: AuditedMailDetail?
    @State private var trainingLabel: String?
    @State private var learnedTotalCount: Int?
    @State private var isTeaching = false
    @State private var isLoading = true
    @State private var previewAddress: URL?
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        List {
            if let mailDetail {
                let mail = mailDetail.mail
                Section {
                    HStack(spacing: 10) {
                        ManageTag(text: ManageLabels.mailStatus(mail.mailStatus), tone: ManagedTone.of(mail.mailStatus))
                        if let kind = mail.mailKind, !kind.isEmpty {
                            ManageTag(text: ManageLabels.mailKind(kind), tone: .plain)
                        }
                    }
                    Text(verbatim: outcomeText(MailOutcome.of(mail, deliveries: mailDetail.deliveries)))
                        .font(.callout)
                        .foregroundStyle(Color(.label))
                }
                if let classifier = mailDetail.classifier, classifier.canBeTaught {
                    teaching(classifier)
                }
                Section {
                    DetailRow(label: "From", fieldText: mail.shownSender)
                    DetailRow(label: "Envelope sender", fieldText: mail.sender, isMonospaced: true)
                    DetailRow(label: "To", fieldText: (mail.recipients ?? []).joined(separator: ", "))
                    DetailRow(label: "Received", fieldText: receivedText(mail))
                    DetailRow(label: "Connection", fieldText: connectionText(mail))
                    DetailRow(label: "Message-ID", fieldText: mail.messageId, isMonospaced: true)
                    DetailRow(label: "Size", fieldText: ManageLabels.byteText(mail.byteCount))
                }
                if let results = mail.authenticationResults {
                    AuthenticationSection(results: results)
                }
                if let opens = mailDetail.opens, opens.isTrackable {
                    opensSection(opens)
                }
                Section {
                    if mailDetail.deliveries.isEmpty {
                        Text("No deliveries. The message was rejected or matched no alias.")
                            .foregroundStyle(Color(.secondaryLabel))
                    }
                    ForEach(mailDetail.deliveries) { delivery in
                        NavigationLink {
                            DeliveryDetailView(delivery: delivery)
                        } label: {
                            DeliveryRow(delivery: delivery)
                        }
                    }
                } header: {
                    Text("Deliveries")
                }
                messageSection(mailDetail)
            }
        }
        .navigationTitle(titleText)
        .navigationBarTitleDisplayMode(.inline)
        .overlay {
            if mailDetail == nil && isLoading {
                ProgressView()
            } else if mailDetail == nil {
                ContentUnavailableView("Not found", systemImage: "questionmark.folder")
            }
        }
        .task { await load() }
        .refreshable { await load() }
        .quickLookPreview($previewAddress)
        .toast(done: $doneText, problem: $problemText)
    }

    private var titleText: String {
        guard let mailDetail else { return "" }
        if let subject = mailDetail.mail.subject, !subject.isEmpty { return subject }
        return String(localized: "(no subject)")
    }

    /// Two choices rather than two links and an undo: the chosen one is the
    /// message's label, and choosing it again clears it.
    private func teaching(_ classifier: AntispamClassifier) -> some View {
        Section {
            Button {
                Task { await teach(trainingLabel == "spam" ? nil : "spam") }
            } label: {
                HStack {
                    Text("Spam").foregroundStyle(Color(.label))
                    Spacer()
                    if trainingLabel == "spam" { Image(systemName: "checkmark").foregroundStyle(.tint) }
                }
            }
            Button {
                Task { await teach(trainingLabel == "ham" ? nil : "ham") }
            } label: {
                HStack {
                    Text("Not spam").foregroundStyle(Color(.label))
                    Spacer()
                    if trainingLabel == "ham" { Image(systemName: "checkmark").foregroundStyle(.tint) }
                }
            }
        } header: {
            Text("Teach the filter")
        } footer: {
            Text(verbatim: teachingText(classifier))
        }
        .disabled(isTeaching)
    }

    private func teachingText(_ classifier: AntispamClassifier) -> String {
        let labelText =
            switch trainingLabel {
            case "spam"?: String(localized: "Learned as spam.")
            case "ham"?: String(localized: "Learned as not spam.")
            default: String(localized: "Not taught yet.")
            }
        let learnedCount = learnedTotalCount ?? (classifier.bayesLearnedSpamCount + classifier.bayesLearnedHamCount)
        let minimumCount = classifier.bayesMinimumMessageCount
        let classifierText =
            learnedCount >= minimumCount
            ? String(localized: "The classifier is contributing, with \(learnedCount) learned.")
            : String(localized: "The classifier stays silent until \(minimumCount) are learned; it has \(learnedCount).")
        return "\(labelText) \(classifierText)"
    }

    private func opensSection(_ opens: AuditedMailOpens) -> some View {
        Section {
            ManageTag(text: opens.isOpened ? String(localized: "Opened") : String(localized: "Not fetched"), tone: opens.isOpened ? .good : .plain)
            if opens.isOpened {
                DetailRow(label: "First fetched", fieldText: ManageLabels.fullTime(opens.openedAt))
                if let lastOpenedAt = opens.lastOpenedAt, lastOpenedAt != opens.openedAt {
                    DetailRow(label: "Last fetched", fieldText: ManageLabels.fullTime(lastOpenedAt))
                }
                DetailRow(
                    label: "Fetched",
                    fieldText: (opens.openCount ?? 0) == 1 ? String(localized: "Once") : String(localized: "\(opens.openCount ?? 0) times"))
                DetailRow(label: "Fetched by", fieldText: opens.ip, isMonospaced: true)
            }
        } header: {
            Text("Pictures fetched")
        } footer: {
            Text(
                "This says a mail program asked for a picture in this message, which is not the same as somebody reading it. Apple Mail fetches every picture before the message is opened, and most mail programs fetch none until the reader asks, so treat this as a floor with false positives in it, never as a count of readers."
            )
        }
    }

    @ViewBuilder
    private func messageSection(_ mailDetail: AuditedMailDetail) -> some View {
        Section {
            if let content = mailDetail.content, content.available {
                if !content.html.isEmpty {
                    NavigationLink("Rendered") {
                        RenderedMailView(content: content)
                    }
                }
                if !content.text.isEmpty {
                    NavigationLink("Text") {
                        MailSourceView(title: String(localized: "Text"), sourceText: content.text, isMonospaced: false)
                    }
                }
                if !content.html.isEmpty {
                    NavigationLink("HTML") {
                        MailSourceView(title: String(localized: "HTML"), sourceText: content.html, isMonospaced: true)
                    }
                }
                NavigationLink("Headers") {
                    MailHeadersView(headers: (content.headers ?? []).compactMap { $0 })
                }
                if let rawHeaders = content.rawHeaders, !rawHeaders.isEmpty {
                    NavigationLink("Raw headers") {
                        MailSourceView(title: String(localized: "Raw headers"), sourceText: rawHeaders, isMonospaced: true)
                    }
                }
                Button("Download .eml", systemImage: "arrow.down.doc") {
                    Task { await preview("/api/v1/mail/\(mailDetail.mail.id)/raw", name: "\(mailDetail.mail.id).eml") }
                }
                ForEach(content.presentAttachments, id: \.index) { attachment in
                    Button {
                        Task { await preview("/api/v1/mail/\(mailDetail.mail.id)/attachment/\(attachment.index)", name: attachment.filename) }
                    } label: {
                        VStack(alignment: .leading, spacing: 2) {
                            Label {
                                Text(verbatim: attachment.filename.isEmpty ? String(localized: "Attachment") : attachment.filename)
                                    .foregroundStyle(Color(.label))
                            } icon: {
                                Image(systemName: "paperclip")
                            }
                            Text(verbatim: [attachment.contentType, ManageLabels.byteText(attachment.size)].filter { !$0.isEmpty }.joined(separator: " · "))
                                .font(.caption)
                                .foregroundStyle(Color(.secondaryLabel))
                        }
                    }
                }
            } else {
                Text("The content of this message is no longer stored.")
                    .foregroundStyle(Color(.secondaryLabel))
            }
        } header: {
            Text("Message")
        }
    }

    private func outcomeText(_ outcome: MailOutcome) -> String {
        switch outcome {
        case .rejectedForViruses(let viruses):
            let virusText = viruses.joined(separator: ", ")
            return String(localized: "Rejected: \(virusText).")
        case .rejectedAsSpam(let score):
            let scoreText = ManagedWords.score(score)
            return String(localized: "Rejected: the spam filter scored it \(scoreText), at or above this domain’s threshold.")
        case .rejectedByDMARC(let policy):
            return String(localized: "Rejected: it failed DMARC, and the domain publishes p=\(policy).")
        case .rejectedWithErrors(let errorMessages):
            return errorMessages.joined(separator: "; ")
        case .rejected:
            return String(localized: "Rejected before it was accepted for delivery.")
        case .noDeliveries:
            return String(localized: "Accepted, but it matched no alias, so nothing was sent on.")
        case .partlyDelivered(let deliveredCount, let failedCount, let totalCount):
            return String(localized: "\(deliveredCount) delivered, \(failedCount) failed, of \(totalCount).")
        case .delivered(let deliveredCount):
            return deliveredCount == 1 ? String(localized: "Delivered.") : String(localized: "Delivered to all \(deliveredCount) recipients.")
        case .pending(let pendingCount):
            return pendingCount == 1 ? String(localized: "One delivery still waiting.") : String(localized: "\(pendingCount) deliveries still waiting.")
        }
    }

    /// When, from which address and its name, and where that is.
    private func receivedText(_ mail: AuditedMail) -> String {
        var text = ManageLabels.fullTime(mail.receivedAt) ?? ""
        if let ip = mail.ip, !ip.isEmpty {
            let timeText = text
            text = String(localized: "\(timeText) from \(ip)")
        }
        let hostName = ManagedWords.hostName(mail.reverseDnsName)
        if !hostName.isEmpty { text += " (\(hostName))" }
        if let placeText = mail.location?.placeText, !placeText.isEmpty { text += " · \(placeText)" }
        return text
    }

    /// Whether the hop into this server was encrypted, and how the sender
    /// introduced itself.
    private func connectionText(_ mail: AuditedMail) -> String {
        var text: String
        if let tlsVersion = mail.tlsVersion, !tlsVersion.isEmpty {
            text = tlsVersion
            if let cipherSuite = mail.tlsCipherSuite, !cipherSuite.isEmpty { text += " · \(cipherSuite)" }
        } else {
            text = String(localized: "not encrypted")
        }
        if let hello = mail.hello, !hello.isEmpty { text += " · HELO \(hello)" }
        return text
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            let loaded = try await connection.auditedMail(mailId: mailId)
            mailDetail = loaded
            trainingLabel = loaded.trainingLabel
            learnedTotalCount = nil
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// Marked here rather than fetched again: the score on this message is
    /// what it was when it arrived. What changes is what the next one is
    /// scored against.
    private func teach(_ chosenLabel: String?) async {
        isTeaching = true
        defer { isTeaching = false }
        do {
            let training = try await connection.teachSpamFilter(mailId: mailId, trainingLabel: chosenLabel)
            trainingLabel = training.trainingLabel.isEmpty ? nil : training.trainingLabel
            learnedTotalCount = training.learnedSpamCount + training.learnedHamCount
            doneText =
                switch chosenLabel {
                case "spam"?: String(localized: "Learned as spam.")
                case "ham"?: String(localized: "Learned as not spam.")
                default: String(localized: "Forgotten")
                }
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func preview(_ path: String, name: String) async {
        do {
            previewAddress = try await FilePreview.save(path, name: name, connection: connection)
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// AuthenticationSection is a message's checks: each one, what it decided,
/// and what it looked at to decide; then where the domains it claims
/// receive their mail.
private struct AuthenticationSection: View {
    let results: MailAuthenticationResults

    /// CheckRow is one check as a row: its name, its verdict and its
    /// detail.
    private struct CheckRow: Identifiable {
        let id: Int
        let checkName: String
        let verdictText: String
        let tone: ManagedTone
        let detailText: String
    }

    var body: some View {
        Section {
            let rows = checkRows
            if rows.isEmpty {
                Text("Nothing was checked. Outgoing mail is not authenticated against itself.")
                    .foregroundStyle(Color(.secondaryLabel))
            }
            ForEach(rows) { row in
                VStack(alignment: .leading, spacing: 3) {
                    HStack {
                        Text(verbatim: row.checkName).foregroundStyle(Color(.label))
                        Spacer()
                        ManageTag(text: row.verdictText, tone: row.tone)
                    }
                    if !row.detailText.isEmpty {
                        Text(verbatim: row.detailText)
                            .font(.caption)
                            .foregroundStyle(Color(.secondaryLabel))
                            .textSelection(.enabled)
                    }
                }
            }
            if let spamRules = results.spamFilter?.checks, !spamRules.isEmpty {
                NavigationLink {
                    SpamRulesView(spamRules: spamRules)
                } label: {
                    Text("Spam filter rules").foregroundStyle(Color(.label))
                }
            }
            if let senderMx = results.senderMx, let mailServers = senderMx.mailServers, !mailServers.isEmpty {
                mxRow(senderMx, mailServers: mailServers)
            }
            if let fromMx = results.fromMx, let mailServers = fromMx.mailServers, !mailServers.isEmpty,
                fromMx.domain != results.senderMx?.domain || (results.senderMx?.mailServers ?? []).isEmpty
            {
                mxRow(fromMx, mailServers: mailServers)
            }
        } header: {
            Text("Authentication")
        } footer: {
            if let errorMessages = results.errorMessages, !errorMessages.isEmpty {
                Label(errorMessages.joined(separator: "; "), systemImage: "exclamationmark.triangle")
                    .foregroundStyle(.orange)
            }
        }
    }

    private func mxRow(_ lookup: MailAuthenticationResults.MXLookup, mailServers: [String]) -> some View {
        let domainName = lookup.domain ?? ""
        return VStack(alignment: .leading, spacing: 2) {
            Text("Mail servers for \(domainName)")
                .font(.caption)
                .foregroundStyle(Color(.secondaryLabel))
            Text(verbatim: mailServers.joined(separator: ", "))
                .font(.callout.monospaced())
                .foregroundStyle(Color(.label))
                .textSelection(.enabled)
        }
    }

    private var checkRows: [CheckRow] {
        var rows: [CheckRow] = []
        func add(_ checkName: String, _ verdictText: String?, tone: ManagedTone, detailText: String) {
            let verdict = (verdictText ?? "").isEmpty ? String(localized: "none") : verdictText ?? ""
            rows.append(CheckRow(id: rows.count, checkName: checkName, verdictText: verdict, tone: tone, detailText: detailText))
        }
        if let spf = results.spf {
            add("SPF", spf.spfResult, tone: ManagedTone.of(spf.spfResult), detailText: spfDetail(spf))
        }
        for dkim in results.dkims ?? [] {
            var detailText = ManagedWords.dkimRecordName(selector: dkim.selector, domainName: dkim.domain)
            if let identifier = dkim.identifier, !identifier.isEmpty {
                detailText += " · " + String(localized: "signed for \(identifier)")
            }
            add("DKIM", dkim.dkimResult, tone: ManagedTone.of(dkim.dkimResult), detailText: detailText)
        }
        if let dmarc = results.dmarc {
            var parts = [dmarc.domain ?? ""]
            if let policy = dmarc.policy, !policy.isEmpty { parts.append("p=\(policy)") }
            if let subdomainPolicy = dmarc.subdomainPolicy, !subdomainPolicy.isEmpty { parts.append("sp=\(subdomainPolicy)") }
            var alignments: [String] = []
            if let dkimAlignment = dmarc.dkimAlignment, !dkimAlignment.isEmpty {
                let modeText = ManageLabels.alignmentMode(dkimAlignment)
                alignments.append(String(localized: "DKIM alignment \(modeText)"))
            }
            if let spfAlignment = dmarc.spfAlignment, !spfAlignment.isEmpty {
                let modeText = ManageLabels.alignmentMode(spfAlignment)
                alignments.append(String(localized: "SPF alignment \(modeText)"))
            }
            parts.append(alignments.joined(separator: ", "))
            add("DMARC", dmarc.dmarcResult, tone: ManagedTone.of(dmarc.dmarcResult), detailText: parts.filter { !$0.isEmpty }.joined(separator: " · "))
        }
        if let arc = results.arc {
            let instanceCount = arc.instanceCount ?? 0
            add(
                "ARC", arc.arcResult, tone: ManagedTone.of(arc.arcResult),
                detailText: instanceCount > 0 ? String(localized: "Sealed \(instanceCount) times on the way here") : String(localized: "No sealed chain"))
        }
        if let spamFilter = results.spamFilter {
            let symbols = spamFilter.symbols ?? []
            let detailText =
                (spamFilter.checks ?? []).isEmpty
                ? (symbols.isEmpty ? String(localized: "No rules matched") : symbols.joined(separator: " "))
                : ""
            add(
                String(localized: "Spam"), ManagedWords.score(spamFilter.score), tone: spamFilter.spamResult == "fail" ? .bad : .good,
                detailText: detailText)
        }
        if let antivirus = results.antivirus {
            let viruses = antivirus.viruses ?? []
            add(
                String(localized: "Virus"), viruses.isEmpty ? String(localized: "clean") : String(localized: "infected"),
                tone: viruses.isEmpty ? .good : .bad, detailText: viruses.joined(separator: ", "))
        }
        if let unsafeExtensions = results.contentFilter?.unsafeExtensions, !unsafeExtensions.isEmpty {
            add(String(localized: "Attachments"), String(localized: "unsafe"), tone: .bad, detailText: unsafeExtensions.joined(separator: ", "))
        }
        return rows
    }

    private func spfDetail(_ spf: MailAuthenticationResults.SPFCheck) -> String {
        let domainName = spf.domain ?? ""
        let ip = spf.ip ?? ""
        switch (spf.spfResult ?? "").lowercased() {
        case "pass": return String(localized: "\(domainName) authorizes \(ip) to send for it")
        case "fail": return String(localized: "\(domainName) does not authorize \(ip) to send for it")
        case "softfail": return String(localized: "\(domainName) does not list \(ip), but asks that mail from it be accepted anyway")
        case "neutral": return String(localized: "\(domainName) says nothing either way about \(ip)")
        case "none": return String(localized: "\(domainName) publishes no SPF record, so \(ip) could not be checked")
        case "temperror", "permerror": return String(localized: "\(domainName)’s SPF record could not be read, so \(ip) could not be checked")
        default: return String(localized: "\(domainName) was asked about \(ip)")
        }
    }
}

/// SpamRulesView is the built-in filter's rules that matched a message, and
/// the points each added.
private struct SpamRulesView: View {
    let spamRules: [MailAuthenticationResults.SpamRule]

    var body: some View {
        List {
            ForEach(Array(spamRules.enumerated()), id: \.offset) { _, spamRule in
                VStack(alignment: .leading, spacing: 2) {
                    HStack {
                        Text(verbatim: spamRule.symbol)
                            .font(.callout.monospaced())
                            .foregroundStyle(Color(.label))
                        Spacer()
                        Text(verbatim: spamRule.score > 0 ? "+\(ManagedWords.score(spamRule.score))" : ManagedWords.score(spamRule.score))
                            .font(.callout.monospacedDigit())
                            .foregroundStyle(spamRule.score > 0 ? Color.orange : Color(.secondaryLabel))
                    }
                    if let ruleDescription = spamRule.ruleDescription, !ruleDescription.isEmpty {
                        Text(verbatim: ruleDescription)
                            .font(.caption)
                            .foregroundStyle(Color(.secondaryLabel))
                    }
                }
            }
        }
        .navigationTitle("Spam filter rules")
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// DeliveryRow is a delivery in a message's list: its status, who it was
/// for and its kind.
private struct DeliveryRow: View {
    let delivery: MailDelivery

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: delivery.recipient ?? "")
                .foregroundStyle(Color(.label))
                .lineLimit(1)
            HStack(spacing: 8) {
                ManageTag(text: ManageLabels.mailStatus(delivery.deliveryStatus), tone: ManagedTone.of(delivery.deliveryStatus))
                if let kind = delivery.deliveryKind, !kind.isEmpty {
                    Text(verbatim: ManageLabels.mailKind(kind))
                        .font(.caption)
                        .foregroundStyle(Color(.secondaryLabel))
                }
            }
        }
    }
}

/// DeliveryDetailView is one delivery of a message: how it is handed on and
/// where, its attempts, and what the far end said.
private struct DeliveryDetailView: View {
    let delivery: MailDelivery

    var body: some View {
        List {
            Section {
                DeliveryRow(delivery: delivery)
                if let methodText {
                    Text(verbatim: methodText)
                        .font(.callout)
                        .foregroundStyle(Color(.secondaryLabel))
                }
            }
            Section {
                DetailRow(label: "Attempts", fieldText: attemptsText)
                DetailRow(label: "Delivered", fieldText: ManageLabels.fullTime(delivery.deliveredAt))
                DetailRow(label: "Dropped", fieldText: ManageLabels.fullTime(delivery.droppedAt))
                DetailRow(label: "Next attempt", fieldText: ManageLabels.fullTime(delivery.retryAt))
                DetailRow(label: "Sender notified", fieldText: ManageLabels.fullTime(delivery.notifiedAt))
                DetailRow(label: "Size", fieldText: ManageLabels.byteText(delivery.byteCount))
                DetailRow(label: "Last error", fieldText: delivery.errorMessage)
            }
            let recipientReports = delivery.recipientReports
            if !recipientReports.isEmpty {
                Section {
                    ForEach(Array(recipientReports.enumerated()), id: \.offset) { _, entry in
                        VStack(alignment: .leading, spacing: 2) {
                            if let reportStatus = entry.report.reportStatus, !reportStatus.isEmpty {
                                Text("The far end said \(reportStatus)")
                                    .font(.caption)
                                    .foregroundStyle(Color(.secondaryLabel))
                            }
                            Text(verbatim: [entry.report.remoteMta ?? "", entry.report.reportAction ?? ""].filter { !$0.isEmpty }.joined(separator: " · "))
                                .font(.callout.monospaced())
                                .foregroundStyle(Color(.label))
                            if let diagnosticCode = entry.report.diagnosticCode, !diagnosticCode.isEmpty {
                                Text(verbatim: diagnosticCode)
                                    .font(.caption.monospaced())
                                    .foregroundStyle(Color(.secondaryLabel))
                                    .textSelection(.enabled)
                            }
                        }
                    }
                } header: {
                    Text("The far end said")
                }
            }
        }
        .navigationTitle(delivery.recipient ?? String(localized: "Delivery"))
        .navigationBarTitleDisplayMode(.inline)
    }

    /// How it is handed on, and where: the thing to check when a delivery
    /// is stuck.
    private var methodText: String? {
        guard let destination = delivery.destination, !destination.isEmpty, let method = delivery.deliveryMethod, !method.isEmpty else { return nil }
        switch method {
        case "email": return String(localized: "Forwarded to \(destination) by looking up its mail servers")
        case "mailServer": return String(localized: "Relayed to the mail server at \(destination)")
        case "webhook": return String(localized: "Posted to \(destination)")
        default: return String(localized: "Sent to \(destination) by looking up its mail servers")
        }
    }

    private var attemptsText: String {
        guard let attemptCount = delivery.attemptCount, attemptCount > 0 else { return String(localized: "Not attempted yet") }
        let timeText = ManageLabels.fullTime(delivery.attemptedAt) ?? ""
        return String(localized: "\(attemptCount), last at \(timeText)")
    }
}

/// RenderedMailView is a message as sent, in a web view with scripts off,
/// with its pictures held back until asked for. Nothing is recorded: an
/// operator looking at somebody's mail is not choosing for them.
private struct RenderedMailView: View {
    let content: MailContent
    @State private var isShowingPictures = false
    @State private var isOriginalLook = false
    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                if content.hasRemoteContent == true && content.imagesAllowed != true && !isShowingPictures {
                    VStack(alignment: .leading, spacing: 6) {
                        Label(
                            "Remote images are blocked. Loading them tells the sender that this message was opened, and from which address.",
                            systemImage: "photo.badge.exclamationmark"
                        )
                        .font(.footnote)
                        Button("Load them anyway") { isShowingPictures = true }
                            .font(.footnote)
                    }
                    .padding(.horizontal)
                }
                MailBody(
                    html: content.html, mailId: content.mailId, isShowingPictures: isShowingPictures || content.imagesAllowed == true,
                    isOriginalLook: isOriginalLook)
            }
        }
        .navigationTitle("Rendered")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if colorScheme == .dark {
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        isOriginalLook.toggle()
                    } label: {
                        Label(
                            isOriginalLook ? LocalizedStringKey("Show in dark theme") : "Show in original theme",
                            systemImage: "circle.lefthalf.filled")
                    }
                }
            }
        }
    }
}

/// MailSourceView is a part of a message as text: its plain text, its
/// cleaned HTML or its headers as they arrived.
private struct MailSourceView: View {
    let title: String
    let sourceText: String
    let isMonospaced: Bool

    var body: some View {
        ScrollView {
            Text(verbatim: sourceText)
                .font(isMonospaced ? .caption.monospaced() : .body)
                .foregroundStyle(Color(.label))
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding()
        }
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    UIPasteboard.general.string = sourceText
                } label: {
                    Label("Copy", systemImage: "doc.on.doc")
                }
            }
        }
    }
}

/// MailHeadersView is a message's headers, a name and its value to a row.
private struct MailHeadersView: View {
    let headers: [MailContent.Header]

    var body: some View {
        List {
            ForEach(Array(headers.enumerated()), id: \.offset) { _, header in
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: header.key)
                        .font(.caption)
                        .foregroundStyle(Color(.secondaryLabel))
                    Text(verbatim: header.value)
                        .font(.caption.monospaced())
                        .foregroundStyle(Color(.label))
                        .textSelection(.enabled)
                }
            }
        }
        .navigationTitle("Headers")
        .navigationBarTitleDisplayMode(.inline)
    }
}

// MARK: - The queue

/// DeliveryQueueView is the dashboard's Queue page: every delivery still
/// waiting, for every domain, and why, each tried again with a swipe.
/// Delivered and dropped ones are on the message they belong to.
struct DeliveryQueueView: View {
    let connection: Connection
    @State private var deliveries: [MailDelivery]?
    @State private var domains: [ManagedDomain] = []
    @State private var filter: DeliveryQueueFilter
    @State private var shownCount = ManagedWords.pageSize
    @State private var isLoading = true
    @State private var isBusy = false
    @State private var doneText: String?
    @State private var problemText: String?

    /// A domain to start narrowed to, as the domain page links here.
    init(connection: Connection, domainId: String? = nil) {
        self.connection = connection
        _filter = State(initialValue: DeliveryQueueFilter(domainId: domainId))
    }

    private var filtered: [MailDelivery] { filter.applied(to: deliveries ?? []) }

    var body: some View {
        List {
            if let deliveries {
                Section {
                    ForEach(filtered.prefix(shownCount)) { delivery in
                        Group {
                            if let mailId = delivery.mailId, !mailId.isEmpty {
                                NavigationLink {
                                    AuditedMailView(connection: connection, mailId: mailId)
                                } label: {
                                    row(delivery)
                                }
                            } else {
                                row(delivery)
                            }
                        }
                        .swipeActions {
                            Button("Retry now", systemImage: "arrow.clockwise") {
                                Task { await retry(delivery) }
                            }
                            .tint(.indigo)
                        }
                    }
                    ListPaging(shownCount: $shownCount, totalCount: filtered.count)
                } footer: {
                    if !deliveries.isEmpty { Text(verbatim: countText(shownTotal: filtered.count, total: deliveries.count)) }
                }
            }
        }
        .navigationTitle("Queue")
        .searchable(text: $filter.searchText, prompt: Text("Recipient or error"))
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                FilterMenu(isFiltering: filter.domainId != nil || filter.deliveryStatus != nil || filter.deliveryKind != nil, pickers: pickers)
            }
        }
        .overlay {
            if deliveries == nil && isLoading {
                ProgressView()
            } else if let deliveries, deliveries.isEmpty {
                ContentUnavailableView(
                    "Nothing waiting", systemImage: "checkmark.circle",
                    description: Text("Every delivery has either succeeded or been given up on."))
            } else if let deliveries, !deliveries.isEmpty, filtered.isEmpty {
                ContentUnavailableView.search
            }
        }
        .disabled(isBusy)
        .onChange(of: filter) { shownCount = ManagedWords.pageSize }
        .task { await load() }
        .refreshable { await load() }
        .toast(done: $doneText, problem: $problemText)
    }

    private var pickers: [FilterPicker] {
        [
            FilterPicker(
                title: String(localized: "Domain"), allTitle: String(localized: "All domains"),
                choices: domains.map { FilterChoice(filterValue: $0.id, title: $0.domainName) }, selection: $filter.domainId),
            FilterPicker(
                title: String(localized: "Status"), allTitle: String(localized: "Any status"),
                choices: Set((deliveries ?? []).compactMap(\.deliveryStatus)).sorted().map {
                    FilterChoice(filterValue: $0, title: ManageLabels.mailStatus($0))
                },
                selection: $filter.deliveryStatus),
            FilterPicker(
                title: String(localized: "Kind"), allTitle: String(localized: "Any kind"),
                choices: Set((deliveries ?? []).compactMap(\.deliveryKind)).sorted().map {
                    FilterChoice(filterValue: $0, title: ManageLabels.mailKind($0))
                },
                selection: $filter.deliveryKind),
        ]
    }

    private func row(_ delivery: MailDelivery) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: delivery.recipient ?? "")
                    .font(.subheadline)
                    .foregroundStyle(Color(.label))
                    .lineLimit(1)
                Spacer()
                if let retryText = ManageLabels.relativeTime(delivery.retryAt) {
                    Text(verbatim: retryText)
                        .font(.caption)
                        .foregroundStyle(Color(.secondaryLabel))
                        .accessibilityLabel(Text("Next attempt \(retryText)"))
                }
            }
            HStack(spacing: 8) {
                ManageTag(text: ManageLabels.mailStatus(delivery.deliveryStatus), tone: ManagedTone.of(delivery.deliveryStatus))
                Text(verbatim: details(delivery))
                    .font(.caption)
                    .foregroundStyle(Color(.secondaryLabel))
                    .lineLimit(1)
            }
            if let errorMessage = delivery.errorMessage, !errorMessage.isEmpty {
                Text(verbatim: errorMessage)
                    .font(.caption)
                    .foregroundStyle(Color(.secondaryLabel))
                    .lineLimit(2)
            }
        }
    }

    /// The domain, the kind and how many attempts.
    private func details(_ delivery: MailDelivery) -> String {
        let attemptCount = delivery.attemptCount ?? 0
        return [
            ManageLabels.domainName(delivery.domainId, in: domains), ManageLabels.mailKind(delivery.deliveryKind),
            attemptCount == 1 ? String(localized: "1 attempt") : String(localized: "\(attemptCount) attempts"),
        ]
        .filter { !$0.isEmpty }
        .joined(separator: " · ")
    }

    private func countText(shownTotal: Int, total: Int) -> String {
        if filter.isFiltering {
            return shownTotal == 1 ? String(localized: "1 of \(total) deliveries") : String(localized: "\(shownTotal) of \(total) deliveries")
        }
        return total == 1 ? String(localized: "1 delivery waiting") : String(localized: "\(total) deliveries waiting")
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            async let namesLoading = connection.managedDomainNames()
            async let deliveriesLoading = connection.pendingDeliveries()
            let (loadedDomains, loadedDeliveries) = try await (namesLoading, deliveriesLoading)
            domains = loadedDomains
            deliveries = loadedDeliveries
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func retry(_ delivery: MailDelivery) async {
        isBusy = true
        defer { isBusy = false }
        do {
            try await connection.retryDelivery(deliveryId: delivery.id)
            doneText = String(localized: "Trying again now")
        } catch {
            problemText = error.localizedDescription
        }
        await load()
    }
}

// MARK: - Reports

/// ReportsView is the dashboard's Reports page: DMARC aggregate reports,
/// which is other people telling you what they did with mail that claimed
/// to be from your domains. The question each row answers is who is sending
/// as you, and whether anyone believes them.
struct ReportsView: View {
    let connection: Connection
    @State private var reports: [DMARCReport]?
    @State private var domains: [ManagedDomain] = []
    @State private var filter = ReportListFilter()
    @State private var shownCount = ManagedWords.pageSize
    @State private var isLoading = true
    @State private var problemText: String?

    private var filtered: [DMARCReport] { filter.applied(to: reports ?? []) }

    var body: some View {
        List {
            if let reports, !reports.isEmpty {
                Section {
                    ForEach(filtered.prefix(shownCount)) { report in
                        NavigationLink {
                            ReportDetailView(connection: connection, reportId: report.id)
                        } label: {
                            row(report)
                        }
                    }
                    ListPaging(shownCount: $shownCount, totalCount: filtered.count)
                } footer: {
                    Text(verbatim: countText(shownTotal: filtered.count, total: reports.count))
                }
            }
        }
        .navigationTitle("Reports")
        .searchable(text: $filter.searchText, prompt: Text("Domain or sender"))
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                FilterMenu(isFiltering: filter.domainId != nil || filter.alignment != nil || filter.disposition != nil, pickers: pickers)
            }
        }
        .overlay {
            if reports == nil && isLoading {
                ProgressView()
            } else if let reports, reports.isEmpty {
                ContentUnavailableView(
                    "No reports yet", systemImage: "checkmark.shield",
                    description: Text(
                        "No aggregate reports have arrived yet. They come from receiving servers that see mail claiming to be from your domains, and only if a domain publishes a DMARC record asking for them. Expect the first within a day or two of publishing one."
                    ))
            } else if let reports, !reports.isEmpty, filtered.isEmpty {
                ContentUnavailableView.search
            }
        }
        .onChange(of: filter) { shownCount = ManagedWords.pageSize }
        .task { await load() }
        .refreshable { await load() }
        .toast(done: .constant(nil), problem: $problemText)
    }

    private var pickers: [FilterPicker] {
        [
            FilterPicker(
                title: String(localized: "Domain"), allTitle: String(localized: "All domains"),
                choices: domains.map { FilterChoice(filterValue: $0.id, title: $0.domainName) }, selection: $filter.domainId),
            FilterPicker(
                title: String(localized: "Authentication"), allTitle: String(localized: "Any authentication"),
                choices: ReportAlignment.allCases.map { FilterChoice(filterValue: $0.rawValue, title: ManageLabels.alignment($0)) },
                selection: Binding(get: { filter.alignment?.rawValue }, set: { filter.alignment = $0.flatMap(ReportAlignment.init(rawValue:)) })),
            FilterPicker(
                title: String(localized: "What they did"), allTitle: String(localized: "Anything they did"),
                choices: Set((reports ?? []).map { $0.disposition ?? "" }).sorted().map {
                    FilterChoice(filterValue: $0, title: $0.isEmpty ? String(localized: "none") : $0)
                },
                selection: $filter.disposition),
        ]
    }

    private func row(_ report: DMARCReport) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: report.fromDomain ?? String(localized: "unknown domain"))
                    .font(.subheadline)
                    .foregroundStyle(Color(.label))
                    .lineLimit(1)
                Spacer()
                if let periodText = ManageLabels.relativeTime(report.endAt ?? report.beginAt) {
                    Text(verbatim: periodText)
                        .font(.caption)
                        .foregroundStyle(Color(.secondaryLabel))
                }
            }
            // The address is what identifies the sender; the name it claims
            // is context, and often absent.
            Text(verbatim: [report.ip ?? "", ManagedWords.hostName(report.reverseDnsName)].filter { !$0.isEmpty }.joined(separator: " · "))
                .font(.caption.monospaced())
                .foregroundStyle(Color(.secondaryLabel))
                .lineLimit(1)
            HStack(spacing: 8) {
                ManageTag(text: ManageLabels.alignment(report.alignment), tone: report.alignment.isPassed ? .good : .bad)
                ManageTag(text: report.disposition ?? String(localized: "none"), tone: ManagedTone.of(report.disposition))
                Text(verbatim: messageCountText(report.messageCount ?? 0))
                    .font(.caption)
                    .foregroundStyle(Color(.secondaryLabel))
            }
            Text(verbatim: ManageLabels.domainName(report.domainId, in: domains))
                .font(.caption)
                .foregroundStyle(Color(.secondaryLabel))
        }
    }

    private func messageCountText(_ messageCount: Int) -> String {
        messageCount == 1 ? String(localized: "1 message") : String(localized: "\(messageCount) messages")
    }

    private func countText(shownTotal: Int, total: Int) -> String {
        if filter.isFiltering {
            return shownTotal == 1 ? String(localized: "1 of \(total) reports") : String(localized: "\(shownTotal) of \(total) reports")
        }
        return total == 1 ? String(localized: "1 report") : String(localized: "\(total) reports")
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            async let namesLoading = connection.managedDomainNames()
            async let reportsLoading = connection.dmarcReports()
            let (loadedDomains, loadedReports) = try await (namesLoading, reportsLoading)
            domains = loadedDomains
            reports = loadedReports
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// ReportDetailView is one aggregate report, opened out: who reported it,
/// the policy they saw, and what they did with each batch of mail.
struct ReportDetailView: View {
    let connection: Connection
    let reportId: String
    @State private var report: DMARCReport?
    @State private var isLoading = true
    @State private var problemText: String?

    var body: some View {
        List {
            if let report {
                Section {
                    HStack(spacing: 10) {
                        ManageTag(
                            text: report.alignment.isPassed ? String(localized: "One aligned") : String(localized: "Neither aligned"),
                            tone: report.alignment.isPassed ? .good : .bad)
                        ManageTag(
                            text: (report.disposition ?? "").isEmpty ? String(localized: "none") : report.disposition ?? "",
                            tone: ManagedTone.of(report.disposition))
                    }
                    Text(verbatim: summaryText(report))
                        .font(.callout)
                        .foregroundStyle(Color(.label))
                }
                let feedback = report.feedback
                Section {
                    DetailRow(label: "Reporter", fieldText: reporterText(feedback))
                    DetailRow(label: "Report ID", fieldText: feedback?.reportId, isMonospaced: true)
                    DetailRow(label: "Covering", fieldText: periodText(report))
                    DetailRow(label: "Sent by", fieldText: sourceText(report), isMonospaced: true)
                } header: {
                    Text("Who reported this")
                }
                if let feedback {
                    Section {
                        DetailRow(label: "Published at", fieldText: feedback.domain, isMonospaced: true)
                        DetailRow(label: "Policy", fieldText: feedback.policyText)
                        DetailRow(label: "Alignment", fieldText: alignmentText(feedback))
                    } header: {
                        Text("The policy they saw")
                    } footer: {
                        if let errorMessages = feedback.errorMessages, !errorMessages.isEmpty {
                            Label(errorMessages.joined(separator: "; "), systemImage: "exclamationmark.triangle")
                                .foregroundStyle(.orange)
                        }
                    }
                }
                Section {
                    let records = feedback?.records ?? []
                    if records.isEmpty {
                        Text("This report carried no per-source rows, which usually means the original XML could not be parsed.")
                            .foregroundStyle(Color(.secondaryLabel))
                    }
                    ForEach(Array(records.enumerated()), id: \.offset) { _, record in
                        NavigationLink {
                            FeedbackRecordView(record: record)
                        } label: {
                            FeedbackRecordRow(record: record)
                        }
                    }
                } header: {
                    Text("What they did with it")
                }
            }
        }
        .navigationTitle(report.map { $0.fromDomain ?? String(localized: "unknown domain") } ?? "")
        .navigationBarTitleDisplayMode(.inline)
        .overlay {
            if report == nil && isLoading {
                ProgressView()
            } else if report == nil {
                ContentUnavailableView("Not found", systemImage: "questionmark.folder")
            }
        }
        .task { await load() }
        .refreshable { await load() }
        .toast(done: .constant(nil), problem: $problemText)
    }

    private func summaryText(_ report: DMARCReport) -> String {
        let fromDomain = report.fromDomain ?? ""
        let ip = report.ip ?? ""
        let messageCount = report.messageCount ?? 0
        if messageCount == 1 { return String(localized: "One message claiming to be from \(fromDomain), sent by \(ip).") }
        return String(localized: "\(messageCount) messages claiming to be from \(fromDomain), sent by \(ip).")
    }

    private func reporterText(_ feedback: DMARCReport.Feedback?) -> String {
        [feedback?.organizationName ?? "", feedback?.email ?? "", feedback?.extraContactInfo ?? ""].filter { !$0.isEmpty }.joined(separator: "\n")
    }

    private func periodText(_ report: DMARCReport) -> String {
        let fromText = ManageLabels.fullTime(report.beginAt) ?? ""
        let toText = ManageLabels.fullTime(report.endAt) ?? ""
        return String(localized: "\(fromText) to \(toText)")
    }

    private func sourceText(_ report: DMARCReport) -> String {
        [report.ip ?? "", ManagedWords.hostName(report.reverseDnsName), report.location?.placeText ?? ""].filter { !$0.isEmpty }.joined(separator: "\n")
    }

    private func alignmentText(_ feedback: DMARCReport.Feedback) -> String {
        let dkimText = ManageLabels.alignmentMode(feedback.dkimAlignment)
        let spfText = ManageLabels.alignmentMode(feedback.spfAlignment)
        return String(localized: "DKIM alignment \(dkimText)") + ", " + String(localized: "SPF alignment \(spfText)")
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            report = try await connection.dmarcReport(reportId: reportId)
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// FeedbackRecordRow is a batch of messages in a report's list: how many,
/// from where, and what the receiver did.
private struct FeedbackRecordRow: View {
    let record: DMARCReport.FeedbackRecord

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: record.sourceIp ?? "")
                .font(.callout.monospaced())
                .foregroundStyle(Color(.label))
            HStack(spacing: 8) {
                let messageCount = record.messageCount ?? 0
                ManageTag(
                    text: messageCount == 1 ? String(localized: "1 message") : String(localized: "\(messageCount) messages"),
                    tone: record.isPassed ? .good : .bad)
                ManageTag(text: (record.disposition ?? "").isEmpty ? "none" : record.disposition ?? "", tone: ManagedTone.of(record.disposition))
            }
        }
    }
}

/// FeedbackRecordView is one batch of a report: what was evaluated, why the
/// receiver overrode the policy when it did, and the addresses and
/// signatures it saw.
private struct FeedbackRecordView: View {
    let record: DMARCReport.FeedbackRecord

    var body: some View {
        List {
            Section {
                FeedbackRecordRow(record: record)
            }
            Section {
                DetailRow(label: "Evaluated as", fieldText: evaluatedText)
                // A forwarder and a mailing list both break alignment
                // legitimately, and this is the only place that is said.
                DetailRow(label: "Overridden because", fieldText: overrideText)
                DetailRow(label: "From header", fieldText: record.headerFrom, isMonospaced: true)
                DetailRow(label: "Envelope sender", fieldText: record.envelopeFrom, isMonospaced: true)
                DetailRow(label: "Envelope recipient", fieldText: record.envelopeTo, isMonospaced: true)
            }
            let dkimResults = record.dkimResults ?? []
            let spfResults = record.spfResults ?? []
            if !dkimResults.isEmpty || !spfResults.isEmpty {
                Section {
                    ForEach(Array(dkimResults.enumerated()), id: \.offset) { _, dkim in
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: "DKIM \(dkim.dkimResult ?? "")")
                                .font(.caption)
                                .foregroundStyle(Color(.secondaryLabel))
                            Text(verbatim: ManagedWords.dkimRecordName(selector: dkim.selector, domainName: dkim.domain))
                                .font(.callout.monospaced())
                                .foregroundStyle(Color(.label))
                            if let humanResult = dkim.humanResult, !humanResult.isEmpty {
                                Text(verbatim: humanResult)
                                    .font(.caption)
                                    .foregroundStyle(Color(.secondaryLabel))
                            }
                        }
                    }
                    ForEach(Array(spfResults.enumerated()), id: \.offset) { _, spf in
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: "SPF \(spf.spfResult ?? "")")
                                .font(.caption)
                                .foregroundStyle(Color(.secondaryLabel))
                            Text(verbatim: [spf.domain ?? "", (spf.scope ?? "").isEmpty ? "" : "(\(spf.scope ?? ""))"].filter { !$0.isEmpty }.joined(separator: " "))
                                .font(.callout.monospaced())
                                .foregroundStyle(Color(.label))
                        }
                    }
                }
            }
        }
        .navigationTitle(record.sourceIp ?? "")
        .navigationBarTitleDisplayMode(.inline)
    }

    private var evaluatedText: String {
        [
            (record.dkimEvaluation ?? "").isEmpty ? "" : "DKIM \(record.dkimEvaluation ?? "")",
            (record.spfEvaluation ?? "").isEmpty ? "" : "SPF \(record.spfEvaluation ?? "")",
        ]
        .filter { !$0.isEmpty }
        .joined(separator: " · ")
    }

    private var overrideText: String? {
        guard let reasonType = record.reasonType, !reasonType.isEmpty else { return nil }
        guard let reasonComment = record.reasonComment, !reasonComment.isEmpty else { return reasonType }
        return "\(reasonType): \(reasonComment)"
    }
}

// MARK: - Domains

/// DomainsView is the dashboard's Domains page: every domain, its DNS in
/// one word, and how many aliases and credentials it has. A new one is
/// added from the toolbar; a domain opens on its own screen.
struct DomainsView: View {
    let connection: Connection
    @State private var domains: [ManagedDomain]?
    @State private var searchText = ""
    @State private var isLoading = true
    @State private var isAdding = false
    @State private var newDomainName = ""
    @State private var isBusy = false
    @State private var doneText: String?
    @State private var problemText: String?

    private var filtered: [ManagedDomain] {
        (domains ?? []).filter { ManagedWords.matches(searchText, in: [$0.domainName, $0.comment]) }
    }

    var body: some View {
        List {
            if let domains, !domains.isEmpty {
                Section {
                    ForEach(filtered) { domain in
                        NavigationLink {
                            DomainView(connection: connection, domainId: domain.id)
                        } label: {
                            row(domain)
                        }
                    }
                } footer: {
                    Text(verbatim: domains.count == 1 ? String(localized: "1 domain") : String(localized: "\(domains.count) domains"))
                }
            }
        }
        .navigationTitle("Domains")
        .searchable(text: $searchText, prompt: Text("Domain"))
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    newDomainName = ""
                    isAdding = true
                } label: {
                    Label("New domain", systemImage: "plus")
                }
            }
        }
        .overlay {
            if domains == nil && isLoading {
                ProgressView()
            } else if let domains, domains.isEmpty {
                ContentUnavailableView(
                    "No domains yet", systemImage: "globe",
                    description: Text("Add the first one and this server will start accepting its mail."))
            } else if let domains, !domains.isEmpty, filtered.isEmpty {
                ContentUnavailableView.search
            }
        }
        .disabled(isBusy)
        .alert("New domain", isPresented: $isAdding) {
            TextField("example.com" as String, text: $newDomainName)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .keyboardType(.URL)
            Button("Cancel", role: .cancel) {}
            Button("Add") {
                let domainName = newDomainName.trimmed
                Task { await add(domainName) }
            }
            .disabled(newDomainName.trimmed.isEmpty)
        } message: {
            Text("Add a domain")
        }
        .task { await load() }
        .refreshable { await load() }
        .toast(done: $doneText, problem: $problemText)
    }

    private func row(_ domain: ManagedDomain) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: domain.domainName)
                .foregroundStyle(Color(.label))
            if let comment = domain.comment, !comment.isEmpty {
                Text(verbatim: comment)
                    .font(.caption)
                    .foregroundStyle(Color(.secondaryLabel))
            }
            HStack(spacing: 8) {
                DNSSummaryTag(summary: domain.dnsSummary)
                Text(verbatim: countsText(domain))
                    .font(.caption)
                    .foregroundStyle(Color(.secondaryLabel))
            }
        }
    }

    private func countsText(_ domain: ManagedDomain) -> String {
        let aliasCount = domain.aliases?.count ?? 0
        let credentialCount = domain.credentials?.count ?? 0
        let aliasText = aliasCount == 1 ? String(localized: "1 alias") : String(localized: "\(aliasCount) aliases")
        let credentialText = credentialCount == 1 ? String(localized: "1 credential") : String(localized: "\(credentialCount) credentials")
        return "\(aliasText) · \(credentialText)"
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            domains = try await connection.managedDomains()
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func add(_ domainName: String) async {
        guard !domainName.isEmpty else { return }
        isBusy = true
        defer { isBusy = false }
        do {
            try await connection.createManagedDomain(domainName: domainName)
            doneText = String(localized: "Added \(domainName)")
        } catch {
            problemText = error.localizedDescription
        }
        await load()
    }
}

/// DNSSummaryTag is a domain's DNS in one word: not checked yet, all
/// published, or how many records are missing.
private struct DNSSummaryTag: View {
    let summary: DomainDNSSummary

    var body: some View {
        switch summary {
        case .notChecked: ManageTag(text: String(localized: "Not checked yet"), tone: .plain)
        case .allPublished: ManageTag(text: String(localized: "All published"), tone: .good)
        case .missing(let missingCount): ManageTag(text: String(localized: "\(missingCount) missing"), tone: .warn)
        }
    }
}

/// ManagedDomainModel is one domain as its screens share it: read once,
/// read again after every change, and the one place a change's outcome is
/// said.
@Observable
@MainActor
private final class ManagedDomainModel {
    let connection: Connection
    let domainId: String
    var domain: ManagedDomain?
    var overview: DomainOverview?
    var isLoading = true
    var isBusy = false
    var doneText: String?
    var problemText: String?

    init(connection: Connection, domainId: String) {
        self.connection = connection
        self.domainId = domainId
    }

    func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            async let domainLoading = connection.managedDomain(domainId: domainId)
            async let overviewLoading = connection.domainOverview(domainId: domainId)
            let (loadedDomain, loadedOverview) = try await (domainLoading, overviewLoading)
            domain = loadedDomain
            overview = loadedOverview
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// Does one change, says so, and reads the domain again.
    @discardableResult
    func act(saying said: String, _ work: () async throws -> Void) async -> Bool {
        isBusy = true
        defer { isBusy = false }
        var isDone = false
        do {
            try await work()
            doneText = said
            isDone = true
        } catch {
            problemText = error.localizedDescription
        }
        await load()
        return isDone
    }
}

/// DomainView is one domain, as the dashboard's tabs show it: at a glance
/// first (its DNS, its mail and its queue), then its settings, aliases,
/// credentials and templates, and the way to its mail and its queue.
struct DomainView: View {
    @State private var model: ManagedDomainModel

    init(connection: Connection, domainId: String) {
        _model = State(initialValue: ManagedDomainModel(connection: connection, domainId: domainId))
    }

    var body: some View {
        List {
            if let domain = model.domain {
                overviewSection(domain)
                Section {
                    NavigationLink {
                        DomainSettingsView(model: model)
                    } label: {
                        Label("Settings", systemImage: "gearshape")
                    }
                    NavigationLink {
                        DomainAliasesView(model: model)
                    } label: {
                        LabeledContent {
                            Text(domain.aliases?.count ?? 0, format: .number)
                        } label: {
                            Label("Aliases", systemImage: "at")
                        }
                    }
                    NavigationLink {
                        DomainCredentialsView(model: model)
                    } label: {
                        LabeledContent {
                            Text(domain.credentials?.count ?? 0, format: .number)
                        } label: {
                            Label("Credentials", systemImage: "key")
                        }
                    }
                    NavigationLink {
                        DomainTemplatesView(connection: model.connection, domainId: domain.id)
                    } label: {
                        Label("Templates", systemImage: "doc.richtext")
                    }
                }
            }
        }
        .navigationTitle(model.domain?.domainName ?? "")
        .overlay {
            if model.domain == nil && model.isLoading {
                ProgressView()
            } else if model.domain == nil {
                ContentUnavailableView("Not found", systemImage: "questionmark.folder")
            }
        }
        .disabled(model.isBusy)
        .task { await model.load() }
        .refreshable { await model.load() }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    /// Every figure here is a number, and each leads to the list behind it,
    /// narrowed to this domain.
    @ViewBuilder
    private func overviewSection(_ domain: ManagedDomain) -> some View {
        Section {
            NavigationLink {
                DomainSettingsView(model: model)
            } label: {
                OverviewRow(title: String(localized: "DNS records"), figureText: dnsFigure(domain), detailText: dnsDetail(domain), isWarning: isMissing(domain))
            }
            if let overview = model.overview {
                NavigationLink {
                    AllMailView(connection: model.connection, domainId: domain.id)
                } label: {
                    OverviewRow(
                        title: String(localized: "Messages"), figureText: overview.totalMailCount.formatted(),
                        detailText: ManageLabels.relativeTime(overview.newestReceivedAt).map { String(localized: "Last received \($0)") }
                            ?? String(localized: "Nothing received yet"))
                }
                NavigationLink {
                    AllMailView(connection: model.connection, domainId: domain.id, mailStatus: "accepted")
                } label: {
                    OverviewRow(title: String(localized: "Accepted"), figureText: overview.acceptedMailCount.formatted(), detailText: String(localized: "Delivered or forwarded"))
                }
                NavigationLink {
                    AllMailView(connection: model.connection, domainId: domain.id, mailStatus: "rejected")
                } label: {
                    OverviewRow(title: String(localized: "Rejected"), figureText: overview.rejectedMailCount.formatted(), detailText: String(localized: "Refused on arrival"))
                }
                NavigationLink {
                    DeliveryQueueView(connection: model.connection, domainId: domain.id)
                } label: {
                    OverviewRow(
                        title: String(localized: "In queue"), figureText: overview.pendingDeliveries.count.formatted(), detailText: queueDetail(overview),
                        isWarning: overview.failingDeliveryCount > 0)
                }
            }
        } header: {
            Text("Overview")
        }
    }

    private func isMissing(_ domain: ManagedDomain) -> Bool {
        if case .missing = domain.dnsSummary { return true }
        return false
    }

    /// Nothing to count until the first check has run, so a question mark
    /// rather than a zero.
    private func dnsFigure(_ domain: ManagedDomain) -> String {
        switch domain.dnsSummary {
        case .notChecked: "?"
        case .allPublished(let recordCount): recordCount.formatted()
        case .missing(let missingCount): missingCount.formatted()
        }
    }

    private func dnsDetail(_ domain: ManagedDomain) -> String {
        guard let checkedText = ManageLabels.relativeTime(domain.dnsCheck?.checkedAt) else { return String(localized: "Not checked yet") }
        let checkedLine = String(localized: "Checked \(checkedText)")
        switch domain.dnsSummary {
        case .notChecked: return checkedLine
        case .allPublished: return String(localized: "All published") + " · " + checkedLine
        case .missing: return String(localized: "Need changing") + " · " + checkedLine
        }
    }

    /// Queued is worth noticing only once something has failed at least
    /// once; a delivery on its first attempt is the system working.
    private func queueDetail(_ overview: DomainOverview) -> String {
        if overview.pendingDeliveries.isEmpty { return String(localized: "Nothing waiting") }
        if overview.failingDeliveryCount > 0 { return String(localized: "\(overview.failingDeliveryCount) waiting on a retry") }
        return String(localized: "Waiting on a first attempt")
    }
}

/// OverviewRow is one figure of a domain's overview: what it counts, the
/// number, and a line about it.
private struct OverviewRow: View {
    let title: String
    let figureText: String
    let detailText: String
    var isWarning = false

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                Text(verbatim: title).foregroundStyle(Color(.label))
                Spacer()
                if isWarning {
                    Image(systemName: "exclamationmark.triangle")
                        .foregroundStyle(.orange)
                        .accessibilityLabel(Text("Needs attention"))
                }
                Text(verbatim: figureText)
                    .font(.headline.monospacedDigit())
                    .foregroundStyle(Color(.label))
                    .contentTransition(.numericText())
            }
            Text(verbatim: detailText)
                .font(.caption)
                .foregroundStyle(Color(.secondaryLabel))
        }
    }
}

/// DomainSettingsView is the domain's settings tab: the DNS records to
/// publish, its logo, the names its MX records point at, the host pictures
/// are fetched from, and the key that signs its mail.
private struct DomainSettingsView: View {
    let model: ManagedDomainModel
    /// Nil while a field is untouched, so it shows what is stored.
    @State private var mailServersText: String?
    @State private var linkHostText: String?
    @State private var selectorText: String?
    @State private var isMovingSelector = false
    @State private var isReplacingKey = false
    @State private var isRemovingLogo = false
    @State private var isPickingLogo = false

    var body: some View {
        Form {
            if let domain = model.domain {
                recordsSection(domain)
                logoSection(domain)
                mailServersSection(domain)
                linkHostSection(domain)
                keySection(domain)
            }
        }
        .navigationTitle("Settings")
        .disabled(model.isBusy)
        .refreshable { await model.load() }
        .fileImporter(isPresented: $isPickingLogo, allowedContentTypes: [.svg]) { pickResult in
            guard case .success(let address) = pickResult else { return }
            Task { await uploadLogo(address) }
        }
        .confirmationDialog("Publish the key under a different label?", isPresented: $isMovingSelector, titleVisibility: .visible) {
            Button("Move the selector") {
                let selector = (selectorText ?? "").trimmed
                Task {
                    let isMoved = await model.act(saying: String(localized: "Selector moved")) {
                        try await model.connection.updateDomainDkimSelector(domainId: model.domainId, selector: selector)
                    }
                    if isMoved { selectorText = nil }
                }
            }
        } message: {
            let recordName = ManagedWords.dkimRecordName(selector: (selectorText ?? "").trimmed, domainName: model.domain?.domainName)
            Text(
                "Mail is signed under the new label from the moment this is saved, so publish the record at \(recordName) first. Until you do, signatures from this domain cannot be verified."
            )
        }
        .confirmationDialog("Replace the signing key?", isPresented: $isReplacingKey, titleVisibility: .visible) {
            Button("Replace the key", role: .destructive) {
                Task {
                    await model.act(saying: String(localized: "Key replaced")) {
                        try await model.connection.regenerateDomainKey(domainId: model.domainId)
                    }
                }
            }
        } message: {
            Text(
                "Mail already sent and signed with the old key stops verifying as soon as you change the DNS record, so do this only when rotating."
            )
        }
        .confirmationDialog("Stop publishing this logo?", isPresented: $isRemovingLogo, titleVisibility: .visible) {
            Button("Remove the logo", role: .destructive) {
                Task {
                    await model.act(saying: String(localized: "Logo removed")) {
                        try await model.connection.deleteDomainLogo(domainId: model.domainId)
                    }
                }
            }
        } message: {
            Text(
                "The file stops being served. Take the BIMI record naming it down as well, or receivers will keep fetching an address that answers nothing."
            )
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    private func recordsSection(_ domain: ManagedDomain) -> some View {
        Section {
            ForEach(Array((domain.dnsCheck?.records ?? []).enumerated()), id: \.offset) { _, record in
                NavigationLink {
                    DNSRecordView(record: record)
                } label: {
                    VStack(alignment: .leading, spacing: 3) {
                        HStack {
                            Text(verbatim: record.recordType ?? "")
                                .font(.callout.weight(.semibold))
                                .foregroundStyle(Color(.label))
                            Spacer()
                            DNSRecordTag(record: record)
                        }
                        Text(verbatim: record.name ?? "")
                            .font(.caption.monospaced())
                            .foregroundStyle(Color(.secondaryLabel))
                            .lineLimit(1)
                            .truncationMode(.middle)
                    }
                }
            }
            Button("Check again", systemImage: "arrow.clockwise") {
                Task {
                    await model.act(saying: String(localized: "Checked")) {
                        try await model.connection.checkDomain(domainId: model.domainId)
                    }
                }
            }
        } header: {
            Text("DNS records")
        } footer: {
            VStack(alignment: .leading, spacing: 6) {
                Text("Publish these with whoever runs DNS for this domain. Until the MX record is right, no mail arrives here at all.")
                if let errorMessage = domain.dnsCheck?.errorMessage, !errorMessage.isEmpty {
                    Label(errorMessage, systemImage: "exclamationmark.triangle").foregroundStyle(.orange)
                }
            }
        }
    }

    /// The mark this server publishes for the domain, which the BIMI record
    /// points at. The server checks the file before publishing it, and says
    /// what is wrong with one it refuses.
    private func logoSection(_ domain: ManagedDomain) -> some View {
        Section {
            if let logo = domain.logo {
                DetailRow(label: "File", fieldText: logo.filename)
                if let title = logo.title, !title.isEmpty {
                    Text("Titled “\(title)”, which is what a screen reader says in place of the mark.")
                        .font(.footnote)
                        .foregroundStyle(Color(.secondaryLabel))
                }
                DetailRow(label: "Uploaded", fieldText: ManageLabels.fullTime(logo.uploadedAt))
            } else {
                Text("No logo has been uploaded for this domain yet.")
                    .foregroundStyle(Color(.secondaryLabel))
            }
            Button(domain.logo == nil ? LocalizedStringKey("Upload a logo") : "Replace the logo", systemImage: "square.and.arrow.up") {
                isPickingLogo = true
            }
            if domain.logo != nil {
                Button("Remove the logo", systemImage: "trash", role: .destructive) { isRemovingLogo = true }
            }
        } header: {
            Text("Your logo")
        } footer: {
            VStack(alignment: .leading, spacing: 6) {
                Text(
                    "The mark receivers show beside your mail, which the BIMI record points at. Upload one and this server hosts it, so the domain needs no web server of its own."
                )
                Text(
                    "It has to be an SVG of the restricted kind BIMI accepts: no script, no animation, nothing fetched from elsewhere, and square. This checks that before publishing it, and says what is wrong if anything is."
                )
                Text(
                    "Gmail and Yahoo need a certificate. They show a mark only for senders who also hold a Verified Mark Certificate, which is issued against a registered trademark, renewed yearly, and costs on the order of a thousand dollars. Other receivers show one without. Its address goes in the record as the a= tag once you have one."
                )
            }
        }
    }

    private func mailServersSection(_ domain: ManagedDomain) -> some View {
        let storedText = (domain.mailServers ?? []).joined(separator: ", ")
        let defaultText = (domain.mailHosts ?? []).joined(separator: ", ")
        return Section {
            TextField(
                "Names", text: Binding(get: { mailServersText ?? storedText }, set: { mailServersText = $0 }),
                prompt: Text(verbatim: defaultText)
            )
            .font(.body.monospaced())
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            Button("Save the names") {
                let mailServers = ManagedWords.mailServerNames(mailServersText ?? "")
                Task {
                    let isSaved = await model.act(saying: String(localized: "Saved")) {
                        try await model.connection.updateDomainMailServers(domainId: model.domainId, mailServers: mailServers)
                    }
                    if isSaved { mailServersText = nil }
                }
            }
            .disabled(mailServersText == nil || mailServersText == storedText)
        } header: {
            Text("Mail server names")
        } footer: {
            VStack(alignment: .leading, spacing: 6) {
                Text(
                    "The names this domain’s MX records point at, and so the names a sender connects to. Leave this empty for the default, one name derived from the domain."
                )
                Text(
                    "Comma separated. Currently \(defaultText). A name inside this domain is one you publish an address record for, and one this server can obtain a certificate for; a name outside it belongs to whoever owns that zone."
                )
            }
        }
    }

    /// Where a picture in a message is fetched from. Get it wrong and the
    /// mail is fine while every picture in it is broken.
    private func linkHostSection(_ domain: ManagedDomain) -> some View {
        let defaultName = domain.linkHostname ?? ""
        return Section {
            TextField(
                "Host for pictures", text: Binding(get: { linkHostText ?? domain.linkHost ?? "" }, set: { linkHostText = $0 }),
                prompt: Text(verbatim: defaultName)
            )
            .font(.body.monospaced())
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .keyboardType(.URL)
            Button("Save") {
                let linkHost = (linkHostText ?? "").trimmed
                Task {
                    let isSaved = await model.act(saying: String(localized: "Saved")) {
                        try await model.connection.updateDomainLinkHost(domainId: model.domainId, linkHost: linkHost)
                    }
                    if isSaved { linkHostText = nil }
                }
            }
            .disabled(linkHostText == nil || (linkHostText ?? "").trimmed == (domain.linkHost ?? ""))
        } header: {
            Text("Pictures in mail")
        } footer: {
            VStack(alignment: .leading, spacing: 6) {
                Text(
                    "A picture you put in a template is fetched by the reader’s mail program over HTTPS, from a name under this domain. That is a different question from where mail arrives: the host a mail server name points at may answer HTTPS with something else entirely, and then the mail is fine while every picture in it is broken."
                )
                Text(
                    "Leave it empty to use \(defaultName). Whatever you name has to reach this server over HTTPS with a certificate a mail program will accept, and has to be under this domain: an address in another domain tells every reader who runs the server."
                )
            }
        }
    }

    private func keySection(_ domain: ManagedDomain) -> some View {
        let storedSelector = domain.dkimSelector ?? ""
        let typedSelector = selectorText ?? storedSelector
        let recordName = ManagedWords.dkimRecordName(selector: typedSelector.trimmed, domainName: domain.domainName)
        return Section {
            if domain.hasDkimKey == true {
                Text("Mail sent from this domain is signed under the selector \(storedSelector). The public half is the TXT record above; the private half is kept in this server’s database and never leaves it.")
                    .font(.footnote)
                    .foregroundStyle(Color(.secondaryLabel))
            } else {
                Label(
                    "This domain has no signing key, so its outgoing mail is unsigned and receivers are likely to treat it as spam.",
                    systemImage: "exclamationmark.triangle"
                )
                .font(.footnote)
                .foregroundStyle(.orange)
            }
            TextField("Selector", text: Binding(get: { typedSelector }, set: { selectorText = String($0.prefix(63)) }))
                .font(.body.monospaced())
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            Button("Move the selector") { isMovingSelector = true }
                .disabled(typedSelector == storedSelector || (selectorText ?? "").trimmed.isEmpty)
            if domain.hasDkimKey == true {
                Button("Replace the key", role: .destructive) { isReplacingKey = true }
            } else {
                Button("Generate a key") {
                    Task {
                        await model.act(saying: String(localized: "Key generated")) {
                            try await model.connection.regenerateDomainKey(domainId: model.domainId)
                        }
                    }
                }
            }
        } header: {
            Text("Signing key")
        } footer: {
            Text("The label the key is published under. It only has to be unique within this domain, and the record goes at \(recordName).")
        }
    }

    private func uploadLogo(_ address: URL) async {
        let isScoped = address.startAccessingSecurityScopedResource()
        defer { if isScoped { address.stopAccessingSecurityScopedResource() } }
        let svgData: Data
        do {
            svgData = try Data(contentsOf: address)
        } catch {
            model.problemText = error.localizedDescription
            return
        }
        let fileName = address.lastPathComponent
        await model.act(saying: String(localized: "Logo published")) {
            try await model.connection.uploadDomainLogo(domainId: model.domainId, fileName: fileName, svgData: svgData)
        }
    }
}

/// DNSRecordTag is whether one record is right: ok, optional or change.
private struct DNSRecordTag: View {
    let record: DNSRecord

    var body: some View {
        if record.isVerified == true {
            ManageTag(text: String(localized: "ok"), tone: .good)
        } else if record.isOptional == true {
            ManageTag(text: String(localized: "optional"), tone: .plain)
        } else {
            ManageTag(text: String(localized: "change"), tone: .warn)
        }
    }
}

/// DNSRecordView is one record to publish: its name and the value to put
/// there, each a tap away from the clipboard, what is published now, and
/// what is stopping it when something is.
private struct DNSRecordView: View {
    let record: DNSRecord
    @State private var doneText: String?

    var body: some View {
        List {
            Section {
                HStack {
                    Text(verbatim: record.recordType ?? "").font(.headline).foregroundStyle(Color(.label))
                    Spacer()
                    DNSRecordTag(record: record)
                }
                if let purpose = record.purpose, !purpose.isEmpty, record.isVerified != true {
                    Text(verbatim: purpose)
                        .font(.callout)
                        .foregroundStyle(Color(.secondaryLabel))
                }
                if let blockedReason = record.blockedReason, !blockedReason.isEmpty {
                    Label(blockedReason, systemImage: "exclamationmark.triangle")
                        .font(.callout)
                        .foregroundStyle(.orange)
                }
            }
            Section {
                copyRow(record.name ?? "")
            } header: {
                Text("Name")
            }
            Section {
                if record.publishedValue.isEmpty {
                    Text("Nothing to publish yet").foregroundStyle(Color(.secondaryLabel))
                } else {
                    copyRow(record.publishedValue)
                }
            } header: {
                Text("Required value")
            }
            Section {
                if record.isVerified == true {
                    Text("Matches").foregroundStyle(Color(.secondaryLabel))
                } else if let foundValues = record.foundValues, !foundValues.isEmpty {
                    ForEach(Array(foundValues.enumerated()), id: \.offset) { _, foundValue in
                        Text(verbatim: foundValue)
                            .font(.callout.monospaced())
                            .foregroundStyle(Color(.label))
                            .textSelection(.enabled)
                    }
                } else {
                    Text("Nothing published").foregroundStyle(Color(.secondaryLabel))
                }
            } header: {
                Text("Published now")
            }
        }
        .navigationTitle(record.recordType ?? "")
        .navigationBarTitleDisplayMode(.inline)
        .toast(done: $doneText, problem: .constant(nil))
    }

    private func copyRow(_ copiedText: String) -> some View {
        HStack(alignment: .top) {
            Text(verbatim: copiedText)
                .font(.callout.monospaced())
                .foregroundStyle(Color(.label))
                .textSelection(.enabled)
            Spacer()
            Button {
                UIPasteboard.general.string = copiedText
                doneText = String(localized: "Copied")
            } label: {
                Label("Copy", systemImage: "doc.on.doc").labelStyle(.iconOnly)
            }
            .buttonStyle(.borderless)
        }
    }
}

/// DomainAliasesView is the domain's aliases tab: who receives mail for
/// it. An alias is added from the toolbar and removed with a swipe.
private struct DomainAliasesView: View {
    let model: ManagedDomainModel
    @State private var isAdding = false
    @State private var removing: DomainAlias?
    @State private var mailboxes: [ServerMailbox] = []

    var body: some View {
        List {
            if let domain = model.domain {
                let aliases = domain.aliases ?? []
                Section {
                    if aliases.isEmpty {
                        Text("No address on this domain goes anywhere yet.").foregroundStyle(Color(.secondaryLabel))
                    }
                    ForEach(aliases) { alias in
                        row(alias)
                            .swipeActions {
                                Button("Remove", systemImage: "trash", role: .destructive) { removing = alias }
                            }
                    }
                } footer: {
                    Text("An alias with an empty pattern is a catch-all: it receives whatever no other alias matched.")
                }
            }
        }
        .navigationTitle("Aliases")
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    isAdding = true
                } label: {
                    Label("Add alias", systemImage: "plus")
                }
            }
        }
        .disabled(model.isBusy)
        .refreshable { await model.load() }
        .task { mailboxes = (try? await model.connection.serverMailboxes()) ?? [] }
        .sheet(isPresented: $isAdding) {
            NewAliasForm(model: model, mailboxes: mailboxes)
        }
        .confirmationDialog(
            "Remove address", isPresented: Binding(get: { removing != nil }, set: { if !$0 { removing = nil } }),
            titleVisibility: .visible, presenting: removing
        ) { alias in
            Button("Remove", role: .destructive) {
                Task {
                    await model.act(saying: String(localized: "Removed")) {
                        try await model.connection.deleteDomainAlias(aliasId: alias.id)
                    }
                }
            }
        } message: { alias in
            let shownName = aliasName(alias)
            Text("Remove “\(shownName)”? Mail sent to it will be refused.")
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    private func aliasName(_ alias: DomainAlias) -> String {
        guard let pattern = alias.pattern, !pattern.isEmpty else { return String(localized: "(catch-all)") }
        return pattern
    }

    private func row(_ alias: DomainAlias) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 8) {
                if let pattern = alias.pattern, !pattern.isEmpty {
                    Text(verbatim: pattern)
                        .font(.callout.monospaced())
                        .foregroundStyle(Color(.label))
                } else {
                    Text("(catch-all)").foregroundStyle(Color(.secondaryLabel))
                }
                if alias.isDisabled == true {
                    ManageTag(text: String(localized: "disabled"), tone: .plain)
                }
            }
            Text(verbatim: destinationText(alias))
                .font(.caption)
                .foregroundStyle(Color(.secondaryLabel))
                .lineLimit(2)
        }
    }

    private func destinationText(_ alias: DomainAlias) -> String {
        switch alias.aliasKind ?? "" {
        case "email": return alias.email ?? ""
        case "webhook": return alias.webhook ?? ""
        case "mailServer":
            guard let mailServer = alias.mailServer else { return "" }
            return "\(mailServer.host ?? ""):\(mailServer.port ?? 0)"
        case "null": return String(localized: "discarded")
        case "mailbox":
            let mailboxText = mailboxes.first { $0.id == alias.mailboxId }?.rowText ?? alias.mailboxId ?? ""
            return String(localized: "Into the mailbox of \(mailboxText)")
        default: return ""
        }
    }
}

/// NewAliasForm adds an address to the domain: its pattern, and where its
/// mail goes, into a mailbox, forwarded, posted, or discarded.
private struct NewAliasForm: View {
    let model: ManagedDomainModel
    let mailboxes: [ServerMailbox]
    @Environment(\.dismiss) private var dismiss
    @State private var pattern = ""
    @State private var aliasKind = "email"
    @State private var destination = ""
    @State private var mailboxId = ""
    @State private var isBusy = false
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    // Left blank it is a catch-all, which an empty box does
                    // not say, so the prompt does.
                    TextField("Pattern", text: $pattern, prompt: Text("^hello$, or leave blank to catch everything else"))
                        .font(.body.monospaced())
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    Picker("Kind", selection: $aliasKind) {
                        ForEach(DomainAlias.creatableKinds, id: \.self) { kind in
                            Text(kindTitle(kind)).tag(kind)
                        }
                    }
                }
                if aliasKind == "mailbox" {
                    Picker("Mailbox", selection: $mailboxId) {
                        Text("Choose a mailbox…").tag("")
                        ForEach(mailboxes) { mailbox in
                            Text(verbatim: mailbox.pickerText).tag(mailbox.id)
                        }
                    }
                } else if aliasKind != "null" {
                    TextField(aliasKind == "email" ? LocalizedStringKey("Forward to") : "Post to", text: $destination)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(aliasKind == "email" ? .emailAddress : .URL)
                }
            }
            .navigationTitle("Add alias")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Create") {
                        Task { await create() }
                    }
                    .disabled(isBusy)
                }
            }
            .disabled(isBusy)
            .toast(done: .constant(nil), problem: $problemText)
        }
    }

    private func kindTitle(_ kind: String) -> LocalizedStringKey {
        switch kind {
        case "mailbox": "Deliver into a mailbox"
        case "email": "Email"
        case "webhook": "Webhook"
        default: "Discard"
        }
    }

    private func create() async {
        isBusy = true
        defer { isBusy = false }
        do {
            try await model.connection.createDomainAlias(
                domainId: model.domainId, pattern: pattern.trimmed, aliasKind: aliasKind, destination: destination.trimmed, mailboxId: mailboxId)
            model.doneText = String(localized: "Alias added")
            dismiss()
            await model.load()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// DomainCredentialsView is the domain's credentials tab: who may send
/// through it, one username and password per machine, the password shown
/// once when it is made.
private struct DomainCredentialsView: View {
    let model: ManagedDomainModel
    @State private var isAdding = false
    @State private var removing: DomainCredential?

    var body: some View {
        List {
            if let domain = model.domain {
                let credentials = domain.credentials ?? []
                Section {
                    if credentials.isEmpty {
                        Text("No machine sends through this domain yet.").foregroundStyle(Color(.secondaryLabel))
                    }
                    ForEach(credentials) { credential in
                        VStack(alignment: .leading, spacing: 3) {
                            if let comment = credential.comment, !comment.isEmpty {
                                Text(verbatim: comment).foregroundStyle(Color(.label))
                            } else {
                                Text("(no note)").foregroundStyle(Color(.secondaryLabel))
                            }
                            Text(verbatim: credential.id)
                                .font(.caption.monospaced())
                                .foregroundStyle(Color(.secondaryLabel))
                                .lineLimit(1)
                                .truncationMode(.middle)
                        }
                        .swipeActions {
                            Button("Remove", systemImage: "trash", role: .destructive) { removing = credential }
                        }
                    }
                } footer: {
                    Text("For sending mail through this server as \(domain.domainName), from a mail client or a script.")
                }
            }
        }
        .navigationTitle("Credentials")
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    isAdding = true
                } label: {
                    Label("Create credential", systemImage: "plus")
                }
            }
        }
        .disabled(model.isBusy)
        .refreshable { await model.load() }
        .sheet(isPresented: $isAdding) {
            NewCredentialForm(model: model)
        }
        .confirmationDialog(
            "Remove credential", isPresented: Binding(get: { removing != nil }, set: { if !$0 { removing = nil } }),
            titleVisibility: .visible, presenting: removing
        ) { credential in
            Button("Remove", role: .destructive) {
                Task {
                    await model.act(saying: String(localized: "Removed")) {
                        try await model.connection.deleteDomainCredential(credentialId: credential.id)
                    }
                }
            }
        } message: { credential in
            let credentialName = (credential.comment ?? "").isEmpty ? credential.id : credential.comment ?? ""
            Text("Remove the credential “\(credentialName)”? Whatever sends with it stops being able to.")
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }
}

/// NewCredentialForm makes a credential with a note saying what will hold
/// it, then shows its password in the same sheet, the one time it can be
/// read, with the server and username to type beside it.
private struct NewCredentialForm: View {
    let model: ManagedDomainModel
    @Environment(\.dismiss) private var dismiss
    @State private var comment = ""
    @State private var created: CreatedDomainCredential?
    @State private var isBusy = false
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            Group {
                if let created {
                    createdList(created)
                } else {
                    form
                }
            }
            .toast(done: $doneText, problem: $problemText)
        }
        .interactiveDismissDisabled(created != nil)
    }

    private var form: some View {
        Form {
            TextField("What will hold it", text: $comment, prompt: Text("laptop"))
        }
        .navigationTitle("Create credential")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) {
                Button("Cancel") { dismiss() }
            }
            ToolbarItem(placement: .confirmationAction) {
                Button("Create") {
                    Task { await create() }
                }
                .disabled(isBusy)
            }
        }
        .disabled(isBusy)
    }

    private func createdList(_ created: CreatedDomainCredential) -> some View {
        List {
            Section {
                Text(verbatim: created.password)
                    .font(.body.monospaced())
                    .textSelection(.enabled)
                Button("Copy password", systemImage: "doc.on.doc") {
                    UIPasteboard.general.string = created.password
                    doneText = String(localized: "Copied")
                }
            } header: {
                Text("Save this password now; it is not shown again.")
            } footer: {
                Text("Type this into the machine now. It is shown this once and cannot be shown again.")
            }
            Section {
                let host = created.host
                let port = created.port
                DetailRow(label: "Server", fieldText: String(localized: "\(host) port \(port), STARTTLS"), isMonospaced: true)
                DetailRow(label: "Username", fieldText: created.username, isMonospaced: true)
            }
        }
        .navigationTitle("Create credential")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button("Done") { dismiss() }
            }
        }
    }

    private func create() async {
        isBusy = true
        defer { isBusy = false }
        do {
            created = try await model.connection.createDomainCredential(domainId: model.domainId, comment: comment.trimmed)
            await model.load()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

// MARK: - Templates

/// DomainTemplatesView is the domain's templates tab: the templates it
/// sends by name and the layouts around them, each opened to read. Writing
/// them is a large screen's work, so they are edited on the dashboard;
/// here they are read and removed.
private struct DomainTemplatesView: View {
    let connection: Connection
    let domainId: String
    @State private var templates: [DomainTemplate]?
    @State private var layouts: [DomainLayout] = []
    @State private var isLoading = true
    @State private var isBusy = false
    @State private var removingTemplate: DomainTemplate?
    @State private var removingLayout: DomainLayout?
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        List {
            if let templates {
                let templateCountByLayoutId = DomainLayout.templateCountByLayoutId(templates)
                Section {
                    if templates.isEmpty {
                        Text("No templates yet.").foregroundStyle(Color(.secondaryLabel))
                    }
                    ForEach(templates) { template in
                        NavigationLink {
                            DomainTemplateView(connection: connection, templateId: template.id, layoutName: layoutName(template.layoutId))
                        } label: {
                            templateRow(template)
                        }
                        .swipeActions {
                            Button("Remove", systemImage: "trash", role: .destructive) { removingTemplate = template }
                        }
                    }
                } header: {
                    Text("Templates")
                } footer: {
                    Text(
                        "A template is a message this domain sends by name: an application calls the send endpoint with the name and the values. Each can be translated into as many languages as its readers speak."
                    )
                }
                Section {
                    if layouts.isEmpty {
                        Text("No layouts. A template with no layout renders on its own, which is fine for plain messages.")
                            .foregroundStyle(Color(.secondaryLabel))
                    }
                    ForEach(layouts) { layout in
                        NavigationLink {
                            DomainLayoutView(connection: connection, layoutId: layout.id)
                        } label: {
                            layoutRow(layout, templateCount: templateCountByLayoutId[layout.id] ?? 0)
                        }
                        .swipeActions {
                            Button("Remove", systemImage: "trash", role: .destructive) { removingLayout = layout }
                        }
                    }
                } header: {
                    Text("Layouts")
                } footer: {
                    VStack(alignment: .leading, spacing: 8) {
                        Text("A layout is what goes around a template: the header, the footer, the styles.")
                        Text("Templates and layouts are written and changed on the dashboard.")
                    }
                }
            }
        }
        .navigationTitle("Templates")
        .overlay { if templates == nil && isLoading { ProgressView() } }
        .disabled(isBusy)
        .task { await load() }
        .refreshable { await load() }
        .confirmationDialog(
            removingTemplate.map { String(localized: "Remove the template \($0.name)?") } ?? "",
            isPresented: Binding(get: { removingTemplate != nil }, set: { if !$0 { removingTemplate = nil } }),
            titleVisibility: .visible, presenting: removingTemplate
        ) { template in
            Button("Remove", role: .destructive) {
                Task { await act { try await connection.deleteDomainTemplate(templateId: template.id) } }
            }
        } message: { _ in
            Text("Anything sending it by name stops working at once. Mail already sent is unaffected.")
        }
        .confirmationDialog(
            "Remove this layout?", isPresented: Binding(get: { removingLayout != nil }, set: { if !$0 { removingLayout = nil } }),
            titleVisibility: .visible, presenting: removingLayout
        ) { layout in
            Button("Remove", role: .destructive) {
                Task { await act { try await connection.deleteDomainLayout(layoutId: layout.id) } }
            }
        } message: { layout in
            let templateCount = DomainLayout.templateCountByLayoutId(templates ?? [])[layout.id] ?? 0
            if templateCount > 0 {
                Text("\(templateCount) templates sit in it. They keep working, rendered without a layout, until you choose another.")
            } else {
                Text("Nothing uses it, so nothing changes but the list.")
            }
        }
        .toast(done: $doneText, problem: $problemText)
    }

    /// A layout has no name of its own: what it is for is its comment.
    private func layoutName(_ layoutId: String?) -> String? {
        guard let layoutId, !layoutId.isEmpty else { return nil }
        return layoutTitle(layouts.first { $0.id == layoutId })
    }

    private func layoutTitle(_ layout: DomainLayout?) -> String {
        guard let comment = layout?.comment, !comment.isEmpty else { return String(localized: "(untitled layout)") }
        return comment
    }

    private func templateRow(_ template: DomainTemplate) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                Text(verbatim: template.name)
                    .font(.callout.monospaced())
                    .foregroundStyle(Color(.label))
                Spacer()
                Text(verbatim: template.locales.joined(separator: " "))
                    .font(.caption.monospaced())
                    .foregroundStyle(Color(.secondaryLabel))
            }
            if let comment = template.comment, !comment.isEmpty {
                Text(verbatim: comment)
                    .font(.caption)
                    .foregroundStyle(Color(.secondaryLabel))
            }
            Text(verbatim: templateDetails(template))
                .font(.caption)
                .foregroundStyle(Color(.secondaryLabel))
        }
    }

    /// Its layout, the variables it reads, and when it last changed.
    private func templateDetails(_ template: DomainTemplate) -> String {
        var parts: [String] = []
        if let shownLayoutName = layoutName(template.layoutId) {
            parts.append(String(localized: "In layout \(shownLayoutName)"))
        } else {
            parts.append(String(localized: "No layout"))
        }
        let variables = template.variables ?? []
        let variablesText = variables.joined(separator: ", ")
        parts.append(variables.isEmpty ? String(localized: "reads no variables") : String(localized: "reads \(variablesText)"))
        if let changedText = ManageLabels.relativeTime(template.modifiedAt) {
            parts.append(String(localized: "changed \(changedText)"))
        }
        return parts.joined(separator: " · ")
    }

    private func layoutRow(_ layout: DomainLayout, templateCount: Int) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                Text(verbatim: layoutTitle(layout)).foregroundStyle(Color(.label))
                Spacer()
                Text(verbatim: layout.locales.joined(separator: " "))
                    .font(.caption.monospaced())
                    .foregroundStyle(Color(.secondaryLabel))
            }
            Text(verbatim: layoutDetails(layout, templateCount: templateCount))
                .font(.caption)
                .foregroundStyle(Color(.secondaryLabel))
        }
    }

    private func layoutDetails(_ layout: DomainLayout, templateCount: Int) -> String {
        var parts = [templateCount == 1 ? String(localized: "used by 1 template") : String(localized: "used by \(templateCount) templates")]
        if let changedText = ManageLabels.relativeTime(layout.modifiedAt) {
            parts.append(String(localized: "changed \(changedText)"))
        }
        return parts.joined(separator: " · ")
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            let loaded = try await connection.domainTemplates(domainId: domainId)
            templates = loaded.templates
            layouts = loaded.layouts
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func act(_ work: () async throws -> Void) async {
        isBusy = true
        defer { isBusy = false }
        do {
            try await work()
            doneText = String(localized: "Removed")
        } catch {
            problemText = error.localizedDescription
        }
        await load()
    }
}

/// TemplateLanguage is one language a template or layout is written in:
/// the default, or a translation.
private struct TemplateLanguage: Hashable {
    let locale: String
    let subject: String?
    let htmlContent: String?
    let textContent: String?
}

/// TemplateContentSections are a template's or a layout's words in one of
/// its languages, chosen from a picker when it has more than one: the
/// subject, the HTML and the plain text, read only.
private struct TemplateContentSections: View {
    let languages: [TemplateLanguage]
    @Binding var chosenLocale: String

    var body: some View {
        let chosen = languages.first { $0.locale == chosenLocale } ?? languages.first
        if languages.count > 1 {
            Section {
                Picker("Language", selection: $chosenLocale) {
                    ForEach(languages, id: \.locale) { language in
                        Text(verbatim: language.locale.isEmpty ? String(localized: "Default") : language.locale).tag(language.locale)
                    }
                }
                .pickerStyle(.segmented)
            }
        }
        if let chosen {
            if let subject = chosen.subject {
                Section {
                    Text(verbatim: subject.isEmpty ? " " : subject)
                        .foregroundStyle(Color(.label))
                        .textSelection(.enabled)
                } header: {
                    Text("Subject")
                }
            }
            Section {
                Text(verbatim: chosen.htmlContent ?? "")
                    .font(.caption.monospaced())
                    .foregroundStyle(Color(.label))
                    .textSelection(.enabled)
            } header: {
                Text("HTML")
            }
            Section {
                Text(verbatim: chosen.textContent ?? "")
                    .font(.caption.monospaced())
                    .foregroundStyle(Color(.label))
                    .textSelection(.enabled)
            } header: {
                Text("Text")
            } footer: {
                Text("Templates and layouts are written and changed on the dashboard.")
            }
        }
    }
}

/// DomainTemplateView is one template, read only: its name, layout,
/// variables and its words in each language.
private struct DomainTemplateView: View {
    let connection: Connection
    let templateId: String
    let layoutName: String?
    @State private var template: DomainTemplate?
    @State private var chosenLocale = ""
    @State private var isLoading = true
    @State private var problemText: String?

    var body: some View {
        List {
            if let template {
                Section {
                    DetailRow(label: "Name", fieldText: template.name, isMonospaced: true)
                    DetailRow(label: "Layout", fieldText: layoutName ?? String(localized: "No layout"))
                    DetailRow(label: "Comment", fieldText: template.comment)
                    DetailRow(label: "Variables", fieldText: (template.variables ?? []).joined(separator: ", "), isMonospaced: true)
                    DetailRow(label: "Changed", fieldText: ManageLabels.fullTime(template.modifiedAt))
                }
                TemplateContentSections(languages: languages(template), chosenLocale: $chosenLocale)
            }
        }
        .navigationTitle(template?.name ?? "")
        .navigationBarTitleDisplayMode(.inline)
        .overlay { if template == nil && isLoading { ProgressView() } }
        .task { await load() }
        .refreshable { await load() }
        .toast(done: .constant(nil), problem: $problemText)
    }

    private func languages(_ template: DomainTemplate) -> [TemplateLanguage] {
        [TemplateLanguage(locale: template.locale ?? "", subject: template.subject ?? "", htmlContent: template.htmlContent, textContent: template.textContent)]
            + (template.translations ?? []).map {
                TemplateLanguage(locale: $0.locale, subject: $0.subject ?? "", htmlContent: $0.htmlContent, textContent: $0.textContent)
            }
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            template = try await connection.domainTemplate(templateId: templateId)
            if chosenLocale.isEmpty { chosenLocale = template?.locale ?? "" }
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// DomainLayoutView is one layout, read only: what it is for, and its
/// words in each language.
private struct DomainLayoutView: View {
    let connection: Connection
    let layoutId: String
    @State private var layout: DomainLayout?
    @State private var chosenLocale = ""
    @State private var isLoading = true
    @State private var problemText: String?

    var body: some View {
        List {
            if let layout {
                Section {
                    DetailRow(label: "What it is for", fieldText: layout.comment)
                    DetailRow(label: "Changed", fieldText: ManageLabels.fullTime(layout.modifiedAt))
                }
                TemplateContentSections(languages: languages(layout), chosenLocale: $chosenLocale)
            }
        }
        .navigationTitle(layout.map { ($0.comment ?? "").isEmpty ? String(localized: "(untitled layout)") : $0.comment ?? "" } ?? "")
        .navigationBarTitleDisplayMode(.inline)
        .overlay { if layout == nil && isLoading { ProgressView() } }
        .task { await load() }
        .refreshable { await load() }
        .toast(done: .constant(nil), problem: $problemText)
    }

    private func languages(_ layout: DomainLayout) -> [TemplateLanguage] {
        [TemplateLanguage(locale: layout.locale ?? "", subject: nil, htmlContent: layout.htmlContent, textContent: layout.textContent)]
            + (layout.translations ?? []).map { TemplateLanguage(locale: $0.locale, subject: nil, htmlContent: $0.htmlContent, textContent: $0.textContent) }
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            layout = try await connection.domainLayout(layoutId: layoutId)
            if chosenLocale.isEmpty { chosenLocale = layout?.locale ?? "" }
        } catch {
            problemText = error.localizedDescription
        }
    }
}
