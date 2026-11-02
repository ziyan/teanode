import SwiftUI
import TeaNodeKit

// Sharing all of the person's finance at once, with a person or a circle, at
// the spending level or the transactions level. It covers every account the
// person has now and links later, so it names none; the accounts shared one at
// a time are in FinanceSharingViews.swift. The agent has no part in it.

/// FinanceShareAllRow is one share of all finance: who it is with and the level.
struct FinanceShareAllRow: View {
    let share: AllFinanceShareView

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: FinanceShareWords.name(of: share))
            Text(verbatim: FinanceShareWords.name(ofLevel: share.financeShareLevel))
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }
}

/// FinanceShareAllScreen changes the level of one share of all finance, or
/// stops it. A level is saved at once; it is not changed on the screen until
/// the server has taken it.
struct FinanceShareAllScreen: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    let share: AllFinanceShareView
    let onChanged: () async -> Void
    @State private var chosenLevel: FinanceShareLevel
    @State private var isConfirmingStop = false

    init(share: AllFinanceShareView, onChanged: @escaping () async -> Void) {
        self.share = share
        self.onChanged = onChanged
        _chosenLevel = State(initialValue: share.level ?? .spending)
    }

    var body: some View {
        let name = FinanceShareWords.name(of: share)
        Form {
            Section {
                Picker(
                    "How much",
                    selection: Binding(get: { chosenLevel }, set: { updated in save(updated) })
                ) {
                    ForEach(FinanceShareWords.allFinanceLevels, id: \.self) { level in
                        Text(verbatim: FinanceShareWords.name(level)).tag(level)
                    }
                }
                .pickerStyle(.inline)
                .labelsHidden()
            } header: {
                Text("All your finance, shared with \(name)")
            } footer: {
                Text(verbatim: FinanceShareWords.allFinanceHint(chosenLevel))
            }
            Section {
                Button(role: .destructive) {
                    isConfirmingStop = true
                } label: {
                    Label("Stop sharing", systemImage: "person.badge.minus").foregroundStyle(.red)
                }
                .tint(.red)
                .actionColor(isDestructive: true)
                .confirmationDialog("Stop sharing", isPresented: $isConfirmingStop, titleVisibility: .visible) {
                    Button("Stop sharing", role: .destructive) { stop(name: name) }
                } message: {
                    Text("\(name) will no longer see all your finance. Accounts you share one by one stay shared.")
                }
            }
        }
        .navigationTitle("All your finance")
        .navigationBarTitleDisplayMode(.inline)
    }

    /// Saves the level; a refusal leaves the choice where it was.
    private func save(_ updated: FinanceShareLevel) {
        Task {
            let name = FinanceShareWords.name(of: share)
            let isSaved = await finance.act(saying: String(localized: "All your finance is shared with \(name).")) {
                try await finance.connection.shareAllFinance(with: share.target, level: updated)
            }
            if isSaved {
                chosenLevel = updated
                await onChanged()
            }
        }
    }

    private func stop(name: String) {
        Task {
            let isStopped = await finance.act(saying: String(localized: "Stopped sharing with \(name)")) {
                try await finance.connection.unshareAllFinance(financeShareId: share.id)
            }
            if isStopped {
                await onChanged()
                dismiss()
            }
        }
    }
}

/// FinanceShareAllSheet chooses a person or a circle and the level to share
/// all of the person's finance at.
struct FinanceShareAllSheet: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    let onShared: () async -> Void
    @State private var people: [CalendarSharePerson] = []
    @State private var circles: [CalendarShareCircle] = []
    @State private var isLoaded = false
    @State private var loadProblemText: String?
    @State private var chosen: ShareTarget?
    @State private var chosenLevel = FinanceShareLevel.spending
    @State private var isWorking = false
    /// Said inside the sheet: the toast of the screen behind it is hidden.
    @State private var problemText: String?
    @State private var doneText: String?

    var body: some View {
        NavigationStack {
            Form {
                if isLoaded && people.isEmpty && circles.isEmpty {
                    Section {
                        Text("You are not in a circle with anyone yet.")
                            .foregroundStyle(.secondary)
                        OpenCirclesLink(connection: finance.connection)
                    }
                } else if isLoaded {
                    Section {
                        ShareTargetPicker(people: people, circles: circles, chosen: $chosen)
                    } footer: {
                        Text("Choose a person, or a whole circle.")
                    }
                    Section {
                        Picker("How much", selection: $chosenLevel) {
                            ForEach(FinanceShareWords.allFinanceLevels, id: \.self) { level in
                                Text(verbatim: FinanceShareWords.name(level)).tag(level)
                            }
                        }
                        .pickerStyle(.inline)
                        .labelsHidden()
                    } header: {
                        Text("What they see")
                    } footer: {
                        Text(verbatim: FinanceShareWords.allFinanceHint(chosenLevel))
                    }
                }
            }
            .navigationTitle("Share all your finance")
            .navigationBarTitleDisplayMode(.inline)
            .overlay {
                if let loadProblemText, !isLoaded {
                    LoadFailedView(problemText: loadProblemText) { await load() }
                } else if !isLoaded {
                    ProgressView()
                }
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }.disabled(isWorking)
                }
                ToolbarItem(placement: .confirmationAction) {
                    if isWorking {
                        ProgressView()
                    } else {
                        Button("Share") { Task { await share() } }
                            .disabled(chosen == nil)
                    }
                }
            }
            .toast(done: $doneText, problem: $problemText)
            .task { await load() }
        }
        .presentationDetents([.large])
    }

    private func load() async {
        do {
            async let readPeople = finance.connection.calendarSharePeople()
            async let readCircles = finance.connection.calendarShareCircles()
            (people, circles) = try await (readPeople, readCircles)
            isLoaded = true
            loadProblemText = nil
        } catch {
            loadProblemText = error.loadProblemText
        }
    }

    private func share() async {
        guard let target = chosen else { return }
        isWorking = true
        defer { isWorking = false }
        let name = ShareTargetPicker.name(of: target, people: people, circles: circles)
        do {
            try await finance.connection.shareAllFinance(with: target, level: chosenLevel)
            finance.doneText = String(localized: "All your finance is shared with \(name).")
            await onShared()
            dismiss()
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "Could not save the sharing.")).asReasonSentence
        }
    }
}
