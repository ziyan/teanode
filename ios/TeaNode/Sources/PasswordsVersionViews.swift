import SwiftUI
import TeaNodeKit

// The earlier versions of an item the person owns: the list (each with its date and who saved it),
// one version read-only, and the button that puts it back. The server keeps them for 30 days.

/// PasswordsVersionsScreen lists the earlier versions of an item, newest first.
struct PasswordsVersionsScreen: View {
    @Bindable var model: PasswordsModel
    let itemId: String
    @State private var isLoading = true

    var body: some View {
        Group {
            if let item = model.item(id: itemId), !item.isSharedWithMe, !item.isDeleted {
                content(item)
            } else {
                ContentUnavailableView {
                    Label("This item is not here any more", systemImage: "key")
                } description: {
                    Text("It may have been deleted, or it is no longer shared with you.")
                }
            }
        }
        .navigationTitle("Earlier versions")
        .navigationBarTitleDisplayMode(.inline)
        .task {
            // Read once; coming back to this screen from a version shows what was read.
            guard model.versionsByItemId[itemId] == nil, let item = model.item(id: itemId) else { return }
            await load(item)
        }
    }

    @ViewBuilder
    private func content(_ item: PasswordItem) -> some View {
        if let opened = model.versionsByItemId[itemId] {
            if opened.versions.isEmpty && opened.unreadableVersionCount == 0 {
                ContentUnavailableView {
                    Label("No earlier versions", systemImage: "clock.arrow.circlepath")
                } description: {
                    Text("When you change this item, the version it had is kept here for 30 days.")
                }
            } else {
                versionList(opened)
            }
        } else if isLoading {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            LoadFailedView(problemText: String(localized: "The earlier versions could not be read.")) {
                await model.loadVersions(of: item)
            }
        }
    }

    private func versionList(_ opened: PasswordsOpenedVersions) -> some View {
        List {
            Section {
                ForEach(opened.versions) { version in
                    NavigationLink(value: PasswordsDestination.version(itemId: itemId, versionId: version.id)) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: PasswordsWords.versionDateText(version))
                                .foregroundStyle(.primary)
                            Text(verbatim: PasswordsWords.versionSavedByText(version))
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                        .accessibilityElement(children: .combine)
                    }
                }
            } footer: {
                if opened.unreadableVersionCount > 0 {
                    let unreadableCount = opened.unreadableVersionCount
                    Text("\(unreadableCount) versions could not be opened.")
                }
            }
        }
    }

    private func load(_ item: PasswordItem) async {
        await model.loadVersions(of: item)
        isLoading = false
    }
}

/// PasswordsVersionScreen shows one earlier version read-only, with the button that puts it back as
/// the item's own. `onRestored` leaves the screens of versions once it is back.
struct PasswordsVersionScreen: View {
    @Bindable var model: PasswordsModel
    let itemId: String
    let versionId: String
    let onRestored: () -> Void
    @State private var isConfirmingRestore = false
    @State private var isWorking = false

    private var version: PasswordItemVersion? {
        model.versionsByItemId[itemId]?.versions.first { $0.id == versionId }
    }

    var body: some View {
        if let item = model.item(id: itemId), let version {
            Form {
                Section {
                    Label(PasswordsWords.versionSavedByText(version), systemImage: "clock.arrow.circlepath")
                        .foregroundStyle(.primary)
                }
                PasswordsFieldSections(model: model, fields: version.fields)
                restoreSection(item, version: version)
            }
            .navigationTitle(PasswordsWords.versionDateText(version))
            .navigationBarTitleDisplayMode(.inline)
            .task { model.noteUse() }
        } else {
            ContentUnavailableView {
                Label("This version is not here any more", systemImage: "clock.arrow.circlepath")
            } description: {
                Text("Go back to the list of earlier versions to see what is kept.")
            }
        }
    }

    private func restoreSection(_ item: PasswordItem, version: PasswordItemVersion) -> some View {
        let dateText = PasswordsWords.versionDateText(version)
        return Section {
            Button("Restore this version", systemImage: "arrow.uturn.backward") { isConfirmingRestore = true }
                .actionColor()
                .disabled(isWorking)
                .confirmationDialog("Restore this version?", isPresented: $isConfirmingRestore, titleVisibility: .visible) {
                    Button("Restore this version") { Task { await restore(item, version: version) } }
                } message: {
                    Text(
                        "This item goes back to how it was on \(dateText), including who it is shared with and whether the agent may use it. What it is now stays here as an earlier version."
                    )
                }
        }
    }

    private func restore(_ item: PasswordItem, version: PasswordItemVersion) async {
        isWorking = true
        defer { isWorking = false }
        let dateText = PasswordsWords.versionDateText(version)
        do {
            try await model.restoreVersion(version, of: item)
            model.doneText = String(localized: "Restored the version from \(dateText).")
            onRestored()
            model.forgetVersions(ofItemId: item.id)
        } catch {
            model.report(error, orSay: String(localized: "The version could not be restored."))
        }
    }
}

extension View {
    /// The question asked before sharing is finished, attached to the button that asks it (on iOS 26
    /// a confirmation points at its control): what would happen, then "Finish sharing". `confirm`
    /// runs when the person says yes.
    func passwordsFinishSharingDialog(_ ask: Binding<PasswordsFinishAsk?>, confirm: @escaping () -> Void) -> some View {
        confirmationDialog(
            ask.wrappedValue?.title ?? "",
            isPresented: Binding(get: { ask.wrappedValue != nil }, set: { isShown in if !isShown { ask.wrappedValue = nil } }),
            titleVisibility: .visible, presenting: ask.wrappedValue
        ) { _ in
            Button("Finish sharing", action: confirm)
        } message: { shown in
            Text(verbatim: PasswordsWords.finishConfirmationLines(shown.plan, isOneItem: shown.isOneItem).joined(separator: "\n"))
        }
    }
}
