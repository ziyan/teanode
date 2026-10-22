import Observation
import SwiftUI
import TeaNodeKit

// The dashboard's Circles page: the circles the person is in, the invitations
// waiting for their answer, and for each circle its members and, for an admin,
// who is still being waited for. The list keeps one CircleDirectory and the
// screens it pushes read from the same one, so a change made on a detail screen
// shows on the list behind it.

/// CircleDirectory is what the circles screens read: the person's circles with
/// their members, the invitations waiting for them, and who they are, so their
/// own row is kept from being removed. It is read again after every change.
@MainActor
@Observable
final class CircleDirectory {
    let connection: Connection
    private(set) var circles: [PeopleCircle] = []
    private(set) var invitations: [CircleInvitation] = []
    private(set) var currentUserId: String?
    /// How the person says they are described, which words their side of a
    /// relationship; not set until they say.
    private(set) var ownPronoun: PersonPronoun = .notSet
    /// Whether they may manage accounts, which lets them make one for
    /// somebody and put it in a circle at once.
    private(set) var canManageUsers = false
    /// Whether the list of circles has been read once.
    private(set) var isLoaded = false
    /// Why the first read of the circles did not get through, to be said with a
    /// way to try again, since an empty list would say the person is in none.
    private(set) var loadProblemText: String?
    private(set) var isLoading = false
    var isBusy = false
    var doneText: String?
    var problemText: String?

    init(connection: Connection) {
        self.connection = connection
    }

    func circle(_ circleId: String) -> PeopleCircle? { circles.first { $0.id == circleId } }

    /// Reads the circles and the invitations. Only the circles' own refusal is
    /// said; who is reading and the invitations are left as they were when
    /// they cannot be read.
    func load() async {
        isLoading = true
        defer { isLoading = false }
        let connection = self.connection
        async let sessionRead = try? connection.accessSession()
        async let invitationsRead = try? connection.circleInvitations()
        async let profileRead = try? connection.accountProfile()
        var didReadCircles = false
        do {
            circles = try await connection.circles()
            didReadCircles = true
            loadProblemText = nil
        } catch {
            if isLoaded { problemText = error.loadProblemText } else { loadProblemText = error.loadProblemText }
        }
        if let readInvitations = await invitationsRead { invitations = readInvitations }
        if let session = await sessionRead {
            currentUserId = session.userId
            canManageUsers = session.canManageUsers
        }
        if let profile = await profileRead {
            ownPronoun = profile.chosenPronoun
            if currentUserId == nil { currentUserId = profile.id }
        }
        // Last, so that the empty state is not drawn in the words for
        // somebody who cannot manage accounts and then changed.
        if didReadCircles { isLoaded = true }
    }

    /// Runs a change, says it was done or why it was refused, and reads
    /// everything again either way.
    @discardableResult
    func act(saying said: String, _ work: () async throws -> Void) async -> Bool {
        isBusy = true
        defer { isBusy = false }
        do {
            try await work()
            doneText = said
            await load()
            return true
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
            await load()
            return false
        }
    }
}

// MARK: - The list

/// CirclesView is the circles the person is in, each opening to its members,
/// with the invitations waiting for an answer above them.
struct CirclesView: View {
    @State private var directory: CircleDirectory
    @State private var isAdding = false
    /// The offer to share the calendar with a circle just joined, made once as
    /// the invitation is accepted.
    @State private var calendarOffer: CircleCalendarOffer?

    init(connection: Connection) {
        _directory = State(initialValue: CircleDirectory(connection: connection))
    }

    var body: some View {
        List {
            ForEach(Array(directory.invitations.enumerated()), id: \.element.id) { position, invitation in
                invitationSection(invitation, isFirst: position == 0)
            }
            if !directory.circles.isEmpty {
                Section {
                    ForEach(directory.circles) { circle in
                        NavigationLink {
                            CircleDetailView(directory: directory, circleId: circle.id)
                        } label: {
                            CircleRow(circle: circle)
                        }
                    }
                } footer: {
                    Text("The people you share this server with, grouped as family, friends or colleagues.")
                }
            }
        }
        .navigationTitle("Circles")
        .overlay {
            if let loadProblemText = directory.loadProblemText, !directory.isLoaded {
                LoadFailedView(problemText: loadProblemText) { await directory.load() }
            } else if !directory.isLoaded && directory.isLoading {
                ProgressView()
            } else if directory.isLoaded && directory.circles.isEmpty && directory.invitations.isEmpty {
                ContentUnavailableView(
                    "No circles yet", systemImage: "person.2",
                    description: directory.canManageUsers
                        // An operator can bring in people who have no account
                        // yet; anybody else can only invite accounts.
                        ? Text("Make a circle and add the people who use this server, with or without accounts yet.")
                        : Text("Make a circle, then invite the people who should be in it. If nobody is listed, ask whoever runs this server to make their account."))
            }
        }
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button("New circle", systemImage: "plus") { isAdding = true }
            }
        }
        .disabled(directory.isBusy)
        .refreshable { await directory.load() }
        .task { await directory.load() }
        .sheet(isPresented: $isAdding) {
            CircleForm(directory: directory, editedCircle: nil)
        }
        .sheet(item: $calendarOffer) { offer in
            CircleCalendarOfferSheet(connection: directory.connection, offer: offer) { said in directory.doneText = said }
        }
        .toast(done: Bindable(directory).doneText, problem: Bindable(directory).problemText)
    }

    /// One invitation: who asked and to what, then its two answers as rows of
    /// their own.
    private func invitationSection(_ invitation: CircleInvitation, isFirst: Bool) -> some View {
        Section {
            VStack(alignment: .leading, spacing: 3) {
                Text(verbatim: invitation.circleName)
                // One line of words, so that at the largest text it wraps as
                // a sentence rather than in columns.
                Text(verbatim: invitationDetails(invitation)).font(.footnote).foregroundStyle(Color(.secondaryLabel))
            }
            Button("Accept", systemImage: "checkmark") { answer(invitation, isAccepted: true) }
                .actionColor()
                .fullWidthRowSeparator()
            Button("Decline", systemImage: "xmark") { answer(invitation, isAccepted: false) }
                .actionColor()
        } header: {
            if isFirst { Text("Invitations") }
        }
    }

    private func invitationDetails(_ invitation: CircleInvitation) -> String {
        let invitedBy = String(localized: "Invited by \(invitation.invitedByName)")
        guard let kind = invitation.knownCircleKind else { return invitedBy }
        return "\(kind.title) · \(invitedBy)"
    }

    private func answer(_ invitation: CircleInvitation, isAccepted: Bool) {
        Task {
            let isDone = await directory.act(saying: isAccepted ? String(localized: "You joined \(invitation.circleName)") : String(localized: "Invitation declined")) {
                try await directory.connection.answerCircleInvitation(invitation.id, isAccepted: isAccepted)
            }
            // Joining a family or colleagues offers to share the calendar with
            // them, once, and a family also where the person is, in the same
            // sheet; a person with no calendar of their own is asked only
            // about the family's location.
            if isDone, isAccepted {
                let own = CircleCalendarOffer.ownCalendar(among: (try? await directory.connection.calendars()) ?? [])
                calendarOffer = CircleCalendarOffer(
                    accepting: invitation, calendarId: own?.id, canShareLocation: AppModel.shared.canUseLocation)
            }
        }
    }
}

/// CircleRow is a circle in a line or two: its name, what kind it is and how
/// many people are in it, beside a few of their pictures.
private struct CircleRow: View {
    let circle: PeopleCircle
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private static let mostAvatars = 3

    var body: some View {
        // At the largest text the pictures go above the words, which would
        // otherwise be squeezed to a letter a line beside them.
        let layout = dynamicTypeSize.isAccessibilitySize ? AnyLayout(VStackLayout(alignment: .leading, spacing: 8)) : AnyLayout(HStackLayout(spacing: 12))
        layout {
            avatars
            VStack(alignment: .leading, spacing: 3) {
                Text(verbatim: circle.name)
                Text(verbatim: details).font(.footnote).foregroundStyle(Color(.secondaryLabel))
            }
        }
    }

    /// A few members' pictures, each overlapping the one before.
    private var avatars: some View {
        HStack(spacing: -10) {
            ForEach(circle.members.prefix(Self.mostAvatars)) { member in
                AvatarView(name: member.displayName, avatarUrl: member.avatarUrl, colorIndex: member.avatarColorIndex, size: 30)
                    .overlay(Circle().stroke(Color(.systemBackground), lineWidth: 2))
            }
        }
    }

    private var details: String {
        let people = CircleWords.memberCount(circle.members.count)
        guard let kind = circle.knownCircleKind else { return people }
        return "\(kind.title) · \(people)"
    }
}

// MARK: - One circle

/// CircleDetailView is one circle: who is in it, and for an admin the form to
/// invite somebody, the invitations still waiting, and renaming and deleting
/// the circle. Leaving is for everyone, behind a confirmation. What is hidden
/// from a member is refused by the server as well.
struct CircleDetailView: View {
    let directory: CircleDirectory
    let circleId: String
    @Environment(\.dismiss) private var dismiss
    @State private var invitationDraft = CircleInvitationDraft()
    @State private var isRenaming = false
    @State private var isConfirmingDelete = false
    @State private var isConfirmingLeave = false
    @State private var isMakingAccount = false
    @State private var removing: CircleMember?
    /// The member whose Relationship sheet is open.
    @State private var relationshipMember: CircleMember?

    private var circle: PeopleCircle? { directory.circle(circleId) }

    var body: some View {
        #if DEBUG
            // `-TeaNodeScreen circle-detail`: scrolled to the invitation waiting for an answer;
            // `circle-detail-operator` the same for somebody who runs the server, with the
            // row that makes an account above it.
            ScrollViewReader { scroller in
                list
                    .task(id: directory.isLoaded) {
                        guard DemoServer.isRequested, ["circle-detail", "circle-detail-operator"].contains(DemoServer.screen), directory.isLoaded,
                            let invitation = circle?.pendingInvitations.first
                        else { return }
                        try? await Task.sleep(for: .seconds(1))
                        withAnimation { scroller.scrollTo(invitation.id, anchor: .bottom) }
                    }
            }
        #else
            list
        #endif
    }

    private var list: some View {
        List {
            if let circle {
                if let kind = circle.knownCircleKind {
                    Section {
                        LabeledContent("Kind") { Text(kind.title) }
                    }
                }
                membersSection(circle)
                if circle.isAdmin {
                    inviteSection(circle)
                    if directory.canManageUsers {
                        accountSection
                    }
                    if !circle.pendingInvitations.isEmpty {
                        pendingSection(circle)
                    }
                }
                endingSection(circle)
            } else if directory.isLoaded {
                ContentUnavailableView("This circle is no longer here", systemImage: "person.2")
            }
        }
        .navigationTitle(circle?.name ?? "")
        .disabled(directory.isBusy)
        .refreshable { await directory.load() }
        .sheet(isPresented: $isRenaming) {
            if let circle {
                CircleForm(directory: directory, editedCircle: circle)
            }
        }
        .sheet(isPresented: $isMakingAccount) {
            if let circle {
                CircleAccountForm(directory: directory, circle: circle)
            }
        }
        .sheet(item: $relationshipMember) { member in
            CircleRelationshipSheet(directory: directory, member: member)
        }
        .toast(done: Bindable(directory).doneText, problem: Bindable(directory).problemText)
    }

    // MARK: Members

    private func membersSection(_ circle: PeopleCircle) -> some View {
        Section {
            ForEach(circle.members) { member in
                let isCurrentUser = member.userId == directory.currentUserId
                memberRow(member, isCurrentUser: isCurrentUser)
                    .confirmationDialog(
                        "Remove from the circle?", isPresented: Binding(get: { removing?.userId == member.userId }, set: { if !$0 { removing = nil } }),
                        titleVisibility: .visible, presenting: removing
                    ) { member in
                        Button("Remove \(member.displayName)", role: .destructive) {
                            Task {
                                await directory.act(saying: String(localized: "\(member.displayName) was removed")) {
                                    try await directory.connection.removeCircleMember(circle.id, userId: member.userId)
                                }
                            }
                        }
                    } message: { member in
                        Text("\(member.displayName) will no longer be in \(circle.name). What they shared with it, and with anyone they then share no other circle with, ends and does not come back if they join again. They can be invited again.")
                    }
                    .swipeActions {
                        if !isCurrentUser { photosAction(member) }
                        if !isCurrentUser { relationshipAction(member) }
                        if circle.isAdmin && !isCurrentUser { memberActions(member, in: circle) }
                    }
                    .contextMenu {
                        if !isCurrentUser { photosAction(member) }
                        if !isCurrentUser { relationshipAction(member) }
                        if circle.isAdmin && !isCurrentUser { memberActions(member, in: circle) }
                    }
            }
        } header: {
            Text("Members")
        }
    }

    /// A member's row. Somebody else's opens their card when pressed, where
    /// what they are to the person is said too; the swipe and the long press
    /// keep the Relationship sheet one move away.
    @ViewBuilder
    private func memberRow(_ member: CircleMember, isCurrentUser: Bool) -> some View {
        if isCurrentUser {
            MemberRow(member: member, isCurrentUser: true)
        } else {
            Button {
                AppModel.shared.openedPerson = .account(userId: member.userId)
            } label: {
                MemberRow(member: member, isCurrentUser: false)
            }
            .foregroundStyle(.primary)
            .accessibilityHint("Opens their card")
        }
    }

    /// The photos of a member the person linked to one of their contacts, in
    /// Photos. Only the person knows of the link, so nothing shows otherwise.
    @ViewBuilder
    private func photosAction(_ member: CircleMember) -> some View {
        if member.isLinkedToContact, AppModel.shared.isSectionAvailable(.photos) {
            Button("Photos", systemImage: "photo.on.rectangle") {
                AppModel.shared.openedPhotoPerson = OpenedPhotoPerson(userId: member.userId, name: member.displayName)
            }
            .tint(.blue)
        }
    }

    /// Saying what somebody is to the person, which is for everyone in the
    /// circle to do for anyone else in it, not only its admins.
    @ViewBuilder
    private func relationshipAction(_ member: CircleMember) -> some View {
        Button("Relationship", systemImage: "person.2") { relationshipMember = member }
            .tint(.teal)
    }

    /// Making a member an admin or a member again, and taking them out. Not
    /// oneself: leaving is its own button, and the server keeps a circle from
    /// losing its last admin.
    @ViewBuilder
    private func memberActions(_ member: CircleMember, in circle: PeopleCircle) -> some View {
        Button("Remove from the circle", systemImage: "person.badge.minus", role: .destructive) { removing = member }
            .tint(.red)
        Button(member.isAdmin ? LocalizedStringKey("Make member") : "Make admin", systemImage: member.isAdmin ? "person" : "person.badge.key") {
            let memberRole: CircleMemberRole = member.isAdmin ? .member : .admin
            Task {
                await directory.act(
                    saying: memberRole == .admin ? String(localized: "\(member.displayName) is now an admin") : String(localized: "\(member.displayName) is now a member")
                ) {
                    try await directory.connection.setCircleMemberRole(circle.id, userId: member.userId, memberRole: memberRole)
                }
            }
        }
        .tint(.indigo)
    }

    // MARK: Invitations

    private func inviteSection(_ circle: PeopleCircle) -> some View {
        Section {
            TextField("Username", text: $invitationDraft.username)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            Toggle("Make them an admin", isOn: $invitationDraft.isAdmin)
            Button {
                Task {
                    // The server answers the same whether or not the account
                    // exists, so this must not say the person was found.
                    let isSent = await directory.act(saying: String(localized: "Invitation sent if that account exists")) {
                        try await directory.connection.inviteToCircle(circle.id, draft: invitationDraft)
                    }
                    if isSent { invitationDraft = CircleInvitationDraft() }
                }
            } label: {
                Label("Send invitation", systemImage: "paperplane")
                    .actionColor()
            }
            .disabled(!invitationDraft.canSubmit)
        } header: {
            Text("Invite someone")
        } footer: {
            // An operator can make the account; anybody else is told whom to ask.
            if directory.canManageUsers {
                Text("Enter the username of an account on this server.")
            } else {
                Text("Enter the username of an account on this server. If nobody is listed, ask whoever runs this server to make their account.")
            }
        }
    }

    /// The other way in, for somebody who manages accounts: the person has no
    /// account yet, so one is made and they are in the circle at once, with
    /// nothing for them to accept.
    private var accountSection: some View {
        Section {
            Button("Make an account for someone", systemImage: "person.badge.plus") { isMakingAccount = true }
                .actionColor()
        } footer: {
            Text("They sign in with that username and password, and can change the password themselves in their settings.")
        }
    }

    private func pendingSection(_ circle: PeopleCircle) -> some View {
        Section {
            ForEach(circle.pendingInvitations) { invitation in
                PendingInvitationRow(invitation: invitation)
                    .swipeActions { withdrawAction(invitation) }
                    .contextMenu { withdrawAction(invitation) }
            }
        } header: {
            Text("Waiting for an answer")
        }
    }

    @ViewBuilder
    private func withdrawAction(_ invitation: CircleInvitation) -> some View {
        Button("Withdraw", systemImage: "xmark", role: .destructive) {
            Task {
                await directory.act(saying: String(localized: "Invitation withdrawn")) {
                    try await directory.connection.withdrawCircleInvitation(invitation.id)
                }
            }
        }
        .tint(.red)
    }

    // MARK: Renaming, deleting and leaving

    private func endingSection(_ circle: PeopleCircle) -> some View {
        Section {
            if circle.isAdmin {
                Button("Rename circle", systemImage: "pencil") { isRenaming = true }
                    .actionColor()
                Button("Delete circle", systemImage: "trash", role: .destructive) { isConfirmingDelete = true }
                    .tint(.red)
                    .actionColor(isDestructive: true)
                    .confirmationDialog("Delete \(circle.name)?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
                        Button("Delete circle", role: .destructive) {
                            Task {
                                let isDeleted = await directory.act(saying: String(localized: "Circle deleted")) {
                                    try await directory.connection.deleteCircle(circle.id)
                                }
                                if isDeleted { dismiss() }
                            }
                        }
                    } message: {
                        Text("The circle is removed for everyone in it, with its calendar, its albums and its chat. What its members shared with it, and with anyone they then share no other circle with, ends and does not come back. No account is deleted.")
                    }
            }
            Button("Leave circle", systemImage: "rectangle.portrait.and.arrow.right", role: .destructive) { isConfirmingLeave = true }
                .tint(.red)
                .actionColor(isDestructive: true)
                .confirmationDialog("Leave the circle \(circle.name)?", isPresented: $isConfirmingLeave, titleVisibility: .visible) {
                    Button("Leave circle", role: .destructive) {
                        Task {
                            let isLeft = await directory.act(saying: String(localized: "You left \(circle.name)")) {
                                try await directory.connection.leaveCircle(circle.id)
                            }
                            if isLeft { dismiss() }
                        }
                    }
                } message: {
                    Text("You will no longer be in \(circle.name). What you shared with it, and with anyone you then share no other circle with, ends and does not come back if you join again, and what it shared with you stops reaching you. An admin can invite you again. The circle is deleted when its last member leaves. If you are its only admin, make somebody else an admin first.")
                }
        }
    }
}

/// OpenCirclesLink is the way to Circles from a screen that has found nobody to
/// share with: it opens the same screen the Settings row does.
struct OpenCirclesLink: View {
    let connection: Connection

    var body: some View {
        NavigationLink {
            CirclesView(connection: connection)
        } label: {
            Label("Open Circles", systemImage: "person.2")
                .foregroundStyle(.primary)
        }
    }
}

/// MemberRow is somebody in a circle: their picture, what to call them, what
/// they are to the person, the username they sign in with when that is
/// different, and whether they are an admin.
private struct MemberRow: View {
    let member: CircleMember
    let isCurrentUser: Bool

    var body: some View {
        HStack(spacing: 12) {
            AvatarView(name: member.displayName, avatarUrl: member.avatarUrl, colorIndex: member.avatarColorIndex, size: 36)
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(verbatim: member.displayName)
                    if isCurrentUser {
                        Text("You").font(.caption).foregroundStyle(Color(.secondaryLabel))
                    }
                }
                if let relationshipText {
                    Text(verbatim: relationshipText).font(.footnote).foregroundStyle(Color(.secondaryLabel))
                }
                if member.displayName != member.username {
                    Text(verbatim: member.username).font(.footnote).foregroundStyle(Color(.secondaryLabel))
                }
                if member.isAdmin {
                    AdminTag()
                }
            }
        }
    }

    /// What they are to the person, such as "Father", when one of them has
    /// said; never for the person's own row.
    private var relationshipText: String? {
        guard !isCurrentUser, let word = RelationshipWords.word(of: member) else { return nil }
        return RelationshipWords.capitalized(word)
    }
}

/// PendingInvitationRow is an invitation nobody has answered: who it is to,
/// who sent it and when it runs out.
private struct PendingInvitationRow: View {
    let invitation: CircleInvitation

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: invitation.invitedUsername)
            if let expiresAt = ServerTime.date(invitation.expiresAt) {
                Text("Invited by \(invitation.invitedByName). Expires \(expiresAt.formatted(date: .abbreviated, time: .omitted)).")
                    .font(.footnote).foregroundStyle(Color(.secondaryLabel))
            } else {
                Text("Invited by \(invitation.invitedByName)").font(.footnote).foregroundStyle(Color(.secondaryLabel))
            }
            if invitation.isAdminInvitation {
                AdminTag()
            }
        }
    }
}

/// AdminTag marks an admin with words and a symbol, since a color alone would
/// say nothing to everybody.
private struct AdminTag: View {
    var body: some View {
        Label("Admin", systemImage: "person.badge.key")
            .labelStyle(.tag)
            .font(.caption)
            .foregroundStyle(Color(.secondaryLabel))
            .symbolRenderingMode(.hierarchical)
    }
}

// MARK: - The forms

/// CircleAccountForm makes an account for somebody and puts it in the circle
/// at once. The two are separate operations on the server (CreateUser, then
/// AddCircleMember), so an account that was made but could not be put in is
/// remembered, and trying again adds it instead of failing on a username that
/// is taken now.
private struct CircleAccountForm: View {
    let directory: CircleDirectory
    let circle: PeopleCircle
    @Environment(\.dismiss) private var dismiss
    @State private var draft = CircleAccountDraft()
    @State private var madeUsername: String?
    @State private var isBusy = false
    @State private var problemText: String?

    /// Whether the account was made and only putting it in the circle is left.
    private var isAccountMade: Bool { madeUsername != nil }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    // Once the account is made these are what it was made
                    // with: a retry adds that account to the circle and
                    // would ignore a change to them without a word.
                    TextField("Username", text: $draft.username)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .disabled(isAccountMade)
                    TextField("Name", text: $draft.name)
                        .textContentType(.name)
                        .disabled(isAccountMade)
                    SecureField("Password", text: $draft.password)
                        .textContentType(.newPassword)
                        .disabled(isAccountMade)
                    Toggle("Make them an admin", isOn: $draft.isAdmin)
                } footer: {
                    if isAccountMade {
                        Text("The account is made; adding it to the circle again")
                    } else {
                        Text("They sign in with that username and password, and can change the password themselves in their settings.")
                    }
                }
            }
            .navigationTitle("Make an account for someone")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Create") {
                        Task { await save() }
                    }
                    .disabled(!draft.canSubmit || isBusy)
                }
            }
            .disabled(isBusy)
            .toast(done: .constant(nil), problem: $problemText)
        }
    }

    private func save() async {
        isBusy = true
        defer { isBusy = false }
        let username = draft.trimmedUsername
        do {
            if madeUsername != username {
                _ = try await directory.connection.createAccessUser(draft.accountDraft)
                madeUsername = username
            }
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
            return
        }
        do {
            try await directory.connection.addCircleMember(circle.id, username: username, memberRole: draft.memberRole)
        } catch {
            // The account is there now; say so, so that it is not made a
            // second time.
            problemText = String(
                localized: "The account \(username) was made, but it could not be put in the circle (\(error.localizedDescription)). Try again to add it.")
            return
        }
        let name = draft.accountDraft.name
        directory.doneText = String(localized: "\(name.isEmpty ? username : name) has an account and is in the circle")
        await directory.load()
        dismiss()
    }
}

/// CircleRelationshipSheet says what one other member of a circle is to the
/// person: a picker of the words there are, in the app's language, and a line
/// saying what the other person will see. When the person has not said how
/// they are described and the other side has gendered words, it asks that too,
/// since "son" and "daughter" depend on it. Saving sets that first, then the
/// relationship, or takes it away for "None".
struct CircleRelationshipSheet: View {
    let directory: CircleDirectory
    let member: CircleMember
    @Environment(\.dismiss) private var dismiss
    @State private var draft: PersonRelationshipDraft?
    @State private var isBusy = false
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            Form {
                if let draft = Binding($draft) {
                    termSection(draft)
                    if draft.wrappedValue.isPronounAsked {
                        pronounSection(draft)
                    }
                }
            }
            .overlay {
                if draft == nil && problemText == nil {
                    ProgressView()
                }
            }
            .navigationTitle(member.displayName)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        Task { await save() }
                    }
                    .disabled(draft?.canSave != true || isBusy)
                }
            }
            .disabled(isBusy)
            .toast(done: .constant(nil), problem: $problemText)
        }
        .task { await load() }
    }

    private func termSection(_ draft: Binding<PersonRelationshipDraft>) -> some View {
        Section {
            Picker("Relationship", selection: draft.selectedTerm) {
                Text("None").tag("")
                ForEach(draft.wrappedValue.terms) { term in
                    Text(verbatim: RelationshipWords.capitalized(RelationshipWords.word(of: term))).tag(term.term)
                }
            }
            .pickerStyle(.menu)
        } footer: {
            if let footerText = seenAsText(draft.wrappedValue) {
                Text(verbatim: footerText)
            }
        }
    }

    /// What the other person will see the person as, worded by how the person
    /// is described, or not at all while no relationship is chosen.
    private func seenAsText(_ draft: PersonRelationshipDraft) -> String? {
        guard let oppositeKind = draft.oppositeKind else { return nil }
        let word = RelationshipWords.word(oppositeKind, draft.oppositeTermGender)
        return String(localized: "\(member.displayName) will see you as their \(word).")
    }

    private func pronounSection(_ draft: Binding<PersonRelationshipDraft>) -> some View {
        Section {
            Picker("How should people describe you?", selection: draft.pronoun) {
                ForEach(PersonPronoun.allCases, id: \.self) { pronoun in
                    Text(verbatim: pronoun.title).tag(pronoun)
                }
            }
            .pickerStyle(.inline)
            .labelsHidden()
        } header: {
            Text("How should people describe you?")
        } footer: {
            Text("Shown to the people who share a circle with you, as your name is. It words how you appear to them, such as son or daughter, and how your agent speaks of you.")
        }
    }

    private func load() async {
        do {
            let terms = try await directory.connection.relationshipTerms()
            draft = PersonRelationshipDraft(member: member, terms: terms, ownPronoun: directory.ownPronoun)
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
        }
    }

    private func save() async {
        guard let saving = draft else { return }
        isBusy = true
        defer { isBusy = false }
        do {
            try await directory.connection.saveRelationship(saving, ownUserId: directory.currentUserId ?? "")
            directory.doneText = saving.isRemoving ? String(localized: "Relationship removed") : String(localized: "Relationship saved")
            await directory.load()
            dismiss()
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
        }
    }
}

/// CircleForm is a new circle's name and kind, or an existing one's name. The
/// kind is chosen once, when the circle is made.
private struct CircleForm: View {
    let directory: CircleDirectory
    let editedCircle: PeopleCircle?
    @Environment(\.dismiss) private var dismiss
    @State private var draft = CircleDraft()
    @State private var isBusy = false
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Name", text: $draft.name)
                }
                if editedCircle == nil {
                    Section {
                        Picker("Kind", selection: $draft.circleKind) {
                            ForEach(CircleKind.allCases, id: \.self) { kind in
                                Text(kind.title).tag(kind)
                            }
                        }
                        .pickerStyle(.inline)
                        .labelsHidden()
                    } header: {
                        Text("Kind")
                    }
                }
            }
            .navigationTitle(editedCircle == nil ? Text("New circle") : Text("Rename circle"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(editedCircle == nil ? LocalizedStringKey("Create") : "Save") {
                        Task { await save() }
                    }
                    .disabled(!draft.canSubmit || isBusy)
                }
            }
            .disabled(isBusy)
            .toast(done: .constant(nil), problem: $problemText)
        }
        .onAppear {
            if let editedCircle { draft = CircleDraft(editedCircle) }
        }
    }

    private func save() async {
        isBusy = true
        defer { isBusy = false }
        do {
            if let editedCircle {
                try await directory.connection.renameCircle(editedCircle.id, name: draft.name)
                directory.doneText = String(localized: "Circle renamed")
            } else {
                _ = try await directory.connection.createCircle(draft)
                directory.doneText = String(localized: "Circle made")
            }
            await directory.load()
            dismiss()
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
        }
    }
}

// MARK: - Words

extension CircleKind {
    /// How the screens name a circle's kind.
    var title: String {
        switch self {
        case .family: String(localized: "Family")
        case .friends: String(localized: "Friends")
        case .colleagues: String(localized: "Colleagues")
        }
    }
}

/// CircleWords are the words the circles screens share.
enum CircleWords {
    static func memberCount(_ memberCount: Int) -> String {
        memberCount == 1 ? String(localized: "1 person") : String(localized: "\(memberCount) people")
    }
}
