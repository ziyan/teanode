import SwiftUI
import TeaNodeKit

// Sharing one item with a circle or a person in one. The owner's iPhone seals
// the item's key for each person who has set up Passwords, so the server never
// sees it. A person who has not set up Passwords is listed as waiting; this
// iPhone gives them their key the next time it is open after they have. Nobody
// is asked to confirm a key: the first key this iPhone sees for a person is
// the one it uses, and a key that changes is used too, with a notice that stays
// until it is dismissed. "Compare codes" is there for anybody who wants to check
// a key with the person, and marks it "Codes compared"; it gates nothing.

/// PasswordsShareGrantee is somebody an item is shared with, and the people
/// that reaches.
struct PasswordsShareGrantee: Identifiable, Hashable {
    let target: ShareTarget
    let name: String
    var recipients: [PasswordRecipient]

    var id: String {
        switch target {
        case .person(let userId): "person:\(userId)"
        case .circle(let circleId): "circle:\(circleId)"
        }
    }

    var input: PasswordShareGranteeInput {
        PasswordShareGranteeInput(granteeUserId: target.granteeUserId, granteeCircleId: target.granteeCircleId)
    }
}

/// PasswordsShareScreen changes who an item is shared with. The changes are
/// kept when the person presses Save.
struct PasswordsShareScreen: View {
    let model: PasswordsModel
    let itemId: String
    @State private var grantees: [PasswordsShareGrantee] = []
    @State private var people: [CalendarSharePerson] = []
    @State private var circles: [CalendarShareCircle] = []
    @State private var chosen: ShareTarget?
    /// The person whose safety codes are being compared, if the sheet is open.
    @State private var comparing: PasswordsCompareTarget?
    @State private var isLoaded = false
    @State private var isAdding = false
    @State private var isSaving = false
    @State private var loadProblemText: String?
    @Environment(\.dismiss) private var dismiss

    private var item: PasswordItem? { model.item(id: itemId) }

    var body: some View {
        Group {
            if item == nil {
                ContentUnavailableView("This item is not here any more", systemImage: "key")
            } else if let loadProblemText, !isLoaded {
                LoadFailedView(problemText: loadProblemText) { await load() }
            } else if !isLoaded {
                ProgressView()
            } else {
                form
            }
        }
        .navigationTitle(shareTitle)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                if isSaving {
                    ProgressView()
                } else {
                    Button("Save") { Task { await save() } }
                        .disabled(!isLoaded)
                }
            }
        }
        .task { await load() }
        .sheet(item: $comparing, onDismiss: { Task { await refreshRecipients() } }) { target in
            PasswordsCompareCodesSheet(model: model, target: target)
        }
    }

    private var shareTitle: String {
        guard let item else { return String(localized: "Share") }
        return String(localized: "Share \(item.name)")
    }

    private var form: some View {
        Form {
            Section {
                if people.isEmpty && circles.isEmpty {
                    Text("You are not in a circle with anyone yet.")
                        .foregroundStyle(.secondary)
                    OpenCirclesLink(connection: model.connection)
                } else {
                    ShareTargetPicker(people: people, circles: circles, chosen: $chosen)
                    Button("Add", systemImage: "plus") { Task { await add() } }
                        .actionColor()
                        .disabled(chosen == nil || isAdding)
                }
            } footer: {
                Text("Each person who has set up Passwords gets this item's key, sealed on this iPhone for their key only. Only you can change the item.")
            }
            if !keyChangesHere.isEmpty {
                Section {
                    ForEach(keyChangesHere) { change in
                        PasswordsKeyChangeRow(model: model, change: change)
                    }
                }
            }
            if let item, item.sharingCheck == .unverified || item.sharingCheck == .replayed {
                Section {
                    if item.sharingCheck == .replayed {
                        Text("This item's sharing was changed back by the server")
                            .font(.body.weight(.semibold))
                    } else {
                        Text("Sharing could not be checked")
                            .font(.body.weight(.semibold))
                    }
                    Text("Choose who has this item and save. That sets sharing again.")
                        .foregroundStyle(.secondary)
                }
            }
            if let item, let unrecordedText = PasswordsWords.unrecordedSharesNotice(of: item) {
                Section {
                    Text(unrecordedText)
                    Text("Saving removes them.")
                        .foregroundStyle(.secondary)
                }
            }
            if grantees.isEmpty {
                Section {
                    Text("Not shared with anyone.")
                        .foregroundStyle(.secondary)
                }
            }
            ForEach(grantees) { grantee in
                granteeSection(grantee)
            }
        }
    }

    /// The notices of changed keys for the people this item is shared with, on this screen as well as in the list.
    private var keyChangesHere: [PasswordsKeyChange] {
        let userIds = Set(grantees.flatMap(\.recipients).map(\.userId))
        return model.keyChanges.filter { userIds.contains($0.userId) }
    }

    private func granteeSection(_ grantee: PasswordsShareGrantee) -> some View {
        Section {
            if grantee.recipients.isEmpty {
                Text("Nobody else is in this circle yet.")
                    .foregroundStyle(.secondary)
            }
            ForEach(grantee.recipients) { recipient in
                PasswordsRecipientRow(recipient: recipient) { comparing = PasswordsCompareTarget(recipient: recipient) }
            }
            Button(role: .destructive) {
                grantees.removeAll { $0.id == grantee.id }
            } label: {
                Label("Stop sharing with \(grantee.name)", systemImage: "person.badge.minus").foregroundStyle(.red)
            }
            .tint(.red)
            .actionColor(isDestructive: true)
        } header: {
            PasswordsValueText(grantee.name, isProse: true)
        }
    }

    // MARK: Reading

    private func load() async {
        guard let item else { return }
        loadProblemText = nil
        do {
            async let readPeople = model.connection.calendarSharePeople()
            async let readCircles = model.connection.calendarShareCircles()
            (people, circles) = try await (readPeople, readCircles)
        } catch {
            // Without a circle nobody can be added, but the shares there are can still be seen and ended.
            if people.isEmpty, circles.isEmpty, grantees.isEmpty, item.shares.isEmpty { loadProblemText = error.loadProblemText }
        }
        if !isLoaded, grantees.isEmpty {
            // The shares the owner's own checked record lists. A share only the server lists is not shown
            // as one the person made (the form says so); saving ends it.
            for share in item.recordedShares {
                let reached = await recipientsReached(by: share)
                grantees.append(
                    PasswordsShareGrantee(
                        target: share.granteeCircleId.map { ShareTarget.circle($0) } ?? ShareTarget.person(share.granteeUserId ?? ""),
                        name: PasswordsWords.granteeName(of: share), recipients: reached))
            }
        }
        if loadProblemText == nil { isLoaded = true }
    }

    /// The people a share reaches, marked with what this iPhone knows of each key; without that, as the server lists them.
    private func recipientsReached(by share: PasswordShareView) async -> [PasswordRecipient] {
        if let known = try? await model.recipients(of: share) { return known }
        let circleIds = share.granteeCircleId.map { [$0] } ?? []
        return share.recipients.map { PasswordRecipient(view: $0, circleIds: circleIds) }
    }

    // MARK: Changing

    private func add() async {
        guard let target = chosen, !isAdding else { return }
        let name = ShareTargetPicker.name(of: target, people: people, circles: circles)
        if grantees.contains(where: { $0.target == target }) {
            chosen = nil
            return
        }
        isAdding = true
        defer { isAdding = false }
        do {
            let recipients = try await model.recipients(for: target)
            grantees.append(PasswordsShareGrantee(target: target, name: name, recipients: recipients))
            chosen = nil
        } catch {
            model.report(error, orSay: String(localized: "The people in that share could not be read."))
        }
    }

    /// Reads the people each share reaches again, so a key just marked as compared shows as such.
    private func refreshRecipients() async {
        for index in grantees.indices {
            if let fresh = try? await model.recipients(for: grantees[index].target) { grantees[index].recipients = fresh }
        }
    }

    private func save() async {
        guard let item else { return }
        isSaving = true
        defer { isSaving = false }
        // Everyone each share reaches; the session seals once for a person who is reached twice.
        let recipients = grantees.flatMap(\.recipients)
        let waitingUserIds = Set(recipients.filter { $0.publicKey == nil }.map(\.userId))
        do {
            try await model.setShares(of: item, grantees: grantees.map(\.input), recipients: recipients)
            let waitingCount = waitingUserIds.count
            if waitingCount > 0 {
                model.doneText = String(localized: "Sharing saved. \(waitingCount) people will get access when they set up Passwords.")
            } else {
                model.doneText = String(localized: "Sharing saved.")
            }
            dismiss()
        } catch PasswordsError.itemNeedsNewKey(let itemIdWithoutNewKey) {
            // The removal went through; the item's screen says it has no new key yet and offers a retry.
            model.report(PasswordsError.itemNeedsNewKey(itemId: itemIdWithoutNewKey), orSay: "")
            dismiss()
        } catch {
            model.report(error, orSay: String(localized: "Sharing could not be saved."))
        }
    }
}

/// PasswordsSafetyCodeLines shows one safety code under a caption.
struct PasswordsSafetyCodeLines: View {
    let caption: LocalizedStringKey
    let code: String

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(caption)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(verbatim: code)
                .font(.footnote.monospaced())
                .foregroundStyle(.secondary)
                .accessibilityLabel(Text(caption))
                .accessibilityValue(Text(verbatim: code))
        }
    }
}

/// PasswordsRecipientRow is one person an item reaches: their name, that they are
/// waiting to set up Passwords, or whether the codes were compared, with "Compare
/// codes" to check the key with them. Nothing here has to be done before sharing.
struct PasswordsRecipientRow: View {
    let recipient: PasswordRecipient
    /// Called by "Compare codes", which only a person with a key has.
    var compare: () -> Void = {}

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            PasswordsValueText(PersonWords.pickerLabel(name: recipient.name, username: recipient.username), isProse: true)
            if recipient.safetyCode == nil {
                Text("Waiting until they set up Passwords")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            } else {
                if recipient.isCompared {
                    Label("Codes compared", systemImage: "checkmark.circle.fill")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                Button(action: compare) {
                    Label("Compare codes", systemImage: "checkmark.shield")
                }
                .buttonStyle(.borderless)
                .actionColor()
            }
        }
        .accessibilityElement(children: .contain)
    }
}

/// PasswordsCompareTarget is a key whose safety codes the person can read out and compare.
struct PasswordsCompareTarget: Identifiable, Hashable {
    let userId: String
    let name: String
    /// The key whose code is shown, standard base64: what "Codes match" marks as compared.
    let publicKey: String
    let safetyCode: String
    /// The code of the key before it changed, when it did.
    let previousSafetyCode: String?
    let isCompared: Bool

    var id: String { userId }

    init(recipient: PasswordRecipient) {
        userId = recipient.userId
        name = recipient.name
        publicKey = recipient.publicKey?.base64EncodedString() ?? ""
        safetyCode = recipient.safetyCode ?? ""
        previousSafetyCode = recipient.knownSafetyCode
        isCompared = recipient.isCompared
    }

    init(change: PasswordsKeyChange) {
        userId = change.userId
        name = change.displayName
        publicKey = change.newPublicKey
        safetyCode = change.newSafetyCode
        previousSafetyCode = change.oldSafetyCode
        isCompared = false
    }
}

extension PasswordsKeyChange {
    /// The person's name, or a word for them when the copy does not hold it.
    var displayName: String {
        name.isEmpty ? String(localized: "Someone") : name
    }
}

/// PasswordsCompareCodesSheet shows the safety codes of a person's key (and the code it had before
/// when it changed) next to the person's own, to read out and compare. "Codes match" marks the
/// key as compared. Nothing waits on it.
struct PasswordsCompareCodesSheet: View {
    let model: PasswordsModel
    let target: PasswordsCompareTarget
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text("Ask \(target.name) to open Passwords settings on their own device and read you their safety code. It should be the same as the one below.")
                    PasswordsSafetyCodeLines(caption: "Their safety code", code: target.safetyCode)
                    if let previousSafetyCode = target.previousSafetyCode {
                        PasswordsSafetyCodeLines(caption: "Their safety code before", code: previousSafetyCode)
                    }
                    if let ownSafetyCode = model.ownSafetyCode {
                        PasswordsSafetyCodeLines(caption: "Your safety code", code: ownSafetyCode)
                    }
                } footer: {
                    Text("Comparing is optional. Sharing and opening items work either way.")
                }
                Section {
                    if target.isCompared {
                        Label("Codes compared", systemImage: "checkmark.circle.fill")
                            .foregroundStyle(.primary)
                    } else {
                        Button {
                            Task {
                                await model.markCompared(userId: target.userId, publicKey: target.publicKey)
                                dismiss()
                            }
                        } label: {
                            Label("The codes match", systemImage: "checkmark.shield")
                                .actionColor()
                        }
                        .actionColor()
                    }
                }
            }
            .navigationTitle("Compare codes")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
    }
}

/// PasswordsKeyChangeRow is the notice that a person's key changed: it was accepted, nothing
/// waits on it, and it stays until it is dismissed. "Compare codes" shows both codes.
struct PasswordsKeyChangeRow: View {
    let model: PasswordsModel
    let change: PasswordsKeyChange
    @State private var isComparing = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("\(change.displayName)'s key changed. This happens when they erase Passwords and set it up again. If you did not expect it, compare safety codes with them.")
                .font(.footnote)
            VStack(alignment: .leading, spacing: 8) {
                Button { isComparing = true } label: {
                    Label("Compare codes", systemImage: "checkmark.shield")
                        .actionColor()
                }
                Button { Task { await model.dismissKeyChange(change) } } label: {
                    Label("Dismiss", systemImage: "xmark")
                        .actionColor()
                }
            }
            .buttonStyle(.borderless)
        }
        .accessibilityElement(children: .contain)
        .sheet(isPresented: $isComparing) {
            PasswordsCompareCodesSheet(model: model, target: PasswordsCompareTarget(change: change))
        }
    }
}
