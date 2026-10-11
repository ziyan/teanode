import SwiftUI
import TeaNodeKit

/// StatusModel is what the drawer's head shows, read every ten seconds while
/// a conversation is on screen: the attached tab and computers, the day's
/// budget, and the commands still running.
@MainActor
@Observable
final class StatusModel {
    private(set) var status: AgentStatus?
    private(set) var runningCommands: [AgentBackgroundCommand] = []

    /// How often the head is read again, as the drawer does.
    static let every = Duration.seconds(10)

    func follow(connection: Connection, conversationId: @escaping () -> String?) async {
        while !Task.isCancelled {
            if let read = try? await connection.status() {
                status = read
            }
            if let commands = try? await connection.backgroundCommands(in: conversationId()) {
                runningCommands = commands.filter(\.isRunning)
            }
            try? await Task.sleep(for: Self.every)
        }
    }
}

/// StatusButton is the head's marks in one button: the day's budget as a
/// ring, and a mark each for an attached tab, attached computers and
/// commands still running. It opens the status sheet.
struct StatusButton: View {
    let status: StatusModel
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                if status.status?.tab?.attached == true {
                    Image(systemName: "globe")
                }
                if !(status.status?.computers.isEmpty ?? true) {
                    Image(systemName: "desktopcomputer")
                }
                if !status.runningCommands.isEmpty {
                    Image(systemName: "terminal")
                        .symbolEffect(.pulse, options: .repeating)
                }
                BudgetRing(fraction: status.status?.budget?.fraction)
            }
        }
        .accessibilityLabel("Usage today")
    }
}

/// BudgetRing is how much of the day's budget is used, colored as it nears
/// the limit; an empty ring when there is no limit.
struct BudgetRing: View {
    let fraction: Double?

    var body: some View {
        ZStack {
            Circle().stroke(.tertiary, lineWidth: 2.5)
            Circle()
                .trim(from: 0, to: min(1, max(0, fraction ?? 0)))
                .stroke(color, style: StrokeStyle(lineWidth: 2.5, lineCap: .round))
                .rotationEffect(.degrees(-90))
        }
        .frame(width: 18, height: 18)
    }

    private var color: Color {
        guard let fraction else { return .secondary }
        if fraction >= 0.85 { return .red }
        if fraction >= 0.6 { return .orange }
        return .accentColor
    }
}

/// StatusSheet is the head's dropdowns in one place: the day's usage, the
/// switches for what a conversation shows, what is attached, and the
/// commands left running.
struct StatusSheet: View {
    let status: StatusModel
    let connection: Connection
    @Environment(\.dismiss) private var dismiss
    @AppStorage(TranscriptPreference.showToolCalls) private var isShowingToolCalls = true
    @AppStorage(TranscriptPreference.showUsage) private var isShowingUsage = false
    @AppStorage(TranscriptPreference.showWorkingNotes) private var isShowingWorkingNotes = false

    var body: some View {
        NavigationStack {
            Form {
                Section("Usage today") {
                    if let budget = status.status?.budget {
                        BudgetSummary(budget: budget, timezone: status.status?.timezone)
                    } else {
                        ProgressView()
                    }
                    Toggle("Show tool calls", isOn: $isShowingToolCalls)
                    Toggle("Show usage", isOn: $isShowingUsage)
                    Toggle("Show the agent's working notes", isOn: $isShowingWorkingNotes)
                }
                if let tab = status.status?.tab, tab.attached {
                    Section("Attached tab") {
                        LabeledContent(tab.title ?? "", value: tab.url.flatMap { URL(string: $0)?.host() } ?? "")
                    }
                }
                if let computers = status.status?.computers, !computers.isEmpty {
                    Section("Attached computers") {
                        ForEach(computers) { computer in
                            VStack(alignment: .leading, spacing: 2) {
                                Text(computer.name)
                                Text([computer.description, computer.system].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                                    .font(.footnote)
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                }
                if !status.runningCommands.isEmpty {
                    Section("Background commands") {
                        ForEach(status.runningCommands) { command in
                            NavigationLink {
                                BackgroundCommandView(command: command, connection: connection)
                            } label: {
                                Text(command.command)
                                    .font(.footnote.monospaced())
                                    .lineLimit(1)
                            }
                        }
                    }
                }
            }
            .navigationTitle("Usage today")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
    }
}

/// BudgetSummary is the day's use against its limit, what it cost, and when
/// the budget starts again.
struct BudgetSummary: View {
    let budget: AgentStatus.Budget
    let timezone: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let fraction = budget.fraction {
                ProgressView(value: min(1, fraction))
                    .tint(fraction >= 0.85 ? .red : fraction >= 0.6 ? .orange : .accentColor)
                if budget.limit > 0 {
                    Text("\(UsageWords.count(Int(budget.used))) of \(UsageWords.count(Int(budget.limit)))")
                } else {
                    Text("\(money(budget.cost)) of \(money(budget.costLimit))")
                }
            } else {
                Text("No limit today")
            }
            Text("Spent today: \(money(budget.cost))")
                .font(.footnote)
                .foregroundStyle(.secondary)
            if let resetsAt = ServerTime.date(budget.resetsAt) {
                Text("Starts again \(resetsAt.formatted(date: .omitted, time: .shortened))")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
    }

    private func money(_ amount: Double) -> String {
        amount.formatted(.currency(code: budget.currency ?? "USD"))
    }
}

/// BackgroundCommandView is one command left running: where it runs, what
/// it printed, read again every two seconds while it runs, and a way to
/// stop it.
struct BackgroundCommandView: View {
    let connection: Connection
    @State private var command: AgentBackgroundCommand
    @State private var isStopping = false

    init(command: AgentBackgroundCommand, connection: Connection) {
        self.connection = connection
        self._command = State(initialValue: command)
    }

    var body: some View {
        List {
            Section {
                LabeledContent("Computer", value: command.computer)
                if let directory = command.directory, !directory.isEmpty {
                    LabeledContent("Folder", value: directory)
                }
                Text(command.command).font(.footnote.monospaced())
                Text(stateText).font(.footnote).foregroundStyle(.secondary)
            }
            Section("Output") {
                CodeBlock(text: (command.stdout ?? "").isEmpty ? String(localized: "Nothing printed.") : command.stdout ?? "", maxHeight: 400)
            }
            if let stderr = command.stderr, !stderr.isEmpty {
                Section("Errors") {
                    CodeBlock(text: stderr, maxHeight: 300)
                }
            }
        }
        .navigationTitle("Background command")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if command.isRunning {
                ToolbarItem(placement: .primaryAction) {
                    Button("Stop", role: .destructive) {
                        isStopping = true
                        Task {
                            try? await connection.stopBackgroundCommand(computer: command.computer, id: command.id)
                            isStopping = false
                        }
                    }
                    .disabled(isStopping)
                }
            }
        }
        .task {
            while !Task.isCancelled {
                if let read = try? await connection.backgroundCommand(computer: command.computer, id: command.id) {
                    command = read
                }
                guard command.isRunning else { return }
                try? await Task.sleep(for: .seconds(2))
            }
        }
    }

    private var stateText: String {
        if command.isRunning { return String(localized: "Running") }
        if let exitCode = command.exitCode { return String(localized: "Exited \(exitCode)") }
        return command.stopReason ?? String(localized: "Stopped")
    }
}
