import SwiftUI
import TeaNodeKit

/// GoalsScreen is the dashboard's Goals tab: what the agent works toward
/// on its own, those waiting for the person first, and starting a new one.
struct GoalsScreen: View {
    let connection: Connection
    @State private var goals: [AgentGoal] = []
    @State private var isLoaded = false
    @State private var isStarting = false
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            List {
                section(String(localized: "Waiting for you"), goals.filter { $0.goalState == "waiting" })
                section(String(localized: "Working"), goals.filter { $0.goalState == "working" })
                section(String(localized: "Paused"), goals.filter { $0.isPaused })
                section(String(localized: "Done"), goals.filter { !$0.isOpen && !$0.isPaused })
            }
            .navigationTitle("Goals")
            .navigationDestination(for: AgentGoal.self) { goal in
                GoalDetail(connection: connection, conversationId: goal.conversationId, doneText: $doneText, problemText: $problemText)
            }
            .overlay {
                if isLoaded, goals.isEmpty {
                    ContentUnavailableView("No goals yet", systemImage: "target", description: Text("Give your agent something to work toward on its own, and it checks in when it needs you."))
                } else if !isLoaded {
                    ProgressView()
                }
            }
            .toolbar {
                MoreBackButton()
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        isStarting = true
                    } label: {
                        Label("New goal", systemImage: "plus")
                    }
                }
            }
            .task {
                while !Task.isCancelled {
                    await load()
                    try? await Task.sleep(for: .seconds(10))
                }
            }
            .refreshable { await load() }
            .sheet(isPresented: $isStarting) {
                GoalStarter(connection: connection) { Task { await load() } }
            }
        }
        .toast(done: $doneText, problem: $problemText)
    }

    @ViewBuilder
    private func section(_ title: String, _ shown: [AgentGoal]) -> some View {
        if !shown.isEmpty {
            Section(title) {
                ForEach(shown) { goal in
                    NavigationLink(value: goal) {
                        GoalRow(goal: goal)
                    }
                }
            }
        }
    }

    private func load() async {
        do {
            goals = try await connection.goals()
            isLoaded = true
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// GoalRow is a goal in the list: its title, what it is doing now, when it
/// runs in words and when it looks next.
struct GoalRow: View {
    let goal: AgentGoal

    private var whenLines: [String] { UpkeepWords.goalWhenLines(goal) }

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(goal.goalTitle)
            if !goal.goalStatus.isEmpty {
                Text(goal.goalStatus).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
            }
            ForEach(whenLines, id: \.self) { line in
                Text(verbatim: line).font(.footnote).foregroundStyle(.secondary)
            }
            if goal.goalState == "working", let next = ServerTime.date(goal.goalNextAt) {
                nextLine(next)
            }
        }
        .padding(.vertical, 2)
    }

    /// A check-in whose time has passed is about to run, not "next 17 hours
    /// ago".
    @ViewBuilder
    private func nextLine(_ next: Date) -> some View {
        if next > Date() {
            Text("Next: \(UpkeepWords.nextText(next))").font(.caption).foregroundStyle(.tertiary)
        } else {
            Text("Next check-in soon").font(.caption).foregroundStyle(.tertiary)
        }
    }
}

/// GoalDetail is one goal: what it is for, what it did, when it runs,
/// telling it something, and running it now, pausing, finishing or
/// dropping it.
struct GoalDetail: View {
    let connection: Connection
    let conversationId: String
    @Binding var doneText: String?
    @Binding var problemText: String?
    @Environment(AppModel.self) private var app
    @State private var goal: AgentGoal?
    @State private var told = ""
    @State private var isConfirmingDrop = false
    @State private var isEditingWhen = false
    @State private var isEditingWords = false

    var body: some View {
        List {
            if let goal {
                aboutSection(goal)
                if goal.isOpen {
                    tellSection
                }
                whenSection(goal)
                activitySection(goal)
                actionsSection(goal)
            } else {
                ProgressView().frame(maxWidth: .infinity)
            }
        }
        .navigationTitle(goal?.goalTitle ?? "")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if goal != nil {
                ToolbarItem(placement: .primaryAction) {
                    Button("Edit", systemImage: "pencil") { isEditingWords = true }
                }
            }
        }
        .task { await load() }
        .refreshable { await load() }
        .sheet(isPresented: $isEditingWhen) {
            if let goal {
                GoalWhenEditor(connection: connection, goal: goal) {
                    doneText = String(localized: "Saved")
                    Task { await load() }
                }
            }
        }
        .sheet(isPresented: $isEditingWords) {
            if let goal {
                GoalWordsEditor(connection: connection, goal: goal) {
                    doneText = String(localized: "Saved")
                    Task { await load() }
                }
            }
        }
    }

    private func aboutSection(_ goal: AgentGoal) -> some View {
        Section {
            Text(goal.goalDescription).textSelection(.enabled)
            if !goal.goalStatus.isEmpty {
                LabeledContent("Now", value: goal.goalStatus)
            }
            if goal.goalState == "paused" {
                Label("Paused", systemImage: "pause.circle").foregroundStyle(.secondary)
            }
            if goal.goalState == "working", let next = ServerTime.date(goal.goalNextAt) {
                LabeledContent("Next check-in", value: next.formatted(date: .abbreviated, time: .shortened))
            }
            Button("Open its conversation", systemImage: "bubble.left") {
                app.openedConversationId = goal.conversationId
            }
            .actionColor()
        }
    }

    private var tellSection: some View {
        Section {
            TextField("Tell it something", text: $told, axis: .vertical)
            Button("Send") { Task { await tell() } }
                .disabled(told.trimmed.isEmpty)
        } footer: {
            Text("It reads this at its next turn.")
        }
    }

    /// When it runs, in words, with Edit: the same editor Start a goal has.
    private func whenSection(_ goal: AgentGoal) -> some View {
        let lines = UpkeepWords.goalWhenLines(goal)
        return Section("When") {
            if lines.isEmpty {
                Text("At its own pace").foregroundStyle(.secondary)
            }
            ForEach(lines, id: \.self) { line in
                Text(verbatim: line)
            }
            if goal.isGoalNotifiedWhenPosted, !goal.goalTimes.isEmpty {
                Text("Notifies you when it posts").foregroundStyle(.secondary)
            }
            Button("Edit", systemImage: "clock") { isEditingWhen = true }
                .actionColor()
        }
    }

    @ViewBuilder
    private func activitySection(_ goal: AgentGoal) -> some View {
        let activity = (goal.activity ?? []).compactMap { $0 }
        if !activity.isEmpty {
            Section("What it did") {
                ForEach(activity) { entry in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(entry.activityHeadline)
                        if !entry.activityDetail.isEmpty {
                            Text(entry.activityDetail).font(.footnote).foregroundStyle(.secondary).lineLimit(4)
                        }
                        if let at = ServerTime.date(entry.createdAt) {
                            Text(at.formatted(.relative(presentation: .named))).font(.caption).foregroundStyle(.tertiary)
                        }
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func actionsSection(_ goal: AgentGoal) -> some View {
        Section {
            if goal.canRunNow {
                Button("Run now", systemImage: "play.circle") { Task { await run() } }
                    .actionColor()
            }
            if goal.isOpen {
                Button("Pause", systemImage: "pause.circle") { Task { await setState("paused", saying: String(localized: "Paused")) } }
                    .actionColor()
            }
            if goal.isPaused {
                Button("Resume", systemImage: "play.circle") { Task { await setState("working", saying: String(localized: "Resumed")) } }
                    .actionColor()
            }
            if goal.isOpen || goal.isPaused {
                Button("Done", systemImage: "checkmark.circle") { Task { await setState("met", saying: String(localized: "Marked met")) } }
                    .actionColor()
                Button("Drop goal", systemImage: "xmark.circle", role: .destructive) { isConfirmingDrop = true }
                    .tint(.red)
                    .actionColor(isDestructive: true)
                    .confirmationDialog("Drop this goal?", isPresented: $isConfirmingDrop, titleVisibility: .visible) {
                        Button("Drop goal", role: .destructive) { Task { await setState("dropped", saying: String(localized: "Dropped")) } }
                    } message: {
                        Text("It takes no more turns. You can reopen it later.")
                    }
            } else {
                Button("Reopen", systemImage: "arrow.uturn.backward") { Task { await setState("working", saying: String(localized: "Reopened")) } }
                    .actionColor()
            }
        }
    }

    private func load() async {
        do {
            goal = try await connection.goal(conversationId)
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func tell() async {
        let text = told.trimmed
        do {
            try await connection.tellGoal(conversationId, text: text)
            told = ""
            doneText = String(localized: "Told")
            await load()
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func run() async {
        do {
            try await connection.runGoal(conversationId)
            doneText = String(localized: "It takes a turn now.")
            await load()
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func setState(_ state: String, saying said: String) async {
        do {
            try await connection.setGoalState(conversationId, to: state)
            doneText = said
            await load()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// GoalStarter starts a goal: what it is, what done looks like, and when it
/// runs.
struct GoalStarter: View {
    let connection: Connection
    let onStarted: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var title = ""
    @State private var details = ""
    @State private var whenDraft = GoalWhenDraft()
    @State private var isStarting = false
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            Form {
                TextField("What to work toward", text: $title)
                Section {
                    TextField("What done looks like, and anything it should know", text: $details, axis: .vertical)
                        .lineLimit(4...)
                }
                GoalWhenSections(draft: $whenDraft)
                if let problemText {
                    Text(problemText.asReasonSentence).foregroundStyle(.red)
                }
            }
            .navigationTitle("New goal")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Start") { Task { await start() } }
                        .disabled(title.trimmed.isEmpty || details.trimmed.isEmpty || !whenDraft.isComplete || isStarting)
                }
            }
        }
    }

    private func start() async {
        isStarting = true
        defer { isStarting = false }
        var change = whenDraft.change(isUpdate: false)
        change.goalTitle = title.trimmed
        change.goalDescription = details.trimmed
        do {
            _ = try await connection.startGoal(change)
            onStarted()
            dismiss()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// GoalWordsEditor changes a goal's title and what it is for.
struct GoalWordsEditor: View {
    let connection: Connection
    let goal: AgentGoal
    let onSaved: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var title: String
    @State private var details: String
    @State private var isSaving = false
    @State private var problemText: String?

    init(connection: Connection, goal: AgentGoal, onSaved: @escaping () -> Void) {
        self.connection = connection
        self.goal = goal
        self.onSaved = onSaved
        _title = State(initialValue: goal.goalTitle)
        _details = State(initialValue: goal.goalDescription)
    }

    var body: some View {
        NavigationStack {
            Form {
                TextField("What to work toward", text: $title)
                Section {
                    TextField("What done looks like, and anything it should know", text: $details, axis: .vertical)
                        .lineLimit(4...)
                }
                if let problemText {
                    Text(problemText.asReasonSentence).foregroundStyle(.red)
                }
            }
            .navigationTitle("Edit")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { Task { await save() } }
                        .disabled(title.trimmed.isEmpty || details.trimmed.isEmpty || isSaving)
                }
            }
        }
    }

    private func save() async {
        isSaving = true
        defer { isSaving = false }
        // Only what changed is sent.
        var change = AgentGoalChange()
        if title.trimmed != goal.goalTitle { change.goalTitle = title.trimmed }
        if details.trimmed != goal.goalDescription { change.goalDescription = details.trimmed }
        do {
            try await connection.updateGoal(goal.conversationId, change: change)
            onSaved()
            dismiss()
        } catch {
            problemText = error.localizedDescription
        }
    }
}
