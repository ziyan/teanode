import SwiftUI
import TeaNodeKit

// Sharing one item with a circle or a person in one. The owner's iPhone seals
// the item's key for each person who has set up Passwords, so the server never
// sees it. A person who has not set up Passwords is listed as waiting; this
// iPhone gives them their key the next time it is open after they have. The
// safety codes are shown so the owner can check each one with the person: a
// code that is not the same on their screen is a key that is not theirs. A key
// is sealed for only after the owner confirmed its code (or this iPhone
// already knew the key), and a key that changed is shown with both codes and
// waits for the owner to trust it.

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
    /// The people whose safety code the owner confirmed on this screen.
    @State private var confirmedUserIds: Set<String> = []
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
                Text("Each person who has set up Passwords gets this item's key, sealed on this iPhone for their key only, once you have confirmed their safety code. Only you can change the item.")
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

    private func granteeSection(_ grantee: PasswordsShareGrantee) -> some View {
        Section {
            if grantee.recipients.isEmpty {
                Text("Nobody else is in this circle yet.")
                    .foregroundStyle(.secondary)
            }
            ForEach(grantee.recipients) { recipient in
                PasswordsRecipientRow(
                    recipient: recipient, isConfirmed: confirmedUserIds.contains(recipient.userId),
                    toggleConfirmed: { toggleConfirmed(recipient) })
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
        } footer: {
            if grantee.id == grantees.last?.id {
                Text("Ask each person to open Passwords settings on their own device and read you their safety code. Confirm only if it matches.")
            }
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

    private func toggleConfirmed(_ recipient: PasswordRecipient) {
        if confirmedUserIds.contains(recipient.userId) {
            confirmedUserIds.remove(recipient.userId)
        } else {
            confirmedUserIds.insert(recipient.userId)
        }
    }

    private func save() async {
        guard let item else { return }
        isSaving = true
        defer { isSaving = false }
        // Everyone each share reaches; the session seals once for a person who is reached twice.
        let recipients = grantees.flatMap(\.recipients)
        let waitingUserIdsHere = Set(
            recipients.filter { $0.publicKey == nil || ($0.needsConfirmation && !confirmedUserIds.contains($0.userId)) }.map(\.userId))
        do {
            let waitingUserIdsForOk = try await model.setShares(
                of: item, grantees: grantees.map(\.input), recipients: recipients, confirmedUserIds: Array(confirmedUserIds))
            let waitingCount = waitingUserIdsHere.union(waitingUserIdsForOk).count
            if waitingCount > 0 {
                model.doneText = String(localized: "Sharing saved. \(waitingCount) people will get access when they set up Passwords and you confirm their key.")
            } else {
                model.doneText = String(localized: "Sharing saved.")
            }
            // Whoever is left waiting for the person's OK is listed at the top of the Passwords screen.
            if !waitingUserIdsForOk.isEmpty { model.openConfirmations() }
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

extension PasswordRecipient {
    /// Whether the owner has to confirm this key before it is sealed for: a key
    /// the owner has not confirmed on this iPhone or that changed, for somebody the item has no key for yet.
    var needsConfirmation: Bool {
        !hasKey && publicKey != nil && (keyTrust == .new || keyTrust == .changed)
    }
}

/// PasswordsSafetyCodeLines shows one safety code under a caption.
private struct PasswordsSafetyCodeLines: View {
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

/// PasswordsRecipientRow is one person an item reaches: their name and the
/// safety code of their key, or that they are waiting to set up Passwords. A
/// key that changed shows the code it had and the code it has, and a key to
/// confirm has a button to say the code was compared.
struct PasswordsRecipientRow: View {
    let recipient: PasswordRecipient
    var isConfirmed = false
    /// Called by the confirm button, which only a key to confirm has.
    var toggleConfirmed: () -> Void = {}

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            PasswordsValueText(PersonWords.pickerLabel(name: recipient.name, username: recipient.username), isProse: true)
            if recipient.safetyCode == nil {
                Text("Waiting until they set up Passwords")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            } else {
                PasswordsRecipientCodes(recipient: recipient)
            }
            if recipient.needsConfirmation {
                Text("Ask \(recipient.name) to open Passwords settings on their own device and read you their safety code. Confirm only if it matches.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                confirmButton
            }
        }
        .accessibilityElement(children: .contain)
    }

    private var confirmButton: some View {
        Button(action: toggleConfirmed) {
            Label(confirmTitle, systemImage: isConfirmed ? "checkmark.circle.fill" : "checkmark.shield")
        }
        .buttonStyle(.borderless)
        .actionColor()
    }

    private var confirmTitle: LocalizedStringKey {
        if isConfirmed { return "Confirmed" }
        return recipient.keyTrust == .changed ? "Trust this key" : "Confirm this code"
    }
}

/// PasswordsWaitingConfirmationRow is a person whose key this iPhone has not
/// seen (or that changed) and who is waiting for an item of the owner's: it
/// shows the item, the codes and a button that seals the key after the owner
/// compared the code with the person.
struct PasswordsWaitingConfirmationRow: View {
    let waiting: PasswordWaitingShare
    let confirm: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            PasswordsValueText(PersonWords.pickerLabel(name: waiting.recipient.name, username: waiting.recipient.username), isProse: true)
            if let itemName = waiting.itemName {
                PasswordsValueText(itemName, isProse: true)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            PasswordsRecipientCodes(recipient: waiting.recipient)
            Text("Ask \(waiting.recipient.name) to open Passwords settings on their own device and read you their safety code. Confirm only if it matches.")
                .font(.footnote)
                .foregroundStyle(.secondary)
            Button(action: confirm) {
                Label(confirmTitle, systemImage: "checkmark.shield")
            }
            .buttonStyle(.borderless)
            .actionColor()
        }
        .accessibilityElement(children: .contain)
    }

    private var confirmTitle: LocalizedStringKey {
        waiting.recipient.keyTrust == .changed ? "Trust this key and share" : "Confirm this code and share"
    }
}

/// PasswordsRecipientCodes is the safety code of a recipient's key, and for a
/// key that changed the code it had before as well.
private struct PasswordsRecipientCodes: View {
    let recipient: PasswordRecipient

    var body: some View {
        if let safetyCode = recipient.safetyCode {
            if recipient.keyTrust == .changed, !recipient.hasKey {
                Text("Their key changed")
                    .font(.footnote.weight(.semibold))
                if let knownSafetyCode = recipient.knownSafetyCode {
                    PasswordsSafetyCodeLines(caption: "Safety code before", code: knownSafetyCode)
                }
                PasswordsSafetyCodeLines(caption: "Safety code now", code: safetyCode)
            } else {
                PasswordsSafetyCodeLines(caption: "Safety code", code: safetyCode)
            }
        }
    }
}

/// PasswordsOwnerKeyChangeRow explains that the person who shared items
/// changed their key, with both safety codes and a button to trust the new
/// key once the codes were compared with them.
struct PasswordsOwnerKeyChangeRow: View {
    let change: PasswordsOwnerKeyChange
    let trust: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("\(change.ownerName)'s key changed")
                .font(.footnote.weight(.semibold))
            Text("Their shared items stay closed until you trust the new key. Trusting it also lets you share items with them.")
                .font(.footnote)
                .foregroundStyle(.secondary)
            Text("Ask \(change.ownerName) to open Passwords settings on their own device and read you their safety code. Confirm only if it matches.")
                .font(.footnote)
                .foregroundStyle(.secondary)
            PasswordsSafetyCodeLines(caption: "Safety code before", code: change.oldSafetyCode)
            PasswordsSafetyCodeLines(caption: "Safety code now", code: change.newSafetyCode)
            Button(action: trust) {
                Label("Trust this key", systemImage: "checkmark.shield")
            }
            .buttonStyle(.borderless)
            .actionColor()
        }
        .accessibilityElement(children: .contain)
    }
}
