import SwiftUI
import TeaNodeKit

// Who the person's Health data is shared with, and which Health categories.
// A person or a circle sees the charts by day of the categories chosen, never
// single readings or routes.

/// HealthSharing names a share and what it shares.
enum HealthSharing {
    /// Who a share is with: "Everyone in Family", or the person's name.
    static func name(of share: HealthShareView) -> String {
        if share.granteeCircleId != nil {
            let circleName = share.granteeCircleName ?? ""
            return String(localized: "Everyone in \(circleName)")
        }
        if let granteeName = share.granteeName, !granteeName.isEmpty { return granteeName }
        return share.granteeUsername ?? ""
    }

    /// The categories of a share, in the Health app's order: "Activity and Sleep".
    static func categoriesText(of share: HealthShareView) -> String {
        ListFormatter.localizedString(byJoining: share.sharedCategories.map { HealthWords.categoryName($0) })
    }
}

/// HealthSharingSections is the Sharing part of the Health settings: the
/// shares, each changeable and removable, and the way to make another.
struct HealthSharingSections: View {
    let model: HealthSettingsModel
    @Binding var isSharing: Bool

    var body: some View {
        Section {
            if model.hasLoaded, model.ownShares.isEmpty {
                Text("You do not share Health data with anyone.").foregroundStyle(.secondary)
            }
            ForEach(model.ownShares) { share in
                NavigationLink {
                    HealthShareScreen(model: model, share: share)
                } label: {
                    HealthShareRow(share: share)
                }
            }
            Button("Share", systemImage: "person.badge.plus") { isSharing = true }
                .actionColor()
        } header: {
            Text("Sharing")
        } footer: {
            Text("People you share with see the charts by day of the categories you choose, never single readings or routes.")
        }
    }
}

/// HealthShareRow is one share: who it is with and which categories.
struct HealthShareRow: View {
    let share: HealthShareView

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: HealthSharing.name(of: share))
            Text(verbatim: HealthSharing.categoriesText(of: share))
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }
}

/// HealthCategoryToggles is a switch for each Health category.
struct HealthCategoryToggles: View {
    @Binding var chosenCategories: Set<HealthCategory>
    var isDisabled = false
    /// Called with the categories before and after one is switched.
    var onChange: (_ previousCategories: Set<HealthCategory>, _ updatedCategories: Set<HealthCategory>) -> Void = { _, _ in }

    var body: some View {
        ForEach(HealthCategory.allCases, id: \.self) { healthCategory in
            Toggle(
                isOn: Binding(
                    get: { chosenCategories.contains(healthCategory) },
                    set: { isOn in
                        let previous = chosenCategories
                        var updated = previous
                        if isOn { updated.insert(healthCategory) } else { updated.remove(healthCategory) }
                        chosenCategories = updated
                        onChange(previous, updated)
                    })
            ) {
                Text(verbatim: HealthWords.categoryName(healthCategory))
            }
            .disabled(isDisabled)
        }
    }
}

/// HealthShareScreen changes which categories one share has, or stops it.
/// Each switch is saved at once; the last one switched off ends the share.
struct HealthShareScreen: View {
    let model: HealthSettingsModel
    let target: ShareTarget
    let name: String
    @State private var chosenCategories: Set<HealthCategory>
    @State private var isConfirmingStop = false
    @Environment(\.dismiss) private var dismiss

    init(model: HealthSettingsModel, share: HealthShareView) {
        self.model = model
        name = HealthSharing.name(of: share)
        if let circleId = share.granteeCircleId {
            target = .circle(circleId)
        } else {
            target = .person(share.granteeUserId ?? "")
        }
        _chosenCategories = State(initialValue: Set(share.sharedCategories))
    }

    var body: some View {
        Form {
            Section {
                HealthCategoryToggles(chosenCategories: $chosenCategories, isDisabled: model.isBusy) { previous, updated in
                    save(from: previous, to: updated)
                }
            } header: {
                Text("Shared with \(name)")
            } footer: {
                Text("They see the charts by day of these categories, never single readings or routes.")
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
                    Button("Stop sharing", role: .destructive) {
                        Task {
                            _ = await model.setShare(with: target, name: name, categories: [])
                            dismiss()
                        }
                    }
                } message: {
                    Text("\(name) will no longer see your Health charts.")
                }
            }
        }
        .navigationTitle(name)
        .navigationBarTitleDisplayMode(.inline)
    }

    /// Saves the categories now switched on; a refusal puts the switches back.
    private func save(from previousCategories: Set<HealthCategory>, to updatedCategories: Set<HealthCategory>) {
        Task {
            let categories = HealthCategory.allCases.filter { updatedCategories.contains($0) }
            let refusalText = await model.setShare(with: target, name: name, categories: categories)
            if refusalText != nil { chosenCategories = previousCategories }
        }
    }
}

/// HealthShareSheet chooses a person or a circle and the categories to share
/// with them.
struct HealthShareSheet: View {
    let model: HealthSettingsModel
    @Environment(\.dismiss) private var dismiss
    @State private var people: [CalendarSharePerson] = []
    @State private var circles: [CalendarShareCircle] = []
    @State private var isLoaded = false
    @State private var loadProblemText: String?
    @State private var chosen: ShareTarget?
    @State private var chosenCategories: Set<HealthCategory> = []
    @State private var isWorking = false
    /// Said inside the sheet: the toast of the screen behind it is hidden.
    @State private var problemText: String?
    @State private var doneText: String?

    var body: some View {
        NavigationStack {
            Form {
                if isLoaded && people.isEmpty && circles.isEmpty {
                    Section {
                        Text("You do not share a circle with anybody yet. Make or join a circle to share your Health data.")
                            .foregroundStyle(.secondary)
                        OpenCirclesLink(connection: model.connection)
                    }
                } else if isLoaded {
                    Section {
                        ShareTargetPicker(people: people, circles: circles, chosen: $chosen)
                    } footer: {
                        Text("Choose a person, or a whole circle.")
                    }
                    Section {
                        HealthCategoryToggles(chosenCategories: $chosenCategories)
                    } header: {
                        Text("What they see")
                    } footer: {
                        Text("They see the charts by day of the categories you choose, never single readings or routes.")
                    }
                }
            }
            .navigationTitle("Share Health")
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
                            .disabled(chosen == nil || chosenCategories.isEmpty)
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
            async let readPeople = model.connection.calendarSharePeople()
            async let readCircles = model.connection.calendarShareCircles()
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
        let categories = HealthCategory.allCases.filter { chosenCategories.contains($0) }
        if let refusalText = await model.setShare(with: target, name: name, categories: categories) {
            problemText = refusalText
        } else {
            dismiss()
        }
    }
}
