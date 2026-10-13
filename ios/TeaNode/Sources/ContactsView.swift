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

    let connection: Connection

    init(connection: Connection) {
        self.connection = connection
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

    init(connection: Connection) {
        _model = State(initialValue: ContactsModel(connection: connection))
    }

    var body: some View {
        NavigationStack {
            // Laid out as the mail and recordings lists are, under a letter
            // instead of a period: no chevrons, no lines, rows close together.
            List {
                ForEach(ContactLetterSection.sections(of: model.contacts)) { section in
                    Section {
                        ForEach(section.contacts) { contact in
                            NavigationLink(value: contact.id) {
                                ContactRow(contact: contact, isMine: contact.id == model.myContactId)
                            }
                            .navigationLinkIndicatorVisibility(.hidden)
                            .listRowInsets(ListRowSpacing.insets)
                            .listRowSeparator(.hidden)
                        }
                    } header: {
                        ListPeriodHeader(title: section.letter)
                    }
                    .listSectionSeparator(.hidden)
                }
            }
            .listStyle(.plain)
            .navigationTitle("Contacts")
            .withoutSwipeBack()
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
            .refreshable { await model.load() }
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
                MenuButton()
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
                    if let emails = contact.emails, !emails.isEmpty {
                        Section("Email") {
                            ForEach(emails, id: \.self) { email in
                                // Written from TeaNode's own mail, as the
                                // dashboard would.
                                Button {
                                    app.composingTo = email
                                } label: {
                                    Label(email, systemImage: "envelope")
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
                        Button("Delete contact", role: .destructive) {
                            isConfirmingDelete = true
                        }
                    } footer: {
                        Text("Your agent knows the contact marked as you is you.")
                    }
                }
            } else if let problemText {
                ContentUnavailableView("That contact could not be read", systemImage: "person.crop.circle.badge.exclamationmark", description: Text(problemText))
            } else {
                ProgressView()
            }
        }
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button("Edit") { isEditing = true }
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
        .confirmationDialog("Delete \(contact?.name ?? "")?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
            Button("Delete contact", role: .destructive) {
                Task {
                    if await model.delete(contactId) { dismiss() }
                }
            }
        } message: {
            Text("It is removed from your address book and from every device that reads it.")
        }
        .task { await read() }
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

/// ContactEditor adds a contact or changes one: every field is written
/// back, the addresses after the first kept as they were.
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
                }
                Section("Phone") {
                    ForEach(phones.indices, id: \.self) { index in
                        TextField("Phone", text: $phones[index])
                            .keyboardType(.phonePad)
                    }
                    .onDelete { phones.remove(atOffsets: $0) }
                    Button("Add phone", systemImage: "plus.circle") { phones.append("") }
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
                        Label(problemText, systemImage: "exclamationmark.triangle")
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
