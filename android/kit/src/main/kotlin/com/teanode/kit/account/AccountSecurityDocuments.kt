package com.teanode.kit.account

/** The GraphQL documents of the account's tokens, devices, apps and sessions. Copied from the iPhone app's Documents (ios/TeaNodeKit), text unchanged. */
object AccountSecurityDocuments {
    val ACCOUNT_SESSIONS = """
query (${'$'}includeRevoked: Boolean) {
  ListSessions(includeRevoked: ${'$'}includeRevoked) {
    id current created expires lastUsed ip userAgent revoked impersonatorUsername
  }
}
""".trimEnd()

    val REVOKE_SESSION = """
mutation (${'$'}sessionId: String!) { RevokeSession(sessionId: ${'$'}sessionId) }
""".trimEnd()

    val REVOKE_ALL_SESSIONS = """
mutation { RevokeAllSessions { authenticated } }
""".trimEnd()

    val API_TOKENS = """
query (${'$'}includeRevoked: Boolean) {
  ListTokens(includeRevoked: ${'$'}includeRevoked) {
    id name username created expires lastUsed lastUsedIp revoked
  }
}
""".trimEnd()

    val CREATE_API_TOKEN = """
mutation (${'$'}name: String!, ${'$'}lifetime: String) {
  CreateToken(name: ${'$'}name, lifetime: ${'$'}lifetime) {
    secret
    token { id name username created expires lastUsed lastUsedIp revoked }
  }
}
""".trimEnd()

    val UPDATE_API_TOKEN = """
mutation (${'$'}tokenId: String!, ${'$'}name: String, ${'$'}lifetime: String) {
  UpdateToken(tokenId: ${'$'}tokenId, name: ${'$'}name, lifetime: ${'$'}lifetime) { id }
}
""".trimEnd()

    val DELETE_API_TOKEN = """
mutation (${'$'}tokenId: String!) { DeleteToken(tokenId: ${'$'}tokenId) }
""".trimEnd()

    val CONNECTED_APPS = """
query {
  ListApps {
    clientId name registeredName redirectHosts registered lastUsed lastUsedIp expires renewableUntil tokenCount
  }
}
""".trimEnd()

    val RENAME_CONNECTED_APP = """
mutation (${'$'}clientId: String!, ${'$'}name: String!) { RenameApp(clientId: ${'$'}clientId, name: ${'$'}name) }
""".trimEnd()

    val DISCONNECT_APP = """
mutation (${'$'}clientId: String!) { DisconnectApp(clientId: ${'$'}clientId) }
""".trimEnd()

}
