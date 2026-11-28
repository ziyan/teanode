import SwiftUI
import TeaNodeKit

/// FinanceImportsView is the files the agent read into finance in the
/// background, newest first, fifty at a time: a statement shared as a picture
/// or a PDF, a receipt, a tax document, with what the run says came of each.
/// It is Finance's Imports part, as the dashboard lists them.
struct FinanceImportsView: View {
    @Environment(FinanceModel.self) private var finance
    @State private var imports: [FinanceImport] = []
    @State private var isLoaded = false
    @State private var hasMore = false
    @State private var isLoadingPage = false
    @State private var loadProblemText: String?
    @State private var problemText: String?

    private static let pageSize = 50

    var body: some View {
        List {
            ForEach(imports) { financeImport in
                FinanceImportRow(financeImport: financeImport)
            }
            if hasMore {
                Section {
                    Button {
                        Task { await loadMore() }
                    } label: {
                        Label("Show more", systemImage: "arrow.down.circle")
                            .actionColor()
                    }
                    .disabled(isLoadingPage)
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
            problemText = error.loadProblemText
        }
    }
}

/// FinanceImportRow is one file read into finance: its name, what it was
/// read as and where it came from, how it went and when, and what came of it,
/// whole.
struct FinanceImportRow: View {
    let financeImport: FinanceImport

    private var kindText: String {
        switch financeImport.financeImportKind {
        case "statement": String(localized: "Statement")
        case "receipt": String(localized: "Receipt")
        case "tax_document": String(localized: "Tax document")
        default: financeImport.financeImportKind
        }
    }

    private var sourceText: String {
        switch financeImport.financeImportSource {
        case "upload": String(localized: "From an upload")
        case "photo": String(localized: "Backed-up photo")
        case "mail": String(localized: "From mail")
        default: financeImport.financeImportSource
        }
    }

    private var statusText: String {
        switch financeImport.agentJobStatus {
        case "queued": String(localized: "Waiting to be read")
        case "running": String(localized: "Reading")
        case "done": String(localized: "Done")
        case "dead": String(localized: "Failed")
        case "cancelled": String(localized: "Cancelled")
        default: financeImport.agentJobStatus
        }
    }

    private var statusSymbol: String {
        switch financeImport.agentJobStatus {
        case "queued": "clock"
        case "running": "hourglass"
        case "done": "checkmark.circle"
        case "dead": "exclamationmark.triangle"
        case "cancelled": "xmark.circle"
        default: "questionmark.circle"
        }
    }

    private var isFailed: Bool { financeImport.agentJobStatus == "dead" }

    /// The status and, after it, the day and time it was last known to change.
    private var statusLine: String {
        guard let shownAt = financeImport.shownAt else { return statusText }
        return statusText + " · " + shownAt.formatted(date: .abbreviated, time: .shortened)
    }

    private var fileNameText: String {
        let fileName = financeImport.fileName ?? ""
        return fileName.isEmpty ? kindText : fileName
    }

    private var outcomeText: String {
        (financeImport.outcomeText ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: fileNameText)
                .font(.body)
                .foregroundStyle(.primary)
            Text(verbatim: kindText + " · " + sourceText)
                .font(.subheadline)
                .foregroundStyle(.secondary)
            Label {
                Text(verbatim: statusLine)
            } icon: {
                Image(systemName: statusSymbol)
            }
            .font(.subheadline)
            .foregroundStyle(isFailed ? Color.red : Color.secondary)
            if !outcomeText.isEmpty {
                Text(verbatim: outcomeText)
                    .font(.subheadline)
                    .foregroundStyle(.primary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}
