import Charts
import QuickLook
import SwiftUI
import TeaNodeKit

// The agent page's upkeep tabs, as the dashboard's: the sources it reads,
// its memory and the checks of it, the dreams, the schedules, the alerts,
// the connections, and what it did on its own. Each tab is one screen.

// MARK: - Shared pieces

/// UpkeepTone is what a tag on these screens says about a state: nothing in
/// particular, all is well, something wants a look, or something failed.
/// Each carries a symbol as well as a color, so the color never says it
/// alone.
enum UpkeepTone {
    case plain, good, warning, bad

    var symbolName: String? {
        switch self {
        case .plain: nil
        case .good: "checkmark.circle"
        case .warning: "exclamationmark.circle"
        case .bad: "exclamationmark.triangle"
        }
    }

    var color: Color {
        switch self {
        case .plain, .good: Color(.secondaryLabel)
        case .warning: .orange
        case .bad: .red
        }
    }
}

/// UpkeepTag is a short word about a row's state, beside its name.
struct UpkeepTag: View {
    let text: String
    var tone: UpkeepTone = .plain

    var body: some View {
        Group {
            if let symbolName = tone.symbolName {
                Label(text, systemImage: symbolName)
                    .labelStyle(.tag)
            } else {
                Text(text)
            }
        }
        .font(.caption)
        .foregroundStyle(tone.color)
        .symbolRenderingMode(.hierarchical)
    }
}

/// UpkeepWords is how these screens say the server's times, amounts and
/// fixed words, as the dashboard says them.
enum UpkeepWords {
    static func when(_ text: String?) -> String {
        ServerTime.date(text)?.formatted(date: .abbreviated, time: .shortened) ?? ""
    }

    static func relative(_ text: String?) -> String {
        ServerTime.date(text)?.formatted(.relative(presentation: .named)) ?? ""
    }

    static func money(_ amount: Double, currency: String? = nil) -> String {
        let code = (currency ?? "").isEmpty ? "USD" : currency ?? "USD"
        return amount.formatted(.currency(code: code).precision(.fractionLength(amount < 0.01 ? 4 : 2)))
    }

    /// A time of day kept as "HH:MM", as a date today for a time picker.
    static func clockDate(_ text: String) -> Date {
        let parts = UpkeepClock.hourAndMinute(text) ?? (hour: 0, minute: 0)
        return Calendar.current.date(bySettingHour: parts.hour, minute: parts.minute, second: 0, of: .now) ?? .now
    }

    /// A time picker's date as the "HH:MM" the server keeps.
    static func clockText(_ date: Date) -> String {
        let parts = Calendar.current.dateComponents([.hour, .minute], from: date)
        return UpkeepClock.text(hour: parts.hour ?? 0, minute: parts.minute ?? 0)
    }

    /// A day the look around may be set to, in the person's language.
    static func lookAroundWeekdayName(_ name: String) -> String {
        guard let index = AgentUpkeepPreferences.lookAroundWeekdays.firstIndex(of: name) else { return name }
        // weekdaySymbols starts on Sunday, the list on Monday.
        return Calendar.current.weekdaySymbols[(index + 1) % 7]
    }

    static func shapeName(_ shape: KnowledgeSource.Shape) -> String {
        switch shape {
        case .files: String(localized: "Files and code")
        case .journal: String(localized: "Dated notes")
        case .records: String(localized: "Records a script writes")
        case .sent: String(localized: "Your own sent mail")
        }
    }

    static func settingProblem(_ problem: KnowledgeSourceType.SettingProblem, settingName: String) -> String {
        let label = KnowledgeSourceType.label(settingName)
        switch problem {
        case .missing: return String(localized: "\(label) needs a value")
        case .notWhole: return String(localized: "\(label) has to be a whole number")
        case .tooSmall(let minimum): return String(localized: "\(label) has to be at least \(minimum)")
        case .tooLarge(let maximum): return String(localized: "\(label) has to be at most \(maximum)")
        case .itemRefused(let item): return String(localized: "\(label): \"\(item)\" is not in the form it asks for")
        case .refused: return String(localized: "\(label) is not in the form it asks for")
        }
    }

    static func dreamOutcome(_ outcome: AgentDream.DreamOutcome) -> String {
        let outcomeCount = outcome.outcomeCount
        switch outcome.outcomeKind {
        case .read: return String(localized: "\(outcomeCount) read")
        case .filed: return String(localized: "\(outcomeCount) filed")
        case .rewritten: return String(localized: "\(outcomeCount) pages rewritten")
        case .merged: return String(localized: "\(outcomeCount) said twice, merged")
        case .moved: return String(localized: "\(outcomeCount) filed away")
        case .retired: return String(localized: "\(outcomeCount) retired")
        case .embedded: return String(localized: "\(outcomeCount) embedded")
        case .strengthened: return String(localized: "\(outcomeCount) links reweighted")
        case .associated: return String(localized: "\(outcomeCount) connections noticed")
        case .rehearsed: return String(localized: "\(outcomeCount) rehearsed")
        case .gaps: return String(localized: "\(outcomeCount) gaps")
        case .unknown: return String(localized: "\(outcomeCount) unknown")
        case .revised: return String(localized: "\(outcomeCount) lines an older version left")
        case .overviewsWritten: return String(localized: "\(outcomeCount) overviews written")
        case .themesMade: return String(localized: "\(outcomeCount) themes made")
        case .themesUpdated: return String(localized: "\(outcomeCount) themes updated")
        case .reflectionsWritten: return String(localized: "\(outcomeCount) reflections written")
        }
    }

    /// When a schedule comes round, in a person's words, "Weekdays at 7:30
    /// AM", with the time and the days' names as the locale writes them; nil
    /// for a line of any other kind, which is shown as it is written.
    static func scheduleTimingText(_ cron: String) -> String? {
        guard let timing = ScheduleTiming(cron: cron) else { return nil }
        let timeText = timing.timeText()
        switch timing.dayPattern {
        case .everyDay:
            return String(localized: "Every day at \(timeText)")
        case .weekdays:
            return String(localized: "Weekdays at \(timeText)")
        case .weekends:
            return String(localized: "Weekends at \(timeText)")
        case .oneDay(let dayNumber):
            let dayName = ScheduleWeekday.name(dayNumber)
            return String(localized: "\(dayName)s at \(timeText)", comment: "A weekly schedule on one day, as \"Fridays at 5:00 PM\": the day's name, then the time.")
        case .someDays(let dayNumbers):
            let dayNames = dayNumbers.map { ScheduleWeekday.shortName($0) }.formatted(.list(type: .and))
            return String(localized: "Every \(dayNames) at \(timeText)", comment: "A weekly schedule on a few days, as \"Every Mon, Wed, and Fri at 9:00 AM\".")
        }
    }

    static func readingSpan(_ span: ReadingProgress.ReadingSpan) -> String {
        switch span {
        case .minutes(let minutes): String(localized: "\(minutes) minutes")
        case .hours(let hours): String(localized: "\(hours) hours")
        case .days(let days): String(localized: "\(days) days")
        }
    }

    /// A run's kind in the reader's words, or as the server names it when
    /// a newer server added one this app has no words for.
    static func runKind(_ jobKind: String) -> String {
        switch jobKind {
        case "triage": String(localized: "Sorting")
        case "reply": String(localized: "Reply")
        case "research": String(localized: "Research")
        case "summarize": String(localized: "Summary")
        case "remember": String(localized: "Remembering")
        case "ingest": String(localized: "Reading a source")
        case "recording": String(localized: "Recording transcription")
        case "dream": String(localized: "Dream")
        case "schedule": String(localized: "Scheduled run")
        case "extract": String(localized: "Extraction")
        case "draft": String(localized: "Draft")
        case "describe": String(localized: "Titling")
        case "react": String(localized: "Picking a reaction")
        case "compact": String(localized: "Compaction")
        case "evaluate": String(localized: "Evaluation")
        case "survey": String(localized: "Survey")
        case "subagent": String(localized: "Subagent")
        case "mcp": String(localized: "Over MCP")
        case "goal": String(localized: "Goal")
        case "background": String(localized: "Background")
        case "speak_first": String(localized: "Speaking first")
        case "alert": String(localized: "Alert")
        case "categorize": String(localized: "Categorizing")
        case "statement_import": String(localized: "Statement import")
        case "read_receipt": String(localized: "Read a receipt")
        case "describe_photo": String(localized: "Describe a photo")
        case "tax_document": String(localized: "Read a tax document")
        case "tax_mail": String(localized: "Tax documents mailed in")
        case "watch": String(localized: "Watching")
        case "follow_up_sweep": String(localized: "Check follow-ups")
        case "nearby": String(localized: "Look around town")
        default: jobKind
        }
    }
}

// MARK: - Sources

/// KnowledgeSourcesView is the dashboard's Sources tab: the places the
/// person pointed their agent at, how far each has got and what became of
/// its files, each added, changed, read again, paused or removed. Banks and
/// cards are on the Finance tab instead.
struct KnowledgeSourcesView: View {
    let connection: Connection
    var highlightedSourceId: String? = nil
    @State private var sources: [KnowledgeSource]?
    @State private var attachmentsBySourceId: [String: KnowledgeSourceAttachments] = [:]
    @State private var sourceTypes: [KnowledgeSourceType] = []
    @State private var mailboxes: [Mailbox] = []
    @State private var editing: SourceEditing?
    @State private var removing: KnowledgeSource?
    @State private var declinedSource: KnowledgeSource?
    @State private var isBusy = false
    @State private var doneText: String?
    @State private var problemText: String?

    /// SourceEditing is the editor open on a source, or on a new one.
    struct SourceEditing: Identifiable {
        let id = UUID()
        let source: KnowledgeSource?
    }

    var body: some View {
        List {
            Section {
                if let sources, sources.isEmpty {
                    Text("Nothing is indexed yet.").foregroundStyle(.secondary)
                }
                ForEach((sources ?? []).filter { highlightedSourceId == nil || $0.id == highlightedSourceId }) { source in
                    row(source)
                        .confirmationDialog(
                            "Stop reading it", isPresented: Binding(get: { removing?.id == source.id }, set: { if !$0 { removing = nil } }), titleVisibility: .visible,
                            presenting: removing
                        ) { source in
                            Button("Stop reading it", role: .destructive) {
                                Task { await act(saying: String(localized: "Stopped")) { try await connection.deleteKnowledgeSource(source.id) } }
                            }
                        } message: { source in
                            Text("Stop reading \(RecordingWords.sourceName(source)) and forget the \(source.documentCount) document(s) it found. What your agent learned from it stays on its pages.")
                        }
                }
            } footer: {
                Text("Places you have pointed your agent at: a checkout on one of your computers, a chat export, your notes. It reads them on a schedule and can search them.")
            }
        }
        .navigationTitle("Sources")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button("Add a source", systemImage: "plus") { editing = SourceEditing(source: nil) }
            }
        }
        .overlay { if sources == nil { ProgressView() } }
        .disabled(isBusy)
        .refreshable { await load(isQuiet: false) }
        .task {
            let connection = connection
            async let typesRead = try? connection.knowledgeSourceTypes()
            async let mailboxesRead = try? connection.mailboxes()
            sourceTypes = await typesRead ?? []
            mailboxes = (await mailboxesRead ?? []).compactMap(\.mailbox)
            await load(isQuiet: false)
            // Read again while it is open, as the dashboard does: a source
            // still reading says so until its pass ends.
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(15))
                if Task.isCancelled { break }
                await load(isQuiet: true)
            }
        }
        .navigationDestination(item: $declinedSource) { source in
            DeclinedFilesView(connection: connection, source: source)
        }
        .sheet(item: $editing) { edited in
            KnowledgeSourceEditor(
                connection: connection, source: edited.source, sourceTypes: sourceTypes, mailboxes: mailboxes
            ) { said in
                doneText = said
                Task { await load(isQuiet: false) }
            }
        }
        .toast(done: $doneText, problem: $problemText)
    }

    @ViewBuilder
    private func row(_ source: KnowledgeSource) -> some View {
        if source.kind == "recording" {
            NavigationLink {
                RecordingsScreen(connection: connection)
            } label: {
                KnowledgeSourceRow(
                    source: source, attachments: attachmentsBySourceId[source.id], isLocalType: false,
                    whereText: String(localized: "Managed from the recording library"))
            }
            .swipeActions {
                Button("Stop reading it", systemImage: "trash", role: .destructive) { removing = source }
                    .tint(.red)
                pauseButton(source).tint(.orange)
            }
            .contextMenu {
                Button("Edit this source", systemImage: "pencil") { editing = SourceEditing(source: source) }
                syncButton(source)
                pauseButton(source)
                Button("Stop reading it", systemImage: "trash", role: .destructive) { removing = source }
                    .tint(.red)
            }
        } else {
            Button {
                editing = SourceEditing(source: source)
            } label: {
                KnowledgeSourceRow(
                    source: source, attachments: attachmentsBySourceId[source.id],
                    isLocalType: sourceTypes.contains { $0.name == source.specification.type && $0.isLocal },
                    whereText: source.whereItReads(mailboxNames: mailboxNames))
            }
            .swipeActions {
                Button("Stop reading it", systemImage: "trash", role: .destructive) { removing = source }
                    .tint(.red)
                pauseButton(source).tint(.orange)
            }
            .contextMenu {
                Button("Edit this source", systemImage: "pencil") { editing = SourceEditing(source: source) }
                syncButton(source)
                pauseButton(source)
                if (attachmentsBySourceId[source.id]?.declined ?? 0) > 0 {
                    Button("Files it decided against opening", systemImage: "doc.badge.ellipsis") { declinedSource = source }
                }
                Button("Stop reading it", systemImage: "trash", role: .destructive) { removing = source }
                    .tint(.red)
            }
        }
    }

    private func syncButton(_ source: KnowledgeSource) -> some View {
        Button("Read it again now", systemImage: "arrow.clockwise") {
            Task { await act(saying: String(localized: "Reading it again")) { try await connection.syncKnowledgeSource(source.id) } }
        }
    }

    private func pauseButton(_ source: KnowledgeSource) -> some View {
        Button(
            source.enabled ? String(localized: "Pause reading this") : String(localized: "Start reading this again"),
            systemImage: source.enabled ? "pause" : "play"
        ) {
            let said =
                source.enabled
                ? String(localized: "Paused; nothing it found is lost, and resuming reads it again.")
                : String(localized: "Reading it again; it starts within the minute.")
            Task { await act(saying: said) { try await connection.switchKnowledgeSource(source.id, isEnabled: !source.enabled) } }
        }
    }

    private var mailboxNames: [String: String] {
        Dictionary(mailboxes.map { ($0.id, KnowledgeSourceEditor.mailboxLabel($0)) }, uniquingKeysWith: { first, _ in first })
    }

    private func load(isQuiet: Bool) async {
        do {
            let details = try await connection.knowledgeSourceDetails()
            sources = details.sources.filter { !$0.isFinance }
            attachmentsBySourceId = Dictionary(details.attachments.map { ($0.sourceId, $0) }, uniquingKeysWith: { first, _ in first })
        } catch {
            if !isQuiet { problemText = error.localizedDescription }
        }
    }

    private func act(saying said: String, _ work: () async throws -> Void) async {
        isBusy = true
        defer { isBusy = false }
        do {
            try await work()
            doneText = said
        } catch {
            problemText = error.localizedDescription
        }
        await load(isQuiet: false)
    }
}

/// KnowledgeSourceRow is a source in a few lines: its name and state, where
/// it reads, how much it read, what became of its files, and why it
/// stopped, if it did.
struct KnowledgeSourceRow: View {
    let source: KnowledgeSource
    let attachments: KnowledgeSourceAttachments?
    let isLocalType: Bool
    let whereText: String

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(RecordingWords.sourceName(source)).foregroundStyle(Color(.label))
            HStack(spacing: 8) {
                if source.kind == "recording" {
                    UpkeepTag(text: String(localized: "Recording"))
                } else if source.specification.type.isEmpty {
                    UpkeepTag(text: UpkeepWords.shapeName(source.shape))
                } else {
                    UpkeepTag(text: source.specification.type)
                    if isLocalType { UpkeepTag(text: String(localized: "local"), tone: .warning) }
                }
                if source.more { UpkeepTag(text: String(localized: "Still reading"), tone: .good) }
                if !source.enabled { UpkeepTag(text: String(localized: "Paused"), tone: .warning) }
            }
            Group {
                if !whereText.isEmpty { Text(verbatim: whereText) }
                Text(countsText)
                if let filesText { Text(filesText) }
                if source.checkoutsKeptToProfile > 0 {
                    Text(
                        "\(source.checkoutsKeptToProfile) checkouts here hold too few of your commits to be your work, so they are kept to their profile (what they are and where they live), and \(source.filesKeptToProfile) files in them are left unread."
                    )
                }
                if !source.enabled { Text("Paused; nothing it found is lost, and resuming reads it again.") }
                if !source.lastError.isEmpty { Text(verbatim: source.lastError.asReasonSentence) }
            }
            .font(.footnote)
            .foregroundStyle(Color(.secondaryLabel))
        }
    }

    private var countsText: String {
        let counts = String(localized: "\(source.documentCount) documents, \(source.chunkCount) passages")
        guard source.refusedCount > 0 else { return counts }
        return counts + " · " + String(localized: "\(source.refusedCount) held back on the machine")
    }

    /// What became of the pictures and files it carried: waiting for a
    /// decision, decided against, or opened and read.
    private var filesText: String? {
        guard let attachments, attachments.hasAny else { return nil }
        var parts: [String] = []
        if attachments.undecided > 0 { parts.append(String(localized: "\(attachments.undecided) files waiting for a decision")) }
        if attachments.declined > 0 { parts.append(String(localized: "\(attachments.declined) it decided against opening")) }
        if attachments.described > 0 { parts.append(String(localized: "\(attachments.described) it opened and read")) }
        return parts.joined(separator: " · ")
    }
}

/// DeclinedFilesView is the files one source's reading decided against
/// opening, gathered under the reason it gave, each opened where its bytes
/// are kept so a person who disagrees can look.
struct DeclinedFilesView: View {
    let connection: Connection
    let source: KnowledgeSource
    @State private var files: [DeclinedAttachment]?
    @State private var previewAddress: URL?
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        List {
            if let files, files.isEmpty {
                Text("It has decided against nothing here yet.").foregroundStyle(.secondary)
            }
            ForEach(Array(DeclinedAttachment.groupedByReason(files ?? []).enumerated()), id: \.offset) { _, group in
                Section {
                    ForEach(group.files) { file in
                        VStack(alignment: .leading, spacing: 2) {
                            if file.path.isEmpty {
                                Text(verbatim: file.name)
                            } else {
                                Button {
                                    Task { await preview(file) }
                                } label: {
                                    Label(file.name, systemImage: "doc")
                                        .actionColor()
                                }
                            }
                            if !file.placeText.isEmpty {
                                Text(verbatim: file.placeText).font(.caption).foregroundStyle(.secondary)
                            }
                        }
                    }
                } header: {
                    Text(verbatim: group.declinedReason).textCase(nil)
                }
            }
        }
        .navigationTitle("Files it decided against opening")
        .navigationBarTitleDisplayMode(.inline)
        .overlay { if files == nil { ProgressView() } }
        .task { await load() }
        .refreshable { await load() }
        .quickLookPreview($previewAddress)
        .toast(done: $doneText, problem: $problemText)
    }

    private func load() async {
        do {
            files = try await connection.declinedAttachments(sourceId: source.id)
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func preview(_ file: DeclinedAttachment) async {
        do {
            let fileData = try await connection.download(file.path)
            let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
            try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
            let fileName = file.name.isEmpty ? "file" : file.name.replacingOccurrences(of: "/", with: "-")
            let address = folder.appendingPathComponent(fileName)
            try fileData.write(to: address)
            previewAddress = address
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// KnowledgeSourceEditor adds a source or changes one, as the dashboard's
/// dialog: an installed type and what it asks for, or a sort of place, then
/// where what it finds is filed and how often it is read. What a source was
/// added as, and the computer it is on, are shown but cannot be changed.
struct KnowledgeSourceEditor: View {
    let connection: Connection
    let source: KnowledgeSource?
    let mailboxes: [Mailbox]
    let onSaved: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var sourceTypes: [KnowledgeSourceType]
    @State private var name: String
    @State private var shape: KnowledgeSource.Shape
    @State private var computer: String
    @State private var path: String
    @State private var rootPath: String
    @State private var cron: String
    @State private var mailboxId: String
    @State private var isReadingEveryCheckout: Bool
    @State private var sourceTypeName: String
    @State private var draft: KnowledgeSourceType.Draft
    @State private var secretDrafts: [String: String] = [:]
    @State private var keptSecretKeys: Set<String> = []
    /// The source a save made, so a save tried again after a secret was
    /// refused saves that source rather than making a second.
    @State private var madeSourceId: String?
    @State private var computerNames: [String] = []
    @State private var isSaving = false
    @State private var doneText: String?
    @State private var problemText: String?

    init(
        connection: Connection, source: KnowledgeSource?, sourceTypes: [KnowledgeSourceType], mailboxes: [Mailbox],
        onSaved: @escaping (String) -> Void
    ) {
        self.connection = connection
        self.source = source
        self.mailboxes = mailboxes
        self.onSaved = onSaved
        _sourceTypes = State(initialValue: sourceTypes)
        _name = State(initialValue: source?.name ?? "")
        _shape = State(initialValue: source?.shape ?? .files)
        _computer = State(initialValue: source?.specification.computer ?? "")
        _path = State(initialValue: source?.specification.path ?? "")
        _rootPath = State(initialValue: source?.rootPath ?? "")
        _cron = State(initialValue: source?.cron ?? "")
        _mailboxId = State(initialValue: source?.specification.mailboxId ?? "")
        _isReadingEveryCheckout = State(initialValue: source?.specification.readEveryCheckout ?? false)
        let typeName = source?.specification.type ?? KnowledgeSourceType.offered(sourceTypes).first?.name ?? ""
        _sourceTypeName = State(initialValue: typeName)
        let chosen = sourceTypes.first { $0.name == typeName }
        _draft = State(initialValue: chosen?.draft(from: source?.specification.storedSettings ?? [:]) ?? KnowledgeSourceType.Draft())
    }

    /// A mailbox as a person knows it: its name, its first address, or its
    /// id.
    static func mailboxLabel(_ mailbox: Mailbox) -> String {
        if !mailbox.name.isEmpty { return mailbox.name }
        return mailbox.sendingAddresses.first ?? mailbox.id
    }

    private var isEditing: Bool { source != nil }
    private var offeredTypes: [KnowledgeSourceType] { KnowledgeSourceType.offered(sourceTypes) }
    private var chosenType: KnowledgeSourceType? { sourceTypes.first { $0.name == sourceTypeName } }
    /// The mailbox a sent source reads: the one chosen, or the first.
    private var readingMailboxId: String { mailboxId.isEmpty ? mailboxes.first?.id ?? "" : mailboxId }

    var body: some View {
        NavigationStack {
            Form {
                if !isEditing && !offeredTypes.isEmpty {
                    Section("What it is") {
                        Picker("What it is", selection: $sourceTypeName) {
                            ForEach(offeredTypes) { sourceType in
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(verbatim: sourceType.isLocal ? "\(sourceType.name) · \(String(localized: "local"))" : sourceType.name)
                                    if !sourceType.description.isEmpty {
                                        Text(verbatim: sourceType.description).font(.caption).foregroundStyle(.secondary)
                                    }
                                }
                                .tag(sourceType.name)
                            }
                        }
                        .pickerStyle(.inline)
                        .labelsHidden()
                    }
                }
                Section("What to call it") {
                    if let source, source.kind == "recording" {
                        Text(RecordingWords.sourceName(source))
                    } else {
                        TextField("What to call it", text: $name)
                    }
                }
                if source?.kind != "recording" {
                    if let chosenType {
                        typedSections(chosenType)
                    } else {
                        plainSections
                    }
                    Section {
                        TextField("Where what it finds is filed", text: $rootPath, prompt: Text(verbatim: "projects"))
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                    } header: {
                        Text("Where what it finds is filed")
                    } footer: {
                        Text("A page in the graph, such as projects. Leave it empty and your agent files what it finds where it thinks it belongs.")
                    }
                }
                Section {
                    TextField("How often to read it, as a cron line", text: $cron, prompt: Text(verbatim: "17 3 * * *"))
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .fontDesign(.monospaced)
                } header: {
                    Text("How often to read it, as a cron line")
                } footer: {
                    VStack(alignment: .leading, spacing: 6) {
                        if source?.kind != "recording" {
                            Text("Five fields, in your own zone. Leave it empty and it is read once a day.")
                        }
                        if source?.kind == "recording" {
                            Text("Five fields (minute hour day month weekday). Leave empty for manual reading.")
                        } else if isEditing {
                            Text("A box you empty keeps what it already says; these can be changed here but not cleared.")
                        }
                    }
                }
            }
            .navigationTitle(isEditing ? LocalizedStringKey("Edit this source") : "Add a source")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(isEditing ? LocalizedStringKey("Save") : "Add a source") { Task { await save() } }
                        .disabled(!canSave || isSaving)
                }
            }
            .onChange(of: sourceTypeName) { _, chosenName in choose(chosenName) }
            .task { await prepare() }
            .toast(done: $doneText, problem: $problemText)
        }
    }

    @ViewBuilder
    private func typedSections(_ sourceType: KnowledgeSourceType) -> some View {
        if isEditing {
            Section {
                LabeledContent("What sort of place", value: sourceType.name)
            }
        }
        if sourceType.isReadOnComputer {
            Section {
                computerField
            } header: {
                Text("Which computer")
            } footer: {
                if !sourceType.requires.isEmpty {
                    Text("Needs \(sourceType.requires.joined(separator: ", ")) installed on that computer.")
                }
            }
        }
        ForEach(sourceType.settings, id: \.name) { setting in
            Section {
                if sourceType.isSentMail && setting.name == "mailbox" {
                    mailboxPicker
                } else {
                    settingField(setting)
                }
            } footer: {
                settingHelp(setting)
            }
        }
        ForEach(sourceType.secrets, id: \.key) { secret in
            Section {
                SecureField(
                    secretLabel(secret), text: Binding(get: { secretDrafts[secret.key] ?? "" }, set: { secretDrafts[secret.key] = $0 }),
                    prompt: keptSecretKeys.contains(secret.key) ? Text("Kept; type here to replace it") : Text(verbatim: secretLabel(secret))
                )
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            } header: {
                Text(verbatim: secretLabel(secret))
            } footer: {
                Text(
                    verbatim: (secret.description.isEmpty ? "" : KnowledgeSourceType.asSentence(secret.description) + " ")
                        + String(localized: "Kept sealed on the server and sent only to the computer that reads this source; it is never shown again."))
            }
        }
    }

    @ViewBuilder
    private var plainSections: some View {
        Section {
            if isEditing {
                LabeledContent("What sort of place", value: UpkeepWords.shapeName(shape))
            } else {
                Picker("What sort of place", selection: $shape) {
                    ForEach(KnowledgeSource.Shape.allCases, id: \.self) { offered in
                        Text(UpkeepWords.shapeName(offered)).tag(offered)
                    }
                }
            }
        } footer: {
            if shape == .records {
                Text("A folder of JSON-lines records, filled by a refresh script; ask the agent to write one.")
            }
        }
        if shape == .sent {
            Section {
                mailboxPicker
            }
        } else {
            Section("Which computer") {
                computerField
            }
            Section("Where on it") {
                TextField("Where on it", text: $path, prompt: Text(verbatim: "~/projects"))
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                if shape == .files {
                    Toggle("Read the files of every checkout, including the ones barely any of which is your work", isOn: $isReadingEveryCheckout)
                }
            }
        }
    }

    /// Which computer, typed, with the ones attached offered from a menu; a
    /// computer not attached now can still be named. Fixed once the source
    /// exists.
    @ViewBuilder
    private var computerField: some View {
        if isEditing {
            LabeledContent("Which computer", value: computer)
        } else {
            HStack {
                TextField("Which computer", text: $computer)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                if !computerNames.isEmpty {
                    Menu {
                        ForEach(computerNames, id: \.self) { computerName in
                            Button(computerName) { computer = computerName }
                        }
                    } label: {
                        Image(systemName: "desktopcomputer")
                    }
                    .accessibilityLabel("Attached computers")
                }
            }
        }
    }

    private var mailboxPicker: some View {
        Picker("Which mailbox", selection: Binding(get: { readingMailboxId }, set: { mailboxId = $0 })) {
            if mailboxes.isEmpty {
                Text("You have no mailboxes yet.").tag("")
            }
            ForEach(mailboxes) { mailbox in
                Text(verbatim: Self.mailboxLabel(mailbox)).tag(mailbox.id)
            }
        }
    }

    @ViewBuilder
    private func settingField(_ setting: KnowledgeSourceType.Setting) -> some View {
        let label = settingLabel(setting)
        let text = Binding(get: { draft.texts[setting.name] ?? "" }, set: { draft.texts[setting.name] = $0 })
        let placeholder = KnowledgeSourceType.text(of: setting.defaultValue).isEmpty && setting.settingType == "path"
            ? "~/notes" : KnowledgeSourceType.text(of: setting.defaultValue)
        if setting.isBoolean {
            Toggle(label, isOn: Binding(get: { draft.switches[setting.name] == true }, set: { draft.switches[setting.name] = $0 }))
        } else if let choices = setting.choices {
            Picker(
                label,
                selection: Binding(
                    get: { (draft.texts[setting.name] ?? "").isEmpty ? KnowledgeSourceType.text(of: setting.defaultValue) : draft.texts[setting.name] ?? "" },
                    set: { draft.texts[setting.name] = $0 })
            ) {
                if setting.isRequired && (draft.texts[setting.name] ?? "").isEmpty && KnowledgeSourceType.text(of: setting.defaultValue).isEmpty {
                    Text(verbatim: "").tag("")
                }
                ForEach(choices, id: \.self) { choice in
                    Text(verbatim: choice).tag(choice)
                }
            }
        } else {
            LabeledContent {
                TextField(label, text: text, prompt: Text(verbatim: placeholder.isEmpty ? label : placeholder))
                    .keyboardType(setting.settingType == "integer" ? .numberPad : .default)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .multilineTextAlignment(.trailing)
            } label: {
                Text(verbatim: label)
            }
        }
    }

    @ViewBuilder
    private func settingHelp(_ setting: KnowledgeSourceType.Setting) -> some View {
        let listHint = setting.settingType == "array" ? String(localized: "Separate several with commas.") : ""
        let described = setting.description.isEmpty ? "" : KnowledgeSourceType.asSentence(setting.description)
        let help = [described, listHint].filter { !$0.isEmpty }.joined(separator: " ")
        if !help.isEmpty {
            Text(verbatim: help)
        }
    }

    private func settingLabel(_ setting: KnowledgeSourceType.Setting) -> String {
        let label = KnowledgeSourceType.label(setting.name)
        return setting.isRequired ? "\(label) · \(String(localized: "required"))" : label
    }

    private func secretLabel(_ secret: KnowledgeSourceType.Secret) -> String {
        let label = KnowledgeSourceType.label(secret.key)
        return secret.isOptional ? label : "\(label) · \(String(localized: "required"))"
    }

    /// Whether Save can be pressed. A source of a type is saved only through
    /// its type's form, so one opened before the types arrived waits.
    private var canSave: Bool {
        if source?.kind == "recording" { return true }
        guard !name.trimmed.isEmpty else { return false }
        if isEditing, !(source?.specification.type ?? "").isEmpty, chosenType == nil { return false }
        if chosenType != nil { return true }
        if shape == .sent { return !readingMailboxId.isEmpty }
        return !path.trimmed.isEmpty && !computer.trimmed.isEmpty
    }

    private func choose(_ chosenName: String) {
        let sourceType = sourceTypes.first { $0.name == chosenName }
        draft = sourceType?.draft() ?? KnowledgeSourceType.Draft()
        secretDrafts = [:]
        // Another type's kept values are forgotten when the source is saved,
        // so they are not shown as kept for this one.
        if chosenName != source?.specification.type { keptSecretKeys = [] }
    }

    private func prepare() async {
        if sourceTypes.isEmpty, let read = try? await connection.knowledgeSourceTypes(), !read.isEmpty {
            sourceTypes = read
            if let source, let chosen = read.first(where: { $0.name == source.specification.type }) {
                draft = chosen.draft(from: source.specification.storedSettings)
            } else if source == nil, sourceTypeName.isEmpty {
                sourceTypeName = KnowledgeSourceType.offered(read).first?.name ?? ""
            }
        }
        if !isEditing {
            computerNames = ((try? await connection.attachedComputers()) ?? []).map(\.name)
        }
        if let source, !source.specification.type.isEmpty {
            let kept = (try? await connection.knowledgeSourceSecrets(source.id)) ?? []
            keptSecretKeys = Set(kept.filter(\.isSet).map(\.key))
        }
    }

    private func save() async {
        isSaving = true
        defer { isSaving = false }
        if let source, source.kind == "recording" {
            do {
                try await connection.saveRecordingSourceSchedule(source, cron: cron.trimmed)
                onSaved(String(localized: "Saved"))
                dismiss()
            } catch { problemText = error.localizedDescription }
            return
        }
        if let chosenType {
            await saveTyped(chosenType)
            return
        }
        let change = KnowledgeSourceChange(
            sourceId: source?.id, name: name.trimmed, kind: source?.kind ?? shape.kind, computer: source?.specification.computer ?? computer.trimmed,
            path: path.trimmed, format: source?.specification.format ?? shape.format, mailboxId: shape == .sent ? readingMailboxId : nil,
            rootPath: rootPath.trimmed.isEmpty ? nil : rootPath.trimmed, cron: cron.trimmed.isEmpty ? nil : cron.trimmed,
            readEveryCheckout: shape == .files ? isReadingEveryCheckout : nil)
        do {
            try await connection.saveKnowledgeSource(change)
            onSaved(String(localized: "Saved"))
            dismiss()
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// Saves a source of a type: its settings checked here first so the
    /// person hears which box is wrong, then the source, then each secret
    /// typed, one at a time; a box left empty keeps what was there.
    private func saveTyped(_ sourceType: KnowledgeSourceType) async {
        if let found = sourceType.firstProblem(in: draft, skipping: sourceType.isSentMail ? ["mailbox"] : []) {
            problemText = UpkeepWords.settingProblem(found.problem, settingName: found.setting.name)
            return
        }
        if sourceType.isReadOnComputer, !isEditing, computer.trimmed.isEmpty {
            problemText = String(localized: "Say which computer it is on.")
            return
        }
        if let missing = sourceType.secrets.first(where: {
            !$0.isOptional && !keptSecretKeys.contains($0.key) && (secretDrafts[$0.key] ?? "").trimmed.isEmpty
        }) {
            problemText = String(localized: "Fill in \(KnowledgeSourceType.label(missing.key)) for this source.")
            return
        }
        var settings = sourceType.values(from: draft)
        if sourceType.isSentMail { settings["mailbox"] = .string(readingMailboxId) }
        let change = KnowledgeSourceChange(
            sourceId: source?.id ?? madeSourceId, name: name.trimmed,
            computer: sourceType.isReadOnComputer ? (source?.specification.computer ?? computer.trimmed) : nil,
            rootPath: rootPath.trimmed.isEmpty ? nil : rootPath.trimmed, cron: cron.trimmed.isEmpty ? nil : cron.trimmed, type: sourceType.name,
            settings: settings)
        do {
            let savedId = try await connection.saveKnowledgeSource(change)
            madeSourceId = savedId
            for secret in sourceType.secrets {
                let secretValue = secretDrafts[secret.key] ?? ""
                if secretValue.trimmed.isEmpty { continue }
                try await connection.setKnowledgeSourceSecret(sourceId: savedId, key: secret.key, value: secretValue)
                secretDrafts[secret.key] = nil
                keptSecretKeys.insert(secret.key)
            }
            onSaved(String(localized: "Saved"))
            dismiss()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

// MARK: - Memory

/// AgentMemorySettingsView is the dashboard's Memory tab: the memory check
/// (the questions the person answered about themselves, how the agent
/// scored against them in each grading, and what it answered), what it
/// filed lately with a way to forget each, and what it learned from the
/// person's own hands.
struct AgentMemorySettingsView: View {
    let connection: Connection
    @State private var questions: [MemoryCheckQuestion]?
    @State private var runs: [MemoryCheckRun] = []
    @State private var learnedFacts: [AgentLearnedFact]?
    @State private var corrections: [AgentCorrection] = []
    @State private var isShowingDropped = false
    @State private var editing: MemoryCheckQuestion?
    @State private var forgetting: AgentLearnedFact?
    @State private var isAskingForCheck = false
    @State private var isAskingForGrading = false
    @State private var doneText: String?
    @State private var problemText: String?
    @State private var undo: (@MainActor () async -> Void)?

    private var hasRunInProgress: Bool { runs.contains { !$0.isFinished } }
    private var latestFinishedRun: MemoryCheckRun? { runs.first(where: \.isFinished) }

    var body: some View {
        List {
            Section {
                Button("Check with me now", systemImage: "questionmark.bubble") { Task { await askForCheck() } }
                    .actionColor()
                    .disabled(isAskingForCheck)
                if let questions, !questions.isEmpty {
                    Button(hasRunInProgress ? LocalizedStringKey("Grading") : "Grade now", systemImage: "checklist") { Task { await askForGrading() } }
                        .actionColor()
                        .disabled(isAskingForGrading || hasRunInProgress)
                }
            } header: {
                Text("Memory check")
            } footer: {
                Text("Questions about you that you have answered, and how well your agent answers them from what it remembers, from your sources, and from both.")
            }
            questionsSection
            if let questions, !questions.isEmpty || !runs.isEmpty {
                scoresSection
            }
            learnedSection
            if !corrections.isEmpty {
                Section {
                    ForEach(corrections) { correction in
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: correction.said)
                            Text(verbatim: UpkeepWords.when(correction.createdAt)).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                } header: {
                    Text("What it learned from you")
                } footer: {
                    Text("Recorded from your own hands (a message you filed elsewhere, a reply you did not let go) and shown to the next run as examples, for a while.")
                }
            }
        }
        .navigationTitle("Memory")
        .overlay { if questions == nil && learnedFacts == nil { ProgressView() } }
        .refreshable { await load() }
        .task {
            await load()
            // A grading under way is read again until it has its scores.
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(10))
                if Task.isCancelled { break }
                if hasRunInProgress, let read = try? await connection.memoryCheckRuns() { runs = read }
            }
        }
        .sheet(item: $editing) { question in
            MemoryCheckQuestionEditor(connection: connection, question: question) { said in
                doneText = said
                Task { await loadQuestions() }
            }
        }
        .toast(done: $doneText, problem: $problemText, undo: $undo)
    }

    @ViewBuilder
    private var questionsSection: some View {
        if let questions {
            if questions.isEmpty {
                Section {
                    Text("No questions yet. Your agent will ask you some once it knows enough about you, or you can start a check now.")
                        .foregroundStyle(.secondary)
                }
            } else {
                let shown = MemoryCheckQuestion.visible(questions, isShowingDropped: isShowingDropped)
                let droppedCount = questions.count - MemoryCheckQuestion.visible(questions, isShowingDropped: false).count
                Section {
                    Toggle("Show dropped questions (\(droppedCount))", isOn: $isShowingDropped)
                    if shown.isEmpty {
                        Text("Every question is dropped.").foregroundStyle(.secondary)
                    }
                    ForEach(shown) { question in
                        Button {
                            editing = question
                        } label: {
                            MemoryCheckQuestionRow(question: question)
                        }
                        .swipeActions {
                            if !question.isDropped {
                                Button("Drop", systemImage: "xmark.bin") { Task { await drop(question) } }
                                    .tint(.orange)
                            }
                        }
                    }
                } header: {
                    Text(shown.count == 1 ? String(localized: "\(shown.count) question") : String(localized: "\(shown.count) questions"))
                }
            }
        }
    }

    @ViewBuilder
    private var scoresSection: some View {
        Section {
            if let latestFinishedRun {
                ForEach(MemoryCheckScore.answerSources, id: \.self) { answerFrom in
                    MemoryCheckScoreRow(answerFrom: answerFrom, latestRun: latestFinishedRun, history: MemoryCheckRun.scoreHistory(runs, answerFrom: answerFrom))
                }
                Text(
                    "Last graded \(UpkeepWords.when(latestFinishedRun.startedAt)) · questions: \(latestFinishedRun.questionCount) · cost \(UpkeepWords.money(latestFinishedRun.cost))"
                )
                .font(.footnote)
                .foregroundStyle(.secondary)
            }
            if runs.isEmpty {
                Text("Your agent has not been graded yet.").foregroundStyle(.secondary)
            }
            ForEach(runs) { run in
                if run.isFinished {
                    NavigationLink {
                        MemoryCheckAnswersView(connection: connection, run: run, questions: questions ?? [])
                    } label: {
                        MemoryCheckRunRow(run: run)
                    }
                } else {
                    MemoryCheckRunRow(run: run)
                }
            }
        } header: {
            Text("Scores")
        } footer: {
            Text("How your agent scored in each grading, answering from what it remembers, from your sources, and from both. A partial answer counts as half.")
        }
    }

    @ViewBuilder
    private var learnedSection: some View {
        Section {
            if let learnedFacts, learnedFacts.isEmpty {
                Text("Nothing filed in the last day.").foregroundStyle(.secondary)
            }
            ForEach(learnedFacts ?? []) { learned in
                VStack(alignment: .leading, spacing: 3) {
                    Text(verbatim: learned.fact.text)
                    HStack(spacing: 8) {
                        Text(verbatim: "\(learned.path)#\(learned.fact.number)").font(.caption.monospaced()).foregroundStyle(.secondary)
                        if learned.fact.inferred { UpkeepTag(text: String(localized: "Worked out, not told"), tone: .warning) }
                    }
                    if let quote = learned.fact.evidence.first?.quote, !quote.isEmpty {
                        Text(verbatim: "“\(quote)”").font(.footnote).foregroundStyle(.secondary)
                    }
                }
                .confirmationDialog(
                    "Forget", isPresented: Binding(get: { forgetting?.id == learned.id }, set: { if !$0 { forgetting = nil } }), titleVisibility: .visible,
                    presenting: forgetting
                ) { learned in
                    Button("Forget", role: .destructive) { Task { await forget(learned) } }
                } message: { learned in
                    Text("Forget this, and show it to your agent as an example of what not to keep: \"\(learned.fact.text)\"")
                }
                .swipeActions {
                    Button("Forget", systemImage: "trash", role: .destructive) { forgetting = learned }
                        .tint(.red)
                }
            }
        } header: {
            Text("Learned lately")
        } footer: {
            Text("What your agent filed after your recent conversations, newest first. Strike anything it should not have kept.")
        }
    }

    private func load() async {
        let connection = connection
        async let runsRead = try? connection.memoryCheckRuns()
        async let learnedRead = try? connection.learnedFacts()
        async let correctionsRead = try? connection.agentCorrections()
        await loadQuestions()
        runs = await runsRead ?? runs
        if let read = await learnedRead {
            learnedFacts = read
        } else if learnedFacts == nil {
            learnedFacts = []
        }
        corrections = await correctionsRead ?? corrections
    }

    private func loadQuestions() async {
        do {
            questions = try await connection.memoryCheckQuestions()
        } catch {
            problemText = error.localizedDescription
            if questions == nil { questions = [] }
        }
    }

    /// Starts a check in the main conversation at once, outside the rules
    /// about when the agent may speak first.
    private func askForCheck() async {
        isAskingForCheck = true
        defer { isAskingForCheck = false }
        do {
            try await connection.startMemoryCheck()
            doneText = String(localized: "Your agent is starting a memory check in your conversation.")
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func askForGrading() async {
        isAskingForGrading = true
        defer { isAskingForGrading = false }
        do {
            try await connection.gradeMemoryNow()
            doneText = String(localized: "Grading starts within the minute.")
            runs = (try? await connection.memoryCheckRuns()) ?? runs
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// Drops a question. It keeps the row and can be taken back, so it asks
    /// nothing first; the toast carries the way back.
    private func drop(_ question: MemoryCheckQuestion) async {
        do {
            try await connection.updateMemoryCheckQuestion(question.id, change: MemoryCheckQuestionChange(questionState: "dropped"))
            await loadQuestions()
            let earlierState = question.questionState
            undo = {
                do {
                    try await connection.updateMemoryCheckQuestion(question.id, change: MemoryCheckQuestionChange(questionState: earlierState))
                    await loadQuestions()
                } catch {
                    problemText = error.localizedDescription
                }
            }
            doneText = String(localized: "The question is dropped.")
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func forget(_ learned: AgentLearnedFact) async {
        do {
            try await connection.deleteKnowledgeFact(path: learned.path, number: learned.fact.number)
            doneText = String(localized: "Forgotten")
            learnedFacts = try? await connection.learnedFacts()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// MemoryCheckWords is how the memory check names its fixed vocabularies.
enum MemoryCheckWords {
    static func questionKind(_ questionKind: String) -> String {
        switch questionKind {
        case "direct": String(localized: "Direct")
        case "paraphrase": String(localized: "Reworded")
        case "changed": String(localized: "Changed")
        case "multihop": String(localized: "Two facts")
        case "abstain": String(localized: "Should not know")
        default: questionKind
        }
    }

    static func questionState(_ questionState: String) -> String {
        switch questionState {
        case "asked": String(localized: "Asked")
        case "confirmed": String(localized: "Confirmed")
        case "corrected": String(localized: "Corrected")
        case "dropped": String(localized: "Dropped")
        case "unsure": String(localized: "Unsure")
        default: questionState
        }
    }

    static func stateTone(_ questionState: String) -> UpkeepTone {
        switch questionState {
        case "confirmed", "corrected": .good
        case "asked", "unsure": .warning
        default: .plain
        }
    }

    static func answerSource(_ answerFrom: String) -> String {
        switch answerFrom {
        case "memory": String(localized: "Memory")
        case "sources": String(localized: "Sources")
        case "both": String(localized: "Both")
        default: answerFrom
        }
    }

    static func verdict(_ answerVerdict: String) -> String {
        switch answerVerdict {
        case "correct": String(localized: "Correct")
        case "partial": String(localized: "Partial")
        case "not_known": String(localized: "Not known")
        case "missed": String(localized: "Missed")
        case "stale": String(localized: "Out of date")
        case "invented": String(localized: "Invented")
        case "wrong": String(localized: "Wrong")
        case "ungraded": String(localized: "Ungraded")
        default: answerVerdict
        }
    }

    static func verdictTone(_ answer: MemoryCheckAnswer) -> UpkeepTone {
        if answer.isRight { return .good }
        if answer.answerVerdict == "partial" { return .warning }
        if answer.isMissedOrWrong { return .bad }
        return .plain
    }

    static func percent(_ scorePercent: Double) -> String {
        "\(Int(scorePercent.rounded()))%"
    }
}

/// MemoryCheckQuestionRow is a question: its words, its kind and state, the
/// answer on record, and when the person answered it.
struct MemoryCheckQuestionRow: View {
    let question: MemoryCheckQuestion

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: question.questionText).foregroundStyle(Color(.label))
            HStack(spacing: 8) {
                UpkeepTag(text: MemoryCheckWords.questionKind(question.questionKind))
                UpkeepTag(text: MemoryCheckWords.questionState(question.questionState), tone: MemoryCheckWords.stateTone(question.questionState))
                if question.isAnswerFiledAfter { UpkeepTag(text: String(localized: "Filed afterwards"), tone: .warning) }
            }
            Group {
                Text(verbatim: question.expectedAnswer)
                if question.questionKind == "changed", let outdated = question.outdatedAnswer, !outdated.isEmpty {
                    Text("was: \(outdated)")
                }
                if question.answeredAt != nil {
                    Text(verbatim: UpkeepWords.relative(question.answeredAt))
                }
            }
            .font(.footnote)
            .foregroundStyle(Color(.secondaryLabel))
        }
    }
}

/// MemoryCheckScoreRow is one source's latest score, its line over the
/// gradings shown, and how far it moved since the first of them.
struct MemoryCheckScoreRow: View {
    let answerFrom: String
    let latestRun: MemoryCheckRun
    let history: [MemoryCheckRun.MemoryCheckPoint]
    /// Where a finger is on the score line.
    @State private var selectedDate: Date?

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            LabeledContent(MemoryCheckWords.answerSource(answerFrom)) {
                if let score = latestRun.score(from: answerFrom) {
                    Text(verbatim: MemoryCheckWords.percent(score.scorePercent)).monospacedDigit().fontWeight(.semibold)
                } else {
                    Text("Not graded").foregroundStyle(.secondary)
                }
            }
            // The callout of a touched grading takes this line's place.
            .opacity(selectedDate == nil ? 1 : 0)
            if !history.isEmpty {
                scoreChart
            }
            if let changeText {
                Text(verbatim: changeText).font(.footnote).foregroundStyle(.secondary)
            }
            if let score = latestRun.score(from: answerFrom), score.filedAfterCount > 0 {
                Text("\(MemoryCheckWords.percent(score.scorePercentWithoutFiledAfter)) without the questions filed afterwards (\(score.filedAfterCount))")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
    }

    /// The score over the gradings shown, on the whole of zero to a
    /// hundred, so a line that moved from 70 to 72 looks as flat as it is.
    /// Touching it shows the nearest grading's day and score, in place of
    /// the line above it.
    private var scoreChart: some View {
        let gradedDates = history.map { ServerTime.date($0.startedAt) ?? .now }
        let selectedIndex = selectedDate.flatMap { ChartShares.nearestIndex(to: $0, in: gradedDates) }
        let lineColor = ChartPalette.seriesColor(at: 0)
        return Chart {
            ForEach(Array(history.enumerated()), id: \.element.id) { index, point in
                LineMark(x: .value("Graded", gradedDates[index]), y: .value("Score", point.scorePercent))
                    .foregroundStyle(lineColor)
                PointMark(x: .value("Graded", gradedDates[index]), y: .value("Score", point.scorePercent))
                    .foregroundStyle(lineColor)
                    .symbolSize(index == selectedIndex ? 60 : 20)
            }
            if let selectedIndex {
                ChartSelectionRule(value: gradedDates[selectedIndex]) {
                    ChartCallout(
                        title: gradedDates[selectedIndex].formatted(.dateTime.month(.abbreviated).day()),
                        figures: [ChartFigure(valueText: MemoryCheckWords.percent(history[selectedIndex].scorePercent))])
                }
            }
        }
        .chartYScale(domain: 0...100)
        .chartXAxis(.hidden)
        .chartYAxis {
            AxisMarks(values: [0.0, 50.0, 100.0]) { value in
                AxisGridLine()
                AxisValueLabel {
                    if let scorePercent = value.as(Double.self) { Text(verbatim: MemoryCheckWords.percent(scorePercent)) }
                }
            }
        }
        .chartXSelection(value: $selectedDate)
        .keepsSidePanelClosed()
        .frame(height: 96)
        .padding(.top, 4)
        .accessibilityLabel(Text(verbatim: chartDescription))
    }

    private var chartDescription: String {
        let points = history.map { point in
            "\(ServerTime.date(point.startedAt)?.formatted(.dateTime.month(.abbreviated).day()) ?? "") \(MemoryCheckWords.percent(point.scorePercent))"
        }
        return "\(MemoryCheckWords.answerSource(answerFrom)): \(points.joined(separator: ", "))"
    }

    private var changeText: String? {
        guard let points = MemoryCheckRun.scoreChangePoints(history), let first = history.first else { return nil }
        let day = ServerTime.date(first.startedAt)?.formatted(.dateTime.month(.abbreviated).day()) ?? ""
        if points > 0 {
            return points == 1 ? String(localized: "Up \(points) point since \(day)") : String(localized: "Up \(points) points since \(day)")
        }
        if points < 0 {
            return -points == 1 ? String(localized: "Down \(-points) point since \(day)") : String(localized: "Down \(-points) points since \(day)")
        }
        return String(localized: "Unchanged since \(day)")
    }
}

/// MemoryCheckRunRow is one grading: when, how many questions, each
/// source's score, and what it cost.
struct MemoryCheckRunRow: View {
    let run: MemoryCheckRun

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                Text(verbatim: UpkeepWords.when(run.startedAt)).foregroundStyle(Color(.label))
                if !run.isFinished { UpkeepTag(text: String(localized: "Grading"), tone: .good) }
            }
            Text(verbatim: details).font(.footnote).foregroundStyle(Color(.secondaryLabel))
        }
    }

    private var details: String {
        var parts = [String(localized: "Questions: \(run.questionCount)")]
        if run.isFinished {
            for answerFrom in MemoryCheckScore.answerSources {
                if let score = run.score(from: answerFrom) {
                    parts.append("\(MemoryCheckWords.answerSource(answerFrom)) \(MemoryCheckWords.percent(score.scorePercent))")
                }
            }
        }
        if run.cost > 0 { parts.append(UpkeepWords.money(run.cost)) }
        return parts.joined(separator: " · ")
    }
}

/// MemoryCheckAnswersView is what the agent answered in one grading, to
/// each question from each source, beside the answer on record and what
/// the grader made of it; the missed and wrong ones first by default.
struct MemoryCheckAnswersView: View {
    let connection: Connection
    let run: MemoryCheckRun
    let questions: [MemoryCheckQuestion]
    @State private var answers: [MemoryCheckAnswer]?
    @State private var isOnlyMissedOrWrong = true
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        let questionsById = Dictionary(questions.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        let shown = (answers ?? []).filter { !isOnlyMissedOrWrong || $0.isMissedOrWrong }
        List {
            Section {
                Toggle("Only the missed and wrong ones", isOn: $isOnlyMissedOrWrong)
            } footer: {
                Text("What your agent answered to each question, from each source, and what the grader made of it.")
            }
            Section {
                if answers != nil && shown.isEmpty {
                    Text(isOnlyMissedOrWrong ? LocalizedStringKey("Nothing was missed or wrong in this grading.") : "This grading has no answers.")
                        .foregroundStyle(.secondary)
                }
                ForEach(shown) { answer in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(verbatim: questionsById[answer.questionId]?.questionText ?? "")
                        HStack(spacing: 8) {
                            UpkeepTag(text: MemoryCheckWords.verdict(answer.answerVerdict), tone: MemoryCheckWords.verdictTone(answer))
                            UpkeepTag(text: MemoryCheckWords.answerSource(answer.answerFrom))
                        }
                        labeled("Answer on record", questionsById[answer.questionId]?.expectedAnswer ?? "")
                        labeled("Agent's answer", answer.answerText)
                        labeled("Why", answer.verdictReason)
                    }
                }
            }
        }
        .navigationTitle(Text("Answers of the grading at \(UpkeepWords.when(run.startedAt))"))
        .navigationBarTitleDisplayMode(.inline)
        .overlay { if answers == nil { ProgressView() } }
        .task { await load() }
        .refreshable { await load() }
        .toast(done: $doneText, problem: $problemText)
    }

    @ViewBuilder
    private func labeled(_ title: LocalizedStringKey, _ text: String) -> some View {
        if !text.isEmpty {
            VStack(alignment: .leading, spacing: 1) {
                Text(title).font(.caption).foregroundStyle(.secondary)
                Text(verbatim: text).font(.footnote)
            }
        }
    }

    private func load() async {
        do {
            answers = try await connection.memoryCheckAnswers(runId: run.id)
        } catch {
            problemText = error.localizedDescription
            if answers == nil { answers = [] }
        }
    }
}

/// MemoryCheckQuestionEditor changes a question: its words, the answer on
/// record, its kind, what used to be true for a question about a change,
/// and its state. Only what changed is sent.
struct MemoryCheckQuestionEditor: View {
    let connection: Connection
    let question: MemoryCheckQuestion
    let onSaved: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var questionText: String
    @State private var expectedAnswer: String
    @State private var questionKind: String
    @State private var outdatedAnswer: String
    @State private var questionState: String
    @State private var isSaving = false
    @State private var doneText: String?
    @State private var problemText: String?

    init(connection: Connection, question: MemoryCheckQuestion, onSaved: @escaping (String) -> Void) {
        self.connection = connection
        self.question = question
        self.onSaved = onSaved
        _questionText = State(initialValue: question.questionText)
        _expectedAnswer = State(initialValue: question.expectedAnswer)
        _questionKind = State(initialValue: question.questionKind)
        _outdatedAnswer = State(initialValue: question.outdatedAnswer ?? "")
        _questionState = State(initialValue: question.questionState)
    }

    var body: some View {
        NavigationStack {
            Form {
                Section("Question") {
                    TextField("Question", text: $questionText, axis: .vertical).lineLimit(2...)
                }
                Section("Answer on record") {
                    TextField("Answer on record", text: $expectedAnswer, axis: .vertical).lineLimit(2...)
                }
                Section {
                    Picker("Kind", selection: $questionKind) {
                        ForEach(MemoryCheckQuestion.questionKinds, id: \.self) { offeredKind in
                            Text(MemoryCheckWords.questionKind(offeredKind)).tag(offeredKind)
                        }
                    }
                    // What used to be true matters only to a question about a
                    // change: an answer that gives it is graded out of date.
                    if questionKind == "changed" {
                        TextField("What used to be true", text: $outdatedAnswer, axis: .vertical).lineLimit(2...)
                    }
                }
                Section {
                    Picker("State", selection: $questionState) {
                        ForEach(MemoryCheckQuestion.questionStates, id: \.self) { offeredState in
                            Text(MemoryCheckWords.questionState(offeredState)).tag(offeredState)
                        }
                    }
                } footer: {
                    Text("Only confirmed and corrected questions are graded.")
                }
            }
            .navigationTitle("Change a question")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { Task { await save() } }
                        .disabled(questionText.trimmed.isEmpty || isSaving)
                }
            }
            .toast(done: $doneText, problem: $problemText)
        }
    }

    private func save() async {
        isSaving = true
        defer { isSaving = false }
        let change = MemoryCheckQuestionChange(
            from: question,
            to: MemoryCheckQuestionChange(
                questionKind: questionKind, questionText: questionText, expectedAnswer: expectedAnswer, outdatedAnswer: outdatedAnswer,
                questionState: questionState))
        do {
            if !change.isEmpty { try await connection.updateMemoryCheckQuestion(question.id, change: change) }
            onSaved(String(localized: "The question is saved."))
            dismiss()
        } catch {
            // The editor stays open with what was typed still in it.
            problemText = error.localizedDescription
        }
    }
}

// MARK: - Dreams

/// DreamsView is the dashboard's Dreams tab: how far the reading has got
/// and at what pace, bootstrapping, the hours a dream may start in, asking
/// for one now, putting back what a dream marked read without reading it,
/// and every dream with what it did, each opening its runs.
struct DreamsView: View {
    let connection: Connection
    @State private var preferences: AgentUpkeepPreferences?
    @State private var isAgentRead = false
    @State private var isAgentMissing = false
    @State private var dreams: [AgentDream]?
    @State private var progress: ReadingProgress?
    @State private var isReadingProgress = true
    @State private var dreamState: AgentDreamState?
    @State private var dreamFrom = "01:00"
    @State private var dreamUntil = "06:00"
    @State private var isStarting = false
    @State private var isAskingReread = false
    @State private var rereadMinutes = "60"
    @State private var isRereading = false
    @State private var isSwitchingBootstrap = false
    @State private var doneText: String?
    @State private var problemText: String?

    private var isBootstrapping: Bool { preferences?.dreamBootstrap == true }
    private var dreamJobStatus: String { dreamState?.dreamJobStatus ?? "" }

    var body: some View {
        List {
            if isAgentMissing {
                ContentUnavailableView("Your agent is off", systemImage: "moon.zzz", description: Text("Turn it on in its overview first."))
            } else {
                bootstrapSection
                Section {
                    DatePicker(
                        "Earliest a dream may start", selection: Binding(get: { UpkeepWords.clockDate(dreamFrom) }, set: { dreamFrom = UpkeepWords.clockText($0) }),
                        displayedComponents: .hourAndMinute)
                    DatePicker(
                        "Latest a dream may start", selection: Binding(get: { UpkeepWords.clockDate(dreamUntil) }, set: { dreamUntil = UpkeepWords.clockText($0) }),
                        displayedComponents: .hourAndMinute)
                } header: {
                    Text("When it may dream")
                } footer: {
                    Text("The hours of the day, in your zone, when a dream may start; it also waits until you have been away half an hour. The same time at both ends means any time.")
                }
                .disabled(preferences == nil)
                dreamsSection
            }
        }
        .navigationTitle("Dreams")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button(dreamNowTitle, systemImage: "moon.stars") { Task { await dreamNow() } }
                    .disabled(isStarting || !dreamJobStatus.isEmpty || preferences == nil)
            }
            ToolbarItem(placement: .secondaryAction) {
                Button("Re-read…", systemImage: "arrow.uturn.backward") {
                    rereadMinutes = "60"
                    isAskingReread = true
                }
                .disabled(isRereading || preferences == nil)
            }
        }
        .overlay { if !isAgentRead { ProgressView() } }
        .refreshable { await load() }
        .task {
            await load()
            // A dream asked for waits for a worker, then runs: when that
            // changes the list is read again, so the new row appears and a
            // finished one gets its numbers.
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(10))
                if Task.isCancelled { break }
                guard let read = try? await connection.agentDreamState() else { continue }
                let hasChanged = read.dreamJobStatus != dreamJobStatus
                dreamState = read
                if hasChanged {
                    dreams = (try? await connection.agentDreams()) ?? dreams
                    if read.isIdle { progress = (try? await connection.readingProgress()) ?? progress }
                }
            }
        }
        .task(id: dreamFrom) { await saveHours(dreamFrom: dreamFrom) }
        .task(id: dreamUntil) { await saveHours(dreamUntil: dreamUntil) }
        .alert("Read again what a dream marked read", isPresented: $isAskingReread) {
            TextField("How far back, in minutes", text: $rereadMinutes).keyboardType(.numberPad)
            Button("Cancel", role: .cancel) {}
            Button("Put them back") { Task { await reread() } }
                .disabled((Int(rereadMinutes) ?? 0) <= 0)
        } message: {
            Text("A dream that marked documents read without reading them can be put right: everything it marked in the last so many minutes goes back into the queue.")
        }
        .toast(done: $doneText, problem: $problemText)
    }

    private var dreamNowTitle: String {
        if dreamState?.isQueued == true { return String(localized: "Queued") }
        if dreamState?.isRunning == true { return String(localized: "Dreaming") }
        return String(localized: "Dream now")
    }

    @ViewBuilder
    private var bootstrapSection: some View {
        Section {
            VStack(alignment: .leading, spacing: 6) {
                if let progress {
                    HStack {
                        ProgressView(value: Double(progress.readPercent), total: 100)
                        Text(verbatim: "\(progress.readPercent)%").font(.footnote).monospacedDigit().contentTransition(.numericText())
                    }
                    Text(verbatim: progressText(progress)).font(.footnote)
                    if progress.spent > 0 && progress.dreams > 0 {
                        let spentText = UpkeepWords.money(progress.spent, currency: progress.currency)
                        Text(
                            progress.dreams == 1
                                ? String(localized: "Measured over the last dream, which cost \(spentText).")
                                : String(localized: "Measured over the last \(progress.dreams) dreams, which cost \(spentText).")
                        )
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    }
                    Text(
                        progress.waiting > 0
                            ? String(
                                localized:
                                    "\(progress.waiting.formatted()) documents wait to be read. Bootstrapping dreams again at every tick, with wider limits, until they are read; best with a model of your own doing the reading."
                            )
                            : String(localized: "Nothing waits to be read.")
                    )
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                } else if isReadingProgress {
                    // Not yet is not nothing: no figure is said while it is
                    // still asked for.
                    ProgressView()
                }
            }
            Button(isBootstrapping ? LocalizedStringKey("Stop") : "Bootstrap", systemImage: isBootstrapping ? "stop.circle" : "bolt.circle") {
                Task { await setBootstrap(!isBootstrapping) }
            }
            .actionColor()
            .disabled(isSwitchingBootstrap || preferences == nil)
        } header: {
            HStack {
                Text("Bootstrap")
                if isBootstrapping {
                    UpkeepTag(text: String(localized: "Bootstrapping"), tone: .good)
                }
            }
        }
    }

    @ViewBuilder
    private var dreamsSection: some View {
        Section {
            if dreamState?.isQueued == true, let queuedAt = dreamState?.queuedAt {
                VStack(alignment: .leading, spacing: 2) {
                    UpkeepTag(text: String(localized: "Queued"), tone: .warning)
                    Text("Asked for at \(UpkeepWords.when(queuedAt)). It starts as soon as a worker is free, usually within a minute or two.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            if let dreams, dreams.isEmpty {
                Text("No dreams yet.").foregroundStyle(.secondary)
            }
            ForEach(dreams ?? []) { dream in
                if let jobId = dream.jobId, !jobId.isEmpty {
                    NavigationLink {
                        AgentActivityView(connection: connection, dreamJobId: jobId, dreamStartedAt: dream.startedAt)
                    } label: {
                        AgentDreamRow(dream: dream)
                    }
                } else {
                    AgentDreamRow(dream: dream)
                }
            }
        } header: {
            Text("What it dreams")
        } footer: {
            Text("While you are asleep your agent works through what arrived, writes up the month, and tidies what it knows. It never forgets anything on its own.")
        }
    }

    private func progressText(_ progress: ReadingProgress) -> String {
        guard progress.total > 0 else { return String(localized: "Nothing has been indexed yet.") }
        var parts = [String(localized: "\(progress.read.formatted()) of \(progress.total.formatted()) documents read")]
        if let span = progress.timeLeft {
            var left = String(localized: "about \(UpkeepWords.readingSpan(span)) left at \(Int(progress.perHour.rounded())) an hour")
            if !progress.bootstrapping { left += " " + String(localized: "if it dreamed without pause") }
            parts.append(left)
        }
        if progress.costLeft > 0 {
            parts.append(String(localized: "about \(UpkeepWords.money(progress.costLeft, currency: progress.currency)) more"))
        }
        return parts.joined(separator: " · ")
    }

    private func load() async {
        let connection = connection
        async let agentRead = connection.agentUpkeep()
        async let dreamsRead = try? connection.agentDreams()
        async let stateRead = try? connection.agentDreamState()
        async let progressRead = try? connection.readingProgress()
        do {
            preferences = try await agentRead
            isAgentMissing = preferences == nil
            if let preferences {
                dreamFrom = preferences.shownDreamFrom
                dreamUntil = preferences.shownDreamUntil
            }
        } catch {
            problemText = error.localizedDescription
        }
        isAgentRead = true
        dreams = await dreamsRead ?? dreams ?? []
        dreamState = await stateRead ?? dreamState
        progress = await progressRead ?? progress
        isReadingProgress = false
    }

    /// Saves a dream hour once the picker has settled on it.
    private func saveHours(dreamFrom chosenFrom: String? = nil, dreamUntil chosenUntil: String? = nil) async {
        guard let preferences else { return }
        if let chosenFrom, chosenFrom == preferences.shownDreamFrom { return }
        if let chosenUntil, chosenUntil == preferences.shownDreamUntil { return }
        try? await Task.sleep(for: .milliseconds(700))
        if Task.isCancelled { return }
        do {
            self.preferences = try await connection.updateAgentUpkeep(dreamFrom: chosenFrom, dreamUntil: chosenUntil) ?? preferences
            doneText = String(localized: "Saved")
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// Queues a dream at once, whatever its hours say: those are for the
    /// dreams nobody asked for.
    private func dreamNow() async {
        isStarting = true
        defer { isStarting = false }
        do {
            try await connection.dreamNow()
            doneText = String(localized: "The dream starts within the minute.")
            dreamState = (try? await connection.agentDreamState()) ?? dreamState
            dreams = (try? await connection.agentDreams()) ?? dreams
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func setBootstrap(_ isOn: Bool) async {
        isSwitchingBootstrap = true
        defer { isSwitchingBootstrap = false }
        do {
            try await connection.setDreamBootstrap(isOn)
            doneText =
                isOn
                ? String(localized: "Bootstrapping: a dream starts within the minute and runs again until nothing waits to be read.")
                : String(localized: "Dreaming is back to its hours.")
            preferences = (try? await connection.agentUpkeep()) ?? preferences
            dreams = (try? await connection.agentDreams()) ?? dreams
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func reread() async {
        guard let minutes = Int(rereadMinutes), minutes > 0 else { return }
        isRereading = true
        defer { isRereading = false }
        do {
            let putBack = try await connection.rereadDocuments(minutes: minutes)
            doneText =
                putBack == 1
                ? String(localized: "\(putBack) document is waiting to be read again.")
                : String(localized: "\(putBack) documents are waiting to be read again.")
            let connection = connection
            async let dreamsRead = try? connection.agentDreams()
            async let progressRead = try? connection.readingProgress()
            dreams = await dreamsRead ?? dreams
            progress = await progressRead ?? progress
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// AgentDreamRow is one dream: when it started and how long it took, then
/// what matters most, what it read, filed and cost; then, smaller, the rest
/// of what it did and what was still waiting.
struct AgentDreamRow: View {
    let dream: AgentDream

    /// What a dream did that its row leads with.
    private static let headlineKinds: Set<AgentDream.OutcomeKind> = [.read, .filed]

    var body: some View {
        // The duration is centered on the whole row, level with the
        // disclosure chevron, rather than on the first line.
        HStack(alignment: .center) {
            VStack(alignment: .leading, spacing: 3) {
                Text(verbatim: UpkeepWords.when(dream.startedAt)).foregroundStyle(Color(.label))
                if !headlineText.isEmpty {
                    Text(verbatim: headlineText).font(.footnote).foregroundStyle(Color(.secondaryLabel))
                }
                if let lastError = dream.lastError, !lastError.isEmpty {
                    Text(verbatim: lastError.asReasonSentence).font(.footnote).foregroundStyle(.red)
                }
                if !moreText.isEmpty {
                    Text(verbatim: moreText).font(.caption).foregroundStyle(Color(.secondaryLabel))
                }
            }
            Spacer()
            if let minutes = dream.minutesTaken {
                Text("\(minutes) min").font(.footnote.monospacedDigit()).foregroundStyle(Color(.secondaryLabel))
            } else {
                UpkeepTag(text: String(localized: "Still working."), tone: .good)
            }
        }
    }

    /// Read, filed and cost, or that there was nothing to do.
    private var headlineText: String {
        var parts = dream.outcomes.filter { Self.headlineKinds.contains($0.outcomeKind) }.map(UpkeepWords.dreamOutcome)
        if dream.cost > 0 { parts.append(String(localized: "Cost: \(UpkeepWords.money(dream.cost, currency: dream.currency))")) }
        if dream.outcomes.isEmpty, dream.finishedAt != nil {
            parts.insert((dream.lastError ?? "").isEmpty ? String(localized: "Nothing needed doing.") : String(localized: "Cut short."), at: 0)
        }
        return parts.joined(separator: " · ")
    }

    /// Everything else it counted, and what was still waiting.
    private var moreText: String {
        var parts = dream.outcomes.filter { !Self.headlineKinds.contains($0.outcomeKind) }.map(UpkeepWords.dreamOutcome)
        if dream.backlog > 0 { parts.append(String(localized: "Waiting: \(dream.backlog.formatted())")) }
        return parts.joined(separator: " · ")
    }
}

// MARK: - Schedules

/// SchedulesView is the dashboard's Schedules tab: the morning brief, set
/// by a time and days and sent now on asking, and every schedule, each
/// added, changed, run now, turned off or removed.
struct SchedulesView: View {
    let connection: Connection
    @State private var schedules: [AgentSchedule]?
    @State private var briefTime = "07:30"
    @State private var briefDayNumbers: Set<Int> = Set(1...5)
    @State private var editing: ScheduleEditing?
    @State private var removing: AgentSchedule?
    @State private var isBusy = false
    @State private var doneText: String?
    @State private var problemText: String?

    /// ScheduleEditing is the editor open on a schedule, or on a new one.
    struct ScheduleEditing: Identifiable {
        let id = UUID()
        let schedule: AgentSchedule?
    }

    private var brief: AgentSchedule? { DailyBrief.schedule(in: schedules ?? []) }

    /// Whether the time or the days differ from what the brief has.
    private var hasBriefChanged: Bool {
        briefTime != DailyBrief.time(fromCron: brief?.cron) || briefDayNumbers != Set(DailyBrief.dayNumbers(fromCron: brief?.cron))
    }

    var body: some View {
        List {
            briefSection
            Section {
                if let schedules, schedules.isEmpty {
                    Text("Nothing scheduled.").foregroundStyle(.secondary)
                }
                ForEach(schedules ?? []) { schedule in
                    Button {
                        editing = ScheduleEditing(schedule: schedule)
                    } label: {
                        AgentScheduleRow(schedule: schedule)
                    }
                    .confirmationDialog(
                        "Remove", isPresented: Binding(get: { removing?.id == schedule.id }, set: { if !$0 { removing = nil } }), titleVisibility: .hidden, presenting: removing
                    ) { schedule in
                        Button("Remove", role: .destructive) {
                            Task { await act(saying: nil) { try await connection.deleteAgentSchedule(schedule.id) } }
                        }
                    } message: { schedule in
                        Text(verbatim: schedule.name)
                    }
                    .swipeActions {
                        Button("Remove", systemImage: "trash", role: .destructive) { removing = schedule }
                            .tint(.red)
                        Button(schedule.enabled ? LocalizedStringKey("Turn off") : "Turn on", systemImage: schedule.enabled ? "pause" : "play") {
                            Task {
                                await act(saying: nil) { try await connection.saveAgentSchedule(scheduleId: schedule.id, isEnabled: !schedule.enabled) }
                            }
                        }
                        .tint(.orange)
                    }
                    .contextMenu {
                        Button("Edit", systemImage: "pencil") { editing = ScheduleEditing(schedule: schedule) }
                        runButton(schedule)
                        Button("Remove", systemImage: "trash", role: .destructive) { removing = schedule }
                            .tint(.red)
                    }
                }
            } header: {
                Text("On its own time")
            } footer: {
                Text("Things your agent does at set times, in your zone: a morning brief, a Friday round-up. A scheduled run can confirm nothing, so it only does what needs no confirmation.")
            }
        }
        .navigationTitle("Schedules")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button("Add a schedule", systemImage: "plus") { editing = ScheduleEditing(schedule: nil) }
            }
        }
        .overlay { if schedules == nil { ProgressView() } }
        .disabled(isBusy)
        .refreshable { await load() }
        .task { await load() }
        .sheet(item: $editing) { edited in
            AgentScheduleEditor(connection: connection, schedule: edited.schedule) {
                Task { await load() }
            }
        }
        .toast(done: $doneText, problem: $problemText)
    }

    private func runButton(_ schedule: AgentSchedule) -> some View {
        Button("Run now", systemImage: "play.circle") {
            Task {
                await act(saying: String(localized: "Queued; the answer arrives where the schedule says.")) {
                    try await connection.runAgentSchedule(schedule.id)
                }
            }
        }
    }

    @ViewBuilder
    private var briefSection: some View {
        Section {
            DatePicker(
                "At", selection: Binding(get: { UpkeepWords.clockDate(briefTime) }, set: { briefTime = UpkeepWords.clockText($0) }),
                displayedComponents: .hourAndMinute)
            WeekdayToggles(dayNumbers: $briefDayNumbers)
            if brief?.enabled == true {
                if hasBriefChanged {
                    Button("Save the new time and days", systemImage: "checkmark.circle") { Task { await saveBrief(isEnabled: true) } }
                        .actionColor()
                        .disabled(briefDayNumbers.isEmpty)
                        .fullWidthRowSeparator()
                }
                Button("Send one now", systemImage: "paperplane") {
                    Task { await act(saying: String(localized: "Sending one now; it will arrive in a moment.")) { try await connection.runDailyBrief() } }
                }
                .actionColor()
                .fullWidthRowSeparator()
                Button("Stop", role: .destructive) { Task { await saveBrief(isEnabled: false) } }
            } else {
                Button("Send me a brief", systemImage: "sun.max") { Task { await saveBrief(isEnabled: true) } }
                    .actionColor()
                    .disabled(briefDayNumbers.isEmpty)
            }
        } header: {
            Text("A brief each morning")
        } footer: {
            Text(
                brief == nil
                    ? LocalizedStringKey("What the day holds and what is waiting for you, sent to you as mail. Turning it on adds an ordinary schedule called “Daily brief” below, where you can change what it asks for.")
                    : "What the day holds and what is waiting for you, sent to you as mail. It is the schedule called “Daily brief” below, where you can change what it asks for."
            )
        }
    }

    private func load() async {
        do {
            let read = try await connection.agentSchedules()
            schedules = read
            let brief = DailyBrief.schedule(in: read)
            briefTime = DailyBrief.time(fromCron: brief?.cron)
            briefDayNumbers = Set(DailyBrief.dayNumbers(fromCron: brief?.cron))
        } catch {
            problemText = error.localizedDescription
            if schedules == nil { schedules = [] }
        }
    }

    private func saveBrief(isEnabled: Bool) async {
        let days = briefDayNumbers.sorted()
        await act(saying: isEnabled ? String(localized: "The brief is on.") : String(localized: "The brief is off. The schedule is still there.")) {
            try await connection.setDailyBrief(isEnabled: isEnabled, at: briefTime, days: days)
        }
    }

    private func act(saying said: String?, _ work: () async throws -> Void) async {
        isBusy = true
        defer { isBusy = false }
        do {
            try await work()
            if let said { doneText = said }
        } catch {
            problemText = error.localizedDescription
        }
        await load()
    }
}

/// AgentScheduleRow is a schedule: its name, whether it is off, when it
/// comes round in a person's words, what it asks, where the answer goes and
/// when it runs next.
struct AgentScheduleRow: View {
    let schedule: AgentSchedule

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 8) {
                Text(verbatim: schedule.name).foregroundStyle(schedule.enabled ? Color(.label) : Color(.secondaryLabel))
                if !schedule.enabled { UpkeepTag(text: String(localized: "off"), tone: .warning) }
            }
            if let timingText = UpkeepWords.scheduleTimingText(schedule.cron) {
                Text(verbatim: timingText).font(.subheadline).foregroundStyle(Color(.secondaryLabel))
            } else {
                Text(verbatim: schedule.cron).font(.subheadline.monospaced()).foregroundStyle(Color(.secondaryLabel))
            }
            Text(verbatim: schedule.prompt).font(.footnote).foregroundStyle(Color(.secondaryLabel)).lineLimit(3)
            Text(verbatim: whereAndWhen).font(.caption).foregroundStyle(Color(.secondaryLabel))
        }
    }

    private var whereAndWhen: String {
        let deliverText = schedule.isDeliveredByMail ? String(localized: "By mail") : String(localized: "Into the conversation")
        guard let next = schedule.nextRunAt, ServerTime.date(next) != nil else { return deliverText }
        return deliverText + " · " + String(localized: "next \(UpkeepWords.when(next))")
    }
}

/// WeekdayToggles is the days of the week something comes on, a toggle
/// each, in the order the person's calendar starts its week and named as
/// their locale names them. Short names in one row when they fit; at large
/// text sizes, a switch a day with the whole name.
struct WeekdayToggles: View {
    @Binding var dayNumbers: Set<Int>

    private var orderedDayNumbers: [Int] { ScheduleWeekday.weekOrder(firstWeekday: Calendar.current.firstWeekday) }

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 6) {
                ForEach(orderedDayNumbers, id: \.self) { dayNumber in
                    Toggle(isOn: isChosen(dayNumber)) {
                        Text(verbatim: ScheduleWeekday.shortName(dayNumber))
                            .lineLimit(1)
                            .frame(maxWidth: .infinity)
                    }
                    .accessibilityLabel(Text(verbatim: ScheduleWeekday.name(dayNumber)))
                }
            }
            .toggleStyle(.button)
            // Its own button each, so a tap in the row turns one day, not
            // every day of the row.
            .buttonStyle(.bordered)
            VStack(spacing: 12) {
                ForEach(orderedDayNumbers, id: \.self) { dayNumber in
                    Toggle(isOn: isChosen(dayNumber)) {
                        Text(verbatim: ScheduleWeekday.name(dayNumber))
                    }
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(Text("On days"))
        .sensoryFeedback(.selection, trigger: dayNumbers)
    }

    private func isChosen(_ dayNumber: Int) -> Binding<Bool> {
        Binding(
            get: { dayNumbers.contains(dayNumber) },
            set: { isOn in
                if isOn { dayNumbers.insert(dayNumber) } else { dayNumbers.remove(dayNumber) }
            })
    }
}

/// AgentScheduleEditor adds a schedule or changes one: its name, its cron
/// line, where the answer goes, and what to do. A new one starts on.
struct AgentScheduleEditor: View {
    let connection: Connection
    let schedule: AgentSchedule?
    let onSaved: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var name: String
    @State private var cron: String
    @State private var prompt: String
    @State private var deliver: String
    @State private var isSaving = false
    @State private var doneText: String?
    @State private var problemText: String?

    init(connection: Connection, schedule: AgentSchedule?, onSaved: @escaping () -> Void) {
        self.connection = connection
        self.schedule = schedule
        self.onSaved = onSaved
        _name = State(initialValue: schedule?.name ?? "")
        _cron = State(initialValue: schedule?.cron ?? "0 8 * * 1-5")
        _prompt = State(initialValue: schedule?.prompt ?? "")
        _deliver = State(initialValue: schedule?.deliver ?? "drawer")
    }

    var body: some View {
        NavigationStack {
            Form {
                Section("Name") {
                    TextField("Name", text: $name)
                }
                Section {
                    TextField("When, as a cron line", text: $cron, prompt: Text(verbatim: "0 8 * * 1-5"))
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .fontDesign(.monospaced)
                    if let timingText = UpkeepWords.scheduleTimingText(cron) {
                        Text(verbatim: timingText).font(.footnote).foregroundStyle(Color(.secondaryLabel))
                    }
                    Picker("Where the answer goes", selection: $deliver) {
                        Text("Into the conversation").tag("drawer")
                        Text("By mail").tag("mail")
                    }
                } header: {
                    Text("When, as a cron line")
                } footer: {
                    Text("Five fields (minute hour day month weekday) in your zone. 0 8 * * 1-5 is weekdays at eight; @at 2030-09-12 09:00 is once, at that moment; @in 20m is once, twenty minutes from now.")
                }
                Section("What to do, as you would say it") {
                    TextField("What to do, as you would say it", text: $prompt, axis: .vertical).lineLimit(3...)
                }
            }
            .navigationTitle(schedule == nil ? LocalizedStringKey("Add a schedule") : "Edit")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(schedule == nil ? LocalizedStringKey("Add") : "Save") { Task { await save() } }
                        .disabled(name.trimmed.isEmpty || cron.trimmed.isEmpty || prompt.trimmed.isEmpty || isSaving)
                }
            }
            .toast(done: $doneText, problem: $problemText)
        }
    }

    private func save() async {
        isSaving = true
        defer { isSaving = false }
        do {
            try await connection.saveAgentSchedule(
                scheduleId: schedule?.id, name: name.trimmed, cron: cron.trimmed, prompt: prompt.trimmed, deliver: deliver,
                isEnabled: schedule == nil ? true : nil)
            onSaved()
            dismiss()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

// MARK: - Alerts

/// AddFollowUpSheet is the form for having the agent follow something by
/// hand, with the fields the dashboard's Add dialog has: a kind, what it
/// is, the day, and what the kind has beyond that.
struct AddFollowUpSheet: View {
    let connection: Connection
    /// A kind in words, as the follow-ups list names it.
    let kindName: (String) -> String
    let onAdded: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var followUp = NewAgentFollowUp()
    @State private var day = Date()
    @State private var hasDay = false
    @State private var isSaving = false
    @State private var problemText: String?

    /// The day is set on purpose, as the dashboard asks it to be typed: a
    /// picker that started on today would file a bill due today at one tap.
    /// A parcel may do without one; every other kind needs it to be added.
    private var followUpToAdd: NewAgentFollowUp {
        var ready = followUp
        ready.dueOn = hasDay ? NewAgentFollowUp.dayText(day) : ""
        return ready
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Kind", selection: $followUp.followUpKind) {
                        ForEach(NewAgentFollowUp.kinds, id: \.self) { kind in
                            Text(verbatim: kindName(kind)).tag(kind)
                        }
                    }
                    TextField("What to follow", text: $followUp.followUpTitle)
                    Toggle("Set a day", isOn: $hasDay)
                    if hasDay {
                        DatePicker("Day", selection: $day, displayedComponents: .date)
                    }
                }
                switch followUp.followUpKind {
                case "bill":
                    Section {
                        TextField("Who is to be paid", text: $followUp.payee)
                        TextField("Amount", text: $followUp.amount)
                            .keyboardType(.decimalPad)
                        TextField("Currency", text: $followUp.currencyCode, prompt: Text(verbatim: "USD"))
                            .textInputAutocapitalization(.characters)
                            .autocorrectionDisabled()
                    }
                case "parcel":
                    Section {
                        TextField("Carrier", text: $followUp.carrier)
                        TextField("Tracking number", text: $followUp.trackingNumber)
                            .textInputAutocapitalization(.characters)
                            .autocorrectionDisabled()
                    }
                case "event":
                    Section {
                        TextField("Where", text: $followUp.place)
                    }
                default:
                    EmptyView()
                }
            }
            .navigationTitle("Add a follow-up")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Add") { Task { await add() } }
                        .disabled(!followUpToAdd.canAdd || isSaving)
                }
            }
            .toast(done: Binding<String?>.constant(nil), problem: $problemText)
        }
    }

    private func add() async {
        isSaving = true
        defer { isSaving = false }
        do {
            try await connection.addAgentFollowUp(followUpToAdd)
            onAdded(followUp.followUpTitle.trimmed)
            dismiss()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// MutedAlertsView is the dashboard's Alerts tab: whether the agent tells
/// the person unasked what their mail says they should know now, their
/// quiet hours, the most a day, and the day and time it looks around their
/// town; what it told them lately, each muted in a scope they choose; what
/// it is following for them (bills, parcels, deadlines and events), each
/// stopped, and a kind of them muted; and what they muted, each unmuted
/// again.
struct MutedAlertsView: View {
    let connection: Connection
    /// Opens scrolled to what the agent is following, below the alerts.
    var focusesFollowing = false
    @State private var preferences: AgentUpkeepPreferences?
    @State private var isAgentRead = false
    @State private var isAgentMissing = false
    @State private var isAlertsEnabled = true
    @State private var quietStart = "22:00"
    @State private var quietEnd = "07:00"
    @State private var dailyMost = 5
    @State private var lookAroundWeekday = "thursday"
    @State private var lookAroundTime = "17:00"
    @State private var alerts: [AgentAlert]?
    @State private var mutes: [AgentAlertMute] = []
    @State private var followUps: [AgentFollowUp]?
    @State private var isMutingFollowUps = false
    @State private var isAddingFollowUp = false
    @State private var categoryNames: [String: String] = [:]
    @State private var muting: AgentAlert?
    @State private var isBusy = false
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        ScrollViewReader { scrollProxy in
            List {
                if isAgentMissing {
                    ContentUnavailableView("Your agent is off", systemImage: "bell.slash", description: Text("Turn it on in its overview first."))
                } else {
                    settingsSection
                    if preferences?.isRunning == true {
                        alertsSection
                        followingSection.id(Self.followingId)
                        mutesSection
                    }
                }
            }
            .task(id: followUps?.count) {
                guard focusesFollowing, followUps != nil else { return }
                try? await Task.sleep(for: .milliseconds(200))
                scrollProxy.scrollTo(Self.followingId, anchor: .top)
            }
        }
        .navigationTitle("Alerts")
        .toolbar {
            if preferences?.isRunning == true {
                ToolbarItem(placement: .primaryAction) {
                    Button("Add a follow-up", systemImage: "plus") { isAddingFollowUp = true }
                }
            }
        }
        .sheet(isPresented: $isAddingFollowUp) {
            AddFollowUpSheet(connection: connection, kindName: { sentenceCase(followUpKindName($0, isSingular: true)) }) { title in
                doneText = String(localized: "Now following: \(title)")
                Task { await load() }
            }
        }
        .overlay { if !isAgentRead { ProgressView() } }
        .disabled(isBusy)
        .refreshable { await load() }
        .task { await load() }
        .task(id: quietStart) { await saveQuiet(start: quietStart) }
        .task(id: quietEnd) { await saveQuiet(end: quietEnd) }
        .task(id: dailyMost) { await saveDailyMost(dailyMost) }
        .task(id: lookAroundWeekday) { await saveLookAround(weekday: lookAroundWeekday) }
        .task(id: lookAroundTime) { await saveLookAround(time: lookAroundTime) }
        .toast(done: $doneText, problem: $problemText)
    }

    @ViewBuilder
    private var settingsSection: some View {
        Section {
            Toggle(
                "Tell me what my mail says I should know now",
                isOn: Binding(get: { isAlertsEnabled }, set: { isOn in Task { await setAlertsEnabled(isOn) } }))
            Group {
                DatePicker(
                    "Quiet from", selection: Binding(get: { UpkeepWords.clockDate(quietStart) }, set: { quietStart = UpkeepWords.clockText($0) }),
                    displayedComponents: .hourAndMinute)
                DatePicker(
                    "Quiet until", selection: Binding(get: { UpkeepWords.clockDate(quietEnd) }, set: { quietEnd = UpkeepWords.clockText($0) }),
                    displayedComponents: .hourAndMinute)
                Stepper(value: $dailyMost, in: 1...50) {
                    LabeledContent("At most a day") {
                        Text(dailyMost, format: .number).monospacedDigit().contentTransition(.numericText())
                    }
                }
                Picker("Look around town on", selection: $lookAroundWeekday) {
                    ForEach(AgentUpkeepPreferences.lookAroundWeekdays, id: \.self) { offeredWeekday in
                        Text(verbatim: UpkeepWords.lookAroundWeekdayName(offeredWeekday)).tag(offeredWeekday)
                    }
                }
                DatePicker(
                    "At", selection: Binding(get: { UpkeepWords.clockDate(lookAroundTime) }, set: { lookAroundTime = UpkeepWords.clockText($0) }),
                    displayedComponents: .hourAndMinute)
            }
            .disabled(!isAlertsEnabled)
        } footer: {
            VStack(alignment: .leading, spacing: 6) {
                Text("Your agent reads what arrives and tells you, unasked, when your mail shows something you should know now: a notice about your family, someone trying your account, a deadline today. It writes in the main conversation and in the chat app you linked.")
                Text("In your quiet hours only what cannot wait is said; the rest waits for the morning. The same time at both ends means no quiet hours.")
                Text("Each week your agent finds what is on near you for the weekend and the week after, and tells you on this day at this time.")
            }
        }
        .disabled(preferences == nil)
    }

    @ViewBuilder
    private var alertsSection: some View {
        Section {
            if let alerts, alerts.isEmpty {
                Text("Your agent has not told you anything unasked yet.").foregroundStyle(.secondary)
            }
            ForEach(alerts ?? []) { alert in
                VStack(alignment: .leading, spacing: 3) {
                    Text(verbatim: alert.alertText)
                    HStack(spacing: 8) {
                        Text(verbatim: UpkeepWords.when(alert.sentAt)).font(.caption).foregroundStyle(.secondary)
                        if alert.isUrgent { UpkeepTag(text: String(localized: "Could not wait"), tone: .warning) }
                    }
                    ForEach(Array(alert.covered.enumerated()), id: \.offset) { _, covered in
                        Text(verbatim: "\(covered.subject.isEmpty ? String(localized: "(no subject)") : covered.subject) · \(covered.fromAddress)")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                }
                .confirmationDialog(
                    "Stop telling me about these", isPresented: Binding(get: { muting?.id == alert.id }, set: { if !$0 { muting = nil } }), titleVisibility: .visible,
                    presenting: muting
                ) { alert in
                    ForEach(alert.muteChoices, id: \.muteScope) { choice in
                        Button(scopeLabel(choice.muteScope, target: choice.muteTarget)) {
                            Task {
                                await act(saying: String(localized: "Muted: \(targetLabel(choice.muteScope, target: choice.muteTarget))")) {
                                    try await connection.muteAgentAlert(alert.id, muteScope: choice.muteScope)
                                }
                            }
                        }
                    }
                } message: { alert in
                    Text(verbatim: alert.alertText)
                }
                .swipeActions {
                    if !alert.muteChoices.isEmpty {
                        Button("Mute", systemImage: "bell.slash") { muting = alert }
                            .tint(.orange)
                    }
                }
                .contextMenu {
                    if !alert.muteChoices.isEmpty {
                        Button("Mute", systemImage: "bell.slash") { muting = alert }
                    }
                }
            }
        } header: {
            Text("Recent alerts")
        } footer: {
            Text("What your agent told you unasked, newest first, with the messages each was about. Mute one to stop hearing about things like it.")
        }
    }

    private static let followingId = "alerts-following"

    @ViewBuilder
    private var followingSection: some View {
        Section {
            if let followUps, followUps.isEmpty {
                Text("Your agent is not following anything right now.").foregroundStyle(.secondary)
            }
            ForEach(followUps ?? []) { followUp in
                VStack(alignment: .leading, spacing: 3) {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Text(verbatim: followUp.followUpTitle)
                        if followUp.isOverdue {
                            UpkeepTag(text: String(localized: "Overdue"), tone: .bad)
                        } else if followUp.isStalled {
                            UpkeepTag(text: String(localized: "Stalled"), tone: .warning)
                        }
                    }
                    Text(verbatim: followUpLine(followUp)).font(.footnote).foregroundStyle(.secondary)
                    if let page = followUp.sourcePageURL, let host = page.host {
                        Link(destination: page) {
                            Label(host, systemImage: "link").font(.footnote)
                        }
                    }
                }
                .accessibilityElement(children: .combine)
                // Some of these rows end with a link that has a symbol and
                // some do not: the line under each starts at the row's edge.
                .fullWidthRowSeparator()
                .swipeActions {
                    stopFollowUpButton(followUp).tint(.orange)
                }
                .contextMenu {
                    stopFollowUpButton(followUp)
                }
            }
            if followUps != nil {
                Button("Mute follow-ups", systemImage: "bell.slash") { isMutingFollowUps = true }
                    .actionColor()
                    .confirmationDialog("Stop telling me about follow-ups", isPresented: $isMutingFollowUps, titleVisibility: .visible) {
                        ForEach(AgentFollowUp.muteKinds, id: \.self) { kind in
                            Button(sentenceCase(followUpKindName(kind))) {
                                Task {
                                    await act(saying: String(localized: "Muted: \(targetLabel("kind", target: kind))")) {
                                        try await connection.muteAgentAlertKind(kind)
                                    }
                                }
                            }
                        }
                    }
            }
        } header: {
            Text("Following")
        } footer: {
            Text("Bills, parcels, deadlines and events your agent is keeping track of from your mail. It tells you when one needs you. Stop one to drop it.")
        }
    }

    private func stopFollowUpButton(_ followUp: AgentFollowUp) -> some View {
        Button("Stop", systemImage: "stop.circle") {
            Task {
                await act(saying: String(localized: "Stopped following: \(followUp.followUpTitle)")) {
                    try await connection.stopAgentFollowUp(followUp.id)
                }
            }
        }
    }

    /// A follow-up's second line: what kind it is, its day, then what the
    /// kind has: who is to be paid and how much, who carries a parcel and
    /// its number, where an event is.
    private func followUpLine(_ followUp: AgentFollowUp) -> String {
        var parts: [String] = [sentenceCase(followUpKindName(followUp.followUpKind, isSingular: true))]
        if let day = followUp.dueDate() {
            parts.append(day.formatted(.dateTime.weekday(.abbreviated).month(.abbreviated).day().year()))
        } else {
            parts.append(String(localized: "No day yet"))
        }
        if let payee = followUp.payee, !payee.isEmpty { parts.append(payee) }
        if let amount = followUp.amount, !amount.isEmpty {
            if let number = Double(amount), let currencyCode = followUp.currencyCode, !currencyCode.isEmpty {
                parts.append(UpkeepWords.money(number, currency: currencyCode))
            } else {
                parts.append(amount)
            }
        }
        let carrier = [followUp.carrier, followUp.trackingNumber].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " ")
        if !carrier.isEmpty { parts.append(carrier) }
        if let place = followUp.place, !place.isEmpty { parts.append(place) }
        return parts.joined(separator: " · ")
    }

    /// A kind of follow-up in words, in lower case so it reads inside a
    /// sentence: "bills", or with isSingular "bill".
    private func followUpKindName(_ kind: String, isSingular: Bool = false) -> String {
        switch (kind, isSingular) {
        case ("bill", true): return String(localized: "bill")
        case ("parcel", true): return String(localized: "parcel")
        case ("deadline", true): return String(localized: "deadline")
        case ("event", true): return String(localized: "event")
        case ("bill", false): return String(localized: "bills")
        case ("parcel", false): return String(localized: "parcels")
        case ("deadline", false): return String(localized: "deadlines")
        case ("event", false): return String(localized: "events")
        case ("nearby", _): return String(localized: "what is on near you")
        case ("follow_up", _): return String(localized: "all follow-ups")
        default: return kind
        }
    }

    /// The first letter in capitals, for a word that begins a line or a
    /// button; the rest as it is, since Chinese and Japanese have no case.
    private func sentenceCase(_ text: String) -> String {
        text.prefix(1).uppercased() + text.dropFirst()
    }

    @ViewBuilder
    private var mutesSection: some View {
        Section {
            if mutes.isEmpty {
                Text("Nothing is muted.").foregroundStyle(.secondary)
            }
            ForEach(mutes) { mute in
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: scopeLabel(mute.muteScope, target: mute.muteTarget))
                    Text("Since \(UpkeepWords.when(mute.createdAt))").font(.caption).foregroundStyle(.secondary)
                }
                .swipeActions {
                    unmuteButton(mute).tint(.indigo)
                }
                .contextMenu {
                    unmuteButton(mute)
                }
            }
        } header: {
            Text("Muted")
        } footer: {
            Text("What you asked not to be told about. Unmute one to hear about it again.")
        }
    }

    private func unmuteButton(_ mute: AgentAlertMute) -> some View {
        Button("Unmute", systemImage: "bell") {
            Task {
                await act(saying: String(localized: "Unmuted: \(targetLabel(mute.muteScope, target: mute.muteTarget))")) {
                    try await connection.unmuteAgentAlert(mute.id)
                }
            }
        }
    }

    /// What a mute names, as a person reads it: a spending category by its
    /// name rather than its id, and budget alerts by what they are.
    private func targetLabel(_ muteScope: String, target: String) -> String {
        if muteScope == "spendingCategory" { return categoryNames[target] ?? String(localized: "a deleted spending category") }
        if muteScope == "kind" && target == "budget" { return String(localized: "budget and savings target alerts") }
        if muteScope == "kind" && AgentFollowUp.muteKinds.contains(target) { return followUpKindName(target) }
        return target
    }

    private func scopeLabel(_ muteScope: String, target: String) -> String {
        let shown = targetLabel(muteScope, target: target)
        switch muteScope {
        case "subjectKey": return String(localized: "This subject: \(shown)")
        case "sender": return String(localized: "Mail from \(shown)")
        case "domain": return String(localized: "Anything from \(shown)")
        case "kind": return String(localized: "Anything of this kind: \(shown)")
        case "spendingCategory": return String(localized: "Budget alerts for \(shown)")
        default: return shown
        }
    }

    private func load() async {
        do {
            let read = try await connection.agentUpkeep()
            preferences = read
            isAgentMissing = read == nil
            if let read {
                isAlertsEnabled = read.isAlertsEnabled
                quietStart = read.shownAlertQuietStart
                quietEnd = read.shownAlertQuietEnd
                dailyMost = read.shownAlertDailyMost
                lookAroundWeekday = read.shownLookAroundWeekday
                lookAroundTime = read.shownLookAroundTime
            }
        } catch {
            problemText = error.localizedDescription
        }
        isAgentRead = true
        guard preferences?.isRunning == true else { return }
        do {
            let read = try await connection.agentAlerts()
            alerts = read.alerts
            mutes = read.mutes
            let isCategoryNamed = (read.mutes.map(\.muteScope) + read.alerts.flatMap { $0.muteChoices.map(\.muteScope) }).contains("spendingCategory")
            if isCategoryNamed, let categories = try? await connection.spendingCategories() {
                categoryNames = Dictionary(categories.map { ($0.id, FinanceCategoryNames.shown($0)) }, uniquingKeysWith: { first, _ in first })
            }
            followUps = try await connection.agentFollowUps()
        } catch {
            problemText = error.localizedDescription
            if alerts == nil { alerts = [] }
        }
    }

    private func setAlertsEnabled(_ isOn: Bool) async {
        isAlertsEnabled = isOn
        do {
            preferences = try await connection.updateAgentUpkeep(isAlertsEnabled: isOn) ?? preferences
            doneText =
                isOn
                ? String(localized: "Your agent will tell you what your mail says you should know.")
                : String(localized: "Your agent will not tell you about your mail unasked.")
        } catch {
            isAlertsEnabled = !isOn
            problemText = error.localizedDescription
        }
    }

    private func saveQuiet(start: String? = nil, end: String? = nil) async {
        guard let preferences else { return }
        if let start, start == preferences.shownAlertQuietStart { return }
        if let end, end == preferences.shownAlertQuietEnd { return }
        try? await Task.sleep(for: .milliseconds(700))
        if Task.isCancelled { return }
        do {
            self.preferences = try await connection.updateAgentUpkeep(alertQuietStart: start, alertQuietEnd: end) ?? preferences
            doneText = String(localized: "Saved")
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func saveLookAround(weekday: String? = nil, time: String? = nil) async {
        guard let preferences else { return }
        if let weekday, weekday == preferences.shownLookAroundWeekday { return }
        if let time, time == preferences.shownLookAroundTime { return }
        try? await Task.sleep(for: .milliseconds(700))
        if Task.isCancelled { return }
        do {
            self.preferences = try await connection.updateAgentUpkeep(lookAroundWeekday: weekday, lookAroundTime: time) ?? preferences
            doneText = String(localized: "Saved")
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func saveDailyMost(_ most: Int) async {
        guard let preferences, most != preferences.shownAlertDailyMost else { return }
        try? await Task.sleep(for: .milliseconds(700))
        if Task.isCancelled { return }
        do {
            self.preferences = try await connection.updateAgentUpkeep(alertDailyMost: most) ?? preferences
            doneText = String(localized: "Saved")
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func act(saying said: String, _ work: () async throws -> Void) async {
        isBusy = true
        defer { isBusy = false }
        do {
            try await work()
            doneText = said
        } catch {
            problemText = error.localizedDescription
        }
        await load()
    }
}

// MARK: - Connections

/// ConnectionsView is the dashboard's Connections tab as a phone has it:
/// the connected servers, connected with the person's credential or
/// disconnected; the keys installed skills ask them for; which computer
/// each skill and server reaches through; the commands left running on
/// their computers; and their own bots in chat apps, set up, turned on or
/// off, unlinked from their chat or removed.
struct ConnectionsView: View {
    let connection: Connection
    @State private var servers: [AgentConnectedServer] = []
    @State private var skillSecrets: [AgentSkillSecret] = []
    @State private var reaches: [AgentReach] = []
    @State private var computers: [AgentStatus.Computer] = []
    @State private var commands: [AgentBackgroundCommand] = []
    @State private var chatChannels: [AgentChatChannel] = []
    @State private var isLoaded = false
    @State private var connecting: AgentConnectedServer?
    @State private var credential = ""
    @State private var disconnecting: AgentConnectedServer?
    @State private var fillingSecret: AgentSkillSecret?
    @State private var secretValue = ""
    @State private var forgettingSecret: AgentSkillSecret?
    @State private var tokenKind: TokenEditing?
    @State private var unlinking: AgentChatChannel?
    @State private var removingChannel: AgentChatChannel?
    @State private var isBusy = false
    @State private var doneText: String?
    @State private var problemText: String?

    /// TokenEditing is the token sheet open on one chat app.
    struct TokenEditing: Identifiable {
        let kind: String
        var id: String { kind }
    }

    var body: some View {
        List {
            if !servers.isEmpty { serversSection }
            if !skillSecrets.isEmpty { skillSecretsSection }
            skillSharingSection
            if !reaches.isEmpty { reachesSection }
            if !commands.isEmpty { commandsSection }
            chatChannelsSection
        }
        .navigationTitle("Connections")
        .overlay { if !isLoaded { ProgressView() } }
        .disabled(isBusy)
        .refreshable { await load() }
        .task { await load() }
        .alert(
            Text(verbatim: "\(String(localized: "Connect")) · \(connecting?.name ?? "")"),
            isPresented: Binding(get: { connecting != nil }, set: { if !$0 { connecting = nil } })
        ) {
            SecureField("Your credential for this server", text: $credential)
            Button("Cancel", role: .cancel) {}
            Button("Connect") {
                guard let server = connecting, !credential.trimmed.isEmpty else { return }
                let given = credential.trimmed
                Task { await connect(server, credential: given) }
            }
        }
        .alert(
            Text(verbatim: "\(fillingSecret?.key ?? "") · \(fillingSecret?.skill ?? "")"),
            isPresented: Binding(get: { fillingSecret != nil }, set: { if !$0 { fillingSecret = nil } })
        ) {
            SecureField(fillingSecret.map { $0.description.isEmpty ? $0.key : $0.description } ?? "", text: $secretValue)
            Button("Cancel", role: .cancel) {}
            Button("Set it") {
                guard let secret = fillingSecret, !secretValue.trimmed.isEmpty else { return }
                let given = secretValue.trimmed
                Task {
                    await act(saying: String(localized: "Kept your \(secret.key) for \(secret.skill)")) {
                        try await connection.setSkillSecret(skill: secret.skill, key: secret.key, value: given)
                    }
                }
            }
        }
        .sheet(item: $tokenKind) { editing in
            ChatBotTokenEditor(connection: connection, kind: editing.kind) { said in
                doneText = said
                Task { await load() }
            }
        }
        .toast(done: $doneText, problem: $problemText)
    }

    // MARK: Servers

    private var serversSection: some View {
        Section {
            ForEach(servers) { server in
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 8) {
                        Text(verbatim: server.name)
                        serverState(server)
                    }
                    Text(verbatim: serverDetails(server)).font(.footnote).foregroundStyle(.secondary)
                }
                .confirmationDialog(
                    "Disconnect", isPresented: Binding(get: { disconnecting?.id == server.id }, set: { if !$0 { disconnecting = nil } }), titleVisibility: .hidden,
                    presenting: disconnecting
                ) { server in
                    Button("Disconnect", role: .destructive) {
                        Task { await act(saying: nil) { try await connection.disconnectServer(server.name) } }
                    }
                } message: { server in
                    Text(verbatim: server.name)
                }
                .swipeActions {
                    if server.isPersonal && server.isConnected {
                        Button("Disconnect", systemImage: "bolt.slash", role: .destructive) { disconnecting = server }
                            .tint(.red)
                    } else if server.isPersonal && !server.doesAuthorizeInBrowser && server.enabled {
                        Button("Connect", systemImage: "bolt") {
                            credential = ""
                            connecting = server
                        }
                        .tint(.indigo)
                    }
                }
                .contextMenu {
                    if server.isPersonal && server.isConnected {
                        Button("Disconnect", systemImage: "bolt.slash", role: .destructive) { disconnecting = server }
                            .tint(.red)
                    } else if server.isPersonal && !server.doesAuthorizeInBrowser && server.enabled {
                        Button("Connect", systemImage: "bolt") {
                            credential = ""
                            connecting = server
                        }
                    }
                }
            }
        } header: {
            Text("Connected servers")
        } footer: {
            Text("Services the operator connected for everyone: a tracker, a wiki, a ticketing system. One that takes your own credential, or asks you to authorize it, is yours to connect; the agent uses it as you.")
        }
    }

    @ViewBuilder
    private func serverState(_ server: AgentConnectedServer) -> some View {
        if !server.enabled {
            UpkeepTag(text: String(localized: "off"))
        } else if !server.isPersonal {
            UpkeepTag(text: String(localized: "Shared"), tone: .good)
        } else {
            switch server.status {
            case "connected": UpkeepTag(text: String(localized: "Connected."), tone: .good)
            case "error": UpkeepTag(text: String(localized: "Not answering:"), tone: .bad)
            case "pending": UpkeepTag(text: String(localized: "Authorization under way."), tone: .warning)
            default: UpkeepTag(text: String(localized: "Not connected."))
            }
        }
    }

    private func serverDetails(_ server: AgentConnectedServer) -> String {
        var details = [server.transport]
        if !server.isPersonal { details.append(String(localized: "shared by everyone, nothing to connect")) }
        if server.headless { details.append(String(localized: "headless")) }
        // An authorization leaves the app for the service's page and comes
        // back to the dashboard, so it is done there.
        if server.doesAuthorizeInBrowser && !server.isConnected {
            details.append(String(localized: "authorize it from the dashboard on a computer"))
        }
        if server.status == "error", let lastError = server.lastError, !lastError.isEmpty { details.append(lastError) }
        return details.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private func connect(_ server: AgentConnectedServer, credential given: String) async {
        isBusy = true
        defer { isBusy = false }
        do {
            if let refusal = try await connection.connectServer(server.name, credential: given) {
                problemText = refusal.isEmpty ? String(localized: "The server did not accept the connection.") : refusal
            } else {
                doneText = String(localized: "Connected \(server.name).")
            }
        } catch {
            problemText = error.localizedDescription
        }
        await load()
    }

    // MARK: Skill secrets

    private var skillSecretsSection: some View {
        Section {
            ForEach(skillSecrets) { secret in
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 8) {
                        Text(verbatim: secret.key).fontDesign(.monospaced)
                        UpkeepTag(text: secret.skill)
                        if secret.set { UpkeepTag(text: String(localized: "Set"), tone: .good) }
                    }
                    if !secret.description.isEmpty {
                        Text(verbatim: secret.description).font(.footnote).foregroundStyle(.secondary)
                    }
                }
                .confirmationDialog(
                    "Forget it", isPresented: Binding(get: { forgettingSecret?.id == secret.id }, set: { if !$0 { forgettingSecret = nil } }), titleVisibility: .hidden,
                    presenting: forgettingSecret
                ) { secret in
                    Button("Forget it", role: .destructive) {
                        Task {
                            await act(saying: String(localized: "Forgot your \(secret.key)")) { try await connection.clearSkillSecret(skill: secret.skill, key: secret.key) }
                        }
                    }
                } message: { secret in
                    Text(verbatim: "\(secret.key) · \(secret.skill)")
                }
                .swipeActions {
                    if secret.set {
                        Button("Forget it", systemImage: "trash", role: .destructive) { forgettingSecret = secret }
                            .tint(.red)
                    }
                    fillButton(secret).tint(.indigo)
                }
                .contextMenu {
                    fillButton(secret)
                    if secret.set {
                        Button("Forget it", systemImage: "trash", role: .destructive) { forgettingSecret = secret }
                            .tint(.red)
                    }
                }
            }
        } header: {
            Text("Your keys for skills")
        } footer: {
            Text("Some installed skills work with a credential of your own rather than one the server shares. They are kept sealed, are used only by your agent, and are never shown again once set.")
        }
    }

    // MARK: Shared skills

    /// The way to lend a skill to someone in a circle and to answer what they
    /// ask; the screen is SkillSharingViews.swift.
    private var skillSharingSection: some View {
        Section {
            NavigationLink {
                SkillSharingView(connection: connection)
            } label: {
                Label("Shared skills", systemImage: "person.2")
                    .foregroundStyle(.primary)
            }
        } footer: {
            Text("Lend a skill to someone in a circle you share, and answer what they ask. Each use runs on your computer, as you.")
        }
    }

    private func fillButton(_ secret: AgentSkillSecret) -> some View {
        Button(secret.set ? LocalizedStringKey("Replace it") : "Set it", systemImage: "key") {
            secretValue = ""
            fillingSecret = secret
        }
    }

    // MARK: Reach

    private var reachesSection: some View {
        Section {
            ForEach(reaches) { reach in
                Picker(selection: Binding(get: { reach.computerName }, set: { chosen in Task { await choose(reach, computerName: chosen) } })) {
                    ForEach(reachOptions(reach), id: \.computerName) { option in
                        Text(verbatim: option.label).tag(option.computerName)
                    }
                } label: {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: reach.name)
                        UpkeepTag(text: reach.kind == "skill" ? String(localized: "Skill") : String(localized: "Connected server"))
                        // A server that runs on a computer, with several
                        // attached and none chosen, has nowhere to run.
                        if reach.isOnComputer && reach.computerName.isEmpty && computers.count > 1 {
                            Text("Several computers are attached: choose the one it runs on, or its calls cannot run.")
                                .font(.footnote)
                                .foregroundStyle(.orange)
                        }
                    }
                }
                .pickerStyle(.menu)
            }
        } header: {
            Text("Reach")
        } footer: {
            Text("Which computer each skill and connected server makes its requests through. A service that answers only inside one network, such as one behind a company VPN, needs a computer on that network.")
        }
    }

    /// ReachOption is one place a reach can go through.
    struct ReachOption {
        let computerName: String
        let label: String
    }

    private func reachOptions(_ reach: AgentReach) -> [ReachOption] {
        let unsetLabel: String
        if !reach.isOnComputer {
            unsetLabel = String(localized: "Through this server")
        } else if computers.count > 1 {
            unsetLabel = String(localized: "No computer chosen")
        } else {
            unsetLabel = String(localized: "Through the only attached computer")
        }
        var options = [ReachOption(computerName: "", label: unsetLabel)]
        options += computers.map { ReachOption(computerName: $0.name, label: String(localized: "Through \($0.name)")) }
        // A reach naming a computer not attached now is kept, and shown as
        // such, rather than quietly looking unset.
        if !reach.computerName.isEmpty, !computers.contains(where: { $0.name == reach.computerName }) {
            options.append(ReachOption(computerName: reach.computerName, label: String(localized: "Through \(reach.computerName) (not attached now)")))
        }
        return options
    }

    private func choose(_ reach: AgentReach, computerName: String) async {
        guard computerName != reach.computerName else { return }
        let through = computerName.isEmpty ? String(localized: "Through this server") : String(localized: "Through \(computerName)")
        isBusy = true
        defer { isBusy = false }
        do {
            try await connection.setAgentReach(kind: reach.kind, name: reach.name, computerName: computerName)
            doneText = String(localized: "\(reach.name) now goes \(through)")
        } catch {
            problemText = String(localized: "The reach could not be set.") + " " + error.localizedDescription
        }
        await load()
    }

    // MARK: Background commands

    private var commandsSection: some View {
        Section {
            ForEach(commands) { command in
                NavigationLink {
                    BackgroundCommandView(command: command, connection: connection)
                } label: {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: command.command).font(.footnote.monospaced()).lineLimit(2)
                        Text(verbatim: "\(command.computer) · \(command.isRunning ? String(localized: "Running") : String(localized: "Stopped"))")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        } header: {
            Text("Running in the background")
        } footer: {
            Text("What your agent left running on your computers, from every chat. A command still running after 24 hours is stopped.")
        }
    }

    // MARK: Chat apps

    static func chatAppName(_ kind: String) -> String {
        kind == "discord" ? String(localized: "Discord") : String(localized: "Telegram")
    }

    static func chatAppHow(_ kind: String) -> String {
        kind == "discord"
            ? String(
                localized:
                    "In the Discord developer portal, make an application with a bot, turn on the message content intent, invite the bot to your server or message it directly, and paste its token."
            )
            : String(localized: "In Telegram, open @BotFather, send /newbot, and paste the token it gives you.")
    }

    private var chatChannelsSection: some View {
        Section {
            ForEach(AgentChatChannel.kinds, id: \.self) { kind in
                let channel = chatChannels.first { $0.kind == kind }
                Button {
                    tokenKind = TokenEditing(kind: kind)
                } label: {
                    VStack(alignment: .leading, spacing: 3) {
                        HStack(spacing: 8) {
                            Text(verbatim: Self.chatAppName(kind)).foregroundStyle(Color(.label))
                            chatState(channel)
                        }
                        Text(verbatim: chatDetails(channel, kind: kind))
                            .font(.footnote)
                            .foregroundStyle(Color(.secondaryLabel))
                            .textSelection(.enabled)
                    }
                }
                .confirmationDialog(
                    "Unlink", isPresented: Binding(get: { unlinking?.kind == kind }, set: { if !$0 { unlinking = nil } }), titleVisibility: .hidden,
                    presenting: unlinking
                ) { channel in
                    Button("Unlink", role: .destructive) {
                        Task { await act(saying: nil) { try await connection.unlinkChatChannel(kind: channel.kind) } }
                    }
                } message: { channel in
                    Text("Linked to \(channel.linkedName ?? "")")
                }
                .confirmationDialog(
                    "Remove", isPresented: Binding(get: { removingChannel?.kind == kind }, set: { if !$0 { removingChannel = nil } }), titleVisibility: .hidden,
                    presenting: removingChannel
                ) { channel in
                    Button("Remove", role: .destructive) {
                        Task { await act(saying: nil) { try await connection.removeChatChannel(kind: channel.kind) } }
                    }
                } message: { channel in
                    Text(verbatim: Self.chatAppName(channel.kind))
                }
                .swipeActions {
                    if let channel {
                        Button("Remove", systemImage: "trash", role: .destructive) { removingChannel = channel }
                            .tint(.red)
                        switchButton(channel).tint(.orange)
                    }
                }
                .contextMenu {
                    Button(channel == nil ? LocalizedStringKey("Set token") : "Replace token", systemImage: "key") { tokenKind = TokenEditing(kind: kind) }
                    if let channel {
                        switchButton(channel)
                        if let linkCode = channel.linkCode, !linkCode.isEmpty, !channel.linked {
                            Button("Copy", systemImage: "doc.on.doc") { UIPasteboard.general.string = "/link \(linkCode)" }
                        }
                        if channel.linked {
                            Button("Unlink", systemImage: "link") { unlinking = channel }
                        }
                        Button("Remove", systemImage: "trash", role: .destructive) { removingChannel = channel }
                            .tint(.red)
                    }
                }
            }
        } header: {
            Text("Chat apps")
        } footer: {
            Text("Talk to your agent's main chat from Telegram or Discord, through a bot of your own. Make the bot in the app, paste its token here, then send the bot the code shown to link that chat. Only the linked chat reaches your agent; it types while it works, sends what it makes, takes files and photos, and asks on the same cards the drawer shows.")
        }
    }

    private func switchButton(_ channel: AgentChatChannel) -> some View {
        Button(channel.enabled ? LocalizedStringKey("Turn off") : "Turn on", systemImage: channel.enabled ? "pause" : "play") {
            Task {
                await act(saying: String(localized: "Saved the \(Self.chatAppName(channel.kind)) bot.")) {
                    try await connection.setChatChannel(kind: channel.kind, isEnabled: !channel.enabled)
                }
            }
        }
    }

    @ViewBuilder
    private func chatState(_ channel: AgentChatChannel?) -> some View {
        if let channel {
            if !channel.enabled {
                UpkeepTag(text: String(localized: "off"))
            } else if let lastError = channel.lastError, !lastError.isEmpty {
                UpkeepTag(text: String(localized: "Not answering:"), tone: .bad)
            } else if channel.running {
                UpkeepTag(text: String(localized: "Running."), tone: .good)
            } else {
                UpkeepTag(text: String(localized: "Starting."), tone: .warning)
            }
        } else {
            UpkeepTag(text: String(localized: "Not set up."))
        }
    }

    private func chatDetails(_ channel: AgentChatChannel?, kind: String) -> String {
        guard let channel else { return Self.chatAppHow(kind) }
        var details: [String] = []
        if let botName = channel.botName, !botName.isEmpty { details.append(botName) }
        if channel.linked {
            details.append(String(localized: "Linked to \(channel.linkedName ?? "")"))
        } else if let linkCode = channel.linkCode, !linkCode.isEmpty {
            details.append(String(localized: "Send the bot: /link \(linkCode)"))
        }
        if let lastError = channel.lastError, !lastError.isEmpty { details.append(lastError) }
        return details.isEmpty ? Self.chatAppHow(kind) : details.joined(separator: " · ")
    }

    // MARK: Loading

    private func load() async {
        let connection = connection
        async let serversRead = try? connection.connectedServers()
        async let secretsRead = try? connection.skillSecrets()
        async let reachesRead = try? connection.agentReaches()
        async let commandsRead = try? connection.backgroundCommands(in: nil)
        async let channelsRead = try? connection.chatChannels()
        servers = await serversRead ?? servers
        skillSecrets = await secretsRead ?? skillSecrets
        if let read = await reachesRead {
            reaches = read.reaches
            computers = read.computers
        }
        commands = await commandsRead ?? commands
        if let read = await channelsRead {
            chatChannels = read
        } else if !isLoaded {
            problemText = String(localized: "That did not work.")
        }
        isLoaded = true
    }

    private func act(saying said: String?, _ work: () async throws -> Void) async {
        isBusy = true
        defer { isBusy = false }
        do {
            try await work()
            if let said { doneText = said }
        } catch {
            problemText = error.localizedDescription
        }
        await load()
    }
}

/// ChatBotTokenEditor hands over a chat app bot's token, once: how to make
/// the bot, and the token, never shown again.
struct ChatBotTokenEditor: View {
    let connection: Connection
    let kind: String
    let onSaved: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var token = ""
    @State private var isSaving = false
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    SecureField("The bot's token", text: $token)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                } footer: {
                    Text(verbatim: ConnectionsView.chatAppHow(kind))
                }
            }
            .navigationTitle(Text("Your \(ConnectionsView.chatAppName(kind)) bot"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { Task { await save() } }
                        .disabled(token.trimmed.isEmpty || isSaving)
                }
            }
            .toast(done: $doneText, problem: $problemText)
        }
    }

    private func save() async {
        isSaving = true
        defer { isSaving = false }
        do {
            try await connection.setChatChannel(kind: kind, token: token.trimmed)
            onSaved(String(localized: "Saved the \(ConnectionsView.chatAppName(kind)) bot."))
            dismiss()
        } catch {
            // The sheet stays open with the token in it.
            problemText = error.localizedDescription
        }
    }
}

// MARK: - Activity

/// RunConversationView is a run's transcript opened from a list of runs. It
/// is pushed on the list's own stack, so going back returns to the list as
/// it was left, as the dashboard opens a run in its drawer over the list.
struct RunConversationView: View {
    let connection: Connection
    let conversationId: String
    @Environment(AppModel.self) private var app

    var body: some View {
        // The kind is learned when the conversation loads.
        ConversationView(
            model: ConversationModel(
                conversation: AgentConversation(id: conversationId, kind: "named", title: nil, summary: nil, lastAt: nil),
                app: app, connection: connection))
    }
}

/// AgentActivityView is the dashboard's Activity tab: the work the agent
/// started in the background, each stopped or opened; every run it made,
/// newest first, a page at a time, narrowed by kind or words or to one
/// dream, each opening its transcript; and what it answered for the
/// person, a held reply cancelled.
struct AgentActivityView: View {
    let connection: Connection
    @State private var dreamJobId: String?
    private let dreamStartedAt: String?
    @State private var works: [AgentBackgroundWork] = []
    @State private var runs: [AgentRunSummary]?
    @State private var runTotal = 0
    @State private var query = ""
    @State private var jobKind = ""
    @State private var isLoadingMore = false
    @State private var replies: [AgentReply] = []
    @State private var openedWork: AgentBackgroundWork?
    /// The run opened from this list, pushed over it.
    @State private var openedRunId: String?
    @State private var stopping: AgentBackgroundWork?
    @State private var cancelling: AgentReply?
    @State private var isBusy = false
    @State private var doneText: String?
    @State private var problemText: String?

    /// How many runs a page holds.
    static let pageSize = 50

    init(connection: Connection, dreamJobId: String? = nil, dreamStartedAt: String? = nil) {
        self.connection = connection
        _dreamJobId = State(initialValue: dreamJobId)
        self.dreamStartedAt = dreamStartedAt
    }

    /// RunFilter is what the runs are narrowed by; a change reads them
    /// again from the first page.
    struct RunFilter: Equatable {
        let query: String
        let jobKind: String
        let dreamJobId: String?
    }

    var body: some View {
        List {
            if !works.isEmpty { worksSection }
            runsSection
            repliesSection
        }
        .navigationTitle("Activity")
        .searchable(text: $query, prompt: Text("Search the runs"))
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Menu {
                    Picker("Kind", selection: $jobKind) {
                        Text("Every kind").tag("")
                        ForEach(AgentRunSummary.jobKinds, id: \.self) { kind in
                            Text(UpkeepWords.runKind(kind)).tag(kind)
                        }
                    }
                } label: {
                    Label("Kind", systemImage: jobKind.isEmpty ? "line.3.horizontal.decrease.circle" : "line.3.horizontal.decrease.circle.fill")
                }
            }
        }
        .overlay { if runs == nil { ProgressView() } }
        .disabled(isBusy)
        .refreshable { await load() }
        .task(id: RunFilter(query: query, jobKind: jobKind, dreamJobId: dreamJobId)) {
            if runs != nil { try? await Task.sleep(for: .milliseconds(400)) }
            if Task.isCancelled { return }
            await loadRuns()
        }
        .task {
            await loadWorks()
            await loadReplies()
            // Read again while it is open: runs are ordered by their last
            // message, and work in the background finishes.
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(15))
                if Task.isCancelled { break }
                await loadWorks()
                if (runs?.count ?? 0) <= Self.pageSize { await loadRuns(isQuiet: true) }
            }
        }
        .sheet(item: $openedWork) { work in
            BackgroundWorkResultSheet(connection: connection, work: work) { runId in
                openedWork = nil
                openedRunId = runId
            }
        }
        .navigationDestination(item: $openedRunId) { runId in
            RunConversationView(connection: connection, conversationId: runId)
        }
        #if DEBUG
            .task {
                // `-TeaNodeScreen activity-run`: a run opened from the list, with the way back to it.
                guard DemoServer.isRequested, DemoServer.screen == "activity-run" else { return }
                try? await Task.sleep(for: .seconds(1))
                openedRunId = "main"
            }
        #endif
        .toast(done: $doneText, problem: $problemText)
    }

    // MARK: Work in the background

    private var worksSection: some View {
        Section {
            ForEach(works) { work in
                Button {
                    if work.isOpenable { openedWork = work }
                } label: {
                    VStack(alignment: .leading, spacing: 3) {
                        HStack(spacing: 8) {
                            Text(verbatim: work.title.isEmpty ? work.id : work.title).foregroundStyle(Color(.label)).lineLimit(2)
                            BackgroundWorkState(workStatus: work.workStatus)
                        }
                        Text(verbatim: workDetails(work)).font(.footnote).foregroundStyle(Color(.secondaryLabel))
                    }
                }
                .disabled(!work.isOpenable && !work.isStoppable)
                .confirmationDialog(
                    "Stop", isPresented: Binding(get: { stopping?.id == work.id }, set: { if !$0 { stopping = nil } }), titleVisibility: .hidden, presenting: stopping
                ) { work in
                    Button("Stop", role: .destructive) {
                        Task {
                            await act(saying: String(localized: "Stopped \(work.title)")) { try await connection.stopBackgroundWork(work.id) }
                            await loadWorks()
                        }
                    }
                } message: { work in
                    Text(verbatim: work.title.isEmpty ? work.id : work.title)
                }
                .swipeActions {
                    if work.isStoppable {
                        Button("Stop", systemImage: "stop.circle", role: .destructive) { stopping = work }
                            .tint(.red)
                    }
                }
                .contextMenu {
                    if work.isOpenable {
                        Button("Open", systemImage: "doc.text") { openedWork = work }
                    }
                    if work.isStoppable {
                        Button("Stop", systemImage: "stop.circle", role: .destructive) { stopping = work }
                            .tint(.red)
                    }
                }
            }
        } header: {
            Text("Work in the background")
        } footer: {
            Text("Surveys and subagents your agent started without waiting for them, and surveys started from the command line. The conversation that started one hears when it finishes.")
        }
    }

    private func workDetails(_ work: AgentBackgroundWork) -> String {
        var details = [
            work.isSurvey ? String(localized: "Survey") : String(localized: "Subagent"),
            String(localized: "started \(UpkeepWords.relative(work.createdAt))"),
        ]
        if !work.errorMessage.isEmpty { details.append(work.errorMessage) }
        return details.joined(separator: " · ")
    }

    // MARK: Runs

    private var runsSection: some View {
        Section {
            if let dreamJobId {
                Label(
                    String(localized: "Dream at \(dreamStartedAt.map { UpkeepWords.when($0) } ?? dreamJobId)"), systemImage: "moon.stars"
                )
                .foregroundStyle(.primary)
                .fullWidthRowSeparator()
                Button("Show all", systemImage: "xmark.circle") { self.dreamJobId = nil }
                    .actionColor()
                    .fullWidthRowSeparator()
            }
            if let runs, runs.isEmpty {
                Text("Nothing yet.").foregroundStyle(.secondary)
            }
            ForEach(runs ?? []) { run in
                Button {
                    openedRunId = run.id
                } label: {
                    AgentRunRow(run: run)
                }
                .onAppear {
                    if run.id == runs?.last?.id { Task { await loadMore() } }
                }
            }
            if isLoadingMore {
                ProgressView().frame(maxWidth: .infinity)
            }
        } header: {
            Text(runCountText)
        } footer: {
            if dreamJobId != nil {
                Text(
                    "The runs of the dream at \(dreamStartedAt.map { UpkeepWords.when($0) } ?? ""): every call it made to a model, newest first. Open one to read it."
                )
            } else {
                Text("The runs your agent made on its own (sorting, answering, looking things up, its schedules), newest first. Open one to read what it did and ask about it.")
            }
        }
    }

    private var runCountText: String {
        runTotal == 1 ? String(localized: "\(runTotal) run") : String(localized: "\(runTotal) runs")
    }

    // MARK: Replies

    private var repliesSection: some View {
        Section {
            if replies.isEmpty {
                Text("Nothing yet. Replies appear here once a mailbox lets the agent answer for you.").foregroundStyle(.secondary)
            }
            ForEach(replies) { reply in
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 8) {
                        Text(verbatim: reply.subject.isEmpty ? String(localized: "(no subject)") : reply.subject).lineLimit(2)
                        replyState(reply.status)
                    }
                    Text(verbatim: replyDetails(reply)).font(.footnote).foregroundStyle(.secondary)
                }
                .confirmationDialog(
                    "Cancel the reply", isPresented: Binding(get: { cancelling?.id == reply.id }, set: { if !$0 { cancelling = nil } }), titleVisibility: .hidden,
                    presenting: cancelling
                ) { reply in
                    Button("Cancel the reply", role: .destructive) {
                        Task {
                            await act(saying: String(localized: "The reply will not be sent.")) { try await connection.cancelAgentReply(reply.id) }
                            await loadReplies()
                        }
                    }
                } message: { reply in
                    Text(verbatim: reply.subject)
                }
                .swipeActions {
                    if reply.isHeld {
                        Button("Cancel", systemImage: "xmark.circle", role: .destructive) { cancelling = reply }
                            .tint(.red)
                    }
                }
            }
        } header: {
            Text("Answered for you")
        } footer: {
            Text("What your agent answered on your behalf, what it is about to, and what it left alone and why.")
        }
    }

    @ViewBuilder
    private func replyState(_ replyStatus: String) -> some View {
        switch replyStatus {
        case "held": UpkeepTag(text: String(localized: "Held"), tone: .warning)
        case "sent": UpkeepTag(text: String(localized: "Sent"), tone: .good)
        case "cancelled": UpkeepTag(text: String(localized: "Cancelled"))
        case "refused": UpkeepTag(text: String(localized: "Not answered"))
        default: UpkeepTag(text: String(localized: "Failed"), tone: .bad)
        }
    }

    private func replyDetails(_ reply: AgentReply) -> String {
        var details = [reply.to ?? "", UpkeepWords.when(reply.sentAt ?? reply.createdAt)]
        if let replyReason = reply.reason, !replyReason.isEmpty { details.append(replyReason) }
        if reply.isHeld, let sendAfter = reply.sendAfter, ServerTime.date(sendAfter) != nil {
            details.append(String(localized: "sends at \(UpkeepWords.when(sendAfter))"))
        }
        return details.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    // MARK: Loading

    private func load() async {
        await loadWorks()
        await loadRuns()
        await loadReplies()
    }

    private func loadWorks() async {
        if let read = try? await connection.backgroundWork() { works = read }
    }

    private func loadReplies() async {
        if let read = try? await connection.agentReplies() { replies = read.replies }
    }

    private func loadRuns(isQuiet: Bool = false) async {
        do {
            let page = try await connection.agentRuns(
                first: Self.pageSize, offset: 0, jobId: dreamJobId, jobKinds: jobKind.isEmpty ? [] : [jobKind], query: query)
            runs = page.runs
            runTotal = page.total
        } catch {
            if !isQuiet { problemText = error.localizedDescription }
            if runs == nil { runs = [] }
        }
    }

    private func loadMore() async {
        guard let loaded = runs, loaded.count < runTotal, !isLoadingMore else { return }
        isLoadingMore = true
        defer { isLoadingMore = false }
        do {
            let page = try await connection.agentRuns(
                first: Self.pageSize, offset: loaded.count, jobId: dreamJobId, jobKinds: jobKind.isEmpty ? [] : [jobKind], query: query)
            let knownIds = Set(loaded.map(\.id))
            runs = loaded + page.runs.filter { !knownIds.contains($0.id) }
            runTotal = page.total
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func act(saying said: String, _ work: () async throws -> Void) async {
        isBusy = true
        defer { isBusy = false }
        do {
            try await work()
            doneText = said
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// AgentRunRow is one run: what it was about, its kind, when it last spoke,
/// and the tokens and money it used.
struct AgentRunRow: View {
    let run: AgentRunSummary

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: run.title).foregroundStyle(Color(.label)).lineLimit(2)
            HStack(spacing: 8) {
                if let jobKind = run.jobKind, !jobKind.isEmpty {
                    UpkeepTag(text: UpkeepWords.runKind(jobKind))
                }
                Text(verbatim: details).font(.caption).foregroundStyle(Color(.secondaryLabel))
            }
        }
    }

    private var details: String {
        var parts = [UpkeepWords.when(run.lastAt), run.tokenCount.formatted(.number.notation(.compactName))]
        if run.usage.cost > 0 { parts.append(UpkeepWords.money(run.usage.cost)) }
        return parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

/// BackgroundWorkState is where a piece of work stands, as a tag.
struct BackgroundWorkState: View {
    let workStatus: String

    var body: some View {
        switch workStatus {
        case "queued": UpkeepTag(text: String(localized: "Queued"))
        case "running": UpkeepTag(text: String(localized: "Running"))
        case "done": UpkeepTag(text: String(localized: "Finished"), tone: .good)
        case "failed": UpkeepTag(text: String(localized: "Failed"), tone: .bad)
        default: UpkeepTag(text: String(localized: "Stopped"), tone: .warning)
        }
    }
}

/// BackgroundWorkResultSheet is what came of one piece of work: the report
/// or the answer to read, or why it failed, and the runs it made, each
/// opening its transcript.
struct BackgroundWorkResultSheet: View {
    let connection: Connection
    let work: AgentBackgroundWork
    let onOpenRun: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var workResult: AgentBackgroundWork?
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        let shown = workResult ?? work
        NavigationStack {
            List {
                Section {
                    HStack(spacing: 8) {
                        BackgroundWorkState(workStatus: shown.workStatus)
                        Text(shown.isSurvey ? LocalizedStringKey("Survey") : "Subagent").font(.caption).foregroundStyle(.secondary)
                        Text("started \(UpkeepWords.relative(shown.createdAt))").font(.caption).foregroundStyle(.secondary)
                    }
                }
                if let workResult {
                    if workResult.workStatus == "failed" {
                        Section("Why it failed") {
                            Text(verbatim: workResult.errorMessage.isEmpty ? String(localized: "It did not say why.") : workResult.errorMessage)
                                .font(.footnote.monospaced())
                                .textSelection(.enabled)
                        }
                    } else {
                        Section(workResult.isSurvey ? LocalizedStringKey("Report") : "Answer") {
                            if let resultText = workResult.resultText, !resultText.trimmed.isEmpty {
                                MarkdownView(text: resultText)
                            } else {
                                Text("It finished without saying anything.").foregroundStyle(.secondary)
                            }
                        }
                    }
                    if !workResult.runIds.isEmpty {
                        Section("The runs it made") {
                            ForEach(Array(workResult.runIds.enumerated()), id: \.offset) { index, runId in
                                Button("Run \(index + 1)") { onOpenRun(runId) }
                            }
                        }
                    }
                }
            }
            .navigationTitle(Text(verbatim: work.title.isEmpty ? work.id : work.title))
            .navigationBarTitleDisplayMode(.inline)
            .overlay { if workResult == nil { ProgressView() } }
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .task {
                do {
                    workResult = try await connection.backgroundWorkResult(work.id)
                } catch {
                    problemText = String(localized: "What came of the work could not be read.")
                }
            }
            .toast(done: $doneText, problem: $problemText)
        }
    }
}
