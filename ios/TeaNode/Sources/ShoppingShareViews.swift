import SwiftUI
import TeaNodeKit

/// ShoppingShareSheet is who one of the person's lists is shared with, with a
/// way to stop, and a form to share it with someone else: one person in a
/// circle they share, or a whole circle. Everybody it reaches can add,
/// change and check off items and upload receipts; only the owner shares it
/// further or deletes it. It also holds the person's own choice to be
/// reminded of the list at stores.
struct ShoppingShareSheet: View {
    let connection: Connection
    let model: ShoppingListModel
    @Environment(\.dismiss) private var dismiss
    @State private var shares: [ShoppingListShare] = []
    @State private var people: [CalendarSharePerson] = []
    @State private var circles: [CalendarShareCircle] = []
    @State private var isLoaded = false
    @State private var loadProblemText: String?
    @State private var chosen: ShareTarget?
    @State private var stopping: ShoppingListShare?
    @State private var isWorking = false
    @State private var problemText: String?
    @State private var doneText: String?

    var body: some View {
        NavigationStack {
            List {
                sharesSection
                if isLoaded && people.isEmpty && circles.isEmpty {
                    Section {
                        Text("You do not share a circle with anybody yet. Make or join a circle to share a list.")
                            .foregroundStyle(.secondary)
                        OpenCirclesLink(connection: connection)
                    }
                } else if isLoaded {
                    Section {
                        ShareTargetPicker(people: people, circles: circles, chosen: $chosen)
                    } footer: {
                        Text("Choose a person, or a whole circle. They can add, change and check off items, and upload receipts.")
                    }
                    Section {
                        Button("Share") { share() }
                            .disabled(isWorking || chosen == nil)
                    }
                }
                Section {
                    Toggle("Remind me at stores", isOn: reminderBinding)
                } footer: {
                    Text("When your phone says you arrived at a store, you are told what this list needs there.")
                }
            }
            .navigationTitle("Share this list")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .overlay {
                if let loadProblemText, !isLoaded {
                    LoadFailedView(problemText: loadProblemText) { await load() }
                } else if !isLoaded {
                    ProgressView()
                }
            }
            .refreshable { await load() }
            .task { await load() }
            .toast(done: $doneText, problem: $problemText)
        }
    }

    private var reminderBinding: Binding<Bool> {
        Binding(get: { model.isReminded }, set: { isOn in Task { await model.setReminder(isOn) } })
    }

    private var sharesSection: some View {
        Section {
            if isLoaded && shares.isEmpty {
                Text("This list is not shared with anybody yet.")
                    .foregroundStyle(.secondary)
            }
            ForEach(shares) { share in
                Text(verbatim: Self.name(of: share))
                    .confirmationDialog(
                        "Stop sharing", isPresented: Binding(get: { stopping?.id == share.id }, set: { if !$0 { stopping = nil } }),
                        titleVisibility: .visible, presenting: stopping
                    ) { share in
                        Button("Stop sharing", role: .destructive) { stop(share) }
                    } message: { share in
                        Text("\(Self.name(of: share)) will no longer see this list.")
                    }
                    .swipeActions {
                        Button("Stop sharing", systemImage: "person.badge.minus", role: .destructive) { stopping = share }
                            .tint(.red)
                    }
                    .contextMenu {
                        Button("Stop sharing", systemImage: "person.badge.minus", role: .destructive) { stopping = share }
                            .tint(.red)
                    }
            }
        } header: {
            Text("Shared with")
        } footer: {
            if !shares.isEmpty {
                Text("Swipe left, or touch and hold, to stop sharing.")
            }
        }
    }

    /// Who a share is with, as a sentence names them.
    private static func name(of share: ShoppingListShare) -> String {
        share.isWithCircle ? String(localized: "Everyone in \(share.shownGrantee)") : share.shownGrantee
    }

    private func load() async {
        do {
            async let readShares = connection.shoppingListShares(shoppingListId: model.listId)
            async let readPeople = connection.calendarSharePeople()
            async let readCircles = connection.calendarShareCircles()
            (shares, people, circles) = try await (readShares, readPeople, readCircles)
            isLoaded = true
            loadProblemText = nil
        } catch {
            // With the screen already drawn a failed refresh leaves it be and says so.
            if isLoaded { problemText = error.loadProblemText } else { loadProblemText = error.loadProblemText }
        }
    }

    private func share() {
        guard let target = chosen else { return }
        let name = ShareTargetPicker.name(of: target, people: people, circles: circles)
        isWorking = true
        Task {
            do {
                try await connection.shareShoppingList(
                    shoppingListId: model.listId, granteeUserId: target.granteeUserId, granteeCircleId: target.granteeCircleId)
                doneText = String(localized: "Shared with \(name)")
                chosen = nil
                shares = try await connection.shoppingListShares(shoppingListId: model.listId)
                await model.load()
            } catch {
                problemText = ShoppingWords.problem(error)
            }
            isWorking = false
        }
    }

    private func stop(_ share: ShoppingListShare) {
        let name = Self.name(of: share)
        isWorking = true
        Task {
            do {
                try await connection.unshareShoppingList(shoppingListShareId: share.id)
                doneText = String(localized: "Stopped sharing with \(name)")
                shares = try await connection.shoppingListShares(shoppingListId: model.listId)
                await model.load()
            } catch {
                problemText = ShoppingWords.problem(error)
            }
            isWorking = false
        }
    }
}
