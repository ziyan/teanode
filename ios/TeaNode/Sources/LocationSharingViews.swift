import SwiftUI
import TeaNodeKit

// Who the person's location is shared with, and the sheet to share it.

/// LocationSharingList is the third segment of Location: the person's shares,
/// each changeable and removable, and the way to make another.
struct LocationSharingList: View {
    let model: LocationModel
    @State private var isSharing = false
    @State private var stopping: LocationShare?

    var body: some View {
        List {
            Section {
                if model.shares.isEmpty {
                    Text("Your location is not shared with anybody yet.").foregroundStyle(.secondary)
                }
                ForEach(model.shares) { share in
                    Menu {
                        Picker(
                            "How much",
                            selection: Binding(
                                get: { share.level ?? .current },
                                set: { level in Task { await model.change(share, to: level) } })
                        ) {
                            ForEach(LocationShareLevel.grantable, id: \.self) { level in
                                Text(level.title).tag(level)
                            }
                        }
                        Button("Stop sharing", systemImage: "person.badge.minus", role: .destructive) { stopping = share }
                            .tint(.red)
                    } label: {
                        HStack {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(verbatim: LocationModel.shareName(of: share)).foregroundStyle(Color(.label))
                                Text(share.level?.title ?? share.shareLevel).font(.footnote).foregroundStyle(Color(.secondaryLabel))
                            }
                            Spacer(minLength: 0)
                            Image(systemName: "chevron.up.chevron.down").font(.footnote).foregroundStyle(Color(.secondaryLabel)).accessibilityHidden(true)
                        }
                        .frame(minHeight: 44)
                        .contentShape(Rectangle())
                    }
                    .confirmationDialog(
                        "Stop sharing", isPresented: Binding(get: { stopping?.id == share.id }, set: { if !$0 { stopping = nil } }),
                        titleVisibility: .visible, presenting: stopping
                    ) { share in
                        Button("Stop sharing", role: .destructive) { Task { await model.stop(share) } }
                    } message: { share in
                        Text("\(LocationModel.shareName(of: share)) will no longer see where you are.")
                    }
                    .swipeActions {
                        Button("Stop sharing", systemImage: "person.badge.minus", role: .destructive) { stopping = share }
                            .tint(.red)
                    }
                }
            } header: {
                Text("Shared with")
            } footer: {
                if !model.shares.isEmpty { Text("Touch a row to change how much they see, or to stop sharing.") }
            }
            Section {
                Button("Share", systemImage: "person.badge.plus") { isSharing = true }
                    .actionColor()
            } footer: {
                Text("Choose a person or a circle, and whether they also see your visits and the places they were at.")
            }
            if let problem = model.sharingProblemText {
                Section { Text(verbatim: problem).foregroundStyle(.secondary) }
            }
        }
        .refreshable { await model.load() }
        .sheet(isPresented: $isSharing) { LocationShareSheet(model: model) }
    }
}

/// LocationShareSheet picks who to share with, the way calendar sharing does,
/// and one of the two levels.
struct LocationShareSheet: View {
    let model: LocationModel
    @Environment(\.dismiss) private var dismiss
    @State private var people: [CalendarSharePerson] = []
    @State private var circles: [CalendarShareCircle] = []
    @State private var isLoaded = false
    @State private var loadProblemText: String?
    @State private var chosen: ShareTarget?
    @State private var level = LocationShareLevel.current
    @State private var isWorking = false
    /// Said inside the sheet: the toast of the screen behind it is hidden.
    @State private var problemText: String?
    @State private var doneText: String?

    var body: some View {
        NavigationStack {
            Form {
                if isLoaded && people.isEmpty && circles.isEmpty {
                    Section {
                        Text("You do not share a circle with anybody yet. Make or join a circle to share where you are.")
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
                        Picker("How much", selection: $level) {
                            ForEach(LocationShareLevel.grantable, id: \.self) { each in
                                Text(each.title).tag(each)
                            }
                        }
                        .pickerStyle(.inline)
                        .labelsHidden()
                    } header: {
                        Text("How much")
                    } footer: {
                        Text(level.explanation)
                    }
                }
            }
            .navigationTitle("Share where I am")
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
                        Button("Share") { Task { await share() } }.disabled(chosen == nil)
                    }
                }
            }
            .toast(done: $doneText, problem: $problemText)
            .task { await load() }
        }
        .presentationDetents([.medium, .large])
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
        if let refusalText = await model.share(with: target, name: name, level: level) {
            problemText = refusalText
        } else {
            dismiss()
        }
    }
}
