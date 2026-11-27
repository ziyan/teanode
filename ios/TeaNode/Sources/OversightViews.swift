import SwiftUI
import TeaNodeKit

// The agents page's Oversight tab: what every person's agent is doing, has
// done and has failed at, read-only. A row opens the conversation, where a
// note takes the composer's place.

/// OversightSegment is the list the segmented control shows.
private enum OversightSegment: String, CaseIterable, Identifiable {
    case running, recent, failed

    var id: String { rawValue }

    var title: LocalizedStringKey {
        switch self {
        case .running: "Running"
        case .recent: "Recent"
        case .failed: "Failed"
        }
    }
}

/// OversightRequest is everything the lists are read for: when it changes,
/// the first page is read again.
private struct OversightRequest: Hashable {
    var segment = OversightSegment.running
    var range = OversightRange.lastWeek
    /// A run's kind, or "main" or "named"; nil is every kind.
    var kind: String?
    var query = ""

    /// What empties the lists when it changes. Typing words only narrows
    /// them, so the rows stay until the new page arrives.
    var scope: [String] { [segment.rawValue, range.rawValue, kind ?? ""] }
}

/// OversightView is the operator's read-only view of every person's agent
/// conversations: running now, recent within a range, and the turns that
/// failed, a page at a time from the server. A row opens the transcript,
/// which is logged as read by this person.
struct OversightView: View {
    let connection: Connection
    @State private var request: OversightRequest
    @State private var runs: [ServerAgentRun]?
    @State private var failedTurns: [ServerFailedTurn]?
    @State private var totalCount = 0
    @State private var isLoadingMore = false
    /// The jobs given up on, read once; nil when this person may not read them.
    @State private var overview: AgentAdminOverview?
    @State private var doneText: String?
    @State private var problemText: String?
    /// The conversation opened from this list and pushed over it.
    @State private var openedConversationId: String?

    private static let pageSize = 50

    /// The list the screen opens on, by its name: "running", "recent" or "failed".
    init(connection: Connection, startingSegmentName: String? = nil) {
        self.connection = connection
        var firstRequest = OversightRequest()
        if let startingSegmentName, let segment = OversightSegment(rawValue: startingSegmentName) {
            firstRequest.segment = segment
        }
        _request = State(initialValue: firstRequest)
    }

    var body: some View {
        List {
            Section {
                Picker("Show", selection: $request.segment) {
                    ForEach(OversightSegment.allCases) { segment in
                        Text(segment.title).tag(segment)
                    }
                }
                .pickerStyle(.segmented)
                .listRowBackground(Color.clear)
            }
            rowsSection
            if request.segment == .failed, let overview, !overview.deadJobs.isEmpty {
                deadJobsSection(overview)
            }
        }
        .navigationTitle("Oversight")
        .searchable(text: $request.query, prompt: Text("Search conversations"))
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                filterMenu
            }
        }
        .task { await loadOverview() }
        .onChange(of: request.scope) {
            runs = nil
            failedTurns = nil
        }
        .task(id: request) {
            // Typing narrows the rows as it goes, once the typing pauses.
            try? await Task.sleep(for: .milliseconds(300))
            guard !Task.isCancelled else { return }
            await loadFirstPage()
        }
        .refreshable {
            await loadOverview()
            await loadFirstPage()
        }
        .navigationDestination(item: $openedConversationId) { conversationId in
            RunConversationView(connection: connection, conversationId: conversationId)
        }
        .toast(done: $doneText, problem: $problemText)
    }

    // MARK: Filters

    /// The range and the kind, in one menu. Running has no range, since it is
    /// about now; Failed has no kind, since a failed turn is listed by when.
    private var filterMenu: some View {
        Menu {
            // Each choice is a titled section of plain rows: a menu-style
            // picker inside the menu opened a submenu that drew a blank,
            // checked row.
            if request.segment != .running {
                Section("Range") {
                    Picker("Range", selection: $request.range) {
                        ForEach(OversightRange.allCases) { range in
                            Text(verbatim: Self.rangeTitle(range)).tag(range)
                        }
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                }
            }
            if request.segment != .failed {
                Section("Kind") {
                    Picker("Kind", selection: $request.kind) {
                        Text("Any kind").tag(String?.none)
                        Text("Main chats").tag(String?.some("main"))
                        Text("Named chats").tag(String?.some("named"))
                        ForEach(AgentRunSummary.jobKinds, id: \.self) { jobKind in
                            Text(verbatim: UpkeepWords.runKind(jobKind)).tag(String?.some(jobKind))
                        }
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                }
            }
        } label: {
            Label("Filter", systemImage: isFiltered ? "line.3.horizontal.decrease.circle.fill" : "line.3.horizontal.decrease.circle")
        }
    }

    /// Whether the menu has been moved off what it opens with.
    private var isFiltered: Bool {
        request.kind != nil || request.range != OversightRequest().range
    }

    private static func rangeTitle(_ range: OversightRange) -> String {
        switch range {
        case .lastDay: String(localized: "Last 24 hours")
        case .lastWeek: String(localized: "Last 7 days")
        case .lastMonth: String(localized: "Last 30 days")
        }
    }

    // MARK: Rows

    private var rowsSection: some View {
        Section {
            if request.segment == .failed {
                failedRows
            } else {
                runRows
            }
            if isLoadingMore {
                ProgressView().frame(maxWidth: .infinity)
            }
        } header: {
            if isLoaded {
                Text(verbatim: headerText)
            }
        } footer: {
            Text(footerText)
        }
    }

    /// Whether the first page of what is shown has come.
    private var isLoaded: Bool {
        request.segment == .failed ? failedTurns != nil : runs != nil
    }

    @ViewBuilder
    private var runRows: some View {
        if let runs {
            if runs.isEmpty {
                emptyRow
            }
            ForEach(runs) { run in
                Button {
                    openedConversationId = run.id
                } label: {
                    runRow(run)
                }
                .onAppear {
                    if run.id == runs.last?.id { Task { await loadMore() } }
                }
            }
        } else {
            ProgressView().frame(maxWidth: .infinity)
        }
    }

    @ViewBuilder
    private var failedRows: some View {
        if let failedTurns {
            if failedTurns.isEmpty {
                emptyRow
            }
            ForEach(failedTurns) { turn in
                Button {
                    openedConversationId = turn.conversationId
                } label: {
                    failedRow(turn)
                }
                .onAppear {
                    if turn.id == failedTurns.last?.id { Task { await loadMore() } }
                }
            }
        } else {
            ProgressView().frame(maxWidth: .infinity)
        }
    }

    @ViewBuilder
    private var emptyRow: some View {
        switch request.segment {
        case .running:
            ContentUnavailableView(
                "Nothing is running", systemImage: "moon.zzz", description: Text("No conversation has a turn in flight right now."))
        case .recent:
            ContentUnavailableView(
                "No conversations", systemImage: "bubble.left.and.bubble.right", description: Text("None had activity in this time."))
        case .failed:
            ContentUnavailableView(
                "No failed turns", systemImage: "checkmark.circle", description: Text("No turn ended in an error in this time."))
        }
    }

    private func runRow(_ run: ServerAgentRun) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: run.ownerLabel.isEmpty ? run.agentId : run.ownerLabel).foregroundStyle(Color(.label))
            Text(verbatim: Self.title(of: run)).font(.subheadline).foregroundStyle(Color(.secondaryLabel)).lineLimit(2)
            HStack(spacing: 8) {
                if run.isRunning {
                    Label("Running", systemImage: "waveform")
                        .labelStyle(.tag)
                        .font(.caption)
                        .foregroundStyle(Color(.secondaryLabel))
                        .symbolEffect(.variableColor.iterative, isActive: true)
                }
                // A main chat with no title is already called that above.
                if let jobKind = run.jobKind, !jobKind.isEmpty, !run.title.isEmpty {
                    UpkeepTag(text: Self.kindTitle(jobKind))
                }
                Text(verbatim: runDetails(run)).font(.caption).foregroundStyle(Color(.secondaryLabel)).lineLimit(2)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private func failedRow(_ turn: ServerFailedTurn) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: turn.ownerLabel.isEmpty ? turn.agentId : turn.ownerLabel).foregroundStyle(Color(.label))
            if !turn.conversationTitle.isEmpty {
                Text(verbatim: turn.conversationTitle).font(.subheadline).foregroundStyle(Color(.secondaryLabel)).lineLimit(1)
            }
            if !turn.errorText.isEmpty {
                Text(verbatim: turn.errorText).font(.footnote).foregroundStyle(Color(.secondaryLabel)).lineLimit(3)
            }
            HStack(spacing: 8) {
                UpkeepTag(text: String(localized: "Failed"), tone: .bad)
                if !turn.conversationKind.isEmpty {
                    UpkeepTag(text: Self.kindTitle(turn.conversationKind))
                }
                Text(verbatim: UpkeepWords.when(turn.failedAt)).font(.caption).foregroundStyle(Color(.secondaryLabel)).lineLimit(1)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private func runDetails(_ run: ServerAgentRun) -> String {
        var parts = [UpkeepWords.when(run.lastAt), ServerWords.count(run.tokenCount)]
        if run.usage.cost > 0 { parts.append(UpkeepWords.money(run.usage.cost)) }
        return parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private static func title(of run: ServerAgentRun) -> String {
        if !run.title.isEmpty { return run.title }
        return run.jobKind == "main" ? String(localized: "Main chat") : String(localized: "Untitled conversation")
    }

    private static func kindTitle(_ kind: String) -> String {
        switch kind {
        case "main": String(localized: "Main chat")
        case "named": String(localized: "Named chat")
        case "run": String(localized: "Background run")
        default: UpkeepWords.runKind(kind)
        }
    }

    private var headerText: String {
        let countText = totalCount.formatted()
        switch request.segment {
        case .running:
            return totalCount == 1 ? String(localized: "1 conversation") : String(localized: "\(countText) conversations")
        case .recent:
            let rangeText = Self.rangeTitle(request.range)
            let conversationsText = totalCount == 1 ? String(localized: "1 conversation") : String(localized: "\(countText) conversations")
            return rangeText + " · " + conversationsText
        case .failed:
            let rangeText = Self.rangeTitle(request.range)
            let turnsText = totalCount == 1 ? String(localized: "1 failed turn") : String(localized: "\(countText) failed turns")
            return rangeText + " · " + turnsText
        }
    }

    private var footerText: LocalizedStringKey {
        switch request.segment {
        case .running: "Conversations with a turn in flight right now. Opening one is read-only, and each read is logged."
        case .recent: "Every person's agent conversations, by last activity. Opening one is read-only, and each read is logged."
        case .failed: "Turns that ended in an error, newest first. Opening one is read-only, and each read is logged."
        }
    }

    // MARK: Jobs given up on

    private func deadJobsSection(_ overview: AgentAdminOverview) -> some View {
        Section {
            ForEach(overview.deadJobs) { job in
                let usernameText = overview.username(agentId: job.agentId)
                VStack(alignment: .leading, spacing: 3) {
                    Text("\(job.jobKind) for \(usernameText)").foregroundStyle(Color(.label))
                    Text(verbatim: deadJobDetails(job)).font(.footnote).foregroundStyle(Color(.secondaryLabel))
                }
                .swipeActions {
                    Button("Retry", systemImage: "arrow.clockwise") {
                        Task { await retry(job) }
                    }
                    .tint(.indigo)
                }
                .contextMenu {
                    Button("Open the conversation", systemImage: "bubble.left.and.text.bubble.right") {
                        Task { await openConversation(job) }
                    }
                    Button("Retry", systemImage: "arrow.clockwise") {
                        Task { await retry(job) }
                    }
                }
            }
        } header: {
            Text("Jobs given up on")
        } footer: {
            Text("Press and hold a job to open what it tried, or retry it.")
        }
    }

    private func deadJobDetails(_ job: AgentDeadJob) -> String {
        let countText = job.attemptCount.formatted()
        let timeText = UpkeepWords.when(job.finishedAt)
        var parts = [String(localized: "after \(countText) attempts, \(timeText)")]
        if let errorMessage = job.errorMessage, !errorMessage.isEmpty { parts.append(errorMessage) }
        return parts.joined(separator: " · ")
    }

    // MARK: Loading

    private func runFilter(for requested: OversightRequest) -> ServerAgentRunFilter {
        ServerAgentRunFilter(
            jobKind: requested.kind, query: requested.query,
            since: requested.segment == .recent ? requested.range.sinceText() : nil,
            isRunningOnly: requested.segment == .running)
    }

    private func loadFirstPage() async {
        let requested = request
        do {
            if requested.segment == .failed {
                let page = try await connection.serverFailedAgentTurns(since: requested.range.sinceText(), first: Self.pageSize)
                guard requested == request else { return }
                failedTurns = page.failedTurns
                totalCount = page.totalCount
            } else {
                let page = try await connection.serverAgentRuns(filter: runFilter(for: requested), first: Self.pageSize)
                guard requested == request else { return }
                runs = page.runs
                totalCount = page.totalCount
            }
        } catch is CancellationError {
            return
        } catch {
            problemText = error.localizedDescription
            guard requested == request else { return }
            if requested.segment == .failed {
                if failedTurns == nil { failedTurns = [] }
            } else if runs == nil {
                runs = []
            }
        }
    }

    private func loadMore() async {
        guard !isLoadingMore else { return }
        let requested = request
        do {
            if requested.segment == .failed {
                guard let loaded = failedTurns, loaded.count < totalCount else { return }
                isLoadingMore = true
                defer { isLoadingMore = false }
                let page = try await connection.serverFailedAgentTurns(
                    since: requested.range.sinceText(), first: Self.pageSize, offset: loaded.count)
                guard requested == request else { return }
                let knownIds = Set(loaded.map(\.id))
                failedTurns = loaded + page.failedTurns.filter { !knownIds.contains($0.id) }
                totalCount = page.totalCount
            } else {
                guard let loaded = runs, loaded.count < totalCount else { return }
                isLoadingMore = true
                defer { isLoadingMore = false }
                let page = try await connection.serverAgentRuns(filter: runFilter(for: requested), first: Self.pageSize, offset: loaded.count)
                guard requested == request else { return }
                let knownIds = Set(loaded.map(\.id))
                runs = loaded + page.runs.filter { !knownIds.contains($0.id) }
                totalCount = page.totalCount
            }
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// The jobs given up on need more than observing, so this person may not
    /// read them, and the section is then left out.
    private func loadOverview() async {
        overview = try? await connection.agentAdminOverview(grouping: .day, range: .lastThirtyDays())
    }

    /// Opens what the job tried: its latest run, which is a conversation of
    /// its own, or what a remember job was about when it never got as far as
    /// a run.
    private func openConversation(_ job: AgentDeadJob) async {
        do {
            if let conversationId = try await connection.deadJobConversationId(job) {
                openedConversationId = conversationId
            } else {
                problemText = String(localized: "This job never got as far as a run.")
            }
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func retry(_ job: AgentDeadJob) async {
        do {
            try await connection.retryAgentJob(jobId: job.id)
            doneText = String(localized: "Queued again.")
            await loadOverview()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

#if DEBUG
    #Preview("Oversight") {
        NavigationStack {
            OversightView(connection: Connection(account: DemoServer.account, transport: DemoServer()))
        }
    }
#endif
