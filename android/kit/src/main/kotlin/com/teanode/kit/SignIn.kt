package com.teanode.kit

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

/** The grant a connect code is exchanged with. */
internal const val connectCodeGrant = "urn:teanode:params:oauth:grant-type:connect-code"

/** SignIn connects the app to a server: it registers the app, exchanges a connect code, refreshes and gives up a sign-in. */
class SignIn(private val server: ServerAddress, private val transport: HTTPTransport) {
    @Serializable
    private class TokenResponse(
        @SerialName("access_token") val accessToken: String,
        @SerialName("refresh_token") val refreshToken: String? = null,
        @SerialName("expires_in") val expiresInSeconds: Double? = null,
    )

    @Serializable
    private class OAuthRefusal(val error: String, @SerialName("error_description") val errorDescription: String? = null)

    @Serializable
    private class Registered(@SerialName("client_id") val clientId: String)

    /** Registers the app with the server and returns its client identifier. */
    suspend fun register(clientName: String = "TeaNode"): String {
        val body = JsonObject(
            mapOf("client_name" to JsonPrimitive(clientName), "redirect_uris" to JsonArray(listOf(JsonPrimitive(appRedirectAddress)))),
        )
        val response = transport.send(
            HTTPRequest(server.endpoint("/oauth/register").toString(), "POST", mapOf("Content-Type" to "application/json"), body.toString().toByteArray()),
        )
        if (response.statusCode != 201 && response.statusCode != 200) throw refusal(response)
        return TeaNodeJson.decodeFromString(Registered.serializer(), response.text).clientId
    }

    /** Exchanges a connect code for the app's sign-in. The username is not known yet; the caller asks the server who the token belongs to. */
    suspend fun exchangeConnectCode(connectCode: String, clientId: String, deviceName: String, nowEpochMS: Long = System.currentTimeMillis()): Account {
        val token = post(
            mapOf("grant_type" to connectCodeGrant, "client_id" to clientId, "connect_code" to connectCode, "device_name" to deviceName),
            invalidGrant = TeaNodeError.ConnectCodeSpent,
        )
        return signedIn(token, clientId, nowEpochMS)
    }

    /**
     * The server's page where the person signs in and allows this app the whole account; it returns to
     * [appRedirectAddress] with a code. The API is named as the resource, which is what asks for the whole account
     * rather than the agent tools (docs/decisions/20261011-an-installed-app-may-sign-in-with-a-password.md).
     */
    fun authorizationAddress(clientId: String, codeChallenge: String, state: String): String =
        server.endpoint("/oauth/authorize").newBuilder()
            .addQueryParameter("response_type", "code")
            .addQueryParameter("client_id", clientId)
            .addQueryParameter("redirect_uri", appRedirectAddress)
            .addQueryParameter("code_challenge", codeChallenge)
            .addQueryParameter("code_challenge_method", "S256")
            .addQueryParameter("state", state)
            .addQueryParameter("resource", server.endpoint("/api/v1").toString())
            .build()
            .toString()

    /** Exchanges the code the sign-in page returned for the app's sign-in, with the verifier whose hash the page was given. */
    suspend fun exchangeAuthorizationCode(authorizationCode: String, codeVerifier: String, clientId: String, nowEpochMS: Long = System.currentTimeMillis()): Account {
        val token = post(
            mapOf(
                "grant_type" to "authorization_code", "client_id" to clientId, "code" to authorizationCode,
                "code_verifier" to codeVerifier, "redirect_uri" to appRedirectAddress,
            ),
            invalidGrant = TeaNodeError.Refused("The sign-in expired before it finished. Try again."),
        )
        return signedIn(token, clientId, nowEpochMS)
    }

    /** Renews an account's sign-in. The old refresh token stops working. */
    suspend fun refresh(account: Account, nowEpochMS: Long = System.currentTimeMillis()): Account {
        val token = post(
            mapOf("grant_type" to "refresh_token", "client_id" to account.clientId, "refresh_token" to account.refreshToken),
            invalidGrant = TeaNodeError.SignedOut,
        )
        return account.copy(
            accessToken = token.accessToken,
            refreshToken = token.refreshToken ?: account.refreshToken,
            expiresAtEpochMS = nowEpochMS + ((token.expiresInSeconds ?: 3600.0) * 1000).toLong(),
        )
    }

    /** Gives up a sign-in, for a person who chose not to connect after all, or who signs the phone out. */
    suspend fun revoke(account: Account) {
        val token = account.refreshToken.ifEmpty { account.accessToken }
        try {
            transport.send(
                HTTPRequest(
                    server.endpoint("/oauth/revoke").toString(), "POST", mapOf("Content-Type" to "application/x-www-form-urlencoded"),
                    QueryString.form(mapOf("token" to token)).toByteArray(),
                ),
            )
        } catch (_: java.io.IOException) {
            // The sign-in is forgotten here either way; the server drops it when its token runs out.
        }
    }

    private fun signedIn(token: TokenResponse, clientId: String, nowEpochMS: Long) = Account(
        server = server, clientId = clientId, accessToken = token.accessToken, refreshToken = token.refreshToken ?: "",
        expiresAtEpochMS = nowEpochMS + ((token.expiresInSeconds ?: 3600.0) * 1000).toLong(), username = "",
    )

    private suspend fun post(form: Map<String, String>, invalidGrant: TeaNodeError): TokenResponse {
        val response = transport.send(
            HTTPRequest(
                server.endpoint("/oauth/token").toString(), "POST", mapOf("Content-Type" to "application/x-www-form-urlencoded"),
                QueryString.form(form).toByteArray(),
            ),
        )
        if (response.statusCode != 200) {
            val parsed = runCatching { TeaNodeJson.decodeFromString(OAuthRefusal.serializer(), response.text) }.getOrNull()
            if (parsed?.error == "invalid_grant") throw invalidGrant
            throw refusal(response)
        }
        return TeaNodeJson.decodeFromString(TokenResponse.serializer(), response.text)
    }

    private fun refusal(response: HTTPResponse): TeaNodeError {
        val parsed = runCatching { TeaNodeJson.decodeFromString(OAuthRefusal.serializer(), response.text) }.getOrNull()
            ?: return TeaNodeError.UnreadableAnswer
        return TeaNodeError.Refused(parsed.errorDescription ?: parsed.error)
    }

    companion object {
        /** The code a return to [appRedirectAddress] carries, when it answers the request made with `state`; null for anything else. */
        fun authorizationCode(returned: String, state: String): String? {
            val items = QueryString.parse(returned.substringAfter('?', ""))
            if (items["state"] != state) return null
            return items["code"]
        }
    }
}
