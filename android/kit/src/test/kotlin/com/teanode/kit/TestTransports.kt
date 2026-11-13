package com.teanode.kit

import kotlinx.coroutines.delay
import java.util.concurrent.CopyOnWriteArrayList

/** FakeServer answers requests the way a test says, and keeps them. */
class FakeServer(private val answer: (HTTPRequest) -> Pair<Int, String>) : HTTPTransport {
    val sent = CopyOnWriteArrayList<HTTPRequest>()

    override suspend fun send(request: HTTPRequest): HTTPResponse {
        sent.add(request)
        val (statusCode, body) = answer(request)
        return HTTPResponse(statusCode, body.toByteArray())
    }

    val paths: List<String> get() = sent.map { request -> request.url.substringAfter("//").substringAfter('/').substringBefore('?').let { path -> "/$path" } }
}

fun formOf(request: HTTPRequest): Map<String, String> = QueryString.parse(request.body?.toString(Charsets.UTF_8).orEmpty())

const val SESSION_ANSWER = """{"data":{"GetSession":{"username":"robin"}}}"""

/**
 * RotatingServer renews a sign-in the way the server does: each refresh token works once, and the one it hands back
 * replaces it. A renewal takes a moment, so callers that arrive meanwhile overlap with it.
 */
class RotatingServer(
    private var refreshToken: String,
    private val onRefresh: () -> Unit = {},
    private val answerGraphQL: (String) -> Pair<Int, String> = { _ -> 200 to SESSION_ANSWER },
) : HTTPTransport {
    private val requests = CopyOnWriteArrayList<HTTPRequest>()
    private var renewalNumber = 1

    override suspend fun send(request: HTTPRequest): HTTPResponse {
        requests.add(request)
        val (statusCode, body) = if (request.url.contains("/oauth/token")) {
            onRefresh()
            delay(50)
            redeem(formOf(request)["refresh_token"].orEmpty())
        } else {
            answerGraphQL(request.header("Authorization").orEmpty().removePrefix("Bearer "))
        }
        return HTTPResponse(statusCode, body.toByteArray())
    }

    @Synchronized
    private fun redeem(presented: String): Pair<Int, String> {
        if (presented != refreshToken) return 400 to """{"error":"invalid_grant","error_description":"The refresh token is not valid."}"""
        renewalNumber += 1
        refreshToken = "refresh-$renewalNumber"
        return 200 to """{"access_token":"access-$renewalNumber","refresh_token":"refresh-$renewalNumber","expires_in":2592000}"""
    }

    val renewalCount: Int get() = requests.count { request -> request.url.contains("/oauth/token") }
    val graphQLCount: Int get() = requests.count { request -> request.url.contains("/api/v1/graphql") }
}

/** MemoryAccountStore keeps sign-ins as the encrypted file does, shared by every connection made with it. */
class MemoryAccountStore(accounts: List<Account>) : AccountStore {
    private val store = SecretAccountStore(MemorySecrets()).also { kept ->
        accounts.forEach { account -> kept.save(account) }
    }

    override fun save(account: Account) = store.save(account)
    override fun load(): Account? = store.load()
    override fun load(accountKey: String): Account? = store.load(accountKey)
    override fun delete() = store.delete()
}
