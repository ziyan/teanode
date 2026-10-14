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
                section(String(localized: "Done"), goals.filter { !$0.isOpen })
            }
            .navigationTitle("Goals")
            .withoutSwipeBack()
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
                MenuButton()
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

/// GoalRow is a goal in the list: its title, what it is doing now, and
/// when it looks next.
struct GoalRow: View {
    let goal: AgentGoal

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(goal.goalTitle)
            if !goal.goalStatus.isEmpty {
                Text(goal.goalStatus).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
            }
            if goal.isOpen, let next = ServerTime.date(goal.goalNextAt) {
                Text("Next \(next.formatted(.relative(presentation: .named)))").font(.caption).foregroundStyle(.tertiary)
            }
        }
        .padding(.vertical, 2)
    }
}

/// GoalDetail is one goal: what it is for, what it did, when it runs,
/// telling it something, and marking it met, dropping or reopening it.
struct GoalDetail: View {
    let connection: Connection
    let conversationId: String
    @Binding var doneText: String?
    @Binding var problemText: String?
    @Environment(AppModel.self) private var app
    @State private var goal: AgentGoal?
    @State private var told = ""
    @State private var isConfirmingDrop = false

    var body: some View {
        List {
            if let goal {
                Section {
                    Text(goal.goalDescription).textSelection(.enabled)
                    if !goal.goalStatus.isEmpty {
                        LabeledContent("Now", value: goal.goalStatus)
                    }
                    if goal.isOpen, let next = ServerTime.date(goal.goalNextAt) {
                        LabeledContent("Next check-in", value: next.formatted(date: .abbreviated, time: .shortened))
                    }
                    Button("Open its conversation", systemImage: "bubble.left") {
                        app.openedConversationId = goal.conversationId
                    }
                }
                if goal.isOpen {
                    Section {
                        TextField("Tell it something", text: $told, axis: .vertical)
                        Button("Send") { Task { await tell() } }
                            .disabled(told.trimmed.isEmpty)
                    } footer: {
                        Text("It reads this at its next turn.")
                    }
                }
                let schedules = (goal.schedules ?? []).compactMap { $0 }
                if !schedules.isEmpty {
                    Section("Schedules") {
                        ForEach(schedules) { schedule in
                            LabeledContent {
                                Text(schedule.enabled ? (ServerTime.date(schedule.nextRunAt)?.formatted(date: .abbreviated, time: .shortened) ?? "") : String(localized: "Off"))
                            } label: {
                                VStack(alignment: .leading) {
                                    Text(schedule.name)
                                    Text(verbatim: schedule.cron).font(.caption.monospaced()).foregroundStyle(.secondary)
                                }
                            }
                        }
                    }
                }
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
                Section {
                    if goal.isOpen {
                        Button("Mark met", systemImage: "checkmark.circle") { Task { await setState("met", saying: String(localized: "Marked met")) } }
                        Button("Drop goal", systemImage: "xmark.circle", role: .destructive) { isConfirmingDrop = true }
                        .confirmationDialog("Drop this goal?", isPresented: $isConfirmingDrop, titleVisibility: .visible) {
                            Button("Drop goal", role: .destructive) { Task { await setState("dropped", saying: String(localized: "Dropped")) } }
                        } message: {
                            Text("It takes no more turns. You can reopen it later.")
                        }
                    } else {
                        Button("Reopen", systemImage: "arrow.uturn.backward") { Task { await setState("working", saying: String(localized: "Reopened")) } }
                    }
                }
            } else {
                ProgressView().frame(maxWidth: .infinity)
            }
        }
        .navigationTitle(goal?.goalTitle ?? "")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
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

/// GoalStarter starts a goal: what it is, and what done looks like.
struct GoalStarter: View {
    let connection: Connection
    let onStarted: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var title = ""
    @State private var details = ""
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            Form {
                TextField("What to work toward", text: $title)
                Section {
                    TextField("What done looks like, and anything it should know", text: $details, axis: .vertical)
                        .lineLimit(4...)
                }
                if let problemText {
                    Text(problemText).foregroundStyle(.red)
                }
            }
            .navigationTitle("New goal")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Start") {
                        let goalTitle = title.trimmed
                        let goalDetails = details.trimmed
                        Task {
                            do {
                                _ = try await connection.startGoal(title: goalTitle, description: goalDetails)
                                onStarted()
                                dismiss()
                            } catch {
                                problemText = error.localizedDescription
                            }
                        }
                    }
                    .disabled(title.trimmed.isEmpty || details.trimmed.isEmpty)
                }
            }
        }
    }
}
