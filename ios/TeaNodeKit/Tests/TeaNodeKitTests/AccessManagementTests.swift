import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// AccessManagementTests cover the people, groups, roles and audit log pages:
/// reading the server's answers, what each change sends, and what the pages
/// work out from the lists.
final class AccessManagementTests: XCTestCase {
    let server = try! ServerAddress(parsing: "mail.example.com")

    private func connection(answering body: String) -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(
            server: server, clientId: "a-client", accessToken: "tnt_a", refreshToken: "", expiresAt: Date().addingTimeInterval(30 * 86400),
            username: "robin")
        return (Connection(account: account, transport: fake), fake)
    }

    private func sentBody(_ request: URLRequest) throws -> [String: JSONValue] {
        try JSONDecoder().decode([String: JSONValue].self, from: request.httpBody ?? Data())
    }

    private func sentVariables(_ request: URLRequest) throws -> [String: JSONValue] {
        guard case .object(let variables) = try sentBody(request)["variables"] else {
            XCTFail("no variables")
            return [:]
        }
        return variables
    }

    private func sentDocument(_ request: URLRequest) throws -> String {
        try sentBody(request)["query"]?.stringValue ?? ""
    }

    func testReadsWhoIsReadingAndWhatTheyMayChange() async throws {
        let (connection, _) = connection(
            answering: #"""
                {"data":{"GetSession":{"userId":"user-1","permissions":{"everywhere":["user:manage","audit:read"]},"impersonatorUsername":null}}}
                """#)
        let session = try await connection.accessSession()
        XCTAssertEqual(session.userId, "user-1")
        XCTAssertTrue(session.canManageUsers)
        XCTAssertTrue(session.canReadAudit)
        XCTAssertFalse(session.canManageGroups)
        XCTAssertFalse(session.canManageRoles)
    }

    func testReadsThePeople() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"ListUsers":[
                  {"id":"user-1","username":"robin","name":"Robin Example","email":"robin@example.com","disabledAt":null,
                   "hasPassword":true,"groupIds":["group-1"],"createdAt":"2030-01-01T00:00:00Z"},
                  {"id":"user-2","username":"sam","hasPassword":false,"groupIds":[],"createdAt":"2030-01-02T00:00:00Z",
                   "disabledAt":"2030-02-01T00:00:00.123456789Z"}
                ]}}
                """#)
        let users = try await connection.accessUsers()
        XCTAssertEqual(users.map(\.id), ["user-1", "user-2"])
        XCTAssertEqual(users[0].displayName, "Robin Example")
        XCTAssertEqual(users[1].displayName, "sam")
        XCTAssertFalse(users[0].isDisabled)
        XCTAssertTrue(users[1].isDisabled)
        XCTAssertFalse(users[1].hasPassword)
        XCTAssertTrue(users[0].matches("ROBIN@example"))
        XCTAssertTrue(users[0].matches("  "))
        XCTAssertFalse(users[1].matches("robin"))
        XCTAssertTrue(try sentDocument(fake.sent[0]).contains("ListUsers { id username name email disabledAt hasPassword groupIds createdAt }"))
    }

    func testCreatesAPersonWithOrWithoutAPassword() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"CreateUser":{"id":"user-3","username":"alex","hasPassword":false,"groupIds":["group-1"],"createdAt":"2030-01-03T00:00:00Z"}}}
                """#)
        var draft = AccessUserDraft()
        XCTAssertFalse(draft.canSubmit)
        draft.username = "  alex "
        draft.name = "Alex Example"
        XCTAssertTrue(draft.canSubmit)
        let created = try await connection.createAccessUser(draft)
        XCTAssertEqual(created.id, "user-3")
        XCTAssertEqual(
            try sentVariables(fake.sent[0]),
            ["username": .string("alex"), "password": .null, "name": .string("Alex Example"), "email": .string("")])

        draft.password = "an invented password"
        _ = try await connection.createAccessUser(draft)
        XCTAssertEqual(try sentVariables(fake.sent[1])["password"], .string("an invented password"))
    }

    func testEditsDisablesAndRemovesAPerson() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"UpdateUser":{"id":"user-2","username":"sam","hasPassword":true,"groupIds":[],"createdAt":"2030-01-02T00:00:00Z"},
                  "SetUserPassword":{"id":"user-2"},"DeleteUser":null}}
                """#)
        let user = AccessUser(
            id: "user-2", username: "sam", name: nil, email: "sam@example.com", disabledAt: nil, hasPassword: true, groupIds: [],
            createdAt: "2030-01-02T00:00:00Z")
        var draft = AccessUserDraft(user)
        XCTAssertEqual(draft.email, "sam@example.com")
        draft.username = "samuel "
        draft.email = ""
        _ = try await connection.updateAccessUser("user-2", draft: draft)
        _ = try await connection.setAccessUserDisabled("user-2", isDisabled: true)
        try await connection.setAccessUserPassword("user-2", password: "another invented password")
        try await connection.deleteAccessUser("user-2")

        XCTAssertEqual(
            try sentVariables(fake.sent[0]),
            ["userId": .string("user-2"), "username": .string("samuel"), "name": .string(""), "email": .string("")])
        XCTAssertEqual(try sentVariables(fake.sent[1]), ["userId": .string("user-2"), "disabled": .bool(true)])
        XCTAssertEqual(try sentVariables(fake.sent[2]), ["userId": .string("user-2"), "password": .string("another invented password")])
        XCTAssertEqual(try sentVariables(fake.sent[3]), ["userId": .string("user-2")])
    }

    func testReadsAndChangesGroupsOneListAtATime() async throws {
        let groupAnswer = #"{"id":"group-1","name":"Members","description":"Everyone","userIds":["user-1"],"roleIds":["role-1"],"domainIds":[]}"#
        let (connection, fake) = connection(
            answering: #"{"data":{"ListGroups":[\#(groupAnswer)],"CreateGroup":\#(groupAnswer),"UpdateGroup":\#(groupAnswer),"DeleteGroup":null}}"#)
        let groups = try await connection.accessGroups()
        XCTAssertEqual(groups.first?.name, "Members")
        XCTAssertNil(groups.first?.idpGroup)

        var draft = AccessGroupDraft()
        XCTAssertFalse(draft.canSubmit)
        draft.name = " Helpers "
        draft.idpGroup = " helpers "
        _ = try await connection.createAccessGroup(draft)
        _ = try await connection.updateAccessGroup("group-1", draft: AccessGroupDraft(groups[0]))
        _ = try await connection.setAccessGroupMembers("group-1", userIds: ["user-1", "user-2"])
        _ = try await connection.setAccessGroupRoles("group-1", roleIds: [])
        _ = try await connection.setAccessGroupDomains("group-1", domainIds: ["domain-1"])
        try await connection.deleteAccessGroup("group-1")

        XCTAssertEqual(
            try sentVariables(fake.sent[1]), ["name": .string("Helpers"), "description": .string(""), "idpGroup": .string("helpers")])
        XCTAssertEqual(
            try sentVariables(fake.sent[2]),
            ["groupId": .string("group-1"), "name": .string("Members"), "description": .string("Everyone"), "idpGroup": .string("")])
        // Who is in a group is sent alone, so somebody who may only manage
        // people is not refused for the rest.
        XCTAssertEqual(
            try sentVariables(fake.sent[3]), ["groupId": .string("group-1"), "userIds": .array([.string("user-1"), .string("user-2")])])
        XCTAssertEqual(try sentVariables(fake.sent[4]), ["groupId": .string("group-1"), "roleIds": .array([])])
        XCTAssertEqual(try sentVariables(fake.sent[5]), ["groupId": .string("group-1"), "domainIds": .array([.string("domain-1")])])
        XCTAssertEqual(try sentVariables(fake.sent[6]), ["groupId": .string("group-1")])
    }

    func testReadsRolesAndThePermissionsTheServerKnows() async throws {
        let (connection, _) = connection(
            answering: #"""
                {"data":{"ListRoles":[{"id":"role-1","name":"Member","permissions":["mail:read"]}],
                  "ListPermissions":[{"key":"mail:read","kind":"domain"},{"key":"mail:audit-all","kind":"all-domains","widens":"mail:audit"},
                    {"key":"user:manage","kind":"server"},{"key":"something:new","kind":"elsewhere"}]}}
                """#)
        let roles = try await connection.accessRoles()
        XCTAssertEqual(roles.first?.permissions, ["mail:read"])
        XCTAssertNil(roles.first?.description)
        let permissions = try await connection.accessPermissions()
        XCTAssertEqual(permissions.map(\.permissionKind), [.domain, .allDomains, .server, nil])
        XCTAssertEqual(permissions[1].widens, "mail:audit")
        XCTAssertEqual(permissions[0].id, "mail:read")
    }

    func testCreatesARoleHoldingNothingAndTicksPermissionsInTheServersOrder() async throws {
        let roleAnswer = #"{"id":"role-2","name":"Helpers","permissions":[]}"#
        let (connection, fake) = connection(
            answering: #"{"data":{"CreateRole":\#(roleAnswer),"UpdateRole":\#(roleAnswer),"DeleteRole":null}}"#)
        var draft = AccessRoleDraft()
        draft.name = " Helpers "
        draft.description = "Answer the phones"
        _ = try await connection.createAccessRole(draft)
        _ = try await connection.updateAccessRole("role-2", draft: draft)
        _ = try await connection.setAccessRolePermissions("role-2", permissions: ["user:manage"])
        try await connection.deleteAccessRole("role-2")

        XCTAssertEqual(
            try sentVariables(fake.sent[0]),
            ["name": .string("Helpers"), "description": .string("Answer the phones"), "permissions": .array([])])
        XCTAssertEqual(
            try sentVariables(fake.sent[1]),
            ["roleId": .string("role-2"), "name": .string("Helpers"), "description": .string("Answer the phones")])
        XCTAssertEqual(try sentVariables(fake.sent[2]), ["roleId": .string("role-2"), "permissions": .array([.string("user:manage")])])
        XCTAssertEqual(try sentVariables(fake.sent[3]), ["roleId": .string("role-2")])

        let catalog = [
            AccessPermission(key: "mail:read", kind: "domain", widens: nil),
            AccessPermission(key: "mail:send", kind: "domain", widens: nil),
            AccessPermission(key: "user:manage", kind: "server", widens: nil),
        ]
        let role = AccessRole(id: "role-2", name: "Helpers", description: nil, permissions: ["user:manage", "retired:key", "mail:read"])
        XCTAssertEqual(role.permissions(setting: "mail:send", isHeld: true, catalog: catalog), ["mail:read", "mail:send", "user:manage"])
        XCTAssertEqual(role.permissions(setting: "user:manage", isHeld: false, catalog: catalog), ["mail:read"])
    }

    func testReadsTheDomainsAGroupCanBeGiven() async throws {
        let (connection, _) = connection(answering: #"{"data":{"ListDomains":[{"id":"domain-1","domain":"example.com"}]}}"#)
        let domains = try await connection.accessDomains()
        XCTAssertEqual(domains.map(\.domain), ["example.com"])
    }

    func testPagesTheAuditLogAndNarrowsItToOneKindOfThing() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"ListAuditEvents":{"total":75,"events":[
                  {"id":"event-1","createdAt":"2030-01-01T00:00:00Z","actorKind":"agent","actorLabel":"robin","impersonatorLabel":"operator",
                   "sourceIp":"192.0.2.10","instance":"instance-a","resourceType":"domain","resourceId":"domain-1",
                   "resourceLabel":"example.com","resourceLink":"/manage/domains/domain-1","action":"update",
                   "before":{"enabled":true,"name":"example.com","tags":["a"],"note":"","limit":3},
                   "after":{"enabled":false,"name":"example.com","tags":["a","b"],"note":null,"owner":"robin","limit":3}},
                  {"id":"event-2","createdAt":"2030-01-01T00:00:00Z","actorKind":"console","resourceType":"user","resourceId":"user-9",
                   "action":"delete","before":null,"after":null}
                ]}}}
                """#)
        let page = try await connection.auditEvents(first: 50, offset: 0)
        XCTAssertEqual(page.total, 75)
        XCTAssertEqual(page.events.count, 2)
        _ = try await connection.auditEvents(resourceType: "domain", first: 50, offset: 50)
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["first": .number(50), "offset": .number(0), "resourceType": .null])
        XCTAssertEqual(try sentVariables(fake.sent[1]), ["first": .number(50), "offset": .number(50), "resourceType": .string("domain")])

        let changed = page.events[0]
        XCTAssertEqual(changed.auditAction, .update)
        XCTAssertTrue(changed.isByAgent)
        XCTAssertEqual(changed.actorName, "robin")
        XCTAssertEqual(changed.changedFields, ["enabled", "note", "owner", "tags"])
        XCTAssertEqual(AuditEvent.describe(changed.beforeValue("enabled")), "true")
        XCTAssertEqual(AuditEvent.describe(changed.afterValue("tags")), #"["a","b"]"#)
        XCTAssertEqual(AuditEvent.describe(changed.afterValue("limit")), "3")
        XCTAssertNil(AuditEvent.describe(changed.beforeValue("note")))
        XCTAssertNil(AuditEvent.describe(changed.afterValue("note")))
        XCTAssertNil(AuditEvent.describe(changed.beforeValue("owner")))
        XCTAssertEqual(AuditEvent.describe(changed.afterValue("owner")), "robin")
        XCTAssertEqual(AuditEvent.describe(.object(["b": .number(1.5), "a": .string("x/y")])), #"{"a":"x/y","b":1.5}"#)

        let removed = page.events[1]
        XCTAssertEqual(removed.auditAction, .delete)
        XCTAssertEqual(removed.actorName, "console")
        XCTAssertFalse(removed.isByAgent)
        XCTAssertEqual(removed.changedFields, [])
    }

    func testAnswersARefusalWithTheServersReason() async throws {
        let (connection, _) = connection(answering: #"{"data":{"ListUsers":null},"errors":[{"message":"permission denied"}]}"#)
        do {
            _ = try await connection.accessUsers()
            XCTFail("a refusal was read as people")
        } catch {
            XCTAssertEqual(error as? TeaNodeError, .refused("permission denied"))
        }
    }
}
