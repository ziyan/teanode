import PhotosUI
import SwiftUI
import TeaNodeKit

/// CodingTarget names one coding session to open: the computer and the pane,
/// as no two computers' panes are told apart by the pane's id alone.
struct CodingTarget: Hashable {
    let computer: String
    let paneId: String

    var id: String { "\(computer)/\(paneId)" }
}

/// CodingWorkspace is the coding sessions of one herdr workspace on one
/// computer, in tab order.
struct CodingWorkspace: Identifiable {
    let id: String
    let computer: String
    let title: String
    let sessions: [HerdrSession]
}

/// CodingModel is the list of the person's coding sessions, shared by the
/// list and the session on screen, which keeps the tab strip's states.
@MainActor
@Observable
final class CodingModel {
    let connection: Connection
    var list: HerdrList?
    var loadFailureText: String?

    init(connection: Connection) { self.connection = connection }

    /// Reads the list again; what was known stays when it cannot be read.
    func load() async {
        do {
            let read = try await connection.herdrSessions()
            // A computer that did not answer this time keeps the sessions
            // last known for it, so a session open on screen does not
            // vanish for one slow answer.
            if let previous = list, !read.failedComputerNames.isEmpty {
                let carried = previous.sessions.filter { read.failedComputerNames.contains($0.computer) }
                list = HerdrList(
                    computerNames: read.computerNames, failedComputerNames: read.failedComputerNames,
                    sessions: read.sessions.filter { !read.failedComputerNames.contains($0.computer) } + carried)
            } else {
                list = read
            }
            loadFailureText = nil
        } catch is CancellationError {
        } catch {
            if (error as? URLError)?.code != .cancelled { loadFailureText = error.localizedDescription }
        }
    }

    func session(id: String) -> HerdrSession? {
        list?.sessions.first { $0.id == id }
    }

    /// Puts a session as the server just answered it in the list, so a
    /// change shows before the list is read again.
    func upsert(_ session: HerdrSession) {
        guard let current = list else { return }
        var sessions = current.sessions
        if let index = sessions.firstIndex(where: { $0.id == session.id }) {
            sessions[index] = session
        } else {
            sessions.append(session)
        }
        list = HerdrList(computerNames: current.computerNames, failedComputerNames: current.failedComputerNames, sessions: sessions)
    }

    func remove(id: String) {
        guard let current = list else { return }
        list = HerdrList(
            computerNames: current.computerNames, failedComputerNames: current.failedComputerNames,
            sessions: current.sessions.filter { $0.id != id })
    }

    /// The sessions of the same workspace as this one, by tab number.
    func siblings(of session: HerdrSession) -> [HerdrSession] {
        guard !session.workspaceId.isEmpty else { return [session] }
        let siblings = (list?.sessions ?? []).filter { $0.computer == session.computer && $0.workspaceId == session.workspaceId }
        return siblings.sorted { $0.tabNumber < $1.tabNumber }
    }

    /// Every workspace, by computer in name order, so each stays where the
    /// person last saw it, then by workspace number.
    var workspaces: [CodingWorkspace] {
        guard let list else { return [] }
        var result: [CodingWorkspace] = []
        var computers = list.computerNames
        for session in list.sessions where !computers.contains(session.computer) {
            computers.append(session.computer)
        }
        computers.sort { $0.localizedCaseInsensitiveCompare($1) == .orderedAscending }
        for computer in computers {
            let onComputer = list.sessions.filter { $0.computer == computer }
            var seen: [String] = []
            for session in onComputer where !seen.contains(session.workspaceId) {
                seen.append(session.workspaceId)
            }
            let grouped = seen.map { workspaceId in
                let sessions = onComputer.filter { $0.workspaceId == workspaceId }.sorted { $0.tabNumber < $1.tabNumber }
                return CodingWorkspace(
                    id: "\(computer)/\(workspaceId)", computer: computer, title: sessions.first?.workspaceTitle ?? computer, sessions: sessions)
            }
            result += grouped.sorted { ($0.sessions.first?.workspaceNumber ?? 0) < ($1.sessions.first?.workspaceNumber ?? 0) }
        }
        return result
    }

    /// The workspaces of each computer, computers in name order.
    var computers: [CodingComputer] {
        var result: [CodingComputer] = []
        for workspace in workspaces {
            if let last = result.last, last.name == workspace.computer {
                result[result.count - 1].workspaces.append(workspace)
            } else {
                result.append(CodingComputer(name: workspace.computer, workspaces: [workspace]))
            }
        }
        return result
    }
}

/// CodingComputer is one computer's workspaces, and what its sessions are
/// doing, for its heading in the list.
struct CodingComputer: Identifiable {
    let name: String
    var workspaces: [CodingWorkspace]

    var id: String { name }
    var sessions: [HerdrSession] { workspaces.flatMap(\.sessions) }
    var askingCount: Int { sessions.filter(\.isAsking).count }
    var workingCount: Int { sessions.filter(\.isWorking).count }
}

/// CodingScreen is the Coding section: the coding sessions on the person's
/// computers, by computer and workspace, each opening to read and type into.
struct CodingScreen: View {
    let connection: Connection
    @Environment(AppModel.self) private var app
    @Environment(\.scenePhase) private var scenePhase
    @State private var model: CodingModel
    @State private var path = NavigationPath()
    @State private var isOpening = false
    @State private var doneText: String?
    @State private var problemText: String?

    init(connection: Connection) {
        self.connection = connection
        _model = State(initialValue: CodingModel(connection: connection))
    }

    var body: some View {
        NavigationStack(path: $path) {
            List {
                if let list = model.list {
                    // A section for each computer, and in it each workspace's
                    // name above its tabs, laid out as the knowledge list is:
                    // no chevrons, no lines, rows close together.
                    ForEach(model.computers) { computer in
                        Section {
                            ForEach(computer.workspaces) { workspace in
                                CodingWorkspaceHeader(workspace: workspace)
                                    .listRowInsets(ListRowSpacing.insets)
                                    .listRowSeparator(.hidden)
                                ForEach(workspace.sessions) { session in
                                    NavigationLink(value: CodingTarget(computer: session.computer, paneId: session.paneId)) {
                                        CodingSessionRow(session: session)
                                    }
                                    .knowledgeListRow()
                                }
                            }
                        } header: {
                            CodingComputerHeader(computer: computer)
                        }
                        .listSectionSeparator(.hidden)
                    }
                    if !list.failedComputerNames.isEmpty {
                        let names = list.failedComputerNames.joined(separator: ", ")
                        Section {
                            Label(String(localized: "\(names) did not answer, so their sessions are not listed."), systemImage: "wifi.exclamationmark")
                                .foregroundStyle(.secondary)
                        }
                    }
                }
            }
            .listStyle(.plain)
            .listSectionSpacing(.compact)
            .navigationTitle("Coding")
            .withoutSwipeBack()
            .navigationDestination(for: CodingTarget.self) { target in
                CodingSessionView(model: model, initialId: target.id, doneText: $doneText, problemText: $problemText)
            }
            .overlay {
                if let list = model.list {
                    if list.sessions.isEmpty {
                        ContentUnavailableView(
                            "No coding sessions", systemImage: "chevron.left.forwardslash.chevron.right",
                            description: Text("Open one with the plus button, or start Claude Code or Codex in herdr on your computer."))
                    }
                } else if let failure = model.loadFailureText {
                    ContentUnavailableView("The coding sessions could not be read", systemImage: "wifi.exclamationmark", description: Text(failure))
                } else {
                    ProgressView()
                }
            }
            .toolbar {
                MenuButton()
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        isOpening = true
                    } label: {
                        Label("Open a session", systemImage: "plus")
                    }
                    .disabled(model.list?.computerNames.isEmpty ?? true)
                }
            }
            // Read while this list is what is on screen, and not while a
            // session is open over it, which reads the list itself.
            .task(id: "\(path.isEmpty)-\(scenePhase == .active)") {
                guard path.isEmpty, scenePhase == .active else { return }
                while !Task.isCancelled {
                    await model.load()
                    if model.list?.computerNames.isEmpty == false { app.noteCodingComputers() }
                    do { try await Task.sleep(for: .seconds(10)) } catch { return }
                }
            }
            .refreshable { await model.load() }
            .sheet(isPresented: $isOpening) {
                CodingOpenSheet(model: model) { session in
                    model.upsert(session)
                    path.append(CodingTarget(computer: session.computer, paneId: session.paneId))
                }
            }
            #if DEBUG
                // A screenshot of a session with its tab strip and
                // transcript (`-TeaNodeScreen coding-session`).
                .task {
                    if DemoServer.isRequested, DemoServer.screen == "coding-session", path.isEmpty {
                        path.append(CodingTarget(computer: "laptop", paneId: "p2"))
                    }
                }
            #endif
        }
        .toast(done: $doneText, problem: $problemText)
    }
}

/// CodingComputerHeader heads a computer's section, as the lists head
/// their periods: its name, and how many of its sessions ask and work.
struct CodingComputerHeader: View {
    let computer: CodingComputer

    var body: some View {
        // The symbol close to the name, as the knowledge rows have theirs;
        // a Label sets it in a column of its own, far from the words.
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Image(systemName: "desktopcomputer")
                .foregroundStyle(.secondary)
            Text(verbatim: computer.name)
                .font(.title3.weight(.semibold))
                .foregroundStyle(.primary)
            Spacer(minLength: 8)
            Text(countsText)
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .textCase(nil)
        .accessibilityElement(children: .combine)
    }

    private var countsText: String {
        var parts: [String] = []
        if computer.askingCount > 0 { parts.append(String(localized: "\(computer.askingCount) asking")) }
        if computer.workingCount > 0 { parts.append(String(localized: "\(computer.workingCount) working")) }
        return parts.joined(separator: " · ")
    }
}

/// CodingWorkspaceHeader names a workspace above its tabs, as the shared
/// knowledge list names each owner above their pages.
struct CodingWorkspaceHeader: View {
    let workspace: CodingWorkspace

    var body: some View {
        HStack(spacing: 4) {
            Image(systemName: "folder")
            Text(verbatim: workspace.title)
        }
        .font(.subheadline.weight(.semibold))
        .foregroundStyle(.secondary)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }
}

/// CodingSessionRow is one coding session in the list: its title, its tab
/// and agent, and what it is doing when that is working or asking.
struct CodingSessionRow: View {
    let session: HerdrSession

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: session.rowTitle)
                    .font(.headline)
                    .lineLimit(1)
                if !session.rowDetail.isEmpty {
                    Text(verbatim: session.rowDetail)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 8)
            CodingStateLabel(session: session)
        }
        .accessibilityElement(children: .combine)
    }
}

/// CodingStateLabel says a session is working or asking, with a symbol and
/// words; an idle one is not marked, as the list marks only what calls for
/// a look.
struct CodingStateLabel: View {
    let session: HerdrSession

    var body: some View {
        if session.isAsking {
            Label("Asking", systemImage: "questionmark.bubble")
                .labelStyle(.tag)
                .font(.caption)
                .foregroundStyle(.orange)
        } else if session.isWorking {
            Label("Working", systemImage: "ellipsis")
                .labelStyle(.tag)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }
}

/// CodingPart is what a session shows: what was said, or its screen.
enum CodingPart: String, CaseIterable, Identifiable {
    case turns, screen

    var id: String { rawValue }
}

/// CodingSessionView is one coding session: its workspace's tabs, what was
/// said in it or its screen, the question it asks, and a field to type into
/// it. It reads again while it is on screen.
struct CodingSessionView: View {
    let model: CodingModel
    let initialId: String
    @Binding var doneText: String?
    @Binding var problemText: String?
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase
    @State private var selectedId: String
    @State private var part = CodingPart.turns
    @State private var turns: [HerdrTurn] = []
    @State private var isTruncated = false
    @State private var turnCount = CodingSessionView.initialTurnCount
    @State private var screenText = ""
    @State private var isRead = false
    // What has been typed into each session's box and not sent, by session,
    // kept while the person looks at the workspace's other tabs.
    @State private var drafts: [String: String] = [:]
    @State private var pendingFilesBySession: [String: [PendingFile]] = [:]
    @State private var isSending = false
    @State private var sentCount = 0
    @State private var isOpeningTab = false
    @State private var isConfirmingClose = false
    @State private var isAtEnd = true
    @State private var scrollPosition = ScrollPosition(edge: .bottom)

    /// How many turns are read at first, and the most the server gives.
    static let initialTurnCount = 50
    static let greatestTurnCount = 100

    init(model: CodingModel, initialId: String, doneText: Binding<String?>, problemText: Binding<String?>) {
        self.model = model
        self.initialId = initialId
        _doneText = doneText
        _problemText = problemText
        _selectedId = State(initialValue: initialId)
    }

    private var connection: Connection { model.connection }
    private var session: HerdrSession? { model.session(id: selectedId) }

    var body: some View {
        Group {
            if let session {
                content(session)
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .navigationTitle(session?.workspaceTitle ?? "")
        .navigationSubtitle(session?.computer ?? "")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if let session {
                ToolbarItem(placement: .topBarTrailing) {
                    actionsMenu(session)
                }
            }
        }
        // Read while this is on screen and the app is in front: quickly
        // while the session works or asks, slowly while it is idle.
        .task(id: "\(selectedId)-\(part.rawValue)-\(turnCount)-\(scenePhase == .active)") {
            guard scenePhase == .active else { return }
            while !Task.isCancelled {
                await refresh()
                let isBusy = session?.isWorking == true || session?.isAsking == true
                do { try await Task.sleep(for: .seconds(isBusy ? 3 : 15)) } catch { return }
            }
        }
        .onChange(of: part) {
            // The other part has not been read yet: a spinner until it is.
            screenText = ""
            isRead = false
        }
        .onChange(of: selectedId) {
            turns = []
            screenText = ""
            isRead = false
            isTruncated = false
            turnCount = Self.initialTurnCount
        }
        .sensoryFeedback(.success, trigger: sentCount)
    }

    // MARK: Layout

    @ViewBuilder
    private func content(_ session: HerdrSession) -> some View {
        VStack(spacing: 0) {
            tabStrip(session)
            // Turns or the screen, by one round button floating over the
            // top right of what is shown, rather than a row of its own.
            Group {
                switch part {
                case .turns: transcript(session)
                case .screen: screen(session)
                }
            }
            .overlay(alignment: .topTrailing) {
                Button {
                    part = part == .turns ? .screen : .turns
                } label: {
                    Image(systemName: part == .turns ? "terminal" : "text.bubble")
                        .font(.body.weight(.medium))
                        .frame(width: 44, height: 44)
                        .contentShape(.circle)
                }
                .buttonStyle(.glass)
                .buttonBorderShape(.circle)
                .accessibilityLabel(part == .turns ? Text("Show the screen") : Text("Show the turns"))
                .padding(.trailing)
                .padding(.top, 4)
            }
        }
        // Nothing can be sent while the session asks, and its question takes
        // a typed answer of its own, so the box gives its room to it.
        .safeAreaInset(edge: .bottom) {
            if !session.isAsking {
                composer(session)
            }
        }
    }

    /// The workspace's coding sessions, one chip each in tab order, and the
    /// way to open another beside them. The chips scroll sideways, so the
    /// largest text size does not cut any off; the plus stays in place.
    private func tabStrip(_ session: HerdrSession) -> some View {
        // The chosen tab is kept in view: when the session opens, and
        // whenever another is chosen or the tabs change. The way to open
        // another is the strip's last chip, drawn like the tabs and
        // scrolling with them.
        ScrollViewReader { reader in
            ScrollView(.horizontal) {
                HStack(spacing: 8) {
                    ForEach(model.siblings(of: session)) { tab in
                        Button {
                            selectedId = tab.id
                        } label: {
                            CodingTabChip(session: tab, isSelected: tab.id == selectedId)
                        }
                        .buttonStyle(.plain)
                        .id(tab.id)
                    }
                    Menu {
                        Button { Task { await openTab(beside: session, codingAgentKind: "claude") } } label: { Text(verbatim: "Claude Code") }
                        Button { Task { await openTab(beside: session, codingAgentKind: "codex") } } label: { Text(verbatim: "Codex") }
                    } label: {
                        Group {
                            if isOpeningTab {
                                ProgressView()
                            } else {
                                Image(systemName: "plus")
                                    .font(.subheadline.weight(.semibold))
                            }
                        }
                        .foregroundStyle(.secondary)
                        .frame(width: 44, height: 44)
                        .background(Color(.tertiarySystemFill), in: .circle)
                        .contentShape(.circle)
                    }
                    .disabled(isOpeningTab)
                    // Grey like the tabs' words, not the accent a menu
                    // takes on its own.
                    .tint(.secondary)
                    .accessibilityLabel("New tab")
                }
                .padding(.horizontal)
            }
            .scrollIndicators(.hidden)
            // Dragging the tabs sideways scrolls them, never opens the menu.
            .keepsSidePanelClosed()
            .onAppear { reader.scrollTo(selectedId) }
            .onChange(of: selectedId) {
                withAnimation { reader.scrollTo(selectedId) }
            }
            .onChange(of: model.siblings(of: session).count) { reader.scrollTo(selectedId) }
        }
        .padding(.vertical, 4)
    }

    private func transcript(_ session: HerdrSession) -> some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 12) {
                if isTruncated, turnCount < Self.greatestTurnCount {
                    Button {
                        turnCount = Self.greatestTurnCount
                    } label: {
                        Text("Show earlier turns").frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.bordered)
                    .buttonBorderShape(.capsule)
                    .controlSize(.large)
                }
                ForEach(Array(turns.enumerated()), id: \.offset) { _, turn in
                    CodingTurnView(turn: turn)
                }
                if turns.isEmpty, isRead {
                    Text("Nothing has been said in this session yet.")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 24)
                }
                questionCard(session)
            }
            .padding(.horizontal)
            .padding(.vertical, 12)
        }
        .scrollPosition($scrollPosition)
        .onScrollGeometryChange(for: Bool.self) { geometry in
            geometry.contentSize.height - geometry.visibleRect.maxY < 120
        } action: { _, isNearEnd in
            isAtEnd = isNearEnd
        }
        .defaultScrollAnchor(.bottom)
        .scrollDismissesKeyboard(.immediately)
        .overlay {
            if !isRead {
                ProgressView()
            }
        }
        // New turns keep the view at the end only when the person was
        // there; one reading earlier turns is not pulled away.
        .onChange(of: turns) {
            if isAtEnd { scrollPosition.scrollTo(edge: .bottom) }
        }
        .onChange(of: session.question) {
            if isAtEnd { scrollPosition.scrollTo(edge: .bottom) }
        }
        .onChange(of: isRead) { _, isRead in
            if isRead { scrollPosition.scrollTo(edge: .bottom) }
        }
        .onChange(of: sentCount) {
            withAnimation { scrollPosition.scrollTo(edge: .bottom) }
        }
    }

    /// The screen as the program in the pane colored it, on a terminal's
    /// dark ground whatever the app's appearance, since its colors were
    /// chosen against one.
    private var coloredScreen: AttributedString {
        var screen = AttributedString()
        for run in terminalRuns(screenText) {
            var part = AttributedString(run.text)
            var foreground = run.foreground.map(Color.init(terminal:)) ?? .terminalText
            var background = run.background.map(Color.init(terminal:))
            if run.isInverse {
                (foreground, background) = (background ?? .terminalBackground, foreground)
            }
            part.foregroundColor = run.isDim ? foreground.opacity(0.6) : foreground
            if let background { part.backgroundColor = background }
            if run.isBold { part.inlinePresentationIntent = .stronglyEmphasized }
            if run.isItalic { part.inlinePresentationIntent = (part.inlinePresentationIntent ?? []).union(.emphasized) }
            if run.isUnderlined { part.underlineStyle = .single }
            screen += part
        }
        return screen
    }

    private func screen(_ session: HerdrSession) -> some View {
        ScrollView([.vertical, .horizontal]) {
            VStack(alignment: .leading, spacing: 12) {
                Text(coloredScreen)
                    .font(.caption.monospaced())
                    .textSelection(.enabled)
                    .fixedSize()
                    .padding(10)
                    .background(Color.terminalBackground, in: .rect(cornerRadius: 8))
                questionCard(session)
            }
            .padding()
        }
        // The screen is wider than the phone and is panned both ways; a
        // drag to the right moves it, never opens the side menu.
        .keepsSidePanelClosed()
        .overlay {
            if !isRead {
                ProgressView()
            }
        }
    }

    @ViewBuilder
    private func questionCard(_ session: HerdrSession) -> some View {
        if session.isAsking, let question = session.codingQuestion {
            CardSurface {
                CodingAnswerCard(question: question) { options, freeText in
                    await answer(question, options: options, freeText: freeText)
                }
            }
            .id(question.questionFingerprint)
        }
    }

    private func actionsMenu(_ session: HerdrSession) -> some View {
        Menu {
            Button {
                Task { await show(session) }
            } label: {
                Label("Show on computer", systemImage: "display")
            }
            if session.isWatched {
                Button {} label: {
                    Label("Telling you when it finishes", systemImage: "bell.fill")
                }
                .disabled(true)
            } else {
                Button {
                    Task { await watch(session) }
                } label: {
                    Label("Tell me when it finishes", systemImage: "bell")
                }
            }
            Button(role: .destructive) {
                isConfirmingClose = true
            } label: {
                Label("Close tab", systemImage: "xmark.circle")
            }
            .disabled(session.isWorking)
        } label: {
            Label("Actions", systemImage: "ellipsis.circle")
        }
        // On the menu, not on its item, which goes when the menu closes.
        .confirmationDialog("Close this coding session?", isPresented: $isConfirmingClose, titleVisibility: .visible) {
            Button("Close tab", role: .destructive) {
                Task { await close(session) }
            }
        } message: {
            Text("This ends \(session.codingAgentName) in \(session.tabTitle) on \(session.computer) and closes its pane. What was said stays in its history.")
        }
    }

    private func composer(_ session: HerdrSession) -> some View {
        let sessionId = session.id
        return CodingComposer(
            draft: Binding(get: { drafts[sessionId] ?? "" }, set: { drafts[sessionId] = $0 }),
            pendingFiles: Binding(get: { pendingFilesBySession[sessionId] ?? [] }, set: { pendingFilesBySession[sessionId] = $0 }),
            isSending: isSending, isDisabled: session.isAsking
        ) {
            Task { await send(session) }
        }
    }

    // MARK: Reading

    private func refresh() async {
        // Opened before the list was read (from a link or a notification):
        // read it first, rather than wait for the next turn of the loop.
        if session == nil { await model.load() }
        guard let session else { return }
        let reading = session.id
        let readingPart = part
        // A read for another tab or the other part, finishing late, says
        // nothing about what is on screen now.
        let isCurrent = { reading == selectedId && readingPart == part }
        do {
            switch part {
            // A session that has not said anything yet has no history to
            // read: its transcript is empty, which is not a failure.
            case .turns where session.transcriptPath.isEmpty:
                turns = []
                isTruncated = false
            case .turns:
                let read = try await connection.readHerdrSession(computer: session.computer, paneId: session.paneId, turnCount: turnCount)
                guard isCurrent() else { return }
                turns = read.turns
                isTruncated = read.isTruncated
                model.upsert(read.herdrSession)
            case .screen:
                let read = try await connection.readHerdrScreen(computer: session.computer, paneId: session.paneId)
                guard isCurrent() else { return }
                screenText = read.screenText
                model.upsert(read.herdrSession)
            }
            isRead = true
            // The other tabs' states, for the strip.
            await model.load()
        } catch is CancellationError {
        } catch {
            guard isCurrent(), (error as? URLError)?.code != .cancelled else { return }
            problemText = app.describe(error)
            isRead = true
        }
    }

    // MARK: Actions

    private func send(_ session: HerdrSession) async {
        let sessionId = session.id
        let text = (drafts[sessionId] ?? "").trimmed
        let pendingFiles = pendingFilesBySession[sessionId] ?? []
        guard !text.isEmpty || !pendingFiles.isEmpty, !isSending else { return }
        guard pendingFiles.reduce(0, { $0 + $1.data.count }) <= PendingFile.bytesMost else {
            problemText = String(localized: "The files come to more than 25 MB, more than the server takes in one message.")
            return
        }
        isSending = true
        defer { isSending = false }
        let sending = pendingFiles
        let sendingIds = Set(sending.map(\.id))
        do {
            var attachmentIds: [String] = []
            for file in sending {
                attachmentIds.append(try await connection.upload(name: file.name, contentType: file.contentType, data: file.data))
            }
            let sent = try await connection.sendHerdrSession(computer: session.computer, paneId: session.paneId, text: text, attachmentIds: attachmentIds)
            model.upsert(sent)
            drafts[sessionId] = nil
            // Files added while these were sent stay for the next message.
            pendingFilesBySession[sessionId]?.removeAll { sendingIds.contains($0.id) }
            sentCount += 1
            await refresh()
        } catch {
            problemText = app.describe(error)
        }
    }

    private func answer(_ question: CodingQuestion, options: [CodingQuestion.Option], freeText: String?) async {
        do {
            _ = try await connection.answerCodingQuestion(question, options: options, freeText: freeText)
            sentCount += 1
            await refresh()
        } catch {
            problemText = app.describe(error)
            await refresh()
        }
    }

    private func show(_ session: HerdrSession) async {
        do {
            model.upsert(try await connection.focusHerdrSession(computer: session.computer, paneId: session.paneId))
            doneText = String(localized: "Shown on \(session.computer)")
            await model.load()
        } catch {
            problemText = app.describe(error)
        }
    }

    private func watch(_ session: HerdrSession) async {
        do {
            model.upsert(try await connection.watchHerdrSession(computer: session.computer, paneId: session.paneId))
            doneText = String(localized: "Your agent will tell you when \(session.tabTitle) finishes")
        } catch {
            problemText = app.describe(error)
        }
    }

    private func close(_ session: HerdrSession) async {
        do {
            // The tab to move to, chosen before this one leaves the list: the
            // next one in the workspace, else the one before.
            let tabs = model.siblings(of: session)
            let position = tabs.firstIndex { $0.id == session.id }
            let nextTab = position.flatMap { index -> HerdrSession? in
                if index + 1 < tabs.count { return tabs[index + 1] }
                return index > 0 ? tabs[index - 1] : nil
            }
            _ = try await connection.closeHerdrSession(computer: session.computer, paneId: session.paneId)
            model.remove(id: session.id)
            doneText = String(localized: "Closed \(session.tabTitle)")
            if let nextTab {
                selectedId = nextTab.id
            } else {
                dismiss()
            }
        } catch {
            problemText = app.describe(error)
        }
    }

    /// Opens a new tab in this session's workspace, in its directory, and
    /// shows it.
    private func openTab(beside session: HerdrSession, codingAgentKind: String) async {
        isOpeningTab = true
        defer { isOpeningTab = false }
        do {
            let opened = try await connection.openHerdrSession(computer: session.computer, paneId: session.paneId, codingAgentKind: codingAgentKind)
            model.upsert(opened)
            selectedId = opened.id
            await model.load()
        } catch {
            problemText = app.describe(error)
        }
    }
}

/// CodingTabChip is one tab of the strip: its name, a dot when herdr shows
/// it on the computer's screen, and a mark when it works or asks. The one
/// shown here is on a tinted ground.
struct CodingTabChip: View {
    let session: HerdrSession
    let isSelected: Bool

    var body: some View {
        HStack(spacing: 6) {
            // The room for the dot and the mark is always there, so a tab
            // gaining or losing focus or work does not move its neighbors.
            Circle()
                .fill(.primary)
                .frame(width: 6, height: 6)
                .opacity(session.isTabFocused ? 1 : 0)
                .accessibilityHidden(true)
            // One weight whether chosen or not, so choosing a tab does not
            // move the others; the background says which is chosen.
            Text(verbatim: session.chipTitle)
                .font(.subheadline)
                .lineLimit(1)
                .frame(maxWidth: 220, alignment: .leading)
            Group {
                if session.isAsking {
                    Image(systemName: "questionmark.bubble.fill")
                        .font(.caption)
                        .foregroundStyle(.orange)
                } else if session.isWorking {
                    Image(systemName: "ellipsis")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .frame(width: 16, height: 16)
            // The slot is a fixed size, so its mark stops growing where it
            // would spill out of it.
            .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
            .accessibilityHidden(true)
        }
        .foregroundStyle(isSelected ? .primary : .secondary)
        .padding(.horizontal, 14)
        .frame(minHeight: 44)
        .background(isSelected ? AnyShapeStyle(Color.accentColor.opacity(0.2)) : AnyShapeStyle(Color(.tertiarySystemFill)), in: .capsule)
        .contentShape(.capsule)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityText)
        .accessibilityAddTraits(isSelected ? .isSelected : [])
    }

    private var accessibilityText: String {
        var parts = [session.chipTitle]
        if session.isTabFocused { parts.append(String(localized: "On screen")) }
        if session.isAsking {
            parts.append(String(localized: "Asking"))
        } else if session.isWorking {
            parts.append(String(localized: "Working"))
        }
        return parts.joined(separator: ", ")
    }
}

/// CodingTurnView is one thing said or done in a session: the person's words
/// on the right in a bubble, the agent's on the left, a tool as one quiet
/// line.
/// CodingToolTurnView is a tool call: one line, which opens, when the read
/// gave them, to the tool's whole input and what it answered.
struct CodingToolTurnView: View {
    let turn: HerdrTurn
    @State private var isOpen = false

    var body: some View {
        if turn.hasToolDetail {
            // The system's own disclosure, which says open or closed to
            // VoiceOver as it does everywhere else.
            DisclosureGroup(isExpanded: $isOpen) {
                VStack(alignment: .leading, spacing: 10) {
                    if let input = turn.toolInputText, !input.isEmpty {
                        detail(title: String(localized: "Input"), text: input)
                    }
                    if let result = turn.toolResultText, !result.isEmpty {
                        detail(title: String(localized: "Result"), text: result)
                    }
                }
                .padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 10))
                .padding(.top, 6)
            } label: {
                line
            }
            .tint(.secondary)
            .padding(.horizontal, 4)
        } else {
            line.padding(.horizontal, 4)
        }
    }

    private var line: some View {
        Text(verbatim: turn.turnText)
            .font(.caption.monospaced())
            .foregroundStyle(.secondary)
            .lineLimit(isOpen ? nil : 2)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func detail(title: String, text: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title)
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)
            Text(verbatim: text)
                .font(.caption.monospaced())
                .textSelection(.enabled)
        }
    }
}

struct CodingTurnView: View {
    let turn: HerdrTurn

    var body: some View {
        switch turn.herdrTurnRole {
        case "person":
            HStack {
                Spacer(minLength: 56)
                Text(verbatim: turn.turnText)
                    .textSelection(.enabled)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .background(Color.accentColor.opacity(0.2), in: .rect(cornerRadius: 20, style: .continuous))
            }
        case "tool":
            CodingToolTurnView(turn: turn)
        default:
            AgentBubble {
                Text(Self.markdown(turn.turnText))
                    .textSelection(.enabled)
            }
        }
    }

    /// The agent writes Markdown; its line breaks are kept.
    static func markdown(_ text: String) -> AttributedString {
        (try? AttributedString(markdown: text, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace))) ?? AttributedString(text)
    }
}

/// CodingAnswerCard is the question a session waits on, with its options as
/// rows to choose: one press answers, or with several allowed the chosen ones
/// are sent together. An option that takes typed text has a field.
struct CodingAnswerCard: View {
    let question: CodingQuestion
    let onAnswer: ([CodingQuestion.Option], String?) async -> Void
    @State private var chosen: Set<Int> = []
    @State private var typed = ""
    @State private var isAnswering = false
    @State private var isShowingDetails = false

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                CardHeader(systemImage: "terminal", title: [question.codingAgentName, question.sessionName].filter { !$0.isEmpty }.joined(separator: " · "))
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 4)
                Button(isShowingDetails ? "Hide details" : "Details") {
                    withAnimation { isShowingDetails.toggle() }
                }
                .font(.footnote)
                .buttonStyle(.borderless)
            }
            Text(verbatim: question.headline)
                .lineLimit(isShowingDetails ? 3 : nil)
            if isShowingDetails {
                CodingQuestionDetails(text: question.shownText)
            }
            VStack(spacing: 0) {
                ForEach(Array(question.choices.enumerated()), id: \.element.optionNumber) { index, option in
                    if index > 0 { Divider() }
                    ChoiceRow(
                        title: option.optionLabel, detail: option.optionDescription, isChosen: chosen.contains(option.optionNumber),
                        isMultiple: question.isMultipleChoice
                    ) {
                        if question.isMultipleChoice {
                            if chosen.contains(option.optionNumber) {
                                chosen.remove(option.optionNumber)
                            } else {
                                chosen.insert(option.optionNumber)
                            }
                        } else {
                            submit(options: [option], freeText: nil)
                        }
                    }
                }
            }
            .disabled(isAnswering)
            if question.isMultipleChoice {
                Button {
                    submit(options: question.choices.filter { chosen.contains($0.optionNumber) }, freeText: nil)
                } label: {
                    Text("Answer with these").frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .buttonBorderShape(.capsule)
                .controlSize(.large)
                .disabled(chosen.isEmpty || isAnswering)
            }
            if question.options.contains(where: { $0.herdrOptionKind == "freeText" }) {
                TextField("Type an answer", text: $typed, axis: .vertical)
                    .lineLimit(1...4)
                    .padding(12)
                    .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 12, style: .continuous))
                Button {
                    submit(options: [], freeText: typed.trimmed)
                } label: {
                    Text("Answer").frame(maxWidth: .infinity)
                }
                .buttonStyle(.bordered)
                .buttonBorderShape(.capsule)
                .controlSize(.large)
                .disabled(typed.trimmed.isEmpty || isAnswering)
            }
        }
    }

    private func submit(options: [CodingQuestion.Option], freeText: String?) {
        isAnswering = true
        Task {
            await onAnswer(options, freeText)
            isAnswering = false
        }
    }
}

/// CodingComposer is where the person types into a session: one rounded field
/// with the words on top, adding files at the bottom left and sending at the
/// bottom right, as the conversation's is. It is off while the session asks,
/// since a typed line would answer the question by accident.
struct CodingComposer: View {
    @Binding var draft: String
    @Binding var pendingFiles: [PendingFile]
    let isSending: Bool
    let isDisabled: Bool
    let onSend: () -> Void
    @State private var pickedItems: [PhotosPickerItem] = []
    @State private var isPickingPhotos = false
    @State private var isTakingPicture = false
    @State private var isPickingFiles = false
    @State private var loadingPictureCount = 0
    @FocusState private var isFocused: Bool

    private var canSend: Bool {
        !isDisabled && !isSending && (!draft.trimmed.isEmpty || !pendingFiles.isEmpty)
    }

    var body: some View {
        VStack(spacing: 8) {
            if !pendingFiles.isEmpty || loadingPictureCount > 0 {
                ScrollView(.horizontal) {
                    HStack {
                        ForEach(pendingFiles) { file in
                            PendingFileChip(file: file, isUploading: isSending, isUploaded: false) {
                                pendingFiles.removeAll { $0.id == file.id }
                            }
                        }
                        ForEach(0..<loadingPictureCount, id: \.self) { _ in
                            LoadingFileChip()
                        }
                    }
                }
                .scrollIndicators(.hidden)
            }
            VStack(alignment: .leading, spacing: 4) {
                TextField(isDisabled ? String(localized: "Answer its question first") : String(localized: "Type an instruction for this session"), text: $draft, axis: .vertical)
                    .lineLimit(1...8)
                    .focused($isFocused)
                    .disabled(isDisabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 16)
                    .padding(.top, 14)
                HStack(spacing: 4) {
                    Menu {
                        Button {
                            isPickingPhotos = true
                        } label: {
                            Label("Photo library", systemImage: "photo.on.rectangle")
                        }
                        if UIImagePickerController.isSourceTypeAvailable(.camera) {
                            Button {
                                isTakingPicture = true
                            } label: {
                                Label("Camera", systemImage: "camera")
                            }
                        }
                        Button {
                            isPickingFiles = true
                        } label: {
                            Label("Files", systemImage: "folder")
                        }
                        // A picture copied elsewhere, a screenshot say,
                        // attached as the dashboard attaches a pasted one.
                        if UIPasteboard.general.hasImages {
                            Button {
                                for image in UIPasteboard.general.images ?? [] {
                                    if let file = PendingFile(picture: image, index: pendingFiles.count) {
                                        pendingFiles.append(file)
                                    }
                                }
                            } label: {
                                Label("Paste picture", systemImage: "doc.on.clipboard")
                            }
                        }
                    } label: {
                        Image(systemName: "plus")
                            .font(.body.weight(.medium))
                            .foregroundStyle(.secondary)
                            .frame(width: 36, height: 36)
                            .background(Color(.tertiarySystemFill), in: .circle)
                            .frame(minWidth: 44, minHeight: 44)
                            .contentShape(.rect)
                    }
                    .disabled(isDisabled)
                    .accessibilityLabel("Add files")
                    Spacer()
                    Button(action: onSend) {
                        Group {
                            if isSending {
                                ProgressView().tint(.white)
                            } else {
                                Image(systemName: "arrow.up")
                                    .font(.body.weight(.semibold))
                                    .foregroundStyle(.white)
                            }
                        }
                        .frame(width: 36, height: 36)
                        .background(canSend || isSending ? AnyShapeStyle(.tint) : AnyShapeStyle(Color(.tertiaryLabel)), in: .circle)
                        .frame(minWidth: 44, minHeight: 44)
                    }
                    .disabled(!canSend)
                    .accessibilityLabel(isSending ? Text("Sending…") : Text("Send"))
                }
                // The icons sit in circles of a fixed size; past this size
                // their glyphs would not fit in them.
                .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
                .padding(.horizontal, 6)
                .padding(.bottom, 4)
            }
            .background {
                Color.clear
                    .contentShape(.rect(cornerRadius: 26, style: .continuous))
                    .onTapGesture { isFocused = true }
            }
            .glassEffect(.regular, in: .rect(cornerRadius: 26, style: .continuous))
        }
        .padding(.horizontal)
        .padding(.bottom, 8)
        .photosPicker(isPresented: $isPickingPhotos, selection: $pickedItems, maxSelectionCount: 8, matching: .images)
        .cameraPicker(isPresented: $isTakingPicture) { picture in
            if let file = PendingFile(picture: picture, index: pendingFiles.count) {
                pendingFiles.append(file)
            }
        }
        .fileImporter(isPresented: $isPickingFiles, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
            guard case .success(let addresses) = result else { return }
            for address in addresses {
                if let file = PendingFile(fileAt: address) {
                    pendingFiles.append(file)
                }
            }
        }
        .onChange(of: pickedItems) { _, items in
            guard !items.isEmpty else { return }
            pickedItems = []
            Task { await add(items) }
        }
    }

    private func add(_ items: [PhotosPickerItem]) async {
        loadingPictureCount += items.count
        for item in items {
            defer { loadingPictureCount -= 1 }
            guard let data = try? await item.loadTransferable(type: Data.self),
                let file = PendingFile(pictureData: data, index: pendingFiles.count)
            else { continue }
            pendingFiles.append(file)
        }
    }
}

/// CodingOpenSheet starts Claude Code or Codex in a new herdr pane, in a
/// directory on one of the person's computers.
struct CodingOpenSheet: View {
    let model: CodingModel
    let onOpened: (HerdrSession) -> Void
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var computer = ""
    @State private var codingAgentKind = "claude"
    @State private var directory = ""
    @State private var agentName = ""
    @State private var shouldSkipPermissions = false
    @State private var isOpening = false
    @State private var doneText: String?
    @State private var problemText: String?

    private var computerNames: [String] { model.list?.computerNames ?? [] }
    private var chosenComputer: String { computer.isEmpty ? (computerNames.first ?? "") : computer }

    var body: some View {
        NavigationStack {
            Form {
                if computerNames.count > 1 {
                    Picker("Computer", selection: $computer) {
                        ForEach(computerNames, id: \.self) { name in
                            Text(verbatim: name).tag(name)
                        }
                    }
                }
                Section {
                    Picker("Coding agent", selection: $codingAgentKind) {
                        Text(verbatim: "Claude Code").tag("claude")
                        Text(verbatim: "Codex").tag("codex")
                    }
                    .pickerStyle(.segmented)
                    TextField("Directory", text: $directory, prompt: Text(verbatim: "~/src/example"))
                        .monospaced()
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    TextField("Name in herdr", text: $agentName)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                }
                Section {
                    Toggle(isOn: $shouldSkipPermissions) {
                        if codingAgentKind == "codex" {
                            Text("Skip approvals and the sandbox (--yolo)")
                        } else {
                            Text("Skip permission prompts (--dangerously-skip-permissions)")
                        }
                    }
                }
            }
            .navigationTitle("Open a coding session")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if isOpening {
                        ProgressView()
                    } else {
                        Button("Open") { Task { await open() } }
                            .disabled(directory.trimmed.isEmpty || chosenComputer.isEmpty)
                    }
                }
            }
            .toast(done: $doneText, problem: $problemText)
        }
        .onAppear {
            if computer.isEmpty { computer = computerNames.first ?? "" }
        }
        .interactiveDismissDisabled(isOpening)
    }

    private func open() async {
        isOpening = true
        defer { isOpening = false }
        do {
            let opened = try await model.connection.openHerdrSession(
                computer: chosenComputer, directory: directory.trimmed, codingAgentKind: codingAgentKind,
                agentName: agentName.trimmed.isEmpty ? nil : agentName.trimmed, shouldSkipPermissions: shouldSkipPermissions)
            dismiss()
            onOpened(opened)
        } catch {
            problemText = app.describe(error)
        }
    }
}

#if DEBUG
    /// The demo's sessions, for the previews below.
    @MainActor
    private enum CodingPreviewData {
        static var sessions: [HerdrSession] {
            let body = DemoServer.herdrAnswer(query: "ListAgentHerdrSessions", body: nil) ?? ""
            struct Payload: Decodable { let ListAgentHerdrSessions: HerdrList }
            struct Answer: Decodable { let data: Payload }
            return (try? JSONDecoder().decode(Answer.self, from: Data(body.utf8)))?.data.ListAgentHerdrSessions.sessions ?? []
        }
    }

    #Preview("Asking, light") {
        if let asking = CodingPreviewData.sessions.first(where: \.isAsking), let question = asking.codingQuestion {
            CardSurface { CodingAnswerCard(question: question) { _, _ in } }
                .padding()
        }
    }

    #Preview("Asking, dark") {
        if let asking = CodingPreviewData.sessions.first(where: \.isAsking), let question = asking.codingQuestion {
            CardSurface { CodingAnswerCard(question: question) { _, _ in } }
                .padding()
                .preferredColorScheme(.dark)
        }
    }

    #Preview("Tabs, light") {
        VStack(alignment: .leading) {
            ForEach(CodingPreviewData.sessions.prefix(3)) { session in
                CodingTabChip(session: session, isSelected: session.isAsking)
            }
        }
        .padding()
    }
#endif

extension Color {
    /// A terminal's ground and its text before any color is chosen.
    static let terminalBackground = Color(red: 30 / 255, green: 30 / 255, blue: 30 / 255)
    static let terminalText = Color(red: 212 / 255, green: 212 / 255, blue: 212 / 255)

    init(terminal color: TerminalColor) {
        self.init(red: Double(color.red) / 255, green: Double(color.green) / 255, blue: Double(color.blue) / 255)
    }
}
