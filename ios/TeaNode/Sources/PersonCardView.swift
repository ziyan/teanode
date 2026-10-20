import SwiftUI
import TeaNodeKit

// A person's card: tapping a person anywhere (the author of a message, a row
// on the Location map, a member of a circle, a contact) brings up one card for
// them, with their picture and name, what they are to the person, buttons to
// message, call, write and get directions, and a few items from each part of
// the app that concerns them. Each section ends in a row that opens its part.
// The server reads the sections side by side (ReadPersonCard); one that fails
// says so and the rest still show.

extension AppModel {
    /// Runs something that opens another part of the app from the card. The
    /// card is a sheet over the screen it was opened from, so it is closed
    /// first; pushed on the Contacts list it stays where it is.
    func leavePersonCard(isInSheet: Bool, then action: () -> Void) {
        if isInSheet { openedPerson = nil }
        action()
    }

    /// Writes to an address from the card. The compose sheet belongs to the
    /// app shell as the card does, and a sheet asked for while another is
    /// closing is dropped, so it waits for the card to be gone.
    func writeFromPersonCard(to address: String, isInSheet: Bool) {
        guard isInSheet else {
            composingTo = address
            return
        }
        openedPerson = nil
        Task {
            try? await Task.sleep(for: .milliseconds(600))
            composingTo = address
        }
    }
}

/// PersonCardModel is one card, read when it opens and again after anything
/// that changes it.
@MainActor
@Observable
final class PersonCardModel {
    let connection: Connection
    let target: PersonCardTarget
    private(set) var card: PersonCard?
    /// Why the card could not be read, said with a way to try again; only
    /// while there is no card to show.
    private(set) var loadProblemText: String?
    var doneText: String?
    var problemText: String?
    /// A model made for a preview shows what it was given and reads nothing.
    private let isFixed: Bool

    init(connection: Connection, target: PersonCardTarget) {
        self.connection = connection
        self.target = target
        isFixed = false
    }

    init(connection: Connection, target: PersonCardTarget, card: PersonCard?, loadProblemText: String?) {
        self.connection = connection
        self.target = target
        self.card = card
        self.loadProblemText = loadProblemText
        isFixed = true
    }

    func load() async {
        guard !isFixed else { return }
        do {
            card = try await connection.readPersonCard(target)
            loadProblemText = nil
        } catch {
            guard !Task.isCancelled else { return }
            if card == nil {
                loadProblemText = error.loadProblemText
            } else {
                problemText = error.loadProblemText
            }
        }
    }
}

/// PersonCardSheet is the card as the app shell presents it: half the screen
/// first, so what the person was looking at stays in view, and it can be
/// pulled up.
struct PersonCardSheet: View {
    let connection: Connection
    let target: PersonCardTarget
    @State private var detent: PresentationDetent

    init(connection: Connection, target: PersonCardTarget) {
        self.connection = connection
        self.target = target
        _detent = State(initialValue: Self.startingDetent)
    }

    var body: some View {
        NavigationStack {
            PersonCardView(connection: connection, target: target, isInSheet: true)
        }
        .presentationDetents([.medium, .large], selection: $detent)
        .presentationDragIndicator(.visible)
        // At half height what is under it stays in use, as in Find My: the
        // map can still be moved, the chat still read.
        .presentationBackgroundInteraction(.enabled(upThrough: .medium))
    }

    /// Half the screen; a screenshot of the card whole (`-TeaNodeScreen
    /// person-card-full`, `person-card-unlinked`) opens it at full height.
    private static var startingDetent: PresentationDetent {
        #if DEBUG
            if DemoServer.isRequested, DemoServer.opensPersonCardWhole { return .large }
        #endif
        return .medium
    }
}

/// PersonCardView is a person's card as a list: the header, then a section for
/// each part of the app that has something of theirs. In the sheet it has Done;
/// pushed on the Contacts list it has the way back that list gives it.
struct PersonCardView: View {
    let isInSheet: Bool
    @State private var model: PersonCardModel
    /// The address book, for the contact's edit form and its link to an
    /// account. The Contacts list passes its own, so what changes here shows
    /// there.
    @State private var contacts: ContactsModel
    @State private var directory: CircleDirectory
    @Environment(AppModel.self) private var app
    @State private var isEditing = false
    @State private var isChoosingAccount = false
    @State private var isLinkingContact = false
    @State private var relationshipMember: CircleMember?
    @State private var isPreparingRelationship = false

    init(model: PersonCardModel, isInSheet: Bool, contacts: ContactsModel? = nil) {
        self.isInSheet = isInSheet
        _model = State(initialValue: model)
        _contacts = State(initialValue: contacts ?? ContactsModel(connection: model.connection))
        _directory = State(initialValue: CircleDirectory(connection: model.connection))
    }

    init(connection: Connection, target: PersonCardTarget, isInSheet: Bool, contacts: ContactsModel? = nil) {
        self.init(model: PersonCardModel(connection: connection, target: target), isInSheet: isInSheet, contacts: contacts)
    }

    var body: some View {
        framed
            .sheet(isPresented: $isEditing) { editor }
            .sheet(isPresented: $isChoosingAccount) { accountPicker }
            .sheet(isPresented: $isLinkingContact) { contactPicker }
            .sheet(item: $relationshipMember, onDismiss: { Task { await model.load() } }) { member in
                CircleRelationshipSheet(directory: directory, member: member)
            }
    }

    /// The card with its bar and what it says.
    private var framed: some View {
        content
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { toolbarItems }
            .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
            .toast(done: Bindable(directory).doneText, problem: Bindable(directory).problemText)
            .modifier(PersonCardContactToast(isInSheet: isInSheet, contacts: contacts))
            .task {
                await model.load()
                if model.card?.hasContact == true { await contacts.loadAddressBook() }
            }
    }

    @ViewBuilder
    private var content: some View {
        if let card = model.card {
            cardList(card)
        } else if let loadProblemText = model.loadProblemText {
            LoadFailedView(problemText: loadProblemText) { await model.load() }
        } else {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private func cardList(_ card: PersonCard) -> some View {
        List {
            Section {
                PersonCardHeader(card: card, model: model, isInSheet: isInSheet, isMine: isMine(card))
            }
            .listRowBackground(Color.clear)
            .listRowSeparator(.hidden)
            sections(card)
        }
        .listStyle(.insetGrouped)
        .refreshable { await model.load() }
    }

    /// What the card has of the person, a few small groups at a time so that
    /// adding a section cannot reach the limit of what one builder takes.
    @ViewBuilder
    private func sections(_ card: PersonCard) -> some View {
        linkSection(card)
        accountSections(card)
        librarySections(card)
        contactSections(card)
    }

    /// The one offer to link a contact, for the sections that wait for one.
    @ViewBuilder
    private func linkSection(_ card: PersonCard) -> some View {
        if card.person != nil, card.needsContact {
            PersonCardLinkView(card: card) { isLinkingContact = true }
        }
    }

    /// What hangs on their account: where they are and their health.
    @ViewBuilder
    private func accountSections(_ card: PersonCard) -> some View {
        PersonCardLocationView(card: card, connection: model.connection, isInSheet: isInSheet)
        PersonCardHealthView(card: card, isInSheet: isInSheet)
    }

    /// What hangs on the person's contact for them: photos, mail, recordings
    /// and memory.
    @ViewBuilder
    private func librarySections(_ card: PersonCard) -> some View {
        PersonCardPhotosView(card: card, connection: model.connection, isInSheet: isInSheet)
        PersonCardMailView(card: card, isInSheet: isInSheet)
        PersonCardRecordingsView(card: card, isInSheet: isInSheet)
        PersonCardMemoryView(card: card, isInSheet: isInSheet)
    }

    /// The contact's details, the circles shared, the relationship, and what
    /// can be done about the contact.
    @ViewBuilder
    private func contactSections(_ card: PersonCard) -> some View {
        PersonCardDetailsView(card: card, isInSheet: isInSheet)
        PersonCardCirclesView(card: card)
        PersonCardRelationshipView(card: card, isPreparing: isPreparingRelationship) { openRelationship(of: $0) }
        if isContactInLoadedBook {
            PersonCardManageView(
                card: card, contacts: contacts, isInSheet: isInSheet, isChoosingAccount: $isChoosingAccount,
                didChange: { Task { await model.load() } })
        }
    }

    /// The contact is in the address book this screen edits: the app keeps
    /// one, and a contact of another book is shown but not changed here.
    private var isContactInLoadedBook: Bool {
        guard let bookId = contacts.addressBook?.id else { return false }
        guard let contactBookId = model.card?.contact?.addressBookId else { return true }
        return contactBookId == bookId
    }

    private func isMine(_ card: PersonCard) -> Bool {
        guard let contact = card.contact else { return false }
        return contact.id == contacts.myContactId
    }

    // MARK: Bar

    @ToolbarContentBuilder
    private var toolbarItems: some ToolbarContent {
        if isInSheet {
            ToolbarItem(placement: .topBarLeading) {
                Button("Done") { app.openedPerson = nil }
            }
        }
        ToolbarItem(placement: .topBarTrailing) {
            if model.card?.hasContact == true {
                Button("Edit") { isEditing = true }
                    .disabled(!isContactInLoadedBook)
            }
        }
    }

    // MARK: Sheets

    @ViewBuilder
    private var editor: some View {
        if let contact = model.card?.contact {
            ContactEditor(model: contacts, contact: contact) { _ in
                Task { await model.load() }
            }
        }
    }

    @ViewBuilder
    private var accountPicker: some View {
        if let contact = model.card?.contact {
            ContactAccountPicker(model: contacts, contact: contact) {
                Task { await model.load() }
            }
        }
    }

    @ViewBuilder
    private var contactPicker: some View {
        if let person = model.card?.person {
            PersonContactPicker(contacts: contacts, person: person) {
                Task { await model.load() }
            }
        }
    }

    /// Opens the Relationship sheet once the circles are read: saving it needs
    /// who the person is and how they are described.
    private func openRelationship(of member: CircleMember) {
        guard !isPreparingRelationship else { return }
        isPreparingRelationship = true
        Task {
            await directory.load()
            isPreparingRelationship = false
            relationshipMember = member
        }
    }
}

/// PersonCardContactToast says what happened to the contact (saved, linked,
/// removed) in the sheet. Pushed on the Contacts list, that list says it.
private struct PersonCardContactToast: ViewModifier {
    let isInSheet: Bool
    let contacts: ContactsModel

    @ViewBuilder
    func body(content: Content) -> some View {
        if isInSheet {
            content.toast(done: Bindable(contacts).doneText, problem: Bindable(contacts).problemText)
        } else {
            content
        }
    }
}

/// PersonCardLinkView is the offer to link a contact to an account, made once
/// for all the sections that are found through a contact.
struct PersonCardLinkView: View {
    let card: PersonCard
    let onLink: () -> Void

    var body: some View {
        Section {
            Button(action: onLink) {
                Label("Link a contact", systemImage: "link")
                    .actionColor()
            }
        } footer: {
            Text("Link a contact to see mail, photos, recordings and memory about \(card.displayName).")
        }
    }
}

/// PersonCardPresenter shows the card of the person the app was asked to open,
/// over whatever is on screen. It is a modifier of its own so that the shell's
/// chain of sheets stays short.
struct PersonCardPresenter: ViewModifier {
    let connection: Connection
    @Environment(AppModel.self) private var app

    func body(content: Content) -> some View {
        content
            .sheet(item: Bindable(app).openedPerson) { target in
                PersonCardSheet(connection: connection, target: target)
            }
    }
}

// MARK: - The header

/// PersonCardHeader is who they are: a large picture, their name, what they
/// are to the person, and the buttons for what can be done with them at once.
struct PersonCardHeader: View {
    let card: PersonCard
    let model: PersonCardModel
    let isInSheet: Bool
    let isMine: Bool
    @Environment(AppModel.self) private var app
    @Environment(\.openURL) private var openURL
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @State private var isOpeningChat = false

    var body: some View {
        VStack(spacing: 10) {
            picture
            titles
            if hasButtons {
                buttons
            }
        }
        .frame(maxWidth: .infinity)
    }

    // MARK: Who

    @ViewBuilder
    private var picture: some View {
        if let person = card.person {
            AvatarView(name: person.displayName, avatarUrl: person.avatarUrl, colorIndex: person.avatarColorIndex, size: 64)
        } else if let contact = card.contact {
            ContactPicture(contactId: contact.id, name: contact.name ?? "", hasPhoto: contact.hasPhoto == true, size: 64)
        }
    }

    private var titles: some View {
        VStack(spacing: 3) {
            Text(verbatim: nameText)
                .font(.title2.bold())
                .multilineTextAlignment(.center)
            if let relationshipText {
                Text(verbatim: relationshipText)
                    .foregroundStyle(.secondary)
            }
            if let usernameText {
                Text(verbatim: usernameText)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            if isMine {
                Label("This is you", systemImage: "person.fill.checkmark")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var nameText: String {
        card.displayName.isEmpty ? String(localized: "No name") : card.displayName
    }

    /// What they are to the person, such as "Your mother"; for a contact with
    /// no account, the organization they are with.
    private var relationshipText: String? {
        if let person = card.person {
            guard let word = RelationshipWords.word(of: person) else { return nil }
            return String(localized: "Your \(word)")
        }
        guard let organization = card.contact?.organization, !organization.isEmpty else { return nil }
        return organization
    }

    private var usernameText: String? {
        guard let person = card.person, person.displayName != person.username else { return nil }
        return "@\(person.username)"
    }

    // MARK: Buttons

    /// The account to message, when a chat with them can be opened.
    private var messageUserId: String? {
        card.canMessage ? card.person?.userId : nil
    }

    private var hasButtons: Bool {
        messageUserId != nil || card.callURL != nil || card.mailAddress != nil || card.directionsURL != nil
    }

    /// In a row at the sizes of text that fit, else one under another.
    @ViewBuilder
    private var buttons: some View {
        if dynamicTypeSize.isAccessibilitySize {
            VStack(spacing: 8) { actionButtons }
        } else {
            HStack(spacing: 8) { actionButtons }
        }
    }

    @ViewBuilder
    private var actionButtons: some View {
        if let userId = messageUserId {
            PersonCardActionButton(title: String(localized: "person-card-message", defaultValue: "Message"), systemImage: "message") {
                openChat(with: userId)
            }
            .disabled(isOpeningChat)
        }
        if let callURL = card.callURL {
            PersonCardActionButton(title: String(localized: "Call"), systemImage: "phone") { openURL(callURL) }
        }
        if let mailAddress = card.mailAddress {
            PersonCardActionButton(title: String(localized: "Mail"), systemImage: "envelope") {
                app.writeFromPersonCard(to: mailAddress, isInSheet: isInSheet)
            }
        }
        if let directionsURL = card.directionsURL {
            PersonCardActionButton(title: String(localized: "Directions"), systemImage: "car") { openURL(directionsURL) }
        }
    }

    /// Opens the direct chat with them and closes the card over it.
    private func openChat(with userId: String) {
        guard !isOpeningChat else { return }
        isOpeningChat = true
        Task {
            do {
                let conversationId = try await model.connection.openDirectChat(with: userId)
                isOpeningChat = false
                app.leavePersonCard(isInSheet: isInSheet) { app.openedConversationId = conversationId }
            } catch {
                isOpeningChat = false
                model.problemText = error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
            }
        }
    }
}

/// PersonCardActionButton is one of the buttons under the name, as Contacts
/// and Find My draw them: a symbol over a word on a rounded rectangle. They
/// sit in a row of the list that is no row of its own, so they are plain
/// buttons, each pressed alone.
struct PersonCardActionButton: View {
    let title: String
    let systemImage: String
    let action: () -> Void
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        Button(action: action) {
            label
                .actionColor()
                .frame(maxWidth: .infinity, minHeight: 44)
                .padding(.vertical, 8)
                .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 12))
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
    }

    /// Side by side at the largest sizes of text, where a word under its
    /// symbol would not fit a quarter of the width.
    @ViewBuilder
    private var label: some View {
        if dynamicTypeSize.isAccessibilitySize {
            HStack(spacing: 8) {
                symbol
                words
            }
        } else {
            VStack(spacing: 4) {
                symbol
                words
            }
        }
    }

    private var symbol: some View {
        // One height for every symbol, so the words under them line up
        // (an envelope is shorter than a phone).
        Image(systemName: systemImage)
            .font(.title3)
            .symbolRenderingMode(.hierarchical)
            .frame(height: 24)
            .accessibilityHidden(true)
    }

    private var words: some View {
        Text(verbatim: title)
            .font(.caption)
    }
}

// MARK: - Circles and the relationship

/// PersonCardCirclesView is the circles the person and they are both in.
struct PersonCardCirclesView: View {
    let card: PersonCard

    var body: some View {
        if !card.sharedCircles.isEmpty {
            Section {
                ForEach(card.sharedCircles) { circle in
                    HStack {
                        Label(circle.circleName, systemImage: "person.2")
                            .foregroundStyle(.primary)
                        Spacer(minLength: 8)
                        // Left out when the name already says it ("Family").
                        if let kind = circle.knownCircleKind, kind.title.lowercased() != circle.circleName.lowercased() {
                            Text(verbatim: kind.title)
                                .foregroundStyle(.secondary)
                        }
                    }
                    .accessibilityElement(children: .combine)
                }
            } header: {
                Text("Circles")
            }
        }
    }
}

/// PersonCardRelationshipView is what they are to the person, saying it again
/// in the sheet the Circles screen has: the word, and the pronoun asked for
/// when the person has not said how they are described.
struct PersonCardRelationshipView: View {
    let card: PersonCard
    let isPreparing: Bool
    let open: (CircleMember) -> Void

    var body: some View {
        if let person = card.person {
            Section {
                Button {
                    open(person)
                } label: {
                    LabeledContent("Relationship") {
                        Text(verbatim: valueText(of: person))
                            .foregroundStyle(.secondary)
                    }
                    .foregroundStyle(.primary)
                    .contentShape(.rect)
                }
                .disabled(isPreparing)
                .accessibilityHint("Says what they are to you")
            }
        }
    }

    private func valueText(of person: CircleMember) -> String {
        guard let word = RelationshipWords.word(of: person) else { return String(localized: "None") }
        return RelationshipWords.capitalized(word)
    }
}

// MARK: - The contact

/// PersonCardManageView is what the person can do about their contact for
/// them, which the contact screen had: say which account they are, mark them
/// as the person, take their picture off, and delete them.
struct PersonCardManageView: View {
    let card: PersonCard
    let contacts: ContactsModel
    let isInSheet: Bool
    @Binding var isChoosingAccount: Bool
    let didChange: () -> Void
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var isConfirmingDelete = false
    @State private var isConfirmingRemovePicture = false

    var body: some View {
        if let contact = card.contact {
            accountSection(of: contact)
            actionsSection(of: contact)
        }
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
                        if await contacts.unlink(contactId: contact.id) { didChange() }
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

    @ViewBuilder
    private func actionsSection(of contact: ContactDetail) -> some View {
        Section {
            if contact.id == contacts.myContactId {
                Button("This is not me") {
                    Task { await contacts.setMine(nil) }
                }
            } else {
                Button("This is me") {
                    Task { await contacts.setMine(contact.id) }
                }
            }
            if contact.hasPhoto == true {
                Button("Remove picture", role: .destructive) {
                    isConfirmingRemovePicture = true
                }
                .confirmationDialog("Remove the picture of \(contact.name ?? "")?", isPresented: $isConfirmingRemovePicture, titleVisibility: .visible) {
                    Button("Remove picture", role: .destructive) {
                        Task {
                            if await contacts.removePicture(of: contact.id) { didChange() }
                        }
                    }
                } message: {
                    Text("Devices synchronizing your address book lose it too.")
                }
            }
            Button("Delete contact", role: .destructive) {
                isConfirmingDelete = true
            }
            .confirmationDialog("Delete \(contact.name ?? "")?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
                Button("Delete contact", role: .destructive) {
                    Task {
                        if await contacts.delete(contact.id) { leave() }
                    }
                }
            } message: {
                Text("It is removed from your address book and from every device that reads it.")
            }
        } footer: {
            Text("Your agent knows the contact marked as you is you.")
        }
    }

    /// The contact is gone: the card closes, or goes back to the list.
    private func leave() {
        if isInSheet {
            app.openedPerson = nil
        } else {
            dismiss()
        }
    }
}

// MARK: - Previews

#if DEBUG
    /// The demo's cards, for the previews below: read from the answers the
    /// demo server gives the screenshots.
    @MainActor
    private enum PersonCardPreviewData {
        static var connection: Connection {
            Connection(account: DemoServer.account, transport: DemoServer())
        }

        static func card(_ target: PersonCardTarget) -> PersonCard? {
            let body = DemoServer.personCardAnswer(target: target)
            struct Payload: Decodable { let ReadPersonCard: PersonCard }
            struct Answer: Decodable { let data: Payload }
            return (try? JSONDecoder().decode(Answer.self, from: Data(body.utf8)))?.data.ReadPersonCard
        }

        static func view(_ target: PersonCardTarget, problemText: String? = nil, isLoading: Bool = false) -> some View {
            var shownCard: PersonCard?
            if !isLoading, problemText == nil { shownCard = card(target) }
            let model = PersonCardModel(connection: connection, target: target, card: shownCard, loadProblemText: problemText)
            return NavigationStack {
                PersonCardView(model: model, isInSheet: true)
            }
            .environment(AppModel.shared)
        }
    }

    #Preview("Loading, light") { PersonCardPreviewData.view(.account(userId: "sam"), isLoading: true) }
    #Preview("Full, light") { PersonCardPreviewData.view(.account(userId: "sam")) }
    #Preview("Full, dark") { PersonCardPreviewData.view(.account(userId: "sam")).preferredColorScheme(.dark) }
    #Preview("Unlinked, light") { PersonCardPreviewData.view(.account(userId: "alex")) }
    #Preview("Unlinked, dark") { PersonCardPreviewData.view(.account(userId: "alex")).preferredColorScheme(.dark) }
    #Preview("Contact only, light") { PersonCardPreviewData.view(.contact(addressBookId: "book", contactId: "shop")) }
    #Preview("Failed, light") {
        PersonCardPreviewData.view(.account(userId: "sam"), problemText: "Check your connection and try again.")
    }
#endif
