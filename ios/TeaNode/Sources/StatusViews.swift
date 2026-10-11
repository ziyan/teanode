import AVKit
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
                if let currency = read.budget?.currency, !currency.isEmpty {
                    UsageWords.currency = currency
                }
            }
            if let commands = try? await connection.backgroundCommands(in: conversationId()) {
                runningCommands = commands.filter(\.isRunning)
            }
            try? await Task.sleep(for: Self.every)
        }
    }
}

/// StatusButton opens the status sheet: one plain button, with the day's
/// usage, what is attached and what still runs kept inside the sheet.
struct StatusButton: View {
    let status: StatusModel
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Label("Usage and devices", systemImage: "gearshape")
        }
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

/// CallBar is the composer while a call is on: how loud the person is, what
/// is heard or said, where the sound goes, mute and hang up, on one glass
/// panel so the conversation behind it does not show through its words.
struct CallBar: View {
    let call: VoiceCall

    var body: some View {
        HStack(spacing: 10) {
            Button {
                call.cutAnswer()
            } label: {
                CallMeter(level: call.level, isSpeaking: call.callState == .speaking)
                    .frame(minWidth: 44, minHeight: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .disabled(call.callState != .speaking)
            .accessibilityLabel("Stop the answer")
            Text(caption)
                .font(.callout)
                .foregroundStyle(call.problemText == nil ? .secondary : .primary)
                .lineLimit(3)
                .frame(maxWidth: .infinity, alignment: .leading)
            AudioRoutePicker()
                .frame(width: 44, height: 44)
                .background(Color(.tertiarySystemFill), in: .circle)
            Button {
                call.isMuted.toggle()
            } label: {
                Image(systemName: call.isMuted ? "mic.slash.fill" : "mic.fill")
                    .font(.title3)
                    .frame(width: 44, height: 44)
                    .background(call.isMuted ? AnyShapeStyle(.tint) : AnyShapeStyle(Color(.tertiarySystemFill)), in: .circle)
                    .foregroundStyle(call.isMuted ? AnyShapeStyle(.white) : AnyShapeStyle(.tint))
            }
            .buttonStyle(.plain)
            .accessibilityLabel(call.isMuted ? Text("Unmute") : Text("Mute"))
            Button {
                call.hangUp()
            } label: {
                Image(systemName: "phone.down.fill")
                    .font(.title3)
                    .foregroundStyle(.white)
                    .frame(width: 44, height: 44)
                    .background(.red, in: .circle)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Hang up")
        }
        .padding(.leading, 16)
        .padding(.trailing, 8)
        .padding(.vertical, 8)
        .glassEffect(.regular, in: .rect(cornerRadius: 30, style: .continuous))
        .padding(.horizontal)
        .padding(.bottom, 8)
        .sensoryFeedback(.impact, trigger: call.callState == .hearing)
    }

    private var caption: String {
        if let problemText = call.problemText { return problemText }
        if !call.captionText.isEmpty { return call.captionText }
        switch call.callState {
        case .starting: return String(localized: "Starting the microphone…")
        case .listening: return call.isMuted ? String(localized: "Muted") : String(localized: "Listening…")
        case .hearing: return String(localized: "Hearing you…")
        case .working: return String(localized: "Working on it…")
        case .speaking: return String(localized: "Speaking… talk or tap to cut in")
        case .ended: return ""
        }
    }
}

/// AudioRoutePicker is the system's choice of where a call's sound goes:
/// the iPhone, its speaker, AirPods or other Bluetooth headphones, or a car.
struct AudioRoutePicker: UIViewRepresentable {
    func makeUIView(context: Context) -> AVRoutePickerView {
        let picker = AVRoutePickerView()
        picker.prioritizesVideoDevices = false
        picker.tintColor = .tintColor
        picker.activeTintColor = .tintColor
        picker.accessibilityLabel = String(localized: "Choose where you hear the call")
        return picker
    }

    func updateUIView(_ picker: AVRoutePickerView, context: Context) {}
}

/// CallMeter is five bars moving with the microphone, or with the answer
/// while it is spoken.
struct CallMeter: View {
    let level: Float
    let isSpeaking: Bool

    var body: some View {
        HStack(spacing: 3) {
            ForEach(0..<5, id: \.self) { index in
                let scale = [0.5, 0.8, 1, 0.8, 0.5][index]
                Capsule()
                    .fill(isSpeaking ? Color.accentColor : Color.primary.opacity(0.6))
                    .frame(width: 4, height: 6 + 22 * CGFloat(isSpeaking ? 0.6 : Double(level)) * scale)
            }
        }
        .frame(height: 30)
        .animation(.easeOut(duration: 0.1), value: level)
        .accessibilityHidden(true)
    }
}
