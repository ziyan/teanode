import SwiftUI
import TeaNodeKit

// One item of Passwords: its fields (secrets hidden until shown, a tap
// copies), the one-time code counting down, passkeys, sites that open in
// Safari, and for the owner: the agent, sharing and deleting.

/// PasswordsItemScreen shows one item.
struct PasswordsItemScreen: View {
    @Bindable var model: PasswordsModel
    let itemId: String
    @Binding var editing: PasswordsEditing?
    @State private var revealedFieldNames: Set<String> = []
    @State private var isConfirmingDelete = false
    @State private var isWorking = false
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        Group {
            if let item = model.item(id: itemId) {
                details(item)
            } else {
                ContentUnavailableView {
                    Label("This item is not here any more", systemImage: "key")
                } description: {
                    Text("It may have been deleted, or it is no longer shared with you.")
                }
            }
        }
        // A secret shown stays shown only while the app is in front.
        .onChange(of: scenePhase) { _, current in
            if current != .active { revealedFieldNames = [] }
        }
    }

    private func details(_ item: PasswordItem) -> some View {
        Form {
            fieldSections(item)
            ownerSections(item)
        }
        .navigationTitle(item.name)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if !item.isSharedWithMe, !item.isDeleted {
                ToolbarItem(placement: .primaryAction) {
                    Button("Edit") { editing = PasswordsEditing(itemKind: item.itemKind, item: item) }
                }
            }
        }
        .task { model.noteUse() }
    }

    // MARK: Fields

    @ViewBuilder
    private func fieldSections(_ item: PasswordItem) -> some View {
        switch item.fields {
        case .login(let login): loginSections(login)
        case .card(let card): cardSections(card)
        case .note(let note): noteSections(note)
        }
    }

    private func isRevealed(_ fieldName: String) -> Bool { revealedFieldNames.contains(fieldName) }

    private func toggleReveal(_ fieldName: String) {
        if revealedFieldNames.contains(fieldName) {
            revealedFieldNames.remove(fieldName)
        } else {
            revealedFieldNames.insert(fieldName)
            model.noteUse()
        }
    }

    @ViewBuilder
    private func loginSections(_ login: PasswordsLoginFields) -> some View {
        Section {
            if !login.userName.isEmpty {
                PasswordsFieldRow(model: model, title: String(localized: "User name"), value: login.userName)
            }
            if !login.password.isEmpty {
                PasswordsFieldRow(
                    model: model, title: String(localized: "Password"), value: login.password, isSecret: true,
                    isRevealed: isRevealed("password")
                ) { toggleReveal("password") }
            }
            if let settings = try? PasswordsOneTimeCode.parse(uri: login.oneTimeCodeUri) {
                PasswordsOneTimeCodeRow(model: model, settings: settings)
            } else if !login.oneTimeCodeUri.isEmpty {
                Text("This one-time code could not be read.")
                    .foregroundStyle(.secondary)
            }
        }
        if !login.sites.isEmpty {
            Section("Sites") {
                ForEach(login.sites, id: \.self) { site in
                    siteRow(site)
                }
            }
        }
        if !login.passkeys.isEmpty {
            Section("Passkeys") {
                ForEach(Array(login.passkeys.enumerated()), id: \.offset) { _, passkey in
                    PasswordsPasskeyRow(passkey: passkey, loginUserName: login.userName)
                }
            }
        }
        if !login.notes.isEmpty {
            Section("Notes") {
                PasswordsFieldRow(model: model, title: String(localized: "Notes"), value: login.notes)
            }
        }
        folderSection(login.folder)
    }

    @ViewBuilder
    private func siteRow(_ site: String) -> some View {
        if let url = PasswordsFormat.url(ofSite: site) {
            Link(destination: url) {
                Label {
                    Text(verbatim: PasswordsFormat.shownSite(site))
                } icon: {
                    Image(systemName: "safari")
                }
            }
        } else {
            Text(verbatim: site)
        }
    }

    @ViewBuilder
    private func cardSections(_ card: PasswordsCardFields) -> some View {
        Section {
            if !card.cardholderName.isEmpty {
                PasswordsFieldRow(model: model, title: String(localized: "Name on card"), value: card.cardholderName)
            }
            if !card.number.isEmpty {
                PasswordsFieldRow(
                    model: model, title: String(localized: "Card number"), value: PasswordsFormat.groupedCardNumber(card.number),
                    copiedValue: card.number.filter(\.isNumber), isSecret: true, isRevealed: isRevealed("number"),
                    maskedValue: PasswordsFormat.maskedCardNumber(card.number)
                ) { toggleReveal("number") }
            }
            let expiryText = PasswordsFormat.expiryText(month: card.expiryMonth, year: card.expiryYear)
            if !expiryText.isEmpty {
                PasswordsFieldRow(model: model, title: String(localized: "Expires"), value: expiryText)
            }
            if !card.securityCode.isEmpty {
                PasswordsFieldRow(
                    model: model, title: String(localized: "Security code"), value: card.securityCode, isSecret: true,
                    isRevealed: isRevealed("securityCode")
                ) { toggleReveal("securityCode") }
            }
            if !card.brand.isEmpty {
                PasswordsFieldRow(model: model, title: String(localized: "Card brand"), value: card.brand)
            }
        }
        if !card.notes.isEmpty {
            Section("Notes") {
                PasswordsFieldRow(model: model, title: String(localized: "Notes"), value: card.notes)
            }
        }
        folderSection(card.folder)
    }

    @ViewBuilder
    private func noteSections(_ note: PasswordsNoteFields) -> some View {
        Section {
            PasswordsFieldRow(model: model, title: String(localized: "Note"), value: note.text)
        }
        folderSection(note.folder)
    }

    @ViewBuilder
    private func folderSection(_ folder: String) -> some View {
        if !folder.isEmpty {
            Section {
                LabeledContent("Folder") { Text(verbatim: folder) }
            }
        }
    }

    // MARK: The owner's part

    @ViewBuilder
    private func ownerSections(_ item: PasswordItem) -> some View {
        if item.isDeleted {
            Section {
                Button("Restore", systemImage: "arrow.uturn.backward") { Task { await restore(item) } }
                    .actionColor()
                    .disabled(isWorking)
            } footer: {
                Text("Items you delete stay here for 30 days. Then they are gone for good.")
            }
        } else if item.isSharedWithMe {
            Section {
                Label("Shared by \(item.ownerName)", systemImage: "person.2")
                    .foregroundStyle(.primary)
            } footer: {
                Text("\(item.ownerName) shared this with you. Only \(item.ownerName) can change it.")
            }
        } else {
            if item.itemKind != .card { agentSection(item) }
            shareSection(item)
            deleteSection(item)
        }
    }

    private func agentSection(_ item: PasswordItem) -> some View {
        Section {
            Toggle(
                "Let the agent use this",
                isOn: Binding(
                    get: { item.isUsableByAgent },
                    set: { isOn in Task { await setAgent(item, isOn: isOn) } })
            )
            .disabled(isWorking)
            if item.isUsableByAgent, let usedAt = item.lastUsedByAgentAt {
                LabeledContent("Last used by the agent") {
                    Text(verbatim: usedAt.formatted(.relative(presentation: .named)))
                }
            }
        } header: {
            Text("The agent")
        } footer: {
            if item.itemKind == .note {
                Text("The agent can read this note, and so can the model behind it.")
            } else {
                Text("The agent signs in with this login on its sites and never sees the password.")
            }
        }
    }

    private func shareSection(_ item: PasswordItem) -> some View {
        Section {
            NavigationLink(value: PasswordsDestination.share(item.id)) {
                HStack {
                    Label("Share", systemImage: "person.badge.plus")
                        .foregroundStyle(.primary)
                    Spacer()
                    Text(verbatim: sharedSummary(item))
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.trailing)
                }
            }
        } footer: {
            Text("Choose a circle or a person. Only you can change an item you share.")
        }
    }

    private func sharedSummary(_ item: PasswordItem) -> String {
        if item.shares.isEmpty { return String(localized: "Not shared") }
        return ListFormatter.localizedString(byJoining: item.shares.map { PasswordsWords.granteeName(of: $0) })
    }

    private func deleteSection(_ item: PasswordItem) -> some View {
        Section {
            Button(role: .destructive) {
                isConfirmingDelete = true
            } label: {
                Label("Delete", systemImage: "trash").foregroundStyle(.red)
            }
            .tint(.red)
            .actionColor(isDestructive: true)
            .disabled(isWorking)
            .confirmationDialog("Delete \(item.name)?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
                Button("Delete", role: .destructive) { Task { await delete(item) } }
            } message: {
                Text("It moves to Recently deleted for 30 days. People it is shared with lose it now, and the agent can no longer use it.")
            }
        }
    }

    // MARK: Actions

    private func setAgent(_ item: PasswordItem, isOn: Bool) async {
        isWorking = true
        defer { isWorking = false }
        do {
            try await model.setUsableByAgent(item, isUsable: isOn)
            model.doneText =
                isOn
                ? String(localized: "The agent can use this item now.")
                : String(localized: "The agent can no longer use this item.")
        } catch {
            model.report(error, orSay: String(localized: "The agent setting could not be changed."))
        }
    }

    private func delete(_ item: PasswordItem) async {
        isWorking = true
        defer { isWorking = false }
        do {
            try await model.delete(item)
            model.doneText = String(localized: "\(item.name) was deleted. It stays in Recently deleted for 30 days.")
            dismiss()
        } catch {
            model.report(error, orSay: String(localized: "The item could not be deleted."))
        }
    }

    private func restore(_ item: PasswordItem) async {
        isWorking = true
        defer { isWorking = false }
        do {
            try await model.restore(item)
            model.doneText = String(localized: "\(item.name) was restored.")
            dismiss()
        } catch {
            model.report(error, orSay: String(localized: "The item could not be restored."))
        }
    }
}

// MARK: Rows

/// PasswordsFieldRow is one field of an item: its name above its value. A tap
/// copies the value; a secret shows as dots until the eye is pressed.
struct PasswordsFieldRow: View {
    let model: PasswordsModel
    let title: String
    let value: String
    /// What a tap copies, when it is not the value as shown (a card number without its spaces).
    var copiedValue: String?
    var isSecret = false
    var isRevealed = false
    /// What a secret shows while hidden; dots by default, so a password's length is not told.
    var maskedValue = "••••••••••"
    var onToggleReveal: (() -> Void)?

    var body: some View {
        HStack {
            Button {
                model.copy(copiedValue ?? value, fieldName: title)
            } label: {
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: title)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    Text(verbatim: shownValue)
                        .font(isSecret && isRevealed ? Font.body.monospaced() : Font.body)
                        .foregroundStyle(.primary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(Text(verbatim: title))
            .accessibilityValue(Text(verbatim: isSecret && !isRevealed ? String(localized: "Hidden") : value))
            .accessibilityHint(Text("Copies it"))
            if isSecret, let onToggleReveal {
                Button(action: onToggleReveal) {
                    Image(systemName: isRevealed ? "eye.slash" : "eye")
                        .frame(minWidth: 44, minHeight: 44)
                }
                .buttonStyle(.borderless)
                .accessibilityLabel(isRevealed ? Text("Hide \(title)") : Text("Show \(title)"))
            }
        }
    }

    private var shownValue: String { isSecret && !isRevealed ? maskedValue : value }
}

/// PasswordsOneTimeCodeRow is a login's one-time code, changing as it does,
/// with the seconds left before the next. A tap copies the code.
struct PasswordsOneTimeCodeRow: View {
    let model: PasswordsModel
    let settings: PasswordsOneTimeCodeSettings

    var body: some View {
        TimelineView(.periodic(from: .now, by: 0.5)) { context in
            let code = PasswordsOneTimeCode.code(settings: settings, date: context.date)
            let secondsLeft = PasswordsOneTimeCode.secondsLeft(settings: settings, date: context.date)
            Button {
                model.copy(code, fieldName: String(localized: "One-time code"))
            } label: {
                VStack(alignment: .leading, spacing: 6) {
                    Text("One-time code")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    Text(verbatim: PasswordsFormat.groupedOneTimeCode(code))
                        .font(.title2.monospaced())
                        .foregroundStyle(.primary)
                        .contentTransition(.numericText())
                    ProgressView(value: Double(secondsLeft), total: Double(settings.periodSeconds))
                    Text("Changes in \(secondsLeft) seconds")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .monospacedDigit()
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(Text("One-time code"))
            .accessibilityValue(Text(verbatim: code))
            .accessibilityHint(Text("Copies it"))
        }
    }
}

/// PasswordsPasskeyRow is a passkey kept in a login: the site it is for, the
/// account, and when it was made.
struct PasswordsPasskeyRow: View {
    let passkey: PasswordsPasskeyFields
    let loginUserName: String

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: passkey.relyingPartyId)
            let accountName = passkey.userName.isEmpty ? loginUserName : passkey.userName
            if !accountName.isEmpty {
                Text(verbatim: accountName)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if let createdAt = ServerTime.date(passkey.createdAt) {
                Text("Created \(createdAt.formatted(date: .abbreviated, time: .omitted))")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }
}
