import SwiftUI
import TeaNodeKit

/// ContactsModel is the person's address book, as the dashboard's Contacts
/// page shows it: searched as they type, and which contact is them.
@MainActor
@Observable
final class ContactsModel {
    private(set) var addressBook: AddressBook?
    private(set) var contacts: [ContactSummary] = []
    private(set) var myContactId: String?
    private(set) var isLoaded = false
    var searchText = ""
    var doneText: String?
    var problemText: String?
    /// Contacts that look like people in the person's circles, as the server
    /// last offered them, for the person to confirm.
    private(set) var suggestions: [ContactAccountSuggestion] = []
    /// The contacts whose suggestion the person answered "Not now", kept on
    /// this phone only: the server never hears of it.
    private(set) var dismissedSuggestionContactIds: Set<String>

    let connection: Connection
    private let dismissedSuggestionsKey: String

    init(connection: Connection) {
        self.connection = connection
        let key = "contact-account-suggestions-dismissed.\(connection.accountKey)"
        dismissedSuggestionsKey = key
        dismissedSuggestionContactIds = Set(UserDefaults.standard.stringArray(forKey: key) ?? [])
    }

    /// The suggestions the person has not put off.
    var shownSuggestions: [ContactAccountSuggestion] {
        suggestions.filter { !dismissedSuggestionContactIds.contains($0.contact.id) }
    }

    /// Reads the suggestions. Quietly: a server that does not offer them, or
    /// a person without circles, simply has none.
    func loadSuggestions() async {
        suggestions = (try? await connection.contactAccountSuggestions()) ?? []
    }

    func dismissSuggestion(of contactId: String) {
        dismissedSuggestionContactIds.insert(contactId)
        UserDefaults.standard.set(Array(dismissedSuggestionContactIds), forKey: dismissedSuggestionsKey)
    }

    /// Says the contact is the account; false when the server refused, with
    /// the reason in `problemText` unless `isQuiet`.
    /// `addressBookId` is the book the contact is in; a suggestion carries it,
    /// otherwise it is the book on this screen.
    func link(contactId: String, to account: ChatPerson, contactName: String, addressBookId: String? = nil, isQuiet: Bool = false) async -> Bool {
        guard let bookId = addressBookId ?? addressBook?.id else { return false }
        do {
            _ = try await connection.setContactAccount(addressBookId: bookId, contactId: contactId, accountUserId: account.userId)
            suggestions.removeAll { $0.contact.id == contactId }
            doneText = String(localized: "\(contactName) is in your circles as \(account.displayName)")
            return true
        } catch {
            if !isQuiet { problemText = error.localizedDescription }
            return false
        }
    }

    func unlink(contactId: String) async -> Bool {
        guard let addressBook else { return false }
        do {
            _ = try await connection.setContactAccount(addressBookId: addressBook.id, contactId: contactId, accountUserId: nil)
            doneText = String(localized: "Removed the link to your circles")
            await loadSuggestions()
            return true
        } catch {
            problemText = error.localizedDescription
            return false
        }
    }

    func load() async {
        do {
            if addressBook == nil {
                addressBook = try await connection.addressBooks().first
                myContactId = try? await connection.myContactId()
            }
            guard let addressBook else {
                isLoaded = true
                return
            }
            let words = searchText.trimmingCharacters(in: .whitespaces)
            contacts = try await connection.contacts(addressBookId: addressBook.id, matching: words, first: 500)
            isLoaded = true
            problemText = nil
        } catch {
            problemText = error.localizedDescription
        }
    }

    func setMine(_ contactId: String?) async {
        do {
            try await connection.setMyContact(contactId)
            myContactId = contactId
            doneText = contactId == nil ? String(localized: "No contact is you now") : String(localized: "Marked as you")
        } catch {
            problemText = error.localizedDescription
        }
    }

    func removePicture(of contactId: String) async -> Bool {
        do {
            try await connection.removeContactPicture(contactId)
            doneText = String(localized: "Removed their picture")
            await load()
            return true
        } catch {
            problemText = error.localizedDescription
            return false
        }
    }

    func delete(_ contactId: String) async -> Bool {
        do {
            try await connection.deleteContact(contactId)
            doneText = String(localized: "Contact removed")
            await load()
            return true
        } catch {
            problemText = error.localizedDescription
            return false
        }
    }
}

/// ContactsScreen is the address book: search, the contacts with their
/// pictures, and a new contact.
struct ContactsScreen: View {
    @State private var model: ContactsModel
    @State private var isAdding = false
    @State private var isImportingFromPhone = false
    @State private var path = NavigationPath()
    @Environment(AppModel.self) private var app

    init(connection: Connection) {
        _model = State(initialValue: ContactsModel(connection: connection))
    }

    var body: some View {
        NavigationStack(path: $path) {
            // Laid out as the mail and recordings lists are, under a letter
            // instead of a period: no chevrons, no lines, rows close together.
            List {
                if model.searchText.isEmpty, !model.shownSuggestions.isEmpty {
                    ListPeriodHeader(title: String(localized: "Contacts that may be people in your circles"))
                        .listRowInsets(EdgeInsets(top: 12, leading: 16, bottom: 2, trailing: 16))
                        .listRowSeparator(.hidden)
                        .accessibilityAddTraits(.isHeader)
                    ForEach(model.shownSuggestions) { suggestion in
                        ContactSuggestionRows(model: model, suggestion: suggestion)
                    }
                }
                // Each letter is a row of its own rather than a section header: a plain list puts a fixed band
                // above every section, which left a letter with one contact mostly empty space.
                ForEach(ContactLetterSection.sections(of: model.contacts)) { section in
                    ListPeriodHeader(title: section.letter)
                        .listRowInsets(EdgeInsets(top: 12, leading: 16, bottom: 2, trailing: 16))
                        .listRowSeparator(.hidden)
                        .accessibilityAddTraits(.isHeader)
                    ForEach(section.contacts) { contact in
                        NavigationLink(value: contact.id) {
                            ContactRow(contact: contact, isMine: contact.id == model.myContactId)
                        }
                        .navigationLinkIndicatorVisibility(.hidden)
                        .listRowInsets(ListRowSpacing.insets)
                        .listRowSeparator(.hidden)
                    }
                }
            }
            .listStyle(.plain)
            .navigationTitle("Contacts")
            .navigationDestination(for: String.self) { contactId in
                ContactDetailView(model: model, contactId: contactId)
            }
            .searchable(text: $model.searchText, prompt: "Name, address or phone")
            .task(id: model.searchText) {
                // Searched as the person types, a moment after they stop.
                if !model.searchText.isEmpty {
                    try? await Task.sleep(for: .milliseconds(250))
                    guard !Task.isCancelled else { return }
                }
                await model.load()
            }
            .task { await model.loadSuggestions() }
            .refreshable {
                await model.load()
                await model.loadSuggestions()
            }
            .overlay {
                if model.isLoaded, model.contacts.isEmpty {
                    if model.searchText.isEmpty {
                        ContentUnavailableView("No contacts yet", systemImage: "person.crop.circle", description: Text("Add one with +, or save a sender from a message."))
                    } else {
                        ContentUnavailableView.search(text: model.searchText)
                    }
                } else if !model.isLoaded {
                    ProgressView()
                }
            }
            .toolbar {
                MoreBackButton()
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        isAdding = true
                    } label: {
                        Label("New contact", systemImage: "plus")
                    }
                    .disabled(model.addressBook == nil)
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Menu {
                        Button {
                            isImportingFromPhone = true
                        } label: {
                            Label("Import from iPhone", systemImage: "square.and.arrow.down")
                        }
                    } label: {
                        Label("More", systemImage: "ellipsis")
                    }
                    .disabled(model.addressBook == nil)
                }
            }
        }
        // A card a link from the agent's memory names opens as if its row
        // had been chosen.
        .onChange(of: app.openedContactId, initial: true) { _, contactId in
            guard let contactId else { return }
            app.openedContactId = nil
            path = NavigationPath()
            path.append(contactId)
        }
        // The words the Search tab's Show all in Contacts carries.
        .onChange(of: app.openedPartSearch, initial: true) { _, search in
            guard let search, search.area == .contacts else { return }
            app.openedPartSearch = nil
            path = NavigationPath()
            model.searchText = search.words
        }
        .sheet(isPresented: $isAdding) {
            ContactEditor(model: model, contact: nil) { _ in }
        }
        .sheet(isPresented: $isImportingFromPhone) {
            if let addressBook = model.addressBook {
                ContactImportView(contacts: model, addressBookId: addressBook.id)
            }
        }
        .toast(done: $model.doneText, problem: $model.problemText)
    }
}

/// ContactSuggestionRows is one contact the server thinks is somebody in the
/// person's circles: who, and two rows to answer it. Link says so; Not now
/// puts it away on this phone.
struct ContactSuggestionRows: View {
    let model: ContactsModel
    let suggestion: ContactAccountSuggestion

    var body: some View {
        HStack(spacing: 12) {
            AvatarView(
                name: suggestion.account.displayName, avatarUrl: suggestion.account.avatarUrl,
                colorIndex: suggestion.account.avatarColorIndex, size: 40)
            VStack(alignment: .leading, spacing: 2) {
                Text(suggestion.contact.shownName.isEmpty ? String(localized: "No name") : suggestion.contact.shownName)
                    .font(.headline)
                    .lineLimit(2)
                Text("\(suggestion.contact.shownName.isEmpty ? String(localized: "No name") : suggestion.contact.shownName) may be \(suggestion.account.displayName) in your circles")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        }
        .listRowSeparator(.hidden)
        .accessibilityElement(children: .combine)
        Button {
            Task { _ = await model.link(contactId: suggestion.contact.id, to: suggestion.account, contactName: suggestion.contact.shownName, addressBookId: suggestion.addressBookId) }
        } label: {
            Label("Link", systemImage: "link")
                .actionColor()
        }
        .listRowSeparator(.hidden)
        Button {
            model.dismissSuggestion(of: suggestion.contact.id)
        } label: {
            Label("Not now", systemImage: "clock")
                .actionColor()
        }
    }
}

/// ContactRow is a contact in the list: picture, name and what else
/// tells them apart.
struct ContactRow: View {
    let contact: ContactSummary
    let isMine: Bool

    var body: some View {
        HStack(spacing: 12) {
            ContactPicture(contactId: contact.id, name: contact.shownName, hasPhoto: contact.hasPhoto == true, size: 40)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(contact.shownName.isEmpty ? String(localized: "No name") : contact.shownName)
                        .font(.headline)
                        .lineLimit(1)
                    if isMine {
                        Text("You")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 1)
                            .background(Color(.tertiarySystemFill), in: .capsule)
                    }
                }
                if let detail = [contact.organization, contact.emails?.first, contact.phones?.first]
                    .compactMap({ $0 })
                    .first(where: { !$0.isEmpty && $0 != contact.shownName })
                {
                    Text(detail)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
        }
    }
}

/// ContactLetterSection is the contacts under one letter, as an address book
/// groups them: sorted by name, a section for each first letter, and # for a
/// name that starts with neither a letter nor a digit.
struct ContactLetterSection: Identifiable {
    let letter: String
    let contacts: [ContactSummary]
    var id: String { letter }

    static func sections(of contacts: [ContactSummary]) -> [ContactLetterSection] {
        let sorted = contacts.sorted { $0.shownName.localizedStandardCompare($1.shownName) == .orderedAscending }
        var sections: [ContactLetterSection] = []
        for contact in sorted {
            let monogram = Monogram.letter(of: contact.shownName)
            let letter = monogram == "?" ? "#" : monogram
            if let last = sections.last, last.letter == letter {
                sections[sections.count - 1] = ContactLetterSection(letter: letter, contacts: last.contacts + [contact])
            } else {
                sections.append(ContactLetterSection(letter: letter, contacts: [contact]))
            }
        }
        return sections
    }
}

/// ContactPicture is a contact's photo, or the first letter of their name
/// on a color worked out from it, as the dashboard draws them.
struct ContactPicture: View {
    let contactId: String
    let name: String
    let hasPhoto: Bool
    let size: CGFloat
    @Environment(AppModel.self) private var app
    @State private var photo: UIImage?

    var body: some View {
        Group {
            if let photo {
                Image(uiImage: photo)
                    .resizable()
                    .scaledToFill()
            } else {
                Text(Monogram.letter(of: name))
                    .font(size > 60 ? .largeTitle : .headline)
                    .foregroundStyle(.white)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(Monogram.color(of: name))
            }
        }
        .frame(width: size, height: size)
        .clipShape(.circle)
        .accessibilityHidden(true)
        .task(id: contactId) {
            guard hasPhoto else { return }
            photo = await app.picture("/api/v1/contacts/\(contactId)/photo")
        }
    }
}

enum Monogram {
    /// The first letter or digit of a name.
    static func letter(of name: String) -> String {
        name.first(where: { $0.isLetter || $0.isNumber }).map { String($0).uppercased() } ?? "?"
    }

    /// A color worked out from the name, the same name always the same.
    static func color(of name: String) -> Color {
        var hash = 0
        for scalar in name.unicodeScalars {
            hash = (hash * 31 + Int(scalar.value)) % 360
        }
        return Color(hue: Double(hash) / 360, saturation: 0.55, brightness: 0.62)
    }
}

/// ContactDetailView is one contact whole: how to reach them, where they
/// are, notes, and the ways to change them.
struct ContactDetailView: View {
    let model: ContactsModel
    let contactId: String
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL
    @State private var contact: ContactDetail?
    @State private var problemText: String?
    @State private var isEditing = false
    @State private var isConfirmingDelete = false
    @State private var isConfirmingRemovePicture = false
    @State private var isChoosingAccount = false
    @State private var memory = InMemoryModel()

    var body: some View {
        Group {
            if let contact {
                List {
                    Section {
                        VStack(spacing: 8) {
                            ContactPicture(contactId: contact.id, name: contact.name ?? "", hasPhoto: contact.hasPhoto == true, size: 96)
                            Text(contact.name.flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "No name"))
                                .font(.title2.weight(.semibold))
                                .multilineTextAlignment(.center)
                            if let organization = contact.organization, !organization.isEmpty {
                                Text(organization).foregroundStyle(.secondary)
                            }
                            if contact.id == model.myContactId {
                                Label("This is you", systemImage: "person.fill.checkmark")
                                    .font(.footnote)
                                    .foregroundStyle(.secondary)
                            }
                        }
                        .frame(maxWidth: .infinity)
                        .listRowBackground(Color.clear)
                    }
                    accountSection(of: contact)
                    if let emails = contact.emails, !emails.isEmpty {
                        Section("Email") {
                            ForEach(emails, id: \.self) { email in
                                // Written from TeaNode's own mail, as the
                                // dashboard would.
                                Button {
                                    app.composingTo = email
                                } label: {
                                    Label(email, systemImage: "envelope")
                                        .actionColor()
                                }
                                .contextMenu {
                                    Button("Copy", systemImage: "doc.on.doc") { UIPasteboard.general.string = email }
                                }
                            }
                        }
                    }
                    if let phones = contact.phones, !phones.isEmpty {
                        Section("Phone") {
                            ForEach(phones, id: \.self) { phone in
                                ReachRow(
                                    text: phone, systemImage: "phone",
                                    address: URL(string: "tel:\(phone.filter { $0.isNumber || $0 == "+" })"))
                            }
                        }
                    }
                    if let addresses = contact.addresses, !addresses.isEmpty {
                        Section("Address") {
                            ForEach(Array(addresses.enumerated()), id: \.offset) { _, address in
                                let written = ContactWords.written(address)
                                ReachRow(
                                    text: written, systemImage: "mappin.and.ellipse",
                                    address: written.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed)
                                        .flatMap { URL(string: "maps://?q=\($0)") })
                            }
                        }
                    }
                    if let note = contact.note, !note.isEmpty {
                        Section("Notes") {
                            Text(note).textSelection(.enabled)
                        }
                    }
                    InMemorySection(model: memory)
                    Section {
                        if contact.id == model.myContactId {
                            Button("This is not me") {
                                Task { await model.setMine(nil) }
                            }
                        } else {
                            Button("This is me") {
                                Task { await model.setMine(contact.id) }
                            }
                        }
                        if contact.hasPhoto == true {
                            Button("Remove picture", role: .destructive) {
                                isConfirmingRemovePicture = true
                            }
                            .confirmationDialog("Remove the picture of \(contact.name)?", isPresented: $isConfirmingRemovePicture, titleVisibility: .visible) {
                                Button("Remove picture", role: .destructive) {
                                    Task {
                                        if await model.removePicture(of: contactId) { await read() }
                                    }
                                }
                            } message: {
                                Text("Devices synchronizing your address book lose it too.")
                            }
                        }
                        Button("Delete contact", role: .destructive) {
                            isConfirmingDelete = true
                        }
                        .confirmationDialog("Delete \(contact.name)?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
                            Button("Delete contact", role: .destructive) {
                                Task {
                                    if await model.delete(contactId) { dismiss() }
                                }
                            }
                        } message: {
                            Text("It is removed from your address book and from every device that reads it.")
                        }
                    } footer: {
                        Text("Your agent knows the contact marked as you is you.")
                    }
                }
            } else if let problemText {
                ContentUnavailableView("That contact could not be read", systemImage: "person.crop.circle.badge.exclamationmark", description: Text(problemText.asReasonSentence))
            } else {
                ProgressView()
            }
        }
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button("Edit", systemImage: "pencil") { isEditing = true }
                    .disabled(contact == nil)
            }
        }
        .sheet(isPresented: $isEditing) {
            if let contact {
                ContactEditor(model: model, contact: contact) { _ in
                    Task { await read() }
                }
            }
        }
        .sheet(isPresented: $isChoosingAccount) {
            if let contact {
                ContactAccountPicker(model: model, contact: contact) {
                    Task { await read() }
                }
            }
        }
        .inMemory(memory, recordKind: "contact", recordId: contactId)
        .task { await read() }
    }

    /// Which account in the person's circles this contact is, or the way to
    /// say so. Only the person sees it: the account is not told.
    @ViewBuilder
    private func accountSection(of contact: ContactDetail) -> some View {
        Section {
            if let account = contact.account, contact.isLinkedToAccount {
                HStack(spacing: 12) {
                    AvatarView(name: account.displayName, avatarUrl: account.avatarUrl, colorIndex: account.avatarColorIndex, size: 36)
                    Text("In your circles as \(account.displayName)")
                }
                .accessibilityElement(children: .combine)
                Button {
                    isChoosingAccount = true
                } label: {
                    Label("Change", systemImage: "person.2")
                        .actionColor()
                }
                Button(role: .destructive) {
                    Task {
                        if await model.unlink(contactId: contactId) { await read() }
                    }
                } label: {
                    Label("Remove", systemImage: "person.crop.circle.badge.minus")
                }
                .tint(.red)
                .actionColor(isDestructive: true)
            } else {
                Button {
                    isChoosingAccount = true
                } label: {
                    Label("Is in your circles as…", systemImage: "person.2")
                        .actionColor()
                }
            }
        } footer: {
            Text("Only you see this. They are not told.")
        }
    }

    private func read() async {
        do {
            contact = try await model.connection.contact(contactId)
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// ReachRow is an address, a number or a place, opened on tap and copied
/// on a long press.
struct ReachRow: View {
    let text: String
    let systemImage: String
    let address: URL?
    @Environment(\.openURL) private var openURL

    var body: some View {
        Button {
            if let address { openURL(address) }
        } label: {
            Label(text, systemImage: systemImage)
                .actionColor()
        }
        .contextMenu {
            Button("Copy", systemImage: "doc.on.doc") {
                UIPasteboard.general.string = text
            }
        }
    }
}

enum ContactWords {
    /// An address as it is written on an envelope.
    static func written(_ address: ContactAddress) -> String {
        let cityLine = [address.locality, address.region, address.postalCode].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " ")
        return [address.street, cityLine, address.country].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: "\n")
    }
}

/// ContactAccountPicker chooses which of the people the person shares a
/// circle with a contact is. It makes no contact and changes nothing on the
/// card; the server keeps the link apart from the card's fields.
struct ContactAccountPicker: View {
    let model: ContactsModel
    let contact: ContactDetail
    let onLinked: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var people: [ChatPerson] = []
    @State private var hasReadPeople = false
    /// The accounts another contact of this book already is: a book keeps one
    /// contact for each person, so they are not offered.
    @State private var takenAccountIds: Set<String> = []
    @State private var searchText = ""
    @State private var isLinking = false
    /// Why the link was not made, or the people not read: shown in the
    /// sheet, since the toast of the screen under it cannot be seen.
    @State private var failureText: String?

    private var shownPeople: [ChatPerson] {
        let words = searchText.trimmed
        let available = people.filter { !takenAccountIds.contains($0.userId) }
        guard !words.isEmpty else { return available }
        return available.filter { $0.displayName.localizedStandardContains(words) || $0.username.localizedStandardContains(words) }
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
                Section {
                    ForEach(shownPeople) { person in
                        Button {
                            choose(person)
                        } label: {
                            HStack(spacing: 12) {
                                AvatarView(name: person.displayName, avatarUrl: person.avatarUrl, colorIndex: person.avatarColorIndex, size: 36)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(verbatim: person.displayName)
                                        .foregroundStyle(.primary)
                                    if person.displayName != person.username {
                                        Text(verbatim: person.username)
                                            .font(.footnote)
                                            .foregroundStyle(.secondary)
                                    }
                                }
                                Spacer()
                                if person.userId == contact.accountUserId {
                                    Image(systemName: "checkmark")
                                        .foregroundStyle(.tint)
                                        .accessibilityLabel("Current choice")
                                }
                            }
                        }
                    }
                    if hasReadPeople, people.isEmpty {
                        Text("You do not share a circle with anybody yet.")
                            .foregroundStyle(.secondary)
                    }
                } footer: {
                    Text("Choose who \(contact.name ?? "") is among the people you share a circle with. Only you see this.")
                }
            }
            .navigationTitle("Is in your circles as…")
            .navigationBarTitleDisplayMode(.inline)
            .searchable(text: $searchText, placement: .navigationBarDrawer(displayMode: .automatic), prompt: "Search people")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
            .disabled(isLinking)
            .task {
                do {
                    people = try await model.connection.chatPeople()
                    // A failure here only costs the filter: the server still refuses a second link.
                    if let bookId = model.addressBook?.id,
                        let bookContacts = try? await model.connection.contacts(addressBookId: bookId, first: 500)
                    {
                        takenAccountIds = Set(
                            bookContacts.filter { $0.id != contact.id }.compactMap(\.accountUserId).filter { !$0.isEmpty })
                    }
                    hasReadPeople = true
                } catch {
                    failureText = error.localizedDescription.asReasonSentence
                }
            }
        }
    }

    private func choose(_ person: ChatPerson) {
        isLinking = true
        Task {
            if await model.link(contactId: contact.id, to: person, contactName: contact.name ?? "", isQuiet: true) {
                onLinked()
                dismiss()
            } else {
                failureText = String(localized: "That could not be linked. Another contact may already be them.")
            }
            isLinking = false
        }
    }
}

/// ContactEditor adds a contact or changes one: every field is written
/// back, the addresses after the first kept as they were. The link to an
/// account in the person's circles is not part of the card and is left alone.
struct ContactEditor: View {
    let model: ContactsModel
    let contact: ContactDetail?
    let onSaved: (Bool) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var organization = ""
    @State private var emails: [String] = []
    @State private var phones: [String] = []
    @State private var address = ContactAddress()
    @State private var note = ""
    @State private var isSaving = false
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Name", text: $name)
                        .textContentType(.name)
                    TextField("Organization", text: $organization)
                        .textContentType(.organizationName)
                }
                Section("Email") {
                    ForEach(emails.indices, id: \.self) { index in
                        TextField("Email", text: $emails[index])
                            .keyboardType(.emailAddress)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                    }
                    .onDelete { emails.remove(atOffsets: $0) }
                    Button("Add email", systemImage: "plus.circle") { emails.append("") }
                        .actionColor()
                }
                Section("Phone") {
                    ForEach(phones.indices, id: \.self) { index in
                        TextField("Phone", text: $phones[index])
                            .keyboardType(.phonePad)
                    }
                    .onDelete { phones.remove(atOffsets: $0) }
                    Button("Add phone", systemImage: "plus.circle") { phones.append("") }
                        .actionColor()
                }
                Section("Address") {
                    TextField("Street", text: Binding($address.street, default: ""), axis: .vertical)
                    TextField("City", text: Binding($address.locality, default: ""))
                    TextField("State or region", text: Binding($address.region, default: ""))
                    TextField("Postal code", text: Binding($address.postalCode, default: ""))
                    TextField("Country", text: Binding($address.country, default: ""))
                }
                Section("Notes") {
                    TextField("Notes", text: $note, axis: .vertical)
                        .lineLimit(3...)
                }
                if let problemText {
                    Section {
                        Label(problemText.asReasonSentence, systemImage: "exclamationmark.triangle")
                            .foregroundStyle(.red)
                    }
                }
            }
            .navigationTitle(contact == nil ? String(localized: "New contact") : String(localized: "Edit contact"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save() }
                        .disabled(isSaving || (name.trimmed.isEmpty && emails.allSatisfy(\.trimmed.isEmpty)))
                }
            }
            .interactiveDismissDisabled(isSaving)
            .onAppear(perform: fill)
        }
    }

    private func fill() {
        guard let contact else { return }
        name = contact.name ?? ""
        organization = contact.organization ?? ""
        emails = contact.emails ?? []
        phones = contact.phones ?? []
        address = contact.addresses?.first ?? ContactAddress()
        note = contact.note ?? ""
    }

    private func save() {
        guard let addressBook = model.addressBook else { return }
        let kept = Array((contact?.addresses ?? []).dropFirst())
        let first = ContactWords.written(address).isEmpty ? [] : [address]
        isSaving = true
        Task {
            do {
                try await model.connection.saveContact(
                    addressBookId: addressBook.id, id: contact?.id, name: name.trimmed, organization: organization.trimmed,
                    emails: emails.map(\.trimmed).filter { !$0.isEmpty }, phones: phones.map(\.trimmed).filter { !$0.isEmpty },
                    note: note.trimmed, addresses: first + kept)
                model.doneText = String(localized: "Contact saved")
                await model.load()
                onSaved(true)
                dismiss()
            } catch {
                problemText = error.localizedDescription
            }
            isSaving = false
        }
    }
}

extension String {
    var trimmed: String { trimmingCharacters(in: .whitespacesAndNewlines) }
}

extension Binding where Value == String {
    /// A text field's binding to an optional string, empty standing for nil.
    init(_ optional: Binding<String?>, default empty: String) {
        self.init(
            get: { optional.wrappedValue ?? empty },
            set: { optional.wrappedValue = $0.isEmpty ? nil : $0 })
    }
}
