package com.teanode.kit.access

/** The GraphQL documents of people, groups and roles. Copied from the iPhone app's Documents (ios/TeaNodeKit), text unchanged. */
object AccessManagementDocuments {
    val ACCESS_USER_FIELDS = """
id username name email avatarUrl avatarColorIndex disabledAt hasPassword groupIds createdAt
""".trimEnd()

    val ACCESS_GROUP_FIELDS = """
id name description idpGroup userIds roleIds domainIds
""".trimEnd()

    val ACCESS_ROLE_FIELDS = """
id name description permissions
""".trimEnd()

    val ACCESS_SESSION = """
query { GetSession { userId permissions { everywhere } impersonatorUsername } }
""".trimEnd()

    val ACCESS_USERS = """
query { ListUsers { ${ACCESS_USER_FIELDS} } }
""".trimEnd()

    val CREATE_ACCESS_USER = """
mutation (${'$'}username: String!, ${'$'}password: String, ${'$'}name: String, ${'$'}email: String) {
  CreateUser(username: ${'$'}username, password: ${'$'}password, name: ${'$'}name, email: ${'$'}email) { ${ACCESS_USER_FIELDS} }
}
""".trimEnd()

    val UPDATE_ACCESS_USER = """
mutation (${'$'}userId: String!, ${'$'}username: String, ${'$'}name: String, ${'$'}email: String, ${'$'}disabled: Boolean) {
  UpdateUser(userId: ${'$'}userId, username: ${'$'}username, name: ${'$'}name, email: ${'$'}email, disabled: ${'$'}disabled) { ${ACCESS_USER_FIELDS} }
}
""".trimEnd()

    val SET_ACCESS_USER_PASSWORD = """
mutation (${'$'}userId: String!, ${'$'}password: String!) { SetUserPassword(userId: ${'$'}userId, password: ${'$'}password) { id } }
""".trimEnd()

    val DELETE_ACCESS_USER = """
mutation (${'$'}userId: String!) { DeleteUser(userId: ${'$'}userId) }
""".trimEnd()

    val ACCESS_GROUPS = """
query { ListGroups { ${ACCESS_GROUP_FIELDS} } }
""".trimEnd()

    val CREATE_ACCESS_GROUP = """
mutation (${'$'}name: String!, ${'$'}description: String, ${'$'}idpGroup: String) {
  CreateGroup(name: ${'$'}name, description: ${'$'}description, idpGroup: ${'$'}idpGroup) { ${ACCESS_GROUP_FIELDS} }
}
""".trimEnd()

    val UPDATE_ACCESS_GROUP = """
mutation (${'$'}groupId: String!, ${'$'}name: String, ${'$'}description: String, ${'$'}idpGroup: String, ${'$'}userIds: [String!], ${'$'}roleIds: [String!], ${'$'}domainIds: [String!]) {
  UpdateGroup(groupId: ${'$'}groupId, name: ${'$'}name, description: ${'$'}description, idpGroup: ${'$'}idpGroup, userIds: ${'$'}userIds, roleIds: ${'$'}roleIds, domainIds: ${'$'}domainIds) {
    ${ACCESS_GROUP_FIELDS}
  }
}
""".trimEnd()

    val DELETE_ACCESS_GROUP = """
mutation (${'$'}groupId: String!) { DeleteGroup(groupId: ${'$'}groupId) }
""".trimEnd()

    val ACCESS_ROLES = """
query { ListRoles { ${ACCESS_ROLE_FIELDS} } }
""".trimEnd()

    val ACCESS_PERMISSIONS = """
query { ListPermissions { key kind widens } }
""".trimEnd()

    val CREATE_ACCESS_ROLE = """
mutation (${'$'}name: String!, ${'$'}description: String, ${'$'}permissions: [String!]!) {
  CreateRole(name: ${'$'}name, description: ${'$'}description, permissions: ${'$'}permissions) { ${ACCESS_ROLE_FIELDS} }
}
""".trimEnd()

    val UPDATE_ACCESS_ROLE = """
mutation (${'$'}roleId: String!, ${'$'}name: String, ${'$'}description: String, ${'$'}permissions: [String!]) {
  UpdateRole(roleId: ${'$'}roleId, name: ${'$'}name, description: ${'$'}description, permissions: ${'$'}permissions) { ${ACCESS_ROLE_FIELDS} }
}
""".trimEnd()

    val DELETE_ACCESS_ROLE = """
mutation (${'$'}roleId: String!) { DeleteRole(roleId: ${'$'}roleId) }
""".trimEnd()

    val ACCESS_DOMAINS = """
query { ListDomains { id domain } }
""".trimEnd()

    val AUDIT_EVENTS = """
query (${'$'}resourceType: String, ${'$'}first: Int, ${'$'}offset: Int) {
  ListAuditEvents(resourceType: ${'$'}resourceType, first: ${'$'}first, offset: ${'$'}offset) {
    total
    events {
      id createdAt actorKind actorLabel impersonatorLabel sourceIp instance
      resourceType resourceId resourceLabel resourceLink action before after
    }
  }
}
""".trimEnd()

}
