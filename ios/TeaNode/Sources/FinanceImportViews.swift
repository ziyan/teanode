import SwiftUI
import TeaNodeKit

// MARK: What an import came to

/// FinanceImportPhase is how an import stands, for the words, the mark and the
/// color of the row and of its page: a job that finished but imported nothing is
/// a problem, even though the job itself did not fail.
enum FinanceImportPhase {
    case waiting, reading, imported, nothingImported, failed, cancelled

    /// Whether the person has something to look into or retry.
    var isProblem: Bool { self == .nothingImported || self == .failed }
}

extension FinanceImport {
    /// What the run says came of it, trimmed.
    var outcomeSentence: String {
        (outcomeText ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Whether the job finished but the run says it imported nothing: the
    /// server starts such a sentence with "Imported nothing" or "Not read".
    var isNothingImported: Bool {
        guard agentJobStatus == "done" else { return false }
        return outcomeSentence.hasPrefix("Imported nothing") || outcomeSentence.hasPrefix("Not read")
    }

    var phase: FinanceImportPhase {
        switch agentJobStatus {
        case "running": .reading
        case "done": isNothingImported ? .nothingImported : .imported
        case "dead": .failed
        case "cancelled": .cancelled
        default: .waiting
        }
    }

    /// The outcome without the words the status beside it already says:
    /// "Imported nothing: it shows no account number" reads "It shows no
    /// account number" under a status of Imported nothing.
    var reasonSentence: String {
        for prefix in ["Imported nothing: ", "Not read: "] where outcomeSentence.hasPrefix(prefix) {
            let reason = String(outcomeSentence.dropFirst(prefix.count))
            return reason.prefix(1).uppercased() + reason.dropFirst()
        }
        return outcomeSentence
    }

    var hasAttachment: Bool { !(agentAttachmentId ?? "").isEmpty }
    var hasPicture: Bool { hasAttachment && isPicture }
    var hasOtherFile: Bool { hasAttachment && !isPicture }
    var hasMessage: Bool { !(mailboxItemId ?? "").isEmpty }

    /// What it was read as.
    var kindText: String {
        switch financeImportKind {
        case "statement": String(localized: "Statement")
        case "receipt": String(localized: "Receipt")
        case "tax_document": String(localized: "Tax document")
        default: financeImportKind
        }
    }

    /// Where it came from.
    var sourceText: String {
        switch financeImportSource {
        case "upload": String(localized: "Shared or uploaded")
        case "photo": String(localized: "Backed-up photo")
        case "mail": String(localized: "Mail")
        default: financeImportSource
        }
    }

    /// The words of a row and of the page's first line: the outcome once it
    /// is known, else what the job is doing.
    var headlineText: String {
        switch phase {
        case .waiting: String(localized: "Waiting to be read")
        case .reading: String(localized: "Reading…")
        case .cancelled: String(localized: "Cancelled")
        case .imported, .nothingImported, .failed:
            if !outcomeSentence.isEmpty { outcomeSentence } else if phase == .failed { String(localized: "Failed") } else { String(localized: "Done") }
        }
    }

    /// The name of the file, or what it was read as when it has no name.
    var shownFileName: String {
        let name = fileName ?? ""
        return name.isEmpty ? kindText : name
    }

    /// The address the file is read from.
    var attachmentPath: String { "/api/v1/agent/attachments/\(agentAttachmentId ?? "")" }

    /// Whether it can be read again: a statement or a tax document whose file
    /// is still there, after it failed or imported nothing.
    var canReadAgain: Bool {
        guard hasAttachment, phase.isProblem else { return false }
        return financeImportKind == "statement" || financeImportKind == "tax_document"
    }
}

// MARK: The list

/// FinanceImportDay is the imports of one day, for a section of the list.
private struct FinanceImportDay: Identifiable {
    /// The start of the day in the person's time zone; `distantPast` for
    /// imports with no time.
    let day: Date
    let imports: [FinanceImport]
    var id: Date { day }
}

/// FinanceImportsView is the files the agent read into finance in the
/// background, newest first in sections by day, fifty at a time: a statement
/// shared as a picture or a PDF, a receipt, a tax document, with what the run
/// says came of each. It is Finance's Imports part, as the dashboard lists
/// them. While one waits or is read, the first page is read again every few
/// seconds, so the person watches it finish.
struct FinanceImportsView: View {
    @Environment(FinanceModel.self) private var finance
    @State private var imports: [FinanceImport] = []
    @State private var isLoaded = false
    @State private var hasMore = false
    @State private var isLoadingPage = false
    @State private var loadProblemText: String?
    @State private var problemText: String?
    #if DEBUG
        @State private var demoOpenedImport: FinanceImport?
    #endif

    private static let pageSize = 50
    private static let refreshInterval: Duration = .seconds(5)

    /// Whether any listed import still waits or is being read.
    private var isWatching: Bool {
        imports.contains { $0.phase == .waiting || $0.phase == .reading }
    }

    private var days: [FinanceImportDay] {
        let calendar = Calendar.current
        var days: [FinanceImportDay] = []
        for financeImport in imports {
            let day = ServerTime.date(financeImport.queuedAt).map { calendar.startOfDay(for: $0) } ?? Date.distantPast
            if let last = days.last, last.day == day {
                days[days.count - 1] = FinanceImportDay(day: day, imports: last.imports + [financeImport])
            } else {
                days.append(FinanceImportDay(day: day, imports: [financeImport]))
            }
        }
        return days
    }

    private static func dayTitle(_ day: Date) -> String {
        if day == Date.distantPast { return String(localized: "Earlier") }
        let calendar = Calendar.current
        if calendar.isDateInToday(day) { return String(localized: "Today") }
        if calendar.isDateInYesterday(day) { return String(localized: "Yesterday") }
        return day.formatted(.dateTime.weekday(.wide).month().day())
    }

    var body: some View {
        List {
            ForEach(days) { day in
                Section {
                    ForEach(day.imports) { financeImport in
                        NavigationLink {
                            FinanceImportDetailView(financeImport: financeImport) { Task { await refreshFirstPage() } }
                        } label: {
                            FinanceImportRow(financeImport: financeImport)
                        }
                    }
                } header: {
                    Text(verbatim: Self.dayTitle(day.day)).font(.headline).textCase(nil)
                }
            }
            if hasMore {
                Section {
                    // The next page is read when this row comes into view, which
                    // is when the last import has been scrolled to.
                    ProgressView()
                        .frame(maxWidth: .infinity)
                        .listRowBackground(Color.clear)
                        .onAppear { Task { await loadMore() } }
                }
            }
        }
        .navigationTitle("Imports")
        .navigationBarTitleDisplayMode(.inline)
        .overlay {
            if let loadProblemText, !isLoaded {
                LoadFailedView(problemText: loadProblemText) { await load() }
            } else if !isLoaded {
                ProgressView()
            } else if imports.isEmpty {
                ContentUnavailableView(
                    "Nothing imported yet", systemImage: "tray.and.arrow.down",
                    description: Text("Share a screenshot, a photo or a PDF of a statement to TeaNode and choose Import transactions. What came of each shows here."))
            }
        }
        .refreshable { await load() }
        .task { await load() }
        .task(id: isWatching) {
            guard isWatching else { return }
            while !Task.isCancelled {
                try? await Task.sleep(for: Self.refreshInterval)
                if Task.isCancelled { return }
                await refreshFirstPage()
            }
        }
        #if DEBUG
            // `-TeaNodeScreen finance-import-detail` opens the page of the import that read nothing.
            .navigationDestination(item: $demoOpenedImport) { financeImport in
                FinanceImportDetailView(financeImport: financeImport) {}
            }
            .task(id: isLoaded) {
                guard DemoServer.screen == "finance-import-detail", isLoaded else { return }
                demoOpenedImport = imports.first { $0.phase == .nothingImported }
            }
        #endif
        .toast(done: .constant(nil), problem: $problemText)
    }

    private func load() async {
        do {
            let page = try await finance.connection.financeImports(limit: Self.pageSize, offset: 0)
            imports = page.shownImports
            hasMore = page.hasMore ?? false
            isLoaded = true
            loadProblemText = nil
        } catch {
            if isLoaded { problemText = error.loadProblemText } else { loadProblemText = error.loadProblemText }
        }
    }

    /// Reads the first page again without losing the pages after it, and
    /// without a message when it fails: the next read tries again.
    private func refreshFirstPage() async {
        guard let page = try? await finance.connection.financeImports(limit: Self.pageSize, offset: 0) else { return }
        let firstPage = page.shownImports
        let firstPageIds = Set(firstPage.map(\.agentJobId))
        let isOnlyFirstPage = imports.count <= Self.pageSize
        imports = firstPage + imports.filter { !firstPageIds.contains($0.agentJobId) }
        if isOnlyFirstPage { hasMore = page.hasMore ?? false }
    }

    private func loadMore() async {
        guard !isLoadingPage else { return }
        isLoadingPage = true
        defer { isLoadingPage = false }
        do {
            let page = try await finance.connection.financeImports(limit: Self.pageSize, offset: imports.count)
            let knownJobIds = Set(imports.map(\.agentJobId))
            imports.append(contentsOf: page.shownImports.filter { !knownJobIds.contains($0.agentJobId) })
            hasMore = page.hasMore ?? false
        } catch {
            // Without this the spinner row would stay and never try again.
            hasMore = false
            problemText = error.loadProblemText
        }
    }
}

// MARK: A row

/// FinanceImportRow is one file read into finance, in a day's section: the
/// picture or a symbol for the file, what came of it, what it was read as and
/// when, and a mark only where it needs one (reading, waiting, a problem). The
/// file name does not tell rows apart, since a shared picture always has the
/// same one.
struct FinanceImportRow: View {
    let financeImport: FinanceImport

    private var subtitleText: String {
        guard let queuedAt = ServerTime.date(financeImport.queuedAt) else { return financeImport.kindText }
        return financeImport.kindText + " · " + queuedAt.formatted(date: .omitted, time: .shortened)
    }

    private var statusText: String {
        switch financeImport.phase {
        case .waiting: String(localized: "Waiting to be read")
        case .reading: String(localized: "Reading")
        case .imported: String(localized: "Imported")
        case .nothingImported: String(localized: "Imported nothing")
        case .failed: String(localized: "Failed")
        case .cancelled: String(localized: "Cancelled")
        }
    }

    var body: some View {
        HStack(spacing: 12) {
            FinanceImportLeading(financeImport: financeImport)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: financeImport.headlineText)
                    .font(.body)
                    .foregroundStyle(.primary)
                    .lineLimit(2)
                Text(verbatim: subtitleText)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            statusMark
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(Text(verbatim: "\(financeImport.headlineText), \(subtitleText), \(statusText)"))
    }

    /// The mark at the end of the row: only where it says something.
    @ViewBuilder private var statusMark: some View {
        switch financeImport.phase {
        case .reading:
            ProgressView()
        case .waiting:
            Image(systemName: "clock")
                .foregroundStyle(.secondary)
        case .nothingImported, .failed:
            Image(systemName: "exclamationmark.circle.fill")
                .foregroundStyle(.red)
        case .imported, .cancelled:
            EmptyView()
        }
    }
}

/// FinanceImportLeading is the 44 point square at the start of a row: the
/// picture of an import that is one, else a symbol for what it is.
struct FinanceImportLeading: View {
    let financeImport: FinanceImport
    @Environment(AppModel.self) private var app
    @State private var picture: UIImage?
    @State private var isMissing = false

    private static let side: CGFloat = 44

    private var symbolName: String {
        if financeImport.hasOtherFile { return "doc.text" }
        if financeImport.hasMessage { return "envelope" }
        if financeImport.financeImportKind == "receipt" { return "receipt" }
        return financeImport.hasPicture ? "photo" : "doc.text"
    }

    var body: some View {
        Group {
            if let picture {
                Image(uiImage: picture)
                    .resizable()
                    .scaledToFill()
            } else if financeImport.hasPicture, !isMissing {
                ProgressView()
            } else {
                Image(systemName: symbolName)
                    .symbolRenderingMode(.hierarchical)
                    .font(.title3)
                    .foregroundStyle(.secondary)
            }
        }
        .frame(width: Self.side, height: Self.side)
        .background(Color(.tertiarySystemFill))
        .clipShape(.rect(cornerRadius: 8, style: .continuous))
        .accessibilityHidden(true)
        .task(id: financeImport.agentAttachmentId ?? "") {
            guard financeImport.hasPicture else { return }
            picture = await app.picture(financeImport.attachmentPath)
            isMissing = picture == nil
        }
    }
}

// MARK: The page of one import

/// FinanceImportDetailView is one file read into finance: the picture or the
/// file, what came of it said whole, the details, and what can be done next
/// (open the file or the message it came in, read it again).
struct FinanceImportDetailView: View {
    let financeImport: FinanceImport
    /// Called after the file is queued to be read again, so that the list
    /// shows the new job.
    let onReadAgain: () -> Void
    @Environment(FinanceModel.self) private var finance
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var previewAddress: URL?
    @State private var problemText: String?

    private var statusText: String {
        switch financeImport.phase {
        case .waiting: String(localized: "Waiting to be read")
        case .reading: String(localized: "Reading")
        case .imported: String(localized: "Imported")
        case .nothingImported: String(localized: "Imported nothing")
        case .failed: String(localized: "Failed")
        case .cancelled: String(localized: "Cancelled")
        }
    }

    private var statusSymbol: String {
        switch financeImport.phase {
        case .waiting: "clock"
        case .reading: "hourglass"
        case .imported: "checkmark.circle.fill"
        case .nothingImported, .failed: "exclamationmark.circle.fill"
        case .cancelled: "xmark.circle"
        }
    }

    private var statusColor: Color {
        switch financeImport.phase {
        case .imported: .green
        case .nothingImported, .failed: .red
        case .waiting, .reading, .cancelled: .secondary
        }
    }

    private var largeSymbolName: String {
        if financeImport.hasMessage, !financeImport.hasAttachment { return "envelope" }
        if financeImport.financeImportKind == "receipt", !financeImport.hasAttachment { return "receipt" }
        return "doc.text"
    }

    var body: some View {
        List {
            Section {
                if financeImport.hasPicture {
                    RemotePicture(path: financeImport.attachmentPath, name: financeImport.shownFileName, maxHeight: 220, originalFilePath: financeImport.attachmentPath)
                        .frame(maxWidth: .infinity)
                } else {
                    VStack(spacing: 12) {
                        Image(systemName: largeSymbolName)
                            .symbolRenderingMode(.hierarchical)
                            .font(.largeTitle)
                            .foregroundStyle(.secondary)
                            .frame(width: 88, height: 88)
                            .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 16, style: .continuous))
                            .accessibilityHidden(true)
                        Text(verbatim: financeImport.shownFileName)
                            .font(.headline)
                            .multilineTextAlignment(.center)
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 8)
                }
            }
            Section("What came of it") {
                Label {
                    Text(verbatim: statusText)
                        .font(.headline)
                } icon: {
                    if financeImport.phase == .reading {
                        ProgressView()
                    } else {
                        Image(systemName: statusSymbol)
                            .foregroundStyle(statusColor)
                    }
                }
                .foregroundStyle(.primary)
                if !financeImport.reasonSentence.isEmpty {
                    Text(verbatim: financeImport.reasonSentence)
                        .foregroundStyle(.primary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            Section("Details") {
                LabeledContent("Read as", value: financeImport.kindText)
                LabeledContent("From", value: financeImport.sourceText)
                if !(financeImport.fileName ?? "").isEmpty {
                    LabeledContent("File", value: financeImport.shownFileName)
                }
                if let queuedAt = ServerTime.date(financeImport.queuedAt) {
                    LabeledContent("Received", value: queuedAt.formatted(date: .abbreviated, time: .shortened))
                }
                if let finishedAt = ServerTime.date(financeImport.finishedAt) {
                    LabeledContent("Finished", value: finishedAt.formatted(date: .abbreviated, time: .shortened))
                }
            }
            if financeImport.hasOtherFile || financeImport.hasMessage || financeImport.canReadAgain {
                Section {
                    if financeImport.hasOtherFile {
                        Button("Open file", systemImage: "doc.text") { openFile() }
                            .actionColor()
                    }
                    if financeImport.hasMessage {
                        Button("Open message", systemImage: "envelope") {
                            app.openedMailItemId = financeImport.mailboxItemId ?? ""
                        }
                        .actionColor()
                    }
                    if financeImport.canReadAgain {
                        Button("Read again", systemImage: "arrow.clockwise") { readAgain() }
                            .actionColor()
                    }
                }
            }
        }
        .listStyle(.insetGrouped)
        .navigationTitle(financeImport.kindText)
        .navigationBarTitleDisplayMode(.inline)
        .toast(done: .constant(nil), problem: $problemText)
        .quickLookPreview($previewAddress)
    }

    /// Opens the file that is not a picture in Quick Look.
    private func openFile() {
        let name = financeImport.shownFileName
        Task {
            if let address = await app.downloadedFile(financeImport.attachmentPath, name: name) {
                previewAddress = address
            } else {
                problemText = app.previewProblemText
            }
        }
    }

    /// Has the agent read the file again, then goes back to the list, which
    /// shows the new job reading.
    private func readAgain() {
        let attachmentId = financeImport.agentAttachmentId ?? ""
        let isTaxDocument = financeImport.financeImportKind == "tax_document"
        Task {
            let isQueued = await finance.act(saying: String(localized: "Reading again")) {
                if isTaxDocument {
                    _ = try await finance.connection.readTaxDocument(attachmentId: attachmentId)
                } else {
                    _ = try await finance.connection.readStatement(attachmentId: attachmentId)
                }
            }
            if isQueued {
                onReadAgain()
                dismiss()
            }
        }
    }
}
