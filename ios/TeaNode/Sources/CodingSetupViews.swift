import SwiftUI
import TeaNodeKit

/// CodingTool is a screen the Coding list's menu opens over it.
enum CodingTool: String, Identifiable {
    case hooks
    case preview

    var id: String { rawValue }
}

/// CodingHooksSheet puts teanode's reporting hooks into Claude Code on each
/// computer, or takes them out: the dashboard's "Reporting hooks" dialog.
struct CodingHooksSheet: View {
    let computerNames: [String]
    let connection: Connection
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var busyComputerName: String?
    @State private var removingComputerName: String?
    @State private var setups: [String: HerdrHooksSetup] = [:]
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text("TeaNode can put its own hooks into Claude Code on a computer, beside the ones there. They report what a session does as it does it, so its state is right even where herdr gets it wrong, and they decide nothing. Sessions started afterwards use them.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                ForEach(computerNames, id: \.self) { computer in
                    Section {
                        Button("Install") { Task { await setUp(computer, isRemoval: false) } }
                        Button("Remove", role: .destructive) { removingComputerName = computer }
                            .confirmationDialog(
                                "Remove the reporting hooks?",
                                isPresented: Binding(get: { removingComputerName == computer }, set: { if !$0 { removingComputerName = nil } }),
                                titleVisibility: .visible
                            ) {
                                Button("Remove", role: .destructive) {
                                    removingComputerName = nil
                                    Task { await setUp(computer, isRemoval: true) }
                                }
                            } message: {
                                Text("This takes TeaNode's hooks out of Claude Code's settings on \(computer), and leaves every other hook there as it is.")
                            }
                        if let setup = setups[computer] {
                            CodingHooksReport(setup: setup)
                        }
                    } header: {
                        HStack {
                            Text(verbatim: computer)
                            if busyComputerName == computer {
                                ProgressView()
                            }
                        }
                    }
                    .disabled(busyComputerName != nil)
                }
            }
            .navigationTitle("Reporting hooks")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .toast(done: $doneText, problem: $problemText)
        }
        .interactiveDismissDisabled(busyComputerName != nil)
    }

    private func setUp(_ computer: String, isRemoval: Bool) async {
        busyComputerName = computer
        defer { busyComputerName = nil }
        do {
            let setup = try await connection.setUpHerdrHooks(computer: computer, isRemoval: isRemoval)
            setups[computer] = setup
            doneText =
                setup.isInstalled
                ? String(localized: "Hooks installed on \(setup.computer), in \(setup.settingsPath)")
                : String(localized: "Hooks removed on \(setup.computer), from \(setup.settingsPath)")
        } catch {
            problemText = app.describe(error)
        }
    }
}

/// CodingHooksReport is what the last setup on a computer reported: whether
/// the hooks are in, where, which events, and the copy of the file it kept.
private struct CodingHooksReport: View {
    let setup: HerdrHooksSetup

    var body: some View {
        LabeledContent("Hooks", value: setup.isInstalled ? String(localized: "Installed") : String(localized: "Removed"))
        CodingHooksPathRow(title: "Settings file", path: setup.settingsPath)
        if setup.isInstalled {
            CodingHooksPathRow(title: "Hook script", path: setup.scriptPath)
        }
        CodingHooksPathRow(title: "Backup of the settings", path: setup.backupPath)
        if !setup.hookEventNames.isEmpty {
            LabeledContent("Events", value: setup.hookEventNames.joined(separator: ", "))
        }
    }
}

private struct CodingHooksPathRow: View {
    let title: LocalizedStringKey
    let path: String

    var body: some View {
        if !path.isEmpty {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                Text(verbatim: path)
                    .font(.footnote.monospaced())
                    .textSelection(.enabled)
            }
        }
    }
}

/// CodingPreviewSheet shows the text a coding session would be shown: in a
/// directory on a computer, what a session starting there sees, and with a
/// question, what that question recalls from the directory's project.
/// Nothing is said to a model and nothing is marked as used.
struct CodingPreviewSheet: View {
    let computerNames: [String]
    let connection: Connection
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var computerName = ""
    @State private var directory = ""
    @State private var question = ""
    @State private var shownText: String?
    @State private var isAsking = false
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Directory", text: $directory, prompt: Text(verbatim: "~/code/project"))
                        .monospaced()
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    if computerNames.count > 1 {
                        Picker("Computer (optional)", selection: $computerName) {
                            Text("Any computer").tag("")
                            ForEach(computerNames, id: \.self) { name in
                                Text(verbatim: name).tag(name)
                            }
                        }
                    }
                    TextField("Question (optional)", text: $question, axis: .vertical)
                        .lineLimit(1...4)
                } footer: {
                    Text("A directory on a computer: shows what a Claude Code or Codex session there is shown by teanode hook. With no question, what a session starting there sees.")
                }
                Section {
                    Button("Show") { Task { await ask() } }
                        .disabled(directory.trimmed.isEmpty || isAsking)
                }
                if let shownText {
                    Section("What the session is shown") {
                        if shownText.isEmpty {
                            Text("Nothing: your agent knows no checkout holding that directory, or the question recalls nothing from its project.")
                                .foregroundStyle(.secondary)
                        } else {
                            Text(verbatim: shownText)
                                .font(.footnote.monospaced())
                                .textSelection(.enabled)
                        }
                    }
                }
            }
            .navigationTitle("What a session sees")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    if isAsking {
                        ProgressView()
                    } else {
                        Button("Close") { dismiss() }
                    }
                }
            }
            // A new place or question puts the old answer away.
            .onChange(of: "\(directory)\n\(computerName)\n\(question)") { _, _ in shownText = nil }
            .toast(done: .constant(nil), problem: $problemText)
        }
    }

    private func ask() async {
        isAsking = true
        defer { isAsking = false }
        do {
            let preview = try await connection.codingContextPreview(
                directory: directory.trimmed, computerName: computerName.isEmpty ? nil : computerName,
                prompt: question.trimmed.isEmpty ? nil : question.trimmed)
            shownText = preview.text
        } catch {
            problemText = app.describe(error)
        }
    }
}
