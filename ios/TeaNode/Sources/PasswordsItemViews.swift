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
    @State private var isConfirmingDelete = false
    @State private var isConfirmingDeleteForever = false
    @State private var isWorking = false
    @State private var finishAsk: PasswordsFinishAsk?
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
    }

    private func details(_ item: PasswordItem) -> some View {
        Form {
            PasswordsFieldSections(model: model, fields: item.fields)
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
            deleteForeverSection(item)
        } else if item.isSharedWithMe {
            Section {
                Label("Shared by \(item.ownerName)", systemImage: "person.2")
                    .foregroundStyle(.primary)
            } footer: {
                Text("\(item.ownerName) shared this with you. Only \(item.ownerName) can change it.")
            }
        } else {
            if item.itemKind != .card { agentSection(item) }
            if model.itemIdsNeedingYou.contains(item.id) {
                finishSharingSection(item)
            } else if model.itemIdsNeedingNewKey.contains(item.id) {
                newKeySection(item)
            }
            sharingNoticeSection(item)
            shareSection(item)
            versionsSection(item)
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
            // The agent's consent is the person's own record, which has to be checked first.
            .disabled(isWorking || item.sharingCheck != .verified)
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

    /// Shown when this iPhone left the item for the person: it has no memory of the item from before,
    /// or somebody waiting is a person it never sealed the item for. One button does what it would
    /// have done by itself, for everybody the item's own record reaches.
    private func finishSharingSection(_ item: PasswordItem) -> some View {
        Section {
            Text("Some people are waiting for this item, or it needs a new key. This iPhone has not shared it before, so it waits for you.")
                .foregroundStyle(.primary)
            Button("Finish sharing", systemImage: "person.badge.plus") { Task { await askToFinishSharing(item) } }
                .actionColor()
                .disabled(isWorking)
                .passwordsFinishSharingDialog($finishAsk) { ask in Task { await finishSharing(item, plan: ask.plan) } }
        }
    }

    /// Works out what finishing would do and asks first: who gets a key, and whether the agent
    /// gets its copy back. Nothing is sealed until the person says yes.
    private func askToFinishSharing(_ item: PasswordItem) async {
        let plan = await model.finishPlan(itemIds: [item.id])
        finishAsk = PasswordsFinishAsk(
            itemIds: [item.id], plan: plan, title: PasswordsWords.finishConfirmationTitle(itemCount: 1, itemName: item.name))
    }

    /// Shown when somebody was removed from the item but it could not be given a new key yet.
    private func newKeySection(_ item: PasswordItem) -> some View {
        Section {
            if model.itemIdsNeedingAttention.contains(item.id) {
                Text("This item needs a new key. This iPhone gave it one a moment ago, so it waits before giving another.")
                    .foregroundStyle(.primary)
            } else {
                Text("Removed. The item could not be given a new key.")
                    .foregroundStyle(.primary)
            }
            Button("Try again", systemImage: "arrow.clockwise") { Task { await giveNewKey(item) } }
                .actionColor()
                .disabled(isWorking)
        }
    }

    /// Says when the item's sharing cannot be trusted, or when the server lists somebody the person
    /// never shared the item with. Setting sharing again writes a record that is checked again.
    @ViewBuilder
    private func sharingNoticeSection(_ item: PasswordItem) -> some View {
        let isUnchecked = item.sharingCheck == .unverified || item.sharingCheck == .replayed
        let unrecordedText = PasswordsWords.unrecordedSharesNotice(of: item)
        if isUnchecked || unrecordedText != nil {
            Section {
                if item.sharingCheck == .unverified {
                    Text("Sharing could not be checked")
                        .font(.body.weight(.semibold))
                    Text("Nobody gets this item and the agent cannot use it until you set sharing again.")
                        .foregroundStyle(.secondary)
                } else if item.sharingCheck == .replayed {
                    Text("This item's sharing was changed back by the server")
                        .font(.body.weight(.semibold))
                    Text("Nothing is done with the older sharing. Set sharing again to choose who has this item.")
                        .foregroundStyle(.secondary)
                }
                if let unrecordedText {
                    Text(unrecordedText)
                }
                if isUnchecked {
                    NavigationLink(value: PasswordsDestination.share(item.id)) {
                        Label("Set sharing again", systemImage: "person.badge.plus")
                            .foregroundStyle(.primary)
                    }
                }
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
                    PasswordsValueText(PasswordsWords.shareSummary(of: item), isProse: true)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.trailing)
                }
            }
        } footer: {
            Text("Choose a circle or a person. Only you can change an item you share.")
        }
    }

    private func versionsSection(_ item: PasswordItem) -> some View {
        Section {
            NavigationLink(value: PasswordsDestination.versions(item.id)) {
                Label("Earlier versions", systemImage: "clock.arrow.circlepath")
                    .foregroundStyle(.primary)
            }
        } footer: {
            Text("The versions this item had before you changed it are kept for 30 days.")
        }
    }

    /// Deleting an item that is already in Recently deleted removes it and its old versions for good.
    private func deleteForeverSection(_ item: PasswordItem) -> some View {
        Section {
            Button(role: .destructive) {
                isConfirmingDeleteForever = true
            } label: {
                Label("Delete forever", systemImage: "trash").foregroundStyle(.red)
            }
            .tint(.red)
            .actionColor(isDestructive: true)
            .disabled(isWorking)
            .confirmationDialog("Delete \(item.name) forever?", isPresented: $isConfirmingDeleteForever, titleVisibility: .visible) {
                Button("Delete forever", role: .destructive) { Task { await deleteForever(item) } }
            } message: {
                Text("It is removed from Passwords and from this iPhone, and cannot be brought back.")
            }
        }
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
            // The toast may already say who got access after the new key.
            if model.doneText == nil {
                model.doneText =
                    isOn
                    ? String(localized: "The agent can use this item now.")
                    : String(localized: "The agent can no longer use this item.")
            }
        } catch {
            model.report(error, orSay: String(localized: "The agent setting could not be changed."))
        }
    }

    private func finishSharing(_ item: PasswordItem, plan: PasswordsFinishPlan) async {
        isWorking = true
        defer { isWorking = false }
        await model.finishSharing(item, plan: plan)
    }

    private func giveNewKey(_ item: PasswordItem) async {
        isWorking = true
        defer { isWorking = false }
        await model.giveNewKey(item)
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

    private func deleteForever(_ item: PasswordItem) async {
        isWorking = true
        defer { isWorking = false }
        do {
            try await model.purge(item)
            model.doneText = String(localized: "\(item.name) was deleted forever.")
            dismiss()
        } catch {
            model.report(error, orSay: String(localized: "The item could not be deleted forever."))
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

// MARK: Fields

/// PasswordsFieldSections are the sections that show an item's fields (a login, a card or a note):
/// secrets hidden until shown, a tap copies, sites open in Safari. The item's screen and an earlier
/// version's screen both show them.
struct PasswordsFieldSections: View {
    let model: PasswordsModel
    let fields: PasswordsItemFields
    @State private var revealedFieldNames: Set<String> = []
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        Group {
            switch fields {
            case .login(let login): loginSections(login)
            case .card(let card): cardSections(card)
            case .note(let note): noteSections(note)
            }
        }
        // A secret shown stays shown only while the app is in front.
        .onChange(of: scenePhase) { _, current in
            if current != .active { revealedFieldNames = [] }
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
                PasswordsFieldRow(model: model, fieldTitle: String(localized: "User name"), fieldValue: login.userName)
            }
            if !login.password.isEmpty {
                PasswordsFieldRow(
                    model: model, fieldTitle: String(localized: "Password"), fieldValue: login.password, isSecret: true,
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
                PasswordsFieldRow(model: model, fieldTitle: String(localized: "Notes"), fieldValue: login.notes, isProse: true)
            }
        }
        folderSection(login.folder)
    }

    @ViewBuilder
    private func siteRow(_ site: String) -> some View {
        if let url = PasswordsFormat.url(ofSite: site) {
            Link(destination: url) {
                Label {
                    PasswordsValueText(PasswordsFormat.shownSite(site))
                } icon: {
                    Image(systemName: "safari")
                }
            }
        } else {
            PasswordsValueText(site)
        }
    }

    @ViewBuilder
    private func cardSections(_ card: PasswordsCardFields) -> some View {
        Section {
            if !card.cardholderName.isEmpty {
                PasswordsFieldRow(model: model, fieldTitle: String(localized: "Name on card"), fieldValue: card.cardholderName, isProse: true)
            }
            if !card.number.isEmpty {
                PasswordsFieldRow(
                    model: model, fieldTitle: String(localized: "Card number"), fieldValue: PasswordsFormat.groupedCardNumber(card.number),
                    copiedValue: card.number.filter(\.isNumber), isSecret: true, isRevealed: isRevealed("number"),
                    maskedValue: PasswordsFormat.maskedCardNumber(card.number)
                ) { toggleReveal("number") }
            }
            let expiryText = PasswordsFormat.expiryText(month: card.expiryMonth, year: card.expiryYear)
            if !expiryText.isEmpty {
                PasswordsFieldRow(model: model, fieldTitle: String(localized: "Expires"), fieldValue: expiryText)
            }
            if !card.securityCode.isEmpty {
                PasswordsFieldRow(
                    model: model, fieldTitle: String(localized: "Security code"), fieldValue: card.securityCode, isSecret: true,
                    isRevealed: isRevealed("securityCode")
                ) { toggleReveal("securityCode") }
            }
            if !card.brand.isEmpty {
                PasswordsFieldRow(model: model, fieldTitle: String(localized: "Card brand"), fieldValue: card.brand)
            }
        }
        if !card.notes.isEmpty {
            Section("Notes") {
                PasswordsFieldRow(model: model, fieldTitle: String(localized: "Notes"), fieldValue: card.notes, isProse: true)
            }
        }
        folderSection(card.folder)
    }

    @ViewBuilder
    private func noteSections(_ note: PasswordsNoteFields) -> some View {
        Section {
            PasswordsFieldRow(model: model, fieldTitle: String(localized: "Note"), fieldValue: note.text, isProse: true)
        }
        folderSection(note.folder)
    }

    @ViewBuilder
    private func folderSection(_ folder: String) -> some View {
        if !folder.isEmpty {
            Section {
                LabeledContent("Folder") { PasswordsValueText(folder, isProse: true) }
            }
        }
    }
}

// MARK: Rows

/// PasswordsFieldRow is one field of an item: its name above its value. A tap
/// copies the value; a secret shows as dots until the eye is pressed.
struct PasswordsFieldRow: View {
    let model: PasswordsModel
    let fieldTitle: String
    let fieldValue: String
    /// What a tap copies, when it is not the field value as shown (a card number without its spaces).
    var copiedValue: String?
    /// Whether the value is sentences (a note) rather than a name, a number or a password.
    var isProse = false
    var isSecret = false
    var isRevealed = false
    /// What a secret shows while hidden; dots by default, so a password's length is not told.
    var maskedValue = "••••••••••"
    var onToggleReveal: (() -> Void)?

    var body: some View {
        HStack {
            Button {
                model.copy(copiedValue ?? fieldValue, fieldName: fieldTitle)
            } label: {
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: fieldTitle)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    shownText
                        .font(isSecret && isRevealed ? Font.body.monospaced() : Font.body)
                        .foregroundStyle(.primary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(Text(verbatim: fieldTitle))
            .accessibilityValue(Text(verbatim: isSecret && !isRevealed ? String(localized: "Hidden") : fieldValue))
            .accessibilityHint(Text("Copies it"))
            if isSecret, let onToggleReveal {
                Button(action: onToggleReveal) {
                    Image(systemName: isRevealed ? "eye.slash" : "eye")
                        .frame(minWidth: 44, minHeight: 44)
                }
                .buttonStyle(.borderless)
                .accessibilityLabel(isRevealed ? Text("Hide \(fieldTitle)") : Text("Show \(fieldTitle)"))
            }
        }
    }

    /// The dots of a hidden secret as they are; anything else as a stored value.
    @ViewBuilder private var shownText: some View {
        if isSecret && !isRevealed {
            Text(verbatim: maskedValue)
        } else {
            PasswordsValueText(fieldValue, isProse: isProse)
        }
    }
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
            PasswordsValueText(passkey.relyingPartyId)
            let accountName = passkey.userName.isEmpty ? loginUserName : passkey.userName
            if !accountName.isEmpty {
                PasswordsValueText(accountName)
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
