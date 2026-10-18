import Contacts
import SwiftUI
import TeaNodeKit

/// PhoneContact is one contact on the iPhone, read once and kept as plain
/// values: the Contacts framework's own contacts are not safe to hand from
/// the thread that read them to the screen.
struct PhoneContact: Identifiable, Sendable {
    let id: String
    /// What the list calls them; empty when the contact has neither a name
    /// nor an organization.
    let name: String
    /// Their first email address or, failing that, number.
    let firstAddressText: String
    let identity: ContactIdentity
    /// The vCard that is sent, without a picture; nil when the phone could
    /// not write one.
    let card: String?
}

/// PhoneContacts reads the iPhone's contacts.
enum PhoneContacts {
    /// Whether the person lets TeaNode read their contacts, asking if they
    /// have not been asked. Limited access, where the person picked which
    /// contacts to share, reads as access: the ones shared are all there are.
    static func hasAccess() async -> Bool {
        switch CNContactStore.authorizationStatus(for: .contacts) {
        case .authorized, .limited:
            return true
        case .notDetermined:
            return (try? await CNContactStore().requestAccess(for: .contacts)) ?? false
        case .denied, .restricted:
            return false
        @unknown default:
            return false
        }
    }

    /// Every contact, sorted by name. The framework blocks while it reads,
    /// and a large address book takes long enough to be seen, so it is not
    /// read on the main thread.
    static func readAll() async throws -> [PhoneContact] {
        try await Task.detached(priority: .userInitiated) { try readAllNow() }.value
    }

    private static func readAllNow() throws -> [PhoneContact] {
        // The picture is left out of the keys on purpose: a photograph
        // written into a card makes it hundreds of kilobytes, for a
        // thumbnail the address book list does not need. A card is also
        // stripped of any picture the serializer added anyway.
        let keys: [CNKeyDescriptor] = [
            CNContactVCardSerialization.descriptorForRequiredKeys(),
            CNContactFormatter.descriptorForRequiredKeys(for: .fullName),
            CNContactGivenNameKey as CNKeyDescriptor,
            CNContactFamilyNameKey as CNKeyDescriptor,
            CNContactOrganizationNameKey as CNKeyDescriptor,
            CNContactEmailAddressesKey as CNKeyDescriptor,
            CNContactPhoneNumbersKey as CNKeyDescriptor,
        ]
        let request = CNContactFetchRequest(keysToFetch: keys)
        request.sortOrder = .userDefault
        var found: [PhoneContact] = []
        try CNContactStore().enumerateContacts(with: request) { contact, _ in
            let emails = contact.emailAddresses.map { labeled in labeled.value as String }
            let phones = contact.phoneNumbers.map { labeled in labeled.value.stringValue }
            let name = CNContactFormatter.string(from: contact, style: .fullName) ?? contact.organizationName
            var card: String?
            if let data = try? CNContactVCardSerialization.data(with: [contact]), let written = String(data: data, encoding: .utf8) {
                card = ContactCard.removingPhoto(from: written)
            }
            found.append(
                PhoneContact(
                    id: contact.identifier, name: name, firstAddressText: emails.first ?? phones.first ?? "",
                    identity: ContactIdentity(name: name, emails: emails, phones: phones), card: card))
        }
        return found
    }
}

/// ContactImportModel is the contacts on the iPhone and which of them the
/// person has chosen to bring into the address book.
@MainActor
@Observable
final class ContactImportModel {
    enum Phase {
        case loading
        case denied
        case failed(String)
        case ready
    }

    /// How many contacts are sent at once: enough to be quick, few enough
    /// not to crowd out the rest of the app's requests.
    nonisolated static let concurrentImportCount = 4

    private(set) var phase = Phase.loading
    private(set) var phoneContacts: [PhoneContact] = []
    private(set) var alreadyPresentIds: Set<String> = []
    private(set) var chosenIds: Set<String> = []
    private(set) var isImporting = false
    private(set) var importedCount = 0
    private(set) var finishedCount = 0
    private(set) var totalCount = 0
    var searchText = ""

    let connection: Connection
    let addressBookId: String

    init(connection: Connection, addressBookId: String) {
        self.connection = connection
        self.addressBookId = addressBookId
    }

    /// The contacts the search leaves.
    var shownContacts: [PhoneContact] {
        let words = searchText.trimmed
        guard !words.isEmpty else { return phoneContacts }
        return phoneContacts.filter { contact in contact.name.localizedStandardContains(words) }
    }

    /// The shown contacts the address book does not have yet.
    private var shownNewIds: [String] {
        shownContacts.map(\.id).filter { contactId in !alreadyPresentIds.contains(contactId) }
    }

    /// Whether Select all has nothing left to choose among what is shown,
    /// so that the same row can clear them instead.
    var areAllShownNewChosen: Bool {
        let newIds = shownNewIds
        return !newIds.isEmpty && newIds.allSatisfy { contactId in chosenIds.contains(contactId) }
    }

    func load() async {
        phase = .loading
        guard await PhoneContacts.hasAccess() else {
            phase = .denied
            return
        }
        do {
            let found = try await PhoneContacts.readAll()
            // Every contact in the address book, not a page of them: the
            // server returns the whole book when no count is asked for.
            let existing = try await connection.contacts(addressBookId: addressBookId)
            let matcher = ContactMatcher(existing: existing.map(\.identity))
            let presentIds = found.filter { contact in matcher.isAlreadyPresent(contact.identity) }.map(\.id)
            phoneContacts = found
            alreadyPresentIds = Set(presentIds)
            chosenIds = Set(found.map(\.id)).subtracting(alreadyPresentIds)
            phase = .ready
        } catch {
            phase = .failed(error.localizedDescription)
        }
    }

    func toggle(_ contactId: String) {
        if chosenIds.contains(contactId) {
            chosenIds.remove(contactId)
        } else {
            chosenIds.insert(contactId)
        }
    }

    /// Chooses every shown contact that is not in the address book yet, or,
    /// when they all are chosen already, clears the shown ones.
    func toggleAllShown() {
        if areAllShownNewChosen {
            chosenIds.subtract(shownContacts.map(\.id))
        } else {
            chosenIds.formUnion(shownNewIds)
        }
    }

    /// Sends the chosen contacts, a few at a time, and says how many were
    /// taken and how many were not.
    func importChosen() async -> (importedCount: Int, failedCount: Int) {
        let cards = phoneContacts.filter { contact in chosenIds.contains(contact.id) }.map(\.card)
        let connection = self.connection
        let addressBookId = self.addressBookId
        isImporting = true
        importedCount = 0
        finishedCount = 0
        totalCount = cards.count
        await withTaskGroup(of: Bool.self) { group in
            var runningCount = 0
            for card in cards {
                if runningCount >= Self.concurrentImportCount, let isImported = await group.next() {
                    record(isImported)
                    runningCount -= 1
                }
                group.addTask {
                    await Self.send(card, to: connection, addressBookId: addressBookId)
                }
                runningCount += 1
            }
            for await isImported in group {
                record(isImported)
            }
        }
        isImporting = false
        return (importedCount, cards.count - importedCount)
    }

    private func record(_ isImported: Bool) {
        if isImported { importedCount += 1 }
        finishedCount += 1
    }

    /// Sends one card. A contact that fails does not stop the others, so
    /// the failure is counted rather than thrown.
    private nonisolated static func send(_ card: String?, to connection: Connection, addressBookId: String) async -> Bool {
        guard let card, ContactCard.fits(card) else { return false }
        do {
            try await connection.importContact(addressBookId: addressBookId, card: card)
            return true
        } catch {
            return false
        }
    }
}

/// ContactImportView lets the person choose which of the iPhone's contacts
/// to bring into their address book.
struct ContactImportView: View {
    let contacts: ContactsModel
    @State private var model: ContactImportModel
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL
    @Environment(\.accessibilityReduceMotion) private var isReducingMotion
    @Environment(\.scenePhase) private var scenePhase

    init(contacts: ContactsModel, addressBookId: String) {
        self.contacts = contacts
        _model = State(initialValue: ContactImportModel(connection: contacts.connection, addressBookId: addressBookId))
    }

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Import from iPhone")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel") { dismiss() }
                            .disabled(model.isImporting)
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Import") { startImport() }
                            .disabled(!isReady || model.chosenIds.isEmpty || model.isImporting)
                    }
                }
                .overlay {
                    if model.isImporting { progress }
                }
        }
        .interactiveDismissDisabled(model.isImporting)
        .task { await model.load() }
        .onChange(of: scenePhase) { _, phase in
            // Back from Settings after allowing contacts: read them without
            // making the person close the sheet and open it again.
            if phase == .active, case .denied = model.phase {
                Task { await model.load() }
            }
        }
    }

    private var isReady: Bool {
        if case .ready = model.phase { return true }
        return false
    }

    @ViewBuilder
    private var content: some View {
        switch model.phase {
        case .loading:
            ProgressView("Reading your contacts…")
        case .denied:
            ContentUnavailableView {
                Label("Contacts access is off", systemImage: "person.crop.circle.badge.xmark")
            } description: {
                Text("Allow TeaNode to read your contacts in Settings to bring them into your address book.")
            } actions: {
                Button("Open Settings") {
                    if let address = URL(string: UIApplication.openSettingsURLString) { openURL(address) }
                }
                .buttonStyle(.borderedProminent)
            }
        case .failed(let problemText):
            ContentUnavailableView(
                "Your contacts could not be read", systemImage: "person.crop.circle.badge.exclamationmark", description: Text(problemText.asReasonSentence))
        case .ready:
            if model.phoneContacts.isEmpty {
                ContentUnavailableView("No contacts on this iPhone", systemImage: "person.crop.circle")
            } else {
                list
            }
        }
    }

    private var list: some View {
        List {
            Section {
                Button {
                    model.toggleAllShown()
                } label: {
                    Label(
                        model.areAllShownNewChosen ? String(localized: "Select none") : String(localized: "Select all"),
                        systemImage: "checklist")
                    .actionColor()
                }
                .disabled(model.shownContacts.isEmpty)
            }
            Section {
                ForEach(model.shownContacts) { contact in
                    let isAlreadyPresent = model.alreadyPresentIds.contains(contact.id)
                    ChoiceRow(
                        title: contact.name.isEmpty ? String(localized: "No name") : contact.name,
                        detail: isAlreadyPresent ? String(localized: "Already in TeaNode") : contact.firstAddressText,
                        isChosen: model.chosenIds.contains(contact.id), isMultiple: true
                    ) {
                        model.toggle(contact.id)
                    }
                }
            } header: {
                Text("\(model.chosenIds.count) chosen")
            }
        }
        .searchable(text: $model.searchText, prompt: "Name")
        .overlay {
            if model.shownContacts.isEmpty { ContentUnavailableView.search(text: model.searchText) }
        }
    }

    /// What covers the list while contacts are being sent.
    private var progress: some View {
        VStack(spacing: 16) {
            ProgressView(value: Double(model.finishedCount), total: Double(max(model.totalCount, 1)))
            Text("Importing \(model.finishedCount) of \(model.totalCount)…")
                .font(.headline)
                .contentTransition(.numericText())
        }
        .accessibilityElement(children: .combine)
        .padding(32)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(.systemBackground))
        .animation(isReducingMotion ? nil : .default, value: model.finishedCount)
    }

    private func startImport() {
        Task {
            let outcome = await model.importChosen()
            let totalCount = outcome.importedCount + outcome.failedCount
            // The list is read again first: reading it clears the problem
            // text, which would otherwise take the failure away unseen.
            await contacts.load()
            if outcome.failedCount > 0 {
                contacts.problemText = String(localized: "\(outcome.failedCount) of \(totalCount) could not be imported.")
            } else if outcome.importedCount == 1 {
                contacts.doneText = String(localized: "Imported 1 contact.")
            } else {
                contacts.doneText = String(localized: "Imported \(outcome.importedCount) contacts.")
            }
            dismiss()
        }
    }
}
