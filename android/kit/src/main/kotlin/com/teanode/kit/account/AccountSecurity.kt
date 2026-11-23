package com.teanode.kit.account

import com.teanode.kit.Connection
import com.teanode.kit.photos.ServerTime
import com.teanode.kit.settings.variablesOf
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import java.time.Instant

/** One browser signed in to the person's account, as the dashboard's Sessions page lists it. */
@Serializable
data class AccountSession(
    val id: String,
    /** Whether this is the session reading the list. It is never revoked from the list: signing out is what ends one's own. */
    val current: Boolean = false,
    val created: String,
    val expires: String? = null,
    val lastUsed: String? = null,
    val ip: String? = null,
    val userAgent: String? = null,
    /** When it was ended, or null while it still works. */
    val revoked: String? = null,
    /** The operator who signed in as this account with it, when it was one. */
    val impersonatorUsername: String? = null,
) {
    /** Whether the list offers to end it: not the one reading the list, and not one already ended. */
    val canRevoke: Boolean get() = !current && revoked == null

    /**
     * The part of the user agent a person recognizes, such as "Firefox · Windows NT 10.0"; null when the browser sent
     * none. Not a parser: guessing wrongly is worse than showing less.
     */
    val browserName: String?
        get() {
            val agent = userAgent?.takeIf { text -> text.isNotEmpty() } ?: return null
            val browserMatch = BROWSER.find(agent) ?: return if (agent.length > 60) agent.take(60) + "…" else agent
            val browserToken = browserMatch.groupValues[1]
            val browser = BROWSER_NAMES[browserToken] ?: browserToken
            val platformMatch = PLATFORM.find(agent) ?: return browser
            val platform = platformMatch.groupValues[1].split(';').first()
            return "$browser · ${platform.trim()}"
        }

    companion object {
        private val BROWSER = Regex("""(Firefox|Edg|OPR|Chrome|Safari)/[\d.]+""")
        private val PLATFORM = Regex("""\(([^)]+)\)""")
        private val BROWSER_NAMES = mapOf("Edg" to "Edge", "OPR" to "Opera")

        /** The order the list shows them in: the one reading it first, then the rest as the server sent them, newest first. */
        fun listed(sessions: List<AccountSession>): List<AccountSession> = sessions.filter { session -> session.current } + sessions.filter { session -> !session.current }
    }
}

/** A token the command line tool authenticates with, without its secret, which is shown once when it is made. */
@Serializable
data class APIToken(
    val id: String,
    val name: String,
    /** The account it belongs to, and acts as. */
    val username: String,
    val created: String,
    /** When it stops working, or null when it does not expire. */
    val expires: String? = null,
    val lastUsed: String? = null,
    val lastUsedIp: String? = null,
    /** When it was revoked, or null while it still works. */
    val revoked: String? = null,
) {
    /** When it stops working, as of now. */
    fun expiry(now: Instant = Instant.now()): APITokenExpiry {
        val expiresInstant = ServerTime.instant(expires) ?: return APITokenExpiry.Never
        return if (expiresInstant.isBefore(now)) APITokenExpiry.Expired else APITokenExpiry.ExpiresAt(expiresInstant)
    }
}

/** When a token stops working: never, already, or at a time to come. A token that never expires is a standing risk, so the list marks it quietly. */
sealed interface APITokenExpiry {
    data object Never : APITokenExpiry
    data object Expired : APITokenExpiry
    data class ExpiresAt(val instant: Instant) : APITokenExpiry
}

/** How long a token lasts: a handful of sensible answers rather than a number to type, as the dashboard offers them. */
enum class APITokenLifetime(val serverName: String) {
    ThirtyDays("720h"), NinetyDays("2160h"), Year("8760h"), Never("never");

    /** As CreateToken takes it: a duration, or null for a token that does not expire. */
    val creationLifetime: JsonElement get() = if (this == Never) JsonNull else com.teanode.kit.settings.jsonOf(serverName)

    /** As UpdateToken takes it, counted from now: a duration, or "never". */
    val updatedLifetime: JsonElement get() = com.teanode.kit.settings.jsonOf(serverName)

    companion object {
        /** The lifetime a new token is given unless the person picks another. */
        val standard: APITokenLifetime = NinetyDays
    }
}

/**
 * What the edit form changes about a token: its name, when it differs from the one it has, and a new lifetime counted
 * from now, or null to keep the expiry as it is. The secret stays the same.
 */
data class APITokenChange(val tokenId: String, val name: String?, val lifetime: APITokenLifetime?) {
    /** Whether the change would do anything at all. */
    val hasChanges: Boolean get() = name != null || lifetime != null

    val variables: Map<String, JsonElement>
        get() = variablesOf("tokenId" to tokenId, "name" to name, "lifetime" to lifetime?.updatedLifetime)

    companion object {
        fun of(token: APIToken, newName: String, lifetime: APITokenLifetime?): APITokenChange {
            val trimmedName = newName.trim()
            return APITokenChange(token.id, if (trimmedName == token.name) null else trimmedName, lifetime)
        }
    }
}

/** A program the person authorized over OAuth to act as them. It renews its own token, so the app, not the token, is what is named and disconnected. */
@Serializable
data class ConnectedApp(
    val clientId: String,
    /** What the person calls it: the name on its tokens. */
    val name: String,
    /** The name it gave itself when it registered. */
    val registeredName: String = "",
    /** The host of each address it asked to be sent back to. */
    val redirectHosts: List<String> = emptyList(),
    val registered: String? = null,
    val lastUsed: String? = null,
    val lastUsedIp: String? = null,
    val expires: String? = null,
    /** The last moment it can renew; unused past this, it is disconnected. */
    val renewableUntil: String? = null,
    /** How many tokens it holds for the person: more than one when it was authorized more than once. */
    val tokenCount: Int = 1,
) {
    /** The name the list shows: the person's for it, else the one it registered with; null when it has neither. */
    val displayName: String? get() = name.takeIf { text -> text.isNotEmpty() } ?: registeredName.takeIf { text -> text.isNotEmpty() }

    /** The name it registered with, when it differs from the person's. */
    val differentRegisteredName: String? get() = if (registeredName.isEmpty() || registeredName == name) null else registeredName
}

@Serializable private class ListSessionsAnswer(val ListSessions: List<AccountSession?>)

@Serializable private class ListTokensAnswer(val ListTokens: List<APIToken?>)

@Serializable private class CreatedToken(val secret: String, val token: APIToken? = null)

@Serializable private class CreateTokenAnswer(val CreateToken: CreatedToken)

@Serializable private class ListAppsAnswer(val ListApps: List<ConnectedApp?>)

/** The browsers signed in to the person's account, newest first; the ended ones too when asked, so a revocation stays visible a while. */
suspend fun Connection.accountSessions(includeRevoked: Boolean = false): List<AccountSession> =
    execute<ListSessionsAnswer>(AccountSecurityDocuments.ACCOUNT_SESSIONS, variablesOf("includeRevoked" to includeRevoked)).ListSessions.filterNotNull()

/** Signs one browser out; the others are untouched. */
suspend fun Connection.revokeSession(sessionId: String) {
    execute<JsonObject>(AccountSecurityDocuments.REVOKE_SESSION, variablesOf("sessionId" to sessionId))
}

/** Signs every browser on the account out. This phone signs in with a token of its own, not a session, so it stays signed in. */
suspend fun Connection.revokeAllSessions() {
    execute<JsonObject>(AccountSecurityDocuments.REVOKE_ALL_SESSIONS)
}

/** The person's API tokens, without their secrets; the revoked ones too when asked. */
suspend fun Connection.apiTokens(includeRevoked: Boolean = false): List<APIToken> =
    execute<ListTokensAnswer>(AccountSecurityDocuments.API_TOKENS, variablesOf("includeRevoked" to includeRevoked)).ListTokens.filterNotNull()

/** A token just made: its secret, shown this once since only a hash is kept, and the token. */
data class CreatedAPIToken(val secret: String, val token: APIToken?)

/** Makes an API token and returns its secret, which is shown this once. */
suspend fun Connection.createApiToken(name: String, lifetime: APITokenLifetime): CreatedAPIToken {
    val created = execute<CreateTokenAnswer>(AccountSecurityDocuments.CREATE_API_TOKEN, variablesOf("name" to name, "lifetime" to lifetime.creationLifetime)).CreateToken
    return CreatedAPIToken(created.secret, created.token)
}

/** Renames a token or gives it a new lifetime counted from now; the token itself stays the same. */
suspend fun Connection.updateApiToken(change: APITokenChange) {
    execute<JsonObject>(AccountSecurityDocuments.UPDATE_API_TOKEN, change.variables)
}

/** Revokes a token: whatever uses it stops working at once. */
suspend fun Connection.deleteApiToken(tokenId: String) {
    execute<JsonObject>(AccountSecurityDocuments.DELETE_API_TOKEN, variablesOf("tokenId" to tokenId))
}

/** The apps that hold a token of the person's, one row an app. */
suspend fun Connection.connectedApps(): List<ConnectedApp> = execute<ListAppsAnswer>(AccountSecurityDocuments.CONNECTED_APPS).ListApps.filterNotNull()

/** Renames an app, on the tokens it holds and on each it renews into. */
suspend fun Connection.renameConnectedApp(clientId: String, name: String) {
    execute<JsonObject>(AccountSecurityDocuments.RENAME_CONNECTED_APP, variablesOf("clientId" to clientId, "name" to name))
}

/** Disconnects an app: every token it holds for the person is revoked, so it has to be authorized again to come back. */
suspend fun Connection.disconnectApp(clientId: String) {
    execute<JsonObject>(AccountSecurityDocuments.DISCONNECT_APP, variablesOf("clientId" to clientId))
}
