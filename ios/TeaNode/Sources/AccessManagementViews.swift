import Observation
import SwiftUI
import TeaNodeKit

// The dashboard's people and access pages: the accounts, the groups they are
// in, the roles a group carries, and the log of every administrative change.
// Each top screen keeps its own AccessDirectory, and the screens it pushes
// read from the same one, so a change made on a detail screen shows on the
// list behind it.

/// AccessPage is which of the access pages a directory serves, which decides
/// what it reads and which refusals are worth saying.
enum AccessPage {
    case people
    case groups
    case roles
}

/// AccessDirectory is what the people, groups and roles pages read: who is
/// reading and what they may change, the accounts, the groups, the roles,
/// the permissions and the domains. It is read again after every change.
@MainActor
@Observable
final class AccessDirectory {
    let connection: Connection
    let page: AccessPage
    private(set) var session: AccessSession?
    private(set) var users: [AccessUser] = []
    /// Whether the server refused the accounts: somebody who may manage
    /// groups but not people sees the groups and not who is in them.
    private(set) var isUserListHidden = false
    private(set) var groups: [AccessGroup] = []
    private(set) var roles: [AccessRole] = []
    private(set) var domains: [AccessDomain] = []
    /// The circles the reader is in, which an account can be put in from the
    /// people list. Read for somebody who manages accounts, and empty when they
    /// cannot use circles.
    private(set) var circles: [PeopleCircle] = []
    private(set) var permissionCatalog: [AccessPermission] = []
    /// Whether the page's own list has been read once.
    private(set) var isLoaded = false
    private(set) var isLoading = false
    var isBusy = false
    var doneText: String?
    var problemText: String?

    init(connection: Connection, page: AccessPage) {
        self.connection = connection
        self.page = page
    }

    var currentUserId: String? { session?.userId }
    var canManageUsers: Bool { session?.canManageUsers ?? false }
    var canManageGroups: Bool { session?.canManageGroups ?? false }
    var canManageRoles: Bool { session?.canManageRoles ?? false }
    /// Who is in a group may be changed by somebody who manages people as
    /// well as by somebody who manages groups.
    var canChangeMembers: Bool { canManageUsers || canManageGroups }

    func user(_ userId: String) -> AccessUser? { users.first { $0.id == userId } }
    func group(_ groupId: String) -> AccessGroup? { groups.first { $0.id == groupId } }
    func role(_ roleId: String) -> AccessRole? { roles.first { $0.id == roleId } }

    /// Reads what the page needs. The page's own list is the one whose
    /// refusal is said; the others are read when the person may read them
    /// and left empty when not.
    func load() async {
        isLoading = true
        defer { isLoading = false }
        let connection = self.connection
        if let session = try? await connection.accessSession() {
            self.session = session
        }
        switch page {
        case .people, .groups:
            // Read side by side; only the groups are the page's own list.
            async let usersRead = try? connection.accessUsers()
            async let rolesRead = try? connection.accessRoles()
            async let domainsRead = try? connection.accessDomains()
            do {
                groups = try await connection.accessGroups()
                isLoaded = true
            } catch {
                problemText = error.localizedDescription
            }
            if let readUsers = await usersRead {
                users = readUsers
                isUserListHidden = false
            } else {
                users = []
                isUserListHidden = true
            }
            roles = await rolesRead ?? []
            domains = await domainsRead ?? []
            if page == .people, canManageUsers {
                circles = (try? await connection.circles()) ?? []
            } else {
                circles = []
            }
        case .roles:
            do {
                async let rolesRead = connection.accessRoles()
                async let catalogRead = connection.accessPermissions()
                let readRoles = try await rolesRead
                let readCatalog = try await catalogRead
                roles = readRoles
                permissionCatalog = readCatalog
                isLoaded = true
            } catch {
                problemText = error.localizedDescription
            }
        }
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
            problemText = error.localizedDescription
            await load()
            return false
        }
    }
}

// MARK: - People

/// PeopleView is the dashboard's Users page: everyone with an account, what
/// they are called and whether they may sign in, each opening to what they
/// are given. People are made, edited, disabled and removed here; which
/// groups they are in is set on the groups.
struct PeopleView: View {
    let connection: Connection
    @State private var directory: AccessDirectory
    @State private var searchText = ""
    @State private var isAdding = false
    @State private var editing: AccessUser?
    @State private var removing: AccessUser?

    init(connection: Connection) {
        self.connection = connection
        _directory = State(initialValue: AccessDirectory(connection: connection, page: .people))
    }

    private var shownUsers: [AccessUser] { directory.users.filter { $0.matches(searchText) } }

    /// Whether the server still has its default group, which a new person
    /// joins; without it they join none and can read nothing.
    private var hasMembersGroup: Bool { directory.groups.contains { $0.name.lowercased() == "members" } }

    var body: some View {
        List {
            if directory.isLoaded {
                Section {
                    if directory.isUserListHidden {
                        Text("You may see the groups on this server but not the accounts in them.").foregroundStyle(.secondary)
                    } else if directory.users.isEmpty {
                        Text("No accounts yet.").foregroundStyle(.secondary)
                    }
                    ForEach(shownUsers) { user in
                        NavigationLink {
                            PersonDetailView(directory: directory, userId: user.id)
                        } label: {
                            PersonRow(user: user, isCurrentUser: user.id == directory.currentUserId)
                        }
                        .confirmationDialog(
                            "Remove this person?", isPresented: Binding(get: { removing?.id == user.id }, set: { if !$0 { removing = nil } }),
                            titleVisibility: .visible, presenting: removing
                        ) { user in
                            Button("Remove \(user.username)", role: .destructive) {
                                Task {
                                    await directory.act(saying: String(localized: "Removed")) { try await directory.connection.deleteAccessUser(user.id) }
                                }
                            }
                        } message: { _ in
                            Text("Their sessions, tokens, passkeys and memberships go with them. Mail they received stays.")
                        }
                        .swipeActions { rowActions(user) }
                        .contextMenu {
                            if directory.canManageUsers { AddToCircleMenu(user: user, directory: directory) }
                        }
                    }
                } footer: {
                    if hasMembersGroup {
                        Text("Everyone with an account on this server. A person is put into Members unless told otherwise, so they can read the mailbox they are given.")
                    } else {
                        Text("Everyone with an account on this server. There is no Members group, so a new person joins no group and can sign in and read nothing until they are put in one.")
                    }
                }
            }
        }
        .navigationTitle("People")
        .searchable(text: $searchText, prompt: "Name, username or address")
        .overlay {
            if !directory.isLoaded && directory.isLoading {
                ProgressView()
            } else if directory.isLoaded && !directory.users.isEmpty && shownUsers.isEmpty {
                ContentUnavailableView.search(text: searchText)
            }
        }
        .toolbar {
            if directory.canManageUsers {
                ToolbarItem(placement: .primaryAction) {
                    Button("New user", systemImage: "plus") { isAdding = true }
                }
            }
        }
        .disabled(directory.isBusy)
        .refreshable { await directory.load() }
        .task { await directory.load() }
        .sheet(isPresented: $isAdding) {
            PersonForm(directory: directory, editedUser: nil, hasMembersGroup: hasMembersGroup)
        }
        .sheet(item: $editing) { user in
            PersonForm(directory: directory, editedUser: user, hasMembersGroup: hasMembersGroup)
        }
        .toast(done: Bindable(directory).doneText, problem: Bindable(directory).problemText)
    }

    @ViewBuilder
    private func rowActions(_ user: AccessUser) -> some View {
        if directory.canManageUsers {
            // Not oneself: the server refuses both, since nobody would be
            // left to undo it from the same screen.
            if user.id != directory.currentUserId {
                Button("Remove", systemImage: "trash", role: .destructive) { removing = user }
                Button(user.isDisabled ? LocalizedStringKey("Enable") : "Disable", systemImage: user.isDisabled ? "person.crop.circle.badge.checkmark" : "nosign") {
                    Task { await PersonActions.setDisabled(user, isDisabled: !user.isDisabled, in: directory) }
                }
                .tint(.orange)
            }
            Button("Edit", systemImage: "pencil") { editing = user }
                .tint(.indigo)
        }
    }
}

/// AddToCircleMenu puts an existing account in one of the circles the reader is
/// in, with no invitation to accept: the other order of making somebody's way
/// into a circle, for the operator who knows the account is there already.
private struct AddToCircleMenu: View {
    let user: AccessUser
    let directory: AccessDirectory

    var body: some View {
        Menu {
            if directory.circles.isEmpty {
                Button("You are not in a circle yet. Make one under Circles.") {}
                    .disabled(true)
            }
            ForEach(directory.circles) { circle in
                Button(circle.name) {
                    Task { await PersonActions.addToCircle(user, circle, in: directory) }
                }
            }
        } label: {
            Label("Add to a circle", systemImage: "person.2")
        }
    }
}

/// PersonActions are the changes to a person that more than one screen
/// makes.
@MainActor
private enum PersonActions {
    static func addToCircle(_ user: AccessUser, _ circle: PeopleCircle, in directory: AccessDirectory) async {
        await directory.act(saying: String(localized: "\(user.username) is now in \(circle.name).")) {
            try await directory.connection.addCircleMember(circle.id, username: user.username, memberRole: .member)
        }
    }

    static func setDisabled(_ user: AccessUser, isDisabled: Bool, in directory: AccessDirectory) async {
        await directory.act(saying: isDisabled ? String(localized: "Disabled") : String(localized: "Enabled")) {
            _ = try await directory.connection.setAccessUserDisabled(user.id, isDisabled: isDisabled)
        }
    }
}

/// PersonRow is a person in a line or three: what to call them, the
/// username they sign in with when that is different, whether they cannot
/// sign in or have no password, and their notification address.
private struct PersonRow: View {
    let user: AccessUser
    let isCurrentUser: Bool

    var body: some View {
        HStack(spacing: 12) {
            AvatarView(name: user.displayName, avatarUrl: user.avatarUrl, colorIndex: user.avatarColorIndex, size: 36)
            details
        }
    }

    private var details: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 6) {
                Text(verbatim: user.displayName)
                    .foregroundStyle(user.isDisabled ? Color(.secondaryLabel) : Color(.label))
                if isCurrentUser {
                    Text("You").font(.caption).foregroundStyle(Color(.secondaryLabel))
                }
            }
            if user.displayName != user.username {
                Text(verbatim: user.username).font(.footnote).foregroundStyle(Color(.secondaryLabel))
            }
            if user.isDisabled {
                AccessTag(text: String(localized: "Disabled"), systemImage: "nosign", style: .red)
            } else if !user.hasPassword {
                AccessTag(text: String(localized: "No password"), systemImage: "key", style: .orange)
            }
            if let email = user.email, !email.isEmpty {
                Text(verbatim: email).font(.footnote).foregroundStyle(Color(.secondaryLabel))
            }
        }
    }
}

/// PersonDetailView is one person: their account, whether they may sign in,
/// the groups they are in, and the roles and domains those groups give
/// them. Their password is set and the person removed from here.
struct PersonDetailView: View {
    let directory: AccessDirectory
    let userId: String
    @Environment(\.dismiss) private var dismiss
    @State private var isEditing = false
    @State private var isSettingPassword = false
    @State private var isRemoving = false

    private var user: AccessUser? { directory.user(userId) }
    private var isCurrentUser: Bool { userId == directory.currentUserId }

    var body: some View {
        List {
            if let user {
                accountSection(user)
                if directory.canManageUsers && !isCurrentUser {
                    Section {
                        Toggle(
                            "Disabled",
                            isOn: Binding(
                                get: { user.isDisabled },
                                set: { isDisabled in Task { await PersonActions.setDisabled(user, isDisabled: isDisabled, in: directory) } })
                        )
                    } footer: {
                        Text("A disabled person cannot sign in. Their mail and memberships are kept.")
                    }
                }
                groupsSection(user)
                givenSections(user)
                if directory.canManageUsers {
                    Section {
                        Button("Set password", systemImage: "key") { isSettingPassword = true }
                        AddToCircleMenu(user: user, directory: directory)
                        if !isCurrentUser {
                            Button("Remove this person", systemImage: "trash", role: .destructive) { isRemoving = true }
                                .confirmationDialog("Remove \(user.username)?", isPresented: $isRemoving, titleVisibility: .visible) {
                                    Button("Remove", role: .destructive) {
                                        Task {
                                            let isRemoved = await directory.act(saying: String(localized: "Removed")) {
                                                try await directory.connection.deleteAccessUser(userId)
                                            }
                                            if isRemoved { dismiss() }
                                        }
                                    }
                                } message: {
                                    Text("Their sessions, tokens, passkeys and memberships go with them. Mail they received stays.")
                                }
                        }
                    }
                }
            } else if directory.isLoaded {
                ContentUnavailableView("This person is no longer here", systemImage: "person.crop.circle.badge.questionmark")
            }
        }
        .navigationTitle(user?.displayName ?? "")
        .toolbar {
            if directory.canManageUsers, user != nil {
                ToolbarItem(placement: .primaryAction) {
                    Button("Edit") { isEditing = true }
                }
            }
        }
        .disabled(directory.isBusy)
        .refreshable { await directory.load() }
        .sheet(isPresented: $isEditing) {
            if let user {
                PersonForm(directory: directory, editedUser: user, hasMembersGroup: true)
            }
        }
        .sheet(isPresented: $isSettingPassword) {
            if let user {
                PasswordForm(directory: directory, user: user)
            }
        }
        .toast(done: Bindable(directory).doneText, problem: Bindable(directory).problemText)
    }

    private func accountSection(_ user: AccessUser) -> some View {
        Section {
            LabeledContent("Username") { Text(verbatim: user.username) }
            LabeledContent("Name") { optionalText(user.name) }
            LabeledContent("Notification email") { optionalText(user.email) }
            LabeledContent("Password") {
                Text(user.hasPassword ? LocalizedStringKey("Set") : "No password")
            }
            if let createdAt = ServerTime.date(user.createdAt) {
                LabeledContent("Created") { Text(createdAt.formatted(date: .abbreviated, time: .shortened)) }
            }
            if isCurrentUser {
                Text("This is you.").foregroundStyle(.secondary)
            }
        }
    }

    /// A value the person may not have given, said as not set when empty.
    @ViewBuilder
    private func optionalText(_ text: String?) -> some View {
        if let text, !text.isEmpty {
            Text(verbatim: text)
        } else {
            Text("Not set")
        }
    }

    private func groupsSection(_ user: AccessUser) -> some View {
        Section {
            let memberships = directory.groups.filter { user.groupIds.contains($0.id) }
            if memberships.isEmpty {
                Text("In no group, so they can sign in and read nothing.").foregroundStyle(.secondary)
            }
            ForEach(memberships) { group in
                NavigationLink {
                    GroupDetailView(directory: directory, groupId: group.id)
                } label: {
                    Text(verbatim: group.name)
                }
            }
        } header: {
            Text("Groups")
        } footer: {
            Text("Which groups somebody is in is set on the group, where the roles and domains it carries are.")
        }
    }

    /// The roles and domains the person's groups give them, each with the
    /// groups it comes through.
    @ViewBuilder
    private func givenSections(_ user: AccessUser) -> some View {
        let memberships = directory.groups.filter { user.groupIds.contains($0.id) }
        let givenRoles = directory.roles.filter { role in memberships.contains { $0.roleIds.contains(role.id) } }
        let givenDomains = directory.domains.filter { domain in memberships.contains { $0.domainIds.contains(domain.id) } }
        if !givenRoles.isEmpty {
            Section {
                ForEach(givenRoles) { role in
                    VStack(alignment: .leading, spacing: 3) {
                        Text(verbatim: role.name)
                        Text("Through \(Self.names(memberships.filter { $0.roleIds.contains(role.id) }))")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
            } header: {
                Text("Roles they are given")
            } footer: {
                Text("Everyone in a group holds its roles: over its domains for the domain permissions, and everywhere for the rest.")
            }
        }
        if !givenDomains.isEmpty {
            Section("Domains they are given") {
                ForEach(givenDomains) { domain in
                    VStack(alignment: .leading, spacing: 3) {
                        Text(verbatim: domain.domain)
                        Text("Through \(Self.names(memberships.filter { $0.domainIds.contains(domain.id) }))")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        }
    }

    private static func names(_ groups: [AccessGroup]) -> String {
        groups.map(\.name).formatted(.list(type: .and))
    }
}

/// PersonForm makes a person or changes one: the username they sign in
/// with, a first password for a new one, what to call them and where their
/// notifications go.
private struct PersonForm: View {
    let directory: AccessDirectory
    let editedUser: AccessUser?
    let hasMembersGroup: Bool
    @Environment(\.dismiss) private var dismiss
    @State private var draft = AccessUserDraft()
    @State private var isBusy = false
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Username", text: $draft.username)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                }
                if editedUser == nil {
                    Section {
                        SecureField("Password", text: $draft.password)
                    } footer: {
                        Text("Leave it empty for somebody who will sign in through an identity provider or with a passkey.")
                    }
                }
                Section {
                    TextField("Name", text: $draft.name)
                        .textContentType(.name)
                    TextField("Notification email", text: $draft.email)
                        .keyboardType(.emailAddress)
                        .textContentType(.emailAddress)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                } footer: {
                    if editedUser == nil && !hasMembersGroup {
                        Text("There is no Members group, so this person joins no group and can read nothing until they are put in one.")
                    } else {
                        Text("Which groups somebody is in is set on the group, where the roles and domains it carries are.")
                    }
                }
            }
            .navigationTitle(editedUser.map { Text("Edit \($0.username)") } ?? Text("New user"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(editedUser == nil ? LocalizedStringKey("Create") : "Save") {
                        Task { await save() }
                    }
                    .disabled(!draft.canSubmit || isBusy)
                }
            }
            .disabled(isBusy)
            .toast(done: .constant(nil), problem: $problemText)
        }
        .onAppear {
            if let editedUser { draft = AccessUserDraft(editedUser) }
        }
    }

    private func save() async {
        isBusy = true
        defer { isBusy = false }
        do {
            if let editedUser {
                _ = try await directory.connection.updateAccessUser(editedUser.id, draft: draft)
                directory.doneText = String(localized: "Saved")
            } else {
                let created = try await directory.connection.createAccessUser(draft)
                directory.doneText = String(localized: "Created \(created.username)")
            }
            await directory.load()
            dismiss()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// PasswordForm sets somebody's password. The server signs them out
/// everywhere when it does, taking the account back from whoever had it.
private struct PasswordForm: View {
    let directory: AccessDirectory
    let user: AccessUser
    @Environment(\.dismiss) private var dismiss
    @State private var password = ""
    @State private var isBusy = false
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    SecureField("New password", text: $password)
                } footer: {
                    Text("They are signed out everywhere, and their API tokens stop working.")
                }
            }
            .navigationTitle(Text("Set a password for \(user.username)"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        Task { await save() }
                    }
                    .disabled(password.isEmpty || isBusy)
                }
            }
            .disabled(isBusy)
            .toast(done: .constant(nil), problem: $problemText)
        }
    }

    private func save() async {
        isBusy = true
        defer { isBusy = false }
        do {
            try await directory.connection.setAccessUserPassword(user.id, password: password)
            directory.doneText = String(localized: "Password set")
            await directory.load()
            dismiss()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

// MARK: - Groups

/// GroupsView is the dashboard's Groups page: every group with how many are
/// in it, each opening to its members, the roles it carries and its domains.
struct GroupsView: View {
    let connection: Connection
    @State private var directory: AccessDirectory
    @State private var isAdding = false
    @State private var editing: AccessGroup?
    @State private var removing: AccessGroup?
    /// The group just made, opened so its members and roles can be set.
    @State private var openedGroupId: String?

    init(connection: Connection) {
        self.connection = connection
        _directory = State(initialValue: AccessDirectory(connection: connection, page: .groups))
    }

    var body: some View {
        List {
            if directory.isLoaded {
                Section {
                    if directory.groups.isEmpty {
                        Text("No groups yet.").foregroundStyle(.secondary)
                    }
                    ForEach(directory.groups) { group in
                        NavigationLink {
                            GroupDetailView(directory: directory, groupId: group.id)
                        } label: {
                            GroupRow(group: group)
                        }
                        .confirmationDialog(
                            "Remove this group?", isPresented: Binding(get: { removing?.id == group.id }, set: { if !$0 { removing = nil } }),
                            titleVisibility: .visible, presenting: removing
                        ) { group in
                            Button("Remove \(group.name)", role: .destructive) {
                                Task {
                                    await directory.act(saying: String(localized: "Removed")) { try await directory.connection.deleteAccessGroup(group.id) }
                                }
                            }
                        } message: { _ in
                            Text("Its members keep their accounts and lose what the group gave them.")
                        }
                        .swipeActions {
                            if directory.canManageGroups {
                                Button("Remove", systemImage: "trash", role: .destructive) { removing = group }
                                Button("Edit", systemImage: "pencil") { editing = group }
                                    .tint(.indigo)
                            }
                        }
                    }
                } footer: {
                    Text("A group is the only thing a role or a domain is attached to: everyone in it holds its roles, over its domains for the domain permissions and everywhere for the rest.")
                }
            }
        }
        .navigationTitle("Groups")
        .overlay { if !directory.isLoaded && directory.isLoading { ProgressView() } }
        .toolbar {
            if directory.canManageGroups {
                ToolbarItem(placement: .primaryAction) {
                    Button("New group", systemImage: "plus") { isAdding = true }
                }
            }
        }
        .disabled(directory.isBusy)
        .refreshable { await directory.load() }
        .task { await directory.load() }
        .navigationDestination(item: $openedGroupId) { groupId in
            GroupDetailView(directory: directory, groupId: groupId)
        }
        .sheet(isPresented: $isAdding) {
            GroupForm(directory: directory, editedGroup: nil) { created in openedGroupId = created.id }
        }
        .sheet(item: $editing) { group in
            GroupForm(directory: directory, editedGroup: group) { _ in }
        }
        .toast(done: Bindable(directory).doneText, problem: Bindable(directory).problemText)
    }
}

/// GroupRow is a group in a line or three: its name, what it is for, and how
/// many are in it.
private struct GroupRow: View {
    let group: AccessGroup

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: group.name).foregroundStyle(Color(.label))
            if let description = group.description, !description.isEmpty {
                Text(verbatim: description).font(.footnote).foregroundStyle(Color(.secondaryLabel)).lineLimit(2)
            }
            Text(AccessWords.memberCount(group.userIds.count))
                .font(.caption)
                .foregroundStyle(Color(.secondaryLabel))
        }
    }
}

/// GroupDetailView is one group: who is in it, the roles it carries and the
/// domains they apply to. Every tick and every member taken out is saved as
/// it is made.
struct GroupDetailView: View {
    let directory: AccessDirectory
    let groupId: String
    @Environment(\.dismiss) private var dismiss
    @State private var isEditing = false
    @State private var isAddingMembers = false
    @State private var isRemoving = false

    private var group: AccessGroup? { directory.group(groupId) }

    var body: some View {
        List {
            if let group {
                if !(group.description ?? "").isEmpty || !(group.idpGroup ?? "").isEmpty {
                    Section {
                        if let description = group.description, !description.isEmpty {
                            Text(verbatim: description)
                        }
                        if let idpGroup = group.idpGroup, !idpGroup.isEmpty {
                            LabeledContent("Identity provider group") { Text(verbatim: idpGroup).monospaced() }
                        }
                    }
                }
                membersSection(group)
                if directory.canManageGroups {
                    rolesSection(group)
                    Section {
                        NavigationLink {
                            GroupDomainsView(directory: directory, groupId: groupId)
                        } label: {
                            LabeledContent("Domains") {
                                Text(group.domainIds.count, format: .number)
                            }
                        }
                    } footer: {
                        Text("Where the domain permissions of these roles apply. Permissions over every domain do not need a list.")
                    }
                    Section {
                        Button("Remove this group", systemImage: "trash", role: .destructive) { isRemoving = true }
                            .confirmationDialog("Remove the group \(group.name)?", isPresented: $isRemoving, titleVisibility: .visible) {
                                Button("Remove", role: .destructive) {
                                    Task {
                                        let isRemoved = await directory.act(saying: String(localized: "Removed")) {
                                            try await directory.connection.deleteAccessGroup(groupId)
                                        }
                                        if isRemoved { dismiss() }
                                    }
                                }
                            } message: {
                                Text("Its members keep their accounts and lose what the group gave them.")
                            }
                    }
                }
            } else if directory.isLoaded {
                ContentUnavailableView("This group is no longer here", systemImage: "person.3")
            }
        }
        .navigationTitle(group?.name ?? "")
        .toolbar {
            if directory.canManageGroups, group != nil {
                ToolbarItem(placement: .primaryAction) {
                    Button("Edit") { isEditing = true }
                }
            }
        }
        .disabled(directory.isBusy)
        .refreshable { await directory.load() }
        .sheet(isPresented: $isEditing) {
            if let group {
                GroupForm(directory: directory, editedGroup: group) { _ in }
            }
        }
        .sheet(isPresented: $isAddingMembers) {
            if let group {
                MemberPicker(directory: directory, group: group)
            }
        }
        .toast(done: Bindable(directory).doneText, problem: Bindable(directory).problemText)
    }

    private func membersSection(_ group: AccessGroup) -> some View {
        Section {
            let members = directory.users.filter { group.userIds.contains($0.id) }
            if directory.isUserListHidden {
                Text("You may see the groups on this server but not the accounts in them.").foregroundStyle(.secondary)
            } else if members.isEmpty {
                Text("Nobody is in this group yet.").foregroundStyle(.secondary)
            }
            ForEach(members) { user in
                VStack(alignment: .leading, spacing: 3) {
                    Text(verbatim: user.username).foregroundStyle(Color(.label))
                    let details = [user.name ?? "", user.email ?? ""].filter { !$0.isEmpty }
                    if !details.isEmpty {
                        Text(verbatim: details.joined(separator: " · ")).font(.footnote).foregroundStyle(Color(.secondaryLabel))
                    }
                }
                .swipeActions {
                    if directory.canChangeMembers {
                        Button("Take out of the group", systemImage: "person.badge.minus", role: .destructive) {
                            Task {
                                await directory.act(saying: String(localized: "Taken out of \(group.name)")) {
                                    _ = try await directory.connection.setAccessGroupMembers(
                                        group.id, userIds: group.userIds.filter { $0 != user.id })
                                }
                            }
                        }
                    }
                }
            }
            if directory.canChangeMembers && !directory.isUserListHidden {
                Button("Add members", systemImage: "person.badge.plus") { isAddingMembers = true }
            }
        } header: {
            Text(AccessWords.memberCount(group.userIds.count))
        }
    }

    private func rolesSection(_ group: AccessGroup) -> some View {
        Section {
            if directory.roles.isEmpty {
                Text("No roles yet.").foregroundStyle(.secondary)
            }
            ForEach(directory.roles) { role in
                let isCarried = group.roleIds.contains(role.id)
                Button {
                    let roleIds = isCarried ? group.roleIds.filter { $0 != role.id } : group.roleIds + [role.id]
                    Task {
                        await directory.act(saying: String(localized: "Saved")) {
                            _ = try await directory.connection.setAccessGroupRoles(group.id, roleIds: roleIds)
                        }
                    }
                } label: {
                    CheckRow(title: role.name, subtitle: role.description, isChecked: isCarried)
                }
            }
        } header: {
            Text("Roles")
        }
    }
}

/// GroupDomainsView ticks the domains a group's domain permissions apply
/// to, with a search, since a server can have many. What was ticked when it
/// opened stays at the top, so a row does not jump away as it is ticked.
private struct GroupDomainsView: View {
    let directory: AccessDirectory
    let groupId: String
    @State private var searchText = ""
    @State private var settledDomainIds: Set<String> = []

    private var group: AccessGroup? { directory.group(groupId) }

    private var shownDomains: [AccessDomain] {
        let wanted = searchText.trimmed.lowercased()
        let matching = directory.domains.filter { wanted.isEmpty || $0.domain.lowercased().contains(wanted) }
        return matching.filter { settledDomainIds.contains($0.id) } + matching.filter { !settledDomainIds.contains($0.id) }
    }

    var body: some View {
        List {
            if let group {
                Section {
                    if directory.domains.isEmpty {
                        Text("No domains yet.").foregroundStyle(.secondary)
                    }
                    ForEach(shownDomains) { domain in
                        let isChosen = group.domainIds.contains(domain.id)
                        Button {
                            let domainIds = isChosen ? group.domainIds.filter { $0 != domain.id } : group.domainIds + [domain.id]
                            Task {
                                await directory.act(saying: String(localized: "Saved")) {
                                    _ = try await directory.connection.setAccessGroupDomains(group.id, domainIds: domainIds)
                                }
                            }
                        } label: {
                            CheckRow(title: domain.domain, subtitle: nil, isChecked: isChosen)
                        }
                    }
                } header: {
                    Text(AccessWords.chosenCount(group.domainIds.count))
                } footer: {
                    Text("Where the domain permissions of these roles apply. Permissions over every domain do not need a list.")
                }
            }
        }
        .navigationTitle("Domains")
        .searchable(text: $searchText, prompt: "Search")
        .overlay {
            if !directory.domains.isEmpty && shownDomains.isEmpty {
                ContentUnavailableView.search(text: searchText)
            }
        }
        .disabled(directory.isBusy)
        .refreshable { await directory.load() }
        .onAppear { settledDomainIds = Set(group?.domainIds ?? []) }
        .toast(done: Bindable(directory).doneText, problem: Bindable(directory).problemText)
    }
}

/// MemberPicker chooses who is in a group from everyone with an account,
/// with a search, and saves the choice in one go. Who was in the group when
/// it opened stays at the top.
private struct MemberPicker: View {
    let directory: AccessDirectory
    let group: AccessGroup
    @Environment(\.dismiss) private var dismiss
    @State private var chosenUserIds: [String] = []
    @State private var settledUserIds: Set<String> = []
    @State private var searchText = ""
    @State private var isBusy = false
    @State private var problemText: String?

    private var shownUsers: [AccessUser] {
        let matching = directory.users.filter { $0.matches(searchText) }
        return matching.filter { settledUserIds.contains($0.id) } + matching.filter { !settledUserIds.contains($0.id) }
    }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    if directory.users.isEmpty {
                        Text("No accounts yet.").foregroundStyle(.secondary)
                    }
                    ForEach(shownUsers) { user in
                        let isChosen = chosenUserIds.contains(user.id)
                        Button {
                            if isChosen {
                                chosenUserIds.removeAll { $0 == user.id }
                            } else {
                                chosenUserIds.append(user.id)
                            }
                        } label: {
                            CheckRow(
                                title: user.username, subtitle: user.displayName == user.username ? nil : user.displayName, isChecked: isChosen)
                        }
                    }
                } header: {
                    Text(AccessWords.chosenCount(chosenUserIds.count))
                }
            }
            .navigationTitle(Text("Who is in \(group.name)"))
            .navigationBarTitleDisplayMode(.inline)
            .searchable(text: $searchText, prompt: "Name, username or address")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        Task { await save() }
                    }
                    .disabled(isBusy || Set(chosenUserIds) == Set(group.userIds))
                }
            }
            .disabled(isBusy)
            .toast(done: .constant(nil), problem: $problemText)
        }
        .onAppear {
            chosenUserIds = group.userIds
            settledUserIds = Set(group.userIds)
        }
    }

    private func save() async {
        isBusy = true
        defer { isBusy = false }
        do {
            _ = try await directory.connection.setAccessGroupMembers(group.id, userIds: chosenUserIds)
            directory.doneText = String(localized: "Saved")
            await directory.load()
            dismiss()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// GroupForm makes a group or changes one: its name, what it is for, and
/// the group at the identity provider it follows.
private struct GroupForm: View {
    let directory: AccessDirectory
    let editedGroup: AccessGroup?
    let onCreated: (AccessGroup) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var draft = AccessGroupDraft()
    @State private var isBusy = false
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Name", text: $draft.name)
                    TextField("Description", text: $draft.description, axis: .vertical)
                        .lineLimit(1...4)
                }
                Section {
                    TextField("Identity provider group", text: $draft.idpGroup)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                } footer: {
                    Text("When set, signing in through the identity provider adds and removes people here to match that group.")
                }
            }
            .navigationTitle(editedGroup.map { Text("Edit \($0.name)") } ?? Text("New group"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(editedGroup == nil ? LocalizedStringKey("Create") : "Save") {
                        Task { await save() }
                    }
                    .disabled(!draft.canSubmit || isBusy)
                }
            }
            .disabled(isBusy)
            .toast(done: .constant(nil), problem: $problemText)
        }
        .onAppear {
            if let editedGroup { draft = AccessGroupDraft(editedGroup) }
        }
    }

    private func save() async {
        isBusy = true
        defer { isBusy = false }
        do {
            if let editedGroup {
                _ = try await directory.connection.updateAccessGroup(editedGroup.id, draft: draft)
                directory.doneText = String(localized: "Saved")
                await directory.load()
                dismiss()
            } else {
                let created = try await directory.connection.createAccessGroup(draft)
                directory.doneText = String(localized: "Created \(created.name)")
                await directory.load()
                dismiss()
                onCreated(created)
            }
        } catch {
            problemText = error.localizedDescription
        }
    }
}

// MARK: - Roles

/// RolesView is the dashboard's Roles page: the named sets of permissions,
/// each opening to the permissions it holds, ticked where they are.
struct RolesView: View {
    let connection: Connection
    @State private var directory: AccessDirectory
    @State private var isAdding = false
    @State private var editing: AccessRole?
    @State private var removing: AccessRole?
    /// The role just made, opened so its permissions can be ticked.
    @State private var openedRoleId: String?

    init(connection: Connection) {
        self.connection = connection
        _directory = State(initialValue: AccessDirectory(connection: connection, page: .roles))
    }

    var body: some View {
        List {
            if directory.isLoaded {
                Section {
                    if directory.roles.isEmpty {
                        Text("No roles yet.").foregroundStyle(.secondary)
                    }
                    ForEach(directory.roles) { role in
                        NavigationLink {
                            RoleDetailView(directory: directory, roleId: role.id)
                        } label: {
                            RoleRow(role: role)
                        }
                        .confirmationDialog(
                            "Remove this role?", isPresented: Binding(get: { removing?.id == role.id }, set: { if !$0 { removing = nil } }),
                            titleVisibility: .visible, presenting: removing
                        ) { role in
                            Button("Remove \(role.name)", role: .destructive) {
                                Task {
                                    await directory.act(saying: String(localized: "Removed")) { try await directory.connection.deleteAccessRole(role.id) }
                                }
                            }
                        } message: { _ in
                            Text("Every group that carried it loses it.")
                        }
                        .swipeActions {
                            if directory.canManageRoles {
                                Button("Remove", systemImage: "trash", role: .destructive) { removing = role }
                                Button("Edit", systemImage: "pencil") { editing = role }
                                    .tint(.indigo)
                            }
                        }
                    }
                } footer: {
                    Text("A role is a named set of permissions. The three seeded ones are ordinary roles: rename, edit or delete them like any other.")
                }
            }
        }
        .navigationTitle("Roles")
        .overlay { if !directory.isLoaded && directory.isLoading { ProgressView() } }
        .toolbar {
            if directory.canManageRoles {
                ToolbarItem(placement: .primaryAction) {
                    Button("New role", systemImage: "plus") { isAdding = true }
                }
            }
        }
        .disabled(directory.isBusy)
        .refreshable { await directory.load() }
        .task { await directory.load() }
        .navigationDestination(item: $openedRoleId) { roleId in
            RoleDetailView(directory: directory, roleId: roleId)
        }
        .sheet(isPresented: $isAdding) {
            RoleForm(directory: directory, editedRole: nil) { created in openedRoleId = created.id }
        }
        .sheet(item: $editing) { role in
            RoleForm(directory: directory, editedRole: role) { _ in }
        }
        .toast(done: Bindable(directory).doneText, problem: Bindable(directory).problemText)
    }
}

/// RoleRow is a role in a line or three: its name, what it is for, and how
/// many permissions it holds.
private struct RoleRow: View {
    let role: AccessRole

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: role.name).foregroundStyle(Color(.label))
            if let description = role.description, !description.isEmpty {
                Text(verbatim: description).font(.footnote).foregroundStyle(Color(.secondaryLabel)).lineLimit(2)
            }
            Text(AccessWords.permissionCount(role.permissions.count))
                .font(.caption)
                .foregroundStyle(Color(.secondaryLabel))
        }
    }
}

/// RoleDetailView is one role: what it is for, and every permission the
/// server knows in three lists by where it applies, ticked where the role
/// holds it. A tick is saved as it is made.
struct RoleDetailView: View {
    let directory: AccessDirectory
    let roleId: String
    @Environment(\.dismiss) private var dismiss
    @State private var isEditing = false
    @State private var isRemoving = false

    private var role: AccessRole? { directory.role(roleId) }

    var body: some View {
        List {
            if let role {
                Section("Description") {
                    if let description = role.description, !description.isEmpty {
                        Text(verbatim: description)
                    } else {
                        Text("No description.").foregroundStyle(.secondary)
                    }
                }
                ForEach(AccessPermissionKind.allCases, id: \.self) { permissionKind in
                    let permissions = directory.permissionCatalog.filter { $0.permissionKind == permissionKind }
                    if !permissions.isEmpty {
                        Section {
                            ForEach(permissions) { permission in
                                permissionRow(permission, of: role)
                            }
                        } header: {
                            Text(AccessWords.permissionKindTitle(permissionKind))
                        }
                    }
                }
                if directory.canManageRoles {
                    Section {
                        Button("Remove this role", systemImage: "trash", role: .destructive) { isRemoving = true }
                            .confirmationDialog("Remove the role \(role.name)?", isPresented: $isRemoving, titleVisibility: .visible) {
                                Button("Remove", role: .destructive) {
                                    Task {
                                        let isRemoved = await directory.act(saying: String(localized: "Removed")) {
                                            try await directory.connection.deleteAccessRole(roleId)
                                        }
                                        if isRemoved { dismiss() }
                                    }
                                }
                            } message: {
                                Text("Every group that carried it loses it.")
                            }
                    }
                }
            } else if directory.isLoaded {
                ContentUnavailableView("This role is no longer here", systemImage: "checklist")
            }
        }
        .navigationTitle(role?.name ?? "")
        .toolbar {
            if directory.canManageRoles, role != nil {
                ToolbarItem(placement: .primaryAction) {
                    Button("Edit") { isEditing = true }
                }
            }
        }
        .disabled(directory.isBusy)
        .refreshable { await directory.load() }
        .sheet(isPresented: $isEditing) {
            if let role {
                RoleForm(directory: directory, editedRole: role) { _ in }
            }
        }
        .toast(done: Bindable(directory).doneText, problem: Bindable(directory).problemText)
    }

    /// A permission by what it lets somebody do, with its key under it so
    /// the two cannot be confused; a button that ticks it for somebody who
    /// may change roles.
    @ViewBuilder
    private func permissionRow(_ permission: AccessPermission, of role: AccessRole) -> some View {
        let isHeld = role.permissions.contains(permission.key)
        let row = CheckRow(
            title: AccessWords.permissionLabel(permission.key), subtitle: permission.key, isChecked: isHeld, isSubtitleCode: true)
        if directory.canManageRoles {
            Button {
                let permissions = role.permissions(setting: permission.key, isHeld: !isHeld, catalog: directory.permissionCatalog)
                Task {
                    await directory.act(saying: String(localized: "Saved")) {
                        _ = try await directory.connection.setAccessRolePermissions(role.id, permissions: permissions)
                    }
                }
            } label: {
                row
            }
        } else {
            row
        }
    }
}

/// RoleForm makes a role or changes its name and description. A new role
/// holds no permissions; they are ticked on its page, which opens once it
/// is made.
private struct RoleForm: View {
    let directory: AccessDirectory
    let editedRole: AccessRole?
    let onCreated: (AccessRole) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var draft = AccessRoleDraft()
    @State private var isBusy = false
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            Form {
                TextField("Name", text: $draft.name)
                TextField("Description", text: $draft.description, axis: .vertical)
                    .lineLimit(1...4)
            }
            .navigationTitle(editedRole.map { Text("Edit \($0.name)") } ?? Text("New role"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(editedRole == nil ? LocalizedStringKey("Create") : "Save") {
                        Task { await save() }
                    }
                    .disabled(!draft.canSubmit || isBusy)
                }
            }
            .disabled(isBusy)
            .toast(done: .constant(nil), problem: $problemText)
        }
        .onAppear {
            if let editedRole { draft = AccessRoleDraft(editedRole) }
        }
    }

    private func save() async {
        isBusy = true
        defer { isBusy = false }
        do {
            if let editedRole {
                _ = try await directory.connection.updateAccessRole(editedRole.id, draft: draft)
                directory.doneText = String(localized: "Saved")
                await directory.load()
                dismiss()
            } else {
                let created = try await directory.connection.createAccessRole(draft)
                directory.doneText = String(localized: "Created \(created.name)")
                await directory.load()
                dismiss()
                onCreated(created)
            }
        } catch {
            problemText = error.localizedDescription
        }
    }
}

// MARK: - Audit log

/// AuditLogView is the dashboard's Audit page: every administrative change,
/// newest first, narrowed to one kind of thing from the toolbar, and read
/// further as it is scrolled. Each opens to who made it, from where, and a
/// table of the fields that changed.
struct AuditLogView: View {
    let connection: Connection
    /// The kind of thing the log is narrowed to; empty for everything.
    @State private var resourceType = ""
    @State private var events: [AuditEvent]?
    @State private var eventTotal = 0
    @State private var isLoading = false
    @State private var isLoadingMore = false
    @State private var doneText: String?
    @State private var problemText: String?

    static let pageSize = 50

    var body: some View {
        List {
            if let events {
                Section {
                    if !resourceType.isEmpty {
                        Label {
                            Text("Changes to \(resourceType)")
                        } icon: {
                            Image(systemName: "line.3.horizontal.decrease.circle")
                        }
                        Button("Show everything", systemImage: "xmark.circle") { resourceType = "" }
                    }
                    if events.isEmpty {
                        Text("Nothing has been changed yet.").foregroundStyle(.secondary)
                    }
                    ForEach(events) { event in
                        NavigationLink {
                            AuditEventDetailView(event: event)
                        } label: {
                            AuditEventRow(event: event)
                        }
                        .onAppear {
                            if event.id == events.last?.id { Task { await loadMore() } }
                        }
                    }
                    if isLoadingMore {
                        ProgressView().frame(maxWidth: .infinity)
                    }
                } header: {
                    Text(AccessWords.changeCount(eventTotal))
                } footer: {
                    Text("Every administrative change: who made it, from where, and the row before and after. What people do in their own mailboxes is not recorded.")
                }
            }
        }
        .navigationTitle("Audit")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Picker("Show changes to", selection: $resourceType) {
                        Text("Everything").tag("")
                        ForEach(AuditEvent.resourceTypes, id: \.self) { candidate in
                            Text(verbatim: candidate).tag(candidate)
                        }
                    }
                } label: {
                    Label(
                        "Show changes to",
                        systemImage: resourceType.isEmpty ? "line.3.horizontal.decrease.circle" : "line.3.horizontal.decrease.circle.fill")
                }
            }
        }
        .overlay { if events == nil && isLoading { ProgressView() } }
        .refreshable { await load() }
        .task(id: resourceType) { await load() }
        .onChange(of: resourceType) {
            // Another kind of thing starts again at the first page.
            events = nil
            eventTotal = 0
        }
        .toast(done: $doneText, problem: $problemText)
    }

    /// The first page, for the kind of thing chosen.
    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            let page = try await connection.auditEvents(resourceType: resourceType, first: Self.pageSize, offset: 0)
            events = page.events
            eventTotal = page.total
        } catch {
            problemText = error.localizedDescription
            if events == nil { events = [] }
        }
    }

    /// The next page, once the last row read so far comes into view.
    private func loadMore() async {
        guard let loaded = events, loaded.count < eventTotal, !isLoadingMore else { return }
        isLoadingMore = true
        defer { isLoadingMore = false }
        do {
            let page = try await connection.auditEvents(resourceType: resourceType, first: Self.pageSize, offset: loaded.count)
            let knownIds = Set(loaded.map(\.id))
            events = loaded + page.events.filter { !knownIds.contains($0.id) }
            eventTotal = page.total
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// AuditEventRow is one change in a line or three: who made it, what they
/// did to which thing, and when and from where.
private struct AuditEventRow: View {
    let event: AuditEvent

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 6) {
                Text(verbatim: event.actorName).foregroundStyle(Color(.label))
                if event.isByAgent {
                    Text("via their agent").font(.caption).foregroundStyle(Color(.secondaryLabel))
                }
            }
            Label {
                Text(verbatim: AccessWords.summary(event)).foregroundStyle(Color(.label))
            } icon: {
                Image(systemName: AccessWords.actionSymbol(event))
                    .foregroundStyle(event.auditAction == .delete ? Color.red : Color(.secondaryLabel))
            }
            .font(.subheadline)
            Text(verbatim: AccessWords.details(event))
                .font(.caption)
                .foregroundStyle(Color(.secondaryLabel))
        }
    }
}

/// AuditEventDetailView is one change in full: who made it and from where,
/// the thing it was made to, and the fields that differ as a table of the
/// field, before and after. The table scrolls sideways on a phone rather
/// than folding each row into lines.
struct AuditEventDetailView: View {
    let event: AuditEvent

    /// How wide a value may grow before it wraps, so one long value does
    /// not push the next column off the screen.
    static let valueWidth: CGFloat = 260

    var body: some View {
        List {
            Section {
                LabeledContent("Who") { Text(verbatim: event.actorName) }
                if event.isByAgent {
                    Text("Their agent made this change for them.").foregroundStyle(.secondary)
                }
                if let impersonatorLabel = event.impersonatorLabel, !impersonatorLabel.isEmpty {
                    LabeledContent("Signed in as them by") { Text(verbatim: impersonatorLabel) }
                }
                if let createdAt = ServerTime.date(event.createdAt) {
                    LabeledContent("When") { Text(createdAt.formatted(date: .abbreviated, time: .standard)) }
                }
                if let sourceIp = event.sourceIp, !sourceIp.isEmpty {
                    LabeledContent("From") { Text(verbatim: sourceIp).monospaced() }
                }
                if let instance = event.instance, !instance.isEmpty {
                    LabeledContent("Instance") { Text(verbatim: instance).monospaced() }
                }
            }
            Section {
                LabeledContent("Kind") { Text(verbatim: event.resourceType) }
                if let resourceLabel = event.resourceLabel, !resourceLabel.isEmpty {
                    LabeledContent("Name") { Text(verbatim: resourceLabel) }
                }
                VStack(alignment: .leading, spacing: 3) {
                    Text("ID").foregroundStyle(Color(.label))
                    Text(verbatim: event.resourceId)
                        .font(.footnote.monospaced())
                        .foregroundStyle(Color(.secondaryLabel))
                        .textSelection(.enabled)
                }
            } header: {
                Text(verbatim: AccessWords.summary(event))
            }
            Section("What changed") {
                let fields = event.changedFields
                if fields.isEmpty {
                    Text("Nothing in the row differs.").foregroundStyle(.secondary)
                } else {
                    ScrollView(.horizontal) {
                        Grid(alignment: .topLeading, horizontalSpacing: 16, verticalSpacing: 8) {
                            GridRow {
                                Text("Field")
                                Text("Before")
                                Text("After")
                            }
                            .font(.subheadline.bold())
                            .foregroundStyle(Color(.secondaryLabel))
                            Divider()
                            ForEach(fields, id: \.self) { field in
                                GridRow {
                                    Text(verbatim: field).font(.subheadline.monospaced())
                                    valueCell(AuditEvent.describe(event.beforeValue(field)))
                                    valueCell(AuditEvent.describe(event.afterValue(field)))
                                }
                            }
                        }
                        .padding(.vertical, 4)
                    }
                }
            }
        }
        .navigationTitle(Text(AccessWords.actionTitle(event)))
        .navigationBarTitleDisplayMode(.inline)
    }

    /// A value as a line of text, or "unset" said quietly.
    @ViewBuilder
    private func valueCell(_ text: String?) -> some View {
        Group {
            if let text {
                Text(verbatim: text).foregroundStyle(Color(.label))
            } else {
                Text("unset").foregroundStyle(Color(.secondaryLabel))
            }
        }
        .font(.subheadline)
        .textSelection(.enabled)
        .frame(maxWidth: Self.valueWidth, alignment: .leading)
    }
}

// MARK: - Shared pieces

/// CheckRow is a thing to tick: a role on a group, a domain, a person in a
/// group, a permission on a role. Its words are named colors, since a
/// button's label would otherwise take the tint; the tick is the tint.
private struct CheckRow: View {
    let title: String
    let subtitle: String?
    let isChecked: Bool
    var isSubtitleCode = false

    var body: some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: title).foregroundStyle(Color(.label))
                if let subtitle, !subtitle.isEmpty {
                    Text(verbatim: subtitle)
                        .font(isSubtitleCode ? .footnote.monospaced() : .footnote)
                        .foregroundStyle(Color(.secondaryLabel))
                }
            }
            Spacer(minLength: 0)
            Image(systemName: "checkmark")
                .fontWeight(.semibold)
                .foregroundStyle(.tint)
                .opacity(isChecked ? 1 : 0)
                .accessibilityHidden(true)
        }
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(isChecked ? .isSelected : [])
    }
}

/// AccessTag is the small line under a row's name that says what state it
/// is in, in words and a symbol as well as a color.
private struct AccessTag<Style: ShapeStyle>: View {
    let text: String
    let systemImage: String
    let style: Style

    var body: some View {
        Label(text, systemImage: systemImage)
            .labelStyle(.tag)
            .font(.caption)
            .foregroundStyle(style)
            .symbolRenderingMode(.hierarchical)
    }
}

/// AccessWords are the words the access screens share: counts, what a
/// permission lets somebody do, and how an audit event reads.
private enum AccessWords {
    static func memberCount(_ memberCount: Int) -> String {
        memberCount == 1 ? String(localized: "\(memberCount) member") : String(localized: "\(memberCount) members")
    }

    static func permissionCount(_ permissionCount: Int) -> String {
        permissionCount == 1 ? String(localized: "\(permissionCount) permission") : String(localized: "\(permissionCount) permissions")
    }

    static func chosenCount(_ chosenCount: Int) -> String {
        String(localized: "\(chosenCount) chosen")
    }

    static func changeCount(_ changeCount: Int) -> String {
        changeCount == 1 ? String(localized: "\(changeCount) change") : String(localized: "\(changeCount) changes")
    }

    static func permissionKindTitle(_ permissionKind: AccessPermissionKind) -> String {
        switch permissionKind {
        case .server: String(localized: "Server permissions, wherever the group applies")
        case .domain: String(localized: "Domain permissions, over the group's domains")
        case .allDomains: String(localized: "Permissions over every domain, present and future")
        }
    }

    /// What a permission lets somebody do; its key when the app has no
    /// words for it yet.
    static func permissionLabel(_ permissionKey: String) -> String {
        switch permissionKey {
        case "mail:read": String(localized: "Read own mailboxes")
        case "mail:write": String(localized: "Star, move and delete in own mailboxes")
        case "mail:send": String(localized: "Send as own addresses")
        case "mailbox:manage": String(localized: "Manage own folders, rules and addresses")
        case "mail:audit": String(localized: "See every message of the group's domains")
        case "mail:audit-all": String(localized: "See every message of every domain")
        case "domain:manage": String(localized: "Manage the group's domains")
        case "domain:manage-all": String(localized: "Manage every domain, and create new ones")
        case "queue:manage": String(localized: "Manage the delivery queue")
        case "report:read": String(localized: "Read DMARC reports")
        case "user:manage": String(localized: "Manage users")
        case "group:manage": String(localized: "Manage groups")
        case "role:manage": String(localized: "Manage roles")
        case "server:manage": String(localized: "Manage the server: settings, upgrades, certificates")
        case "audit:read": String(localized: "Read the audit log")
        case "agent:use": String(localized: "Have an agent, grant it own mailboxes, and talk to it")
        case "contacts:use": String(localized: "Keep an address book, and sync it to a device")
        case "calendar:use": String(localized: "Keep a calendar, and sync it to a device")
        case "circle:use": String(localized: "Make circles of people on this server, and join them")
        case "location:use": String(localized: "Record where you go, and share it")
        case "agent:audit": String(localized: "See every person's agent: what it may reach, its usage and limits, never its content")
        case "agent:act": String(localized: "Read every person's agent conversations and runs, and speak to their agent as them")
        default: permissionKey
        }
    }

    /// What was done to which thing, such as "Changed domain example.com".
    /// The thing is named when the server could name it, and by its id when
    /// not.
    static func summary(_ event: AuditEvent) -> String {
        let resourceName = (event.resourceLabel ?? "").isEmpty ? event.resourceId : event.resourceLabel ?? ""
        let resourceType = event.resourceType
        switch event.auditAction {
        case .create: return String(localized: "Created \(resourceType) \(resourceName)")
        case .update: return String(localized: "Changed \(resourceType) \(resourceName)")
        case .delete: return String(localized: "Removed \(resourceType) \(resourceName)")
        case nil: return "\(event.action) \(resourceType) \(resourceName)"
        }
    }

    static func actionTitle(_ event: AuditEvent) -> String {
        switch event.auditAction {
        case .create: String(localized: "Created")
        case .update: String(localized: "Changed")
        case .delete: String(localized: "Removed")
        case nil: event.action
        }
    }

    static func actionSymbol(_ event: AuditEvent) -> String {
        switch event.auditAction {
        case .create: "plus.circle"
        case .update: "pencil.circle"
        case .delete: "trash"
        case nil: "circle"
        }
    }

    /// When, from where and on which instance, and who was signed in as the
    /// actor when somebody was.
    static func details(_ event: AuditEvent) -> String {
        var parts: [String] = []
        if let createdAt = ServerTime.date(event.createdAt) {
            parts.append(createdAt.formatted(date: .abbreviated, time: .shortened))
        }
        if let sourceIp = event.sourceIp, !sourceIp.isEmpty { parts.append(sourceIp) }
        if let instance = event.instance, !instance.isEmpty { parts.append(instance) }
        if let impersonatorLabel = event.impersonatorLabel, !impersonatorLabel.isEmpty {
            parts.append(String(localized: "signed in as them by \(impersonatorLabel)"))
        }
        return parts.joined(separator: " · ")
    }
}
