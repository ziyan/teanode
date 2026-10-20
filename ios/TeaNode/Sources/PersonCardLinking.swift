import SwiftUI
import TeaNodeKit

/// PersonContactPicker chooses which of the person's contacts is somebody in
/// their circles, from the card of that somebody: the other way round from the
/// contact screen, which chooses the person for a contact. The contacts the
/// server thinks are them come first, then a search of the address book. The
/// link is the person's alone; the account is not told.
struct PersonContactPicker: View {
    let contacts: ContactsModel
    let person: CircleMember
    let onLinked: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var suggestions: [ContactAccountSuggestion] = []
    @State private var found: [ContactSummary] = []
    @State private var hasReadContacts = false
    @State private var searchText = ""
    @State private var isLinking = false
    /// Why the link was not made, or the contacts not read: shown in the
    /// sheet, since the toast of the screen under it cannot be seen.
    @State private var failureText: String?

    /// The contacts the server offered for this account, which are not
    /// repeated among the search results.
    private var offered: [ContactSummary] {
        suggestions.filter { $0.account.userId == person.userId }.map(\.contact)
    }

    private var foundOthers: [ContactSummary] {
        let offeredIds = Set(offered.map(\.id))
        return found.filter { !offeredIds.contains($0.id) }
    }

    var body: some View {
        NavigationStack {
            List {
                if let failureText {
                    Section {
                        Label(failureText, systemImage: "exclamationmark.triangle")
                            .foregroundStyle(.red)
                    }
                }
                if searchText.isEmpty, !offered.isEmpty {
                    Section {
                        ForEach(offered) { contact in
                            row(contact)
                        }
                    } header: {
                        Text("Contacts that may be people in your circles")
                    }
                }
                Section {
                    ForEach(foundOthers) { contact in
                        row(contact)
                    }
                    if hasReadContacts, found.isEmpty {
                        Text("No contacts yet")
                            .foregroundStyle(.secondary)
                    }
                } footer: {
                    Text("Choose which contact is \(person.displayName). Only you see this.")
                }
            }
            .navigationTitle("Link a contact")
            .navigationBarTitleDisplayMode(.inline)
            .searchable(text: $searchText, placement: .navigationBarDrawer(displayMode: .always), prompt: "Name, address or phone")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
            .disabled(isLinking)
            .task { await readSuggestions() }
            .task(id: searchText) {
                // A pause in typing is waited for; the first read is at once.
                if !searchText.isEmpty { try? await Task.sleep(for: .milliseconds(250)) }
                guard !Task.isCancelled else { return }
                await readContacts()
            }
        }
    }

    private func row(_ contact: ContactSummary) -> some View {
        Button {
            choose(contact)
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(contact.shownName.isEmpty ? String(localized: "No name") : contact.shownName)
                    .foregroundStyle(.primary)
                if let detail = [contact.organization, contact.emails?.first, contact.phones?.first]
                    .compactMap({ $0 })
                    .first(where: { !$0.isEmpty && $0 != contact.shownName })
                {
                    Text(detail)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(.rect)
        }
    }

    private func readSuggestions() async {
        suggestions = (try? await contacts.connection.contactAccountSuggestions()) ?? []
    }

    private func readContacts() async {
        guard let addressBookId = await readAddressBookId() else { return }
        do {
            found = try await contacts.connection.contacts(addressBookId: addressBookId, matching: searchText.trimmed, first: 50)
            hasReadContacts = true
            failureText = nil
        } catch {
            guard !Task.isCancelled else { return }
            failureText = error.localizedDescription.asReasonSentence
        }
    }

    /// The address book the contacts are in: the one the card read, else read
    /// now.
    private func readAddressBookId() async -> String? {
        if let addressBook = contacts.addressBook { return addressBook.id }
        await contacts.loadAddressBook()
        return contacts.addressBook?.id
    }

    private func choose(_ contact: ContactSummary) {
        isLinking = true
        Task {
            let suggestedBookId = suggestions.first { $0.contact.id == contact.id }?.addressBookId
            let account = ChatPerson(
                userId: person.userId, username: person.username, name: person.name, avatarUrl: person.avatarUrl,
                avatarColorIndex: person.avatarColorIndex)
            let isLinked = await contacts.link(
                contactId: contact.id, to: account, contactName: contact.shownName, addressBookId: suggestedBookId ?? contacts.addressBook?.id,
                isQuiet: true)
            if isLinked {
                onLinked()
                dismiss()
            } else {
                failureText = String(localized: "That could not be linked. Another contact may already be them.")
            }
            isLinking = false
        }
    }
}
