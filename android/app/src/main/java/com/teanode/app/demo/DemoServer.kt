package com.teanode.app.demo

import com.teanode.app.shell.Areas
import com.teanode.kit.Account
import com.teanode.kit.HTTPRequest
import com.teanode.kit.HTTPResponse
import com.teanode.kit.HTTPTransport
import com.teanode.kit.ServerAddress
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.SectionFacts
import com.teanode.kit.AccessSession
import com.teanode.kit.ServiceCapabilities
import kotlinx.serialization.json.JsonObject

/** DemoRequest is what the app asked the demo server: the address, and for GraphQL the document and its variables. */
class DemoRequest(
    val url: String,
    val method: String,
    /** The path of the address, such as `/api/v1/graphql`. */
    val path: String,
    /** The GraphQL document; empty for a request that is not GraphQL. */
    val document: String,
    val variables: JsonObject,
    val body: ByteArray?,
)

/** DemoResponse is what the demo server answers. */
class DemoResponse(val body: ByteArray, val statusCode: Int = 200) {
    constructor(text: String, statusCode: Int = 200) : this(text.toByteArray(), statusCode)
}

/**
 * DemoAnswerer answers one kind of request in demo mode, or returns null when the request is not its to answer so the
 * next one is asked. The answerers of every part are asked in the order [Areas] lists the parts, each part's in the
 * order it lists them, then the foundation's. A part's answerer for a document whose name contains another's (say
 * `ReadAgentSettings` and `ReadAgent`) therefore goes before the shorter one.
 */
fun interface DemoAnswerer {
    fun answer(request: DemoRequest): DemoResponse?
}

/**
 * Answers a GraphQL document that asks for the field [fieldName] (matched as a whole word) with [valueJson], which is
 * the field's value; the answer is `{"data":{"<fieldName>": <valueJson>}}`. For a value that depends on the variables,
 * pass a function.
 */
fun graphQLField(fieldName: String, valueJson: (variables: JsonObject) -> String): DemoAnswerer {
    val asksForField = Regex("""\b${Regex.escape(fieldName)}\b\s*[({]""")
    return DemoAnswerer { request ->
        if (request.document.isNotEmpty() && asksForField.containsMatchIn(request.document)) {
            DemoResponse("""{"data":{"$fieldName":${valueJson(request.variables)}}}""")
        } else {
            null
        }
    }
}

fun graphQLField(fieldName: String, valueJson: String): DemoAnswerer = graphQLField(fieldName) { _ -> valueJson }

/**
 * DemoTransport stands in for the server in demo mode, so a screen can be drawn and photographed without one. It
 * answers what the parts' demo answerers answer, and refuses the rest with a GraphQL error naming the document, so a
 * screen whose demo data is missing says so instead of showing nothing.
 */
class DemoTransport(private val answerers: List<DemoAnswerer>) : HTTPTransport {
    override suspend fun send(request: HTTPRequest): HTTPResponse {
        val body = request.body?.toString(Charsets.UTF_8).orEmpty()
        val parsed = runCatching { TeaNodeJson.parseToJsonElement(body) as? JsonObject }.getOrNull()
        val document = (parsed?.get("query") as? kotlinx.serialization.json.JsonPrimitive)?.content.orEmpty()
        val variables = parsed?.get("variables") as? JsonObject ?: JsonObject(emptyMap())
        val path = request.url.substringAfter("://").substringAfter('/', "").substringBefore('?').let { text -> "/$text" }
        val demoRequest = DemoRequest(request.url, request.method, path, document, variables, request.body)
        for (answerer in answerers) {
            val response = answerer.answer(demoRequest) ?: continue
            return HTTPResponse(response.statusCode, response.body)
        }
        val named = document.lineSequence().firstOrNull { line -> line.isNotBlank() }?.trim().orEmpty().take(120)
        val message = "demo mode has no answer for $named".replace("\"", "'")
        return HTTPResponse(200, """{"errors":[{"message":"$message"}]}""".toByteArray())
    }
}

/** DemoServer is the invented server, account and person that demo mode signs in to. */
object DemoServer {
    /** The extra on the launching intent that turns demo mode on (debug builds only). */
    const val EXTRA_DEMO = "com.teanode.app.extra.DEMO"

    /** The extra that opens a route at launch, such as `mail/home`, in demo mode. */
    const val EXTRA_ROUTE = "com.teanode.app.extra.ROUTE"

    val account = Account(
        server = ServerAddress.parse("demo.example.com"), clientId = "demo", accessToken = "demo", refreshToken = "",
        expiresAtEpochMS = Long.MAX_VALUE / 4, username = "robin",
    )

    /** Every part offered, as a server with every service on would. */
    val facts = SectionFacts(
        services = ServiceCapabilities(
            isMailRoutingEnabled = true, isMailboxEnabled = true, isCalendarEnabled = true, isRemindersEnabled = true,
            isContactsEnabled = true, isPhotosEnabled = true, isAssistantEnabled = true, hasPendingServiceRestart = false,
            hasCompletedSetup = true,
        ),
        session = AccessSession(
            userId = "demo-user",
            permissions = AccessSession.AccessSessionPermissions(
                listOf("circle:use", "location:use", "health:use", "journal:use", "passwords:use", "shopping:use", "photos:use"),
            ),
        ),
        hasCodingComputers = true,
        hasRecordingOwner = true,
    )

    /** What the foundation itself answers: who is signed in, and what the server offers. */
    private val foundationAnswerers: List<DemoAnswerer> = listOf(
        graphQLField("GetServiceCapabilities", """{"isMailRoutingEnabled":true,"isMailboxEnabled":true,"isCalendarEnabled":true,"isRemindersEnabled":true,"isContactsEnabled":true,"isPhotosEnabled":true,"isAssistantEnabled":true,"hasPendingServiceRestart":false,"hasCompletedSetup":true}"""),
        graphQLField("GetSession") { _ ->
            """{"username":"robin","userId":"demo-user","permissions":{"everywhere":["circle:use","location:use","health:use","journal:use","passwords:use","shopping:use","photos:use"]},"impersonatorUsername":null}"""
        },
    )

    /** The answerers of every part, then the foundation's. */
    fun transport(): DemoTransport = DemoTransport(Areas.all.flatMap { area -> area.demoAnswerers } + foundationAnswerers)
}

/** The value under a key of the variables, as text. */
fun JsonObject.text(key: String): String? = (this[key] as? kotlinx.serialization.json.JsonPrimitive)?.content

