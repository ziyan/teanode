import Foundation

/// AccessSession is what the access pages need to know about the person
/// reading them: who they are, so their own row is marked and kept from
/// being disabled or removed, and the server permissions they hold, which
/// decide what each page offers to change.
public struct AccessSession: Codable, Hashable, Sendable {
    /// AccessSessionPermissions is the part of the person's permissions the
    /// access pages read: the ones held everywhere.
    public struct AccessSessionPermissions: Codable, Hashable, Sendable {
        public let everywhere: [String]
    }

    public let userId: String?
    public let permissions: AccessSessionPermissions?
    /// The operator signed in as this account, when somebody is.
    public let impersonatorUsername: String?

    /// Whether the person holds this permission everywhere, such as
    /// user:manage.
    public func holds(_ permissionKey: String) -> Bool {
        permissions?.everywhere.contains(permissionKey) ?? false
    }

    public var canManageUsers: Bool { holds("user:manage") }
    public var canManageGroups: Bool { holds("group:manage") }
    public var canManageRoles: Bool { holds("role:manage") }
    public var canReadAudit: Bool { holds("audit:read") }
    /// Whether the person may keep a photo library and back a device up to it.
    public var canUsePhotos: Bool { holds("photos:use") }
}

/// AccessUser is somebody with an account on the server, as the people page
/// lists them.
public struct AccessUser: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    /// What they sign in with.
    public let username: String
    /// What to call them, when they have said.
    public let name: String?
    /// The address that receives notifications.
    public let email: String?
    /// When they were disabled, or nil while they may sign in.
    public let disabledAt: String?
    /// Whether they have a password at all; one without signs in with a
    /// passkey or through an identity provider.
    public let hasPassword: Bool
    public let groupIds: [String]
    public let createdAt: String

    public var isDisabled: Bool { !(disabledAt ?? "").isEmpty }

    /// What a list of people calls them: their name, or the username when
    /// they have none.
    public var displayName: String {
        guard let name, !name.isEmpty else { return username }
        return name
    }

    /// Whether a search for these words finds them, by username, name or
    /// address, ignoring case.
    public func matches(_ searchText: String) -> Bool {
        let wanted = searchText.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        guard !wanted.isEmpty else { return true }
        return [username, name ?? "", email ?? ""].contains { $0.lowercased().contains(wanted) }
    }
}

/// AccessUserDraft is a person as the new person and edit forms hold them.
public struct AccessUserDraft: Hashable, Sendable {
    public var username = ""
    /// Only for a new person; empty leaves them without one.
    public var password = ""
    public var name = ""
    public var email = ""

    public init() {}

    public init(_ user: AccessUser) {
        username = user.username
        name = user.name ?? ""
        email = user.email ?? ""
    }

    /// Whether the form can be sent: a username is the one thing needed.
    public var canSubmit: Bool { !username.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    /// As CreateUser takes it. No groups are named, so the server puts the
    /// person in its default group when it still has one.
    var createVariables: [String: JSONValue] {
        [
            "username": .string(username.trimmingCharacters(in: .whitespacesAndNewlines)),
            "password": password.isEmpty ? .null : .string(password),
            "name": .string(name), "email": .string(email),
        ]
    }

    /// As UpdateUser takes it; an empty name or address clears it.
    func updateVariables(userId: String) -> [String: JSONValue] {
        [
            "userId": .string(userId), "username": .string(username.trimmingCharacters(in: .whitespacesAndNewlines)),
            "name": .string(name), "email": .string(email),
        ]
    }
}

/// AccessGroup is a group of people: who is in it, the roles it carries, and
/// the domains those roles' domain permissions apply to.
public struct AccessGroup: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let name: String
    public let description: String?
    /// The group's name at the identity provider, which signing in through
    /// it keeps the members in step with.
    public let idpGroup: String?
    public let userIds: [String]
    public let roleIds: [String]
    public let domainIds: [String]
}

/// AccessGroupDraft is a group's name, description and identity provider
/// group as the group form holds them.
public struct AccessGroupDraft: Hashable, Sendable {
    public var name = ""
    public var description = ""
    public var idpGroup = ""

    public init() {}

    public init(_ group: AccessGroup) {
        name = group.name
        description = group.description ?? ""
        idpGroup = group.idpGroup ?? ""
    }

    public var canSubmit: Bool { !name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    var variables: [String: JSONValue] {
        [
            "name": .string(name.trimmingCharacters(in: .whitespacesAndNewlines)), "description": .string(description),
            "idpGroup": .string(idpGroup.trimmingCharacters(in: .whitespacesAndNewlines)),
        ]
    }
}

/// AccessPermissionKind is where a permission applies.
public enum AccessPermissionKind: String, CaseIterable, Sendable {
    /// On the server, wherever the group applies.
    case server
    /// Over the domains of the group carrying the role.
    case domain
    /// Over every domain, present and future.
    case allDomains = "all-domains"
}

/// AccessPermission is one permission the server knows and how far it
/// reaches.
public struct AccessPermission: Codable, Hashable, Sendable, Identifiable {
    /// Such as mail:read or user:manage.
    public let key: String
    /// One of server, domain or all-domains.
    public let kind: String
    /// For an all-domains permission, the domain permission it stands in
    /// for on every domain.
    public let widens: String?

    public var id: String { key }
    public var permissionKind: AccessPermissionKind? { AccessPermissionKind(rawValue: kind) }
}

/// AccessRole is a named set of permissions, carried by groups.
public struct AccessRole: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let name: String
    public let description: String?
    public let permissions: [String]

    /// The role's permissions with one ticked or unticked, in the order the
    /// server lists them. What the server does not list is dropped, as the
    /// dashboard does.
    public func permissions(setting permissionKey: String, isHeld: Bool, catalog: [AccessPermission]) -> [String] {
        catalog.map(\.key).filter { key in
            key == permissionKey ? isHeld : permissions.contains(key)
        }
    }
}

/// AccessRoleDraft is a role's name and description as the role form holds
/// them; its permissions are ticked on the role's own page.
public struct AccessRoleDraft: Hashable, Sendable {
    public var name = ""
    public var description = ""

    public init() {}

    public init(_ role: AccessRole) {
        name = role.name
        description = role.description ?? ""
    }

    public var canSubmit: Bool { !name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    var variables: [String: JSONValue] {
        ["name": .string(name.trimmingCharacters(in: .whitespacesAndNewlines)), "description": .string(description)]
    }
}

/// AccessDomain is a domain a group can be given, by its name.
public struct AccessDomain: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let domain: String
}

/// AuditAction is what an audit event did to the thing it is about.
public enum AuditAction: String, Sendable {
    case create
    case update
    case delete
}

/// AuditEvent is one administrative change: who made it, from where, what
/// it changed, and the row before and after.
public struct AuditEvent: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let createdAt: String
    /// user, agent, console and the like.
    public let actorKind: String
    public let actorLabel: String?
    /// The operator who was signed in as the actor, when one was.
    public let impersonatorLabel: String?
    public let sourceIp: String?
    /// The server instance that made the change.
    public let instance: String?
    /// user, group, role, domain and the like.
    public let resourceType: String
    public let resourceId: String
    /// What the thing is called now, resolved by the server.
    public let resourceLabel: String?
    /// Where the thing is on the dashboard.
    public let resourceLink: String?
    /// create, update or delete.
    public let action: String
    public let before: JSONValue?
    public let after: JSONValue?

    public var auditAction: AuditAction? { AuditAction(rawValue: action) }

    /// Who made the change: their name, or what kind of actor it was.
    public var actorName: String {
        guard let actorLabel, !actorLabel.isEmpty else { return actorKind }
        return actorLabel
    }

    /// Whether the person's agent made the change for them.
    public var isByAgent: Bool { actorKind == "agent" }

    /// The fields that differ between the two sides, by name: nearly always
    /// two or three of thirty.
    public var changedFields: [String] {
        let beforeFields = Self.fields(before)
        let afterFields = Self.fields(after)
        return Set(beforeFields.keys).union(afterFields.keys).filter { beforeFields[$0] != afterFields[$0] }.sorted()
    }

    /// What one side held in a field.
    public func beforeValue(_ field: String) -> JSONValue? { Self.fields(before)[field] }
    public func afterValue(_ field: String) -> JSONValue? { Self.fields(after)[field] }

    private static func fields(_ side: JSONValue?) -> [String: JSONValue] {
        if case .object(let fields) = side { return fields }
        return [:]
    }

    /// A value as a line of text: a string as it is, anything else as JSON,
    /// and nil for a value that is missing, null or empty.
    public static func describe(_ fieldValue: JSONValue?) -> String? {
        switch fieldValue {
        case .none, .null: return nil
        case .string(let text): return text.isEmpty ? nil : text
        case .some(let other):
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
            guard let encoded = try? encoder.encode(other) else { return nil }
            return String(decoding: encoded, as: UTF8.self)
        }
    }

    /// The kinds of thing the log can be narrowed to, as the dashboard
    /// offers them.
    public static let resourceTypes = [
        "user", "group", "role", "domain", "alias", "credential", "mailbox", "mailbox_address", "mailbox_app_password", "token",
        "passkey", "configuration", "agent", "addressbook",
    ]
}

/// AuditEventPage is one page of the audit log, with how many events there
/// are in all.
public struct AuditEventPage: Codable, Hashable, Sendable {
    public let events: [AuditEvent]
    public let total: Int
}

extension Documents {
    /// What every read and change of a person answers with.
    static let accessUserFields = """
        id username name email disabledAt hasPassword groupIds createdAt
        """
    /// What every read and change of a group answers with.
    static let accessGroupFields = """
        id name description idpGroup userIds roleIds domainIds
        """
    /// What every read and change of a role answers with.
    static let accessRoleFields = """
        id name description permissions
        """
    public static let accessSession = """
        query { GetSession { userId permissions { everywhere } impersonatorUsername } }
        """
    public static let accessUsers = """
        query { ListUsers { \(accessUserFields) } }
        """
    public static let createAccessUser = """
        mutation ($username: String!, $password: String, $name: String, $email: String) {
          CreateUser(username: $username, password: $password, name: $name, email: $email) { \(accessUserFields) }
        }
        """
    public static let updateAccessUser = """
        mutation ($userId: String!, $username: String, $name: String, $email: String, $disabled: Boolean) {
          UpdateUser(userId: $userId, username: $username, name: $name, email: $email, disabled: $disabled) { \(accessUserFields) }
        }
        """
    public static let setAccessUserPassword = """
        mutation ($userId: String!, $password: String!) { SetUserPassword(userId: $userId, password: $password) { id } }
        """
    public static let deleteAccessUser = """
        mutation ($userId: String!) { DeleteUser(userId: $userId) }
        """
    public static let accessGroups = """
        query { ListGroups { \(accessGroupFields) } }
        """
    public static let createAccessGroup = """
        mutation ($name: String!, $description: String, $idpGroup: String) {
          CreateGroup(name: $name, description: $description, idpGroup: $idpGroup) { \(accessGroupFields) }
        }
        """
    public static let updateAccessGroup = """
        mutation ($groupId: String!, $name: String, $description: String, $idpGroup: String, $userIds: [String!], $roleIds: [String!], $domainIds: [String!]) {
          UpdateGroup(groupId: $groupId, name: $name, description: $description, idpGroup: $idpGroup, userIds: $userIds, roleIds: $roleIds, domainIds: $domainIds) {
            \(accessGroupFields)
          }
        }
        """
    public static let deleteAccessGroup = """
        mutation ($groupId: String!) { DeleteGroup(groupId: $groupId) }
        """
    public static let accessRoles = """
        query { ListRoles { \(accessRoleFields) } }
        """
    public static let accessPermissions = """
        query { ListPermissions { key kind widens } }
        """
    public static let createAccessRole = """
        mutation ($name: String!, $description: String, $permissions: [String!]!) {
          CreateRole(name: $name, description: $description, permissions: $permissions) { \(accessRoleFields) }
        }
        """
    public static let updateAccessRole = """
        mutation ($roleId: String!, $name: String, $description: String, $permissions: [String!]) {
          UpdateRole(roleId: $roleId, name: $name, description: $description, permissions: $permissions) { \(accessRoleFields) }
        }
        """
    public static let deleteAccessRole = """
        mutation ($roleId: String!) { DeleteRole(roleId: $roleId) }
        """
    public static let accessDomains = """
        query { ListDomains { id domain } }
        """
    public static let auditEvents = """
        query ($resourceType: String, $first: Int, $offset: Int) {
          ListAuditEvents(resourceType: $resourceType, first: $first, offset: $offset) {
            total
            events {
              id createdAt actorKind actorLabel impersonatorLabel sourceIp instance
              resourceType resourceId resourceLabel resourceLink action before after
            }
          }
        }
        """
}

extension Connection {
    /// Who is reading the access pages, and what they may change there.
    public func accessSession() async throws -> AccessSession {
        struct Answer: Decodable, Sendable { let GetSession: AccessSession }
        return try await execute(Documents.accessSession, as: Answer.self).GetSession
    }

    // MARK: People

    /// Everybody with an account. Needs user:manage.
    public func accessUsers() async throws -> [AccessUser] {
        struct Answer: Decodable, Sendable { let ListUsers: [AccessUser] }
        return try await execute(Documents.accessUsers, as: Answer.self).ListUsers
    }

    /// Makes an account, in the server's default group.
    public func createAccessUser(_ draft: AccessUserDraft) async throws -> AccessUser {
        struct Answer: Decodable, Sendable { let CreateUser: AccessUser }
        return try await execute(Documents.createAccessUser, variables: draft.createVariables, as: Answer.self).CreateUser
    }

    /// Changes a person's username, name and notification address.
    public func updateAccessUser(_ userId: String, draft: AccessUserDraft) async throws -> AccessUser {
        struct Answer: Decodable, Sendable { let UpdateUser: AccessUser }
        return try await execute(Documents.updateAccessUser, variables: draft.updateVariables(userId: userId), as: Answer.self).UpdateUser
    }

    /// Disables a person, who then cannot sign in, or lets them sign in
    /// again. Everything of theirs is kept either way.
    public func setAccessUserDisabled(_ userId: String, isDisabled: Bool) async throws -> AccessUser {
        struct Answer: Decodable, Sendable { let UpdateUser: AccessUser }
        return try await execute(
            Documents.updateAccessUser, variables: ["userId": .string(userId), "disabled": .bool(isDisabled)], as: Answer.self
        ).UpdateUser
    }

    /// Sets a person's password. The server signs them out everywhere and
    /// revokes their API tokens.
    public func setAccessUserPassword(_ userId: String, password: String) async throws {
        struct Answer: Decodable, Sendable { let SetUserPassword: JSONValue? }
        _ = try await execute(
            Documents.setAccessUserPassword, variables: ["userId": .string(userId), "password": .string(password)], as: Answer.self)
    }

    /// Removes an account with its sessions, tokens, passkeys and
    /// memberships.
    public func deleteAccessUser(_ userId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteUser: JSONValue? }
        _ = try await execute(Documents.deleteAccessUser, variables: ["userId": .string(userId)], as: Answer.self)
    }

    // MARK: Groups

    public func accessGroups() async throws -> [AccessGroup] {
        struct Answer: Decodable, Sendable { let ListGroups: [AccessGroup] }
        return try await execute(Documents.accessGroups, as: Answer.self).ListGroups
    }

    public func createAccessGroup(_ draft: AccessGroupDraft) async throws -> AccessGroup {
        struct Answer: Decodable, Sendable { let CreateGroup: AccessGroup }
        return try await execute(Documents.createAccessGroup, variables: draft.variables, as: Answer.self).CreateGroup
    }

    /// Changes a group's name, description and identity provider group.
    public func updateAccessGroup(_ groupId: String, draft: AccessGroupDraft) async throws -> AccessGroup {
        var variables = draft.variables
        variables["groupId"] = .string(groupId)
        return try await updateAccessGroup(variables)
    }

    /// Replaces who is in a group. Somebody who may only manage people may
    /// change this and nothing else about a group, so nothing else is sent.
    public func setAccessGroupMembers(_ groupId: String, userIds: [String]) async throws -> AccessGroup {
        try await updateAccessGroup(["groupId": .string(groupId), "userIds": .array(userIds.map { .string($0) })])
    }

    /// Replaces the roles a group carries.
    public func setAccessGroupRoles(_ groupId: String, roleIds: [String]) async throws -> AccessGroup {
        try await updateAccessGroup(["groupId": .string(groupId), "roleIds": .array(roleIds.map { .string($0) })])
    }

    /// Replaces the domains a group's domain permissions apply to.
    public func setAccessGroupDomains(_ groupId: String, domainIds: [String]) async throws -> AccessGroup {
        try await updateAccessGroup(["groupId": .string(groupId), "domainIds": .array(domainIds.map { .string($0) })])
    }

    private func updateAccessGroup(_ variables: [String: JSONValue]) async throws -> AccessGroup {
        struct Answer: Decodable, Sendable { let UpdateGroup: AccessGroup }
        return try await execute(Documents.updateAccessGroup, variables: variables, as: Answer.self).UpdateGroup
    }

    /// Removes a group; its members keep their accounts.
    public func deleteAccessGroup(_ groupId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteGroup: JSONValue? }
        _ = try await execute(Documents.deleteAccessGroup, variables: ["groupId": .string(groupId)], as: Answer.self)
    }

    // MARK: Roles

    /// The roles. Needs role:manage or group:manage.
    public func accessRoles() async throws -> [AccessRole] {
        struct Answer: Decodable, Sendable { let ListRoles: [AccessRole] }
        return try await execute(Documents.accessRoles, as: Answer.self).ListRoles
    }

    /// Every permission the server knows, in its order.
    public func accessPermissions() async throws -> [AccessPermission] {
        struct Answer: Decodable, Sendable { let ListPermissions: [AccessPermission] }
        return try await execute(Documents.accessPermissions, as: Answer.self).ListPermissions
    }

    /// Makes a role holding no permissions; they are ticked once it exists.
    public func createAccessRole(_ draft: AccessRoleDraft) async throws -> AccessRole {
        struct Answer: Decodable, Sendable { let CreateRole: AccessRole }
        var variables = draft.variables
        variables["permissions"] = .array([])
        return try await execute(Documents.createAccessRole, variables: variables, as: Answer.self).CreateRole
    }

    /// Changes a role's name and description.
    public func updateAccessRole(_ roleId: String, draft: AccessRoleDraft) async throws -> AccessRole {
        struct Answer: Decodable, Sendable { let UpdateRole: AccessRole }
        var variables = draft.variables
        variables["roleId"] = .string(roleId)
        return try await execute(Documents.updateAccessRole, variables: variables, as: Answer.self).UpdateRole
    }

    /// Replaces the permissions a role holds.
    public func setAccessRolePermissions(_ roleId: String, permissions: [String]) async throws -> AccessRole {
        struct Answer: Decodable, Sendable { let UpdateRole: AccessRole }
        return try await execute(
            Documents.updateAccessRole, variables: ["roleId": .string(roleId), "permissions": .array(permissions.map { .string($0) })],
            as: Answer.self
        ).UpdateRole
    }

    /// Removes a role; every group that carried it loses it.
    public func deleteAccessRole(_ roleId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteRole: JSONValue? }
        _ = try await execute(Documents.deleteAccessRole, variables: ["roleId": .string(roleId)], as: Answer.self)
    }

    // MARK: Domains and the audit log

    /// The domains a group can be given.
    public func accessDomains() async throws -> [AccessDomain] {
        struct Answer: Decodable, Sendable { let ListDomains: [AccessDomain] }
        return try await execute(Documents.accessDomains, as: Answer.self).ListDomains
    }

    /// A page of the audit log, newest first, narrowed to one kind of thing
    /// when one is given. Needs audit:read.
    public func auditEvents(resourceType: String? = nil, first: Int = 50, offset: Int = 0) async throws -> AuditEventPage {
        struct Answer: Decodable, Sendable { let ListAuditEvents: AuditEventPage }
        var variables: [String: JSONValue] = ["first": .number(Double(first)), "offset": .number(Double(offset))]
        if let resourceType, !resourceType.isEmpty {
            variables["resourceType"] = .string(resourceType)
        } else {
            variables["resourceType"] = .null
        }
        return try await execute(Documents.auditEvents, variables: variables, as: Answer.self).ListAuditEvents
    }
}
