package com.teanode.kit.chats

import com.teanode.kit.Account
import com.teanode.kit.Connection
import com.teanode.kit.FakeServer
import com.teanode.kit.ServerAddress
import com.teanode.kit.TeaNodeJson
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonObject
import com.teanode.kit.HTTPRequest

/** A connection whose server answers every call with the same body, and the server that kept what was sent. */
fun connectionAnswering(body: String): Pair<Connection, FakeServer> {
    val fake = FakeServer { _ -> 200 to body }
    val account = Account(ServerAddress.parse("mail.example.com"), "a-client", "tnt_a", "", Long.MAX_VALUE / 4, "person-a")
    return Connection(account, fake) to fake
}

/** The variables of the GraphQL call a request carried. */
fun sentVariables(request: HTTPRequest): JsonObject {
    val body = TeaNodeJson.parseToJsonElement(request.body!!.toString(Charsets.UTF_8)) as JsonObject
    return body["variables"] as JsonObject
}

fun messagesOf(vararg bodies: String): List<AgentMessage> =
    TeaNodeJson.decodeFromString(ListSerializer(AgentMessage.serializer()), "[${bodies.joinToString(",")}]")

fun messageOf(json: String): AgentMessage = TeaNodeJson.decodeFromString(AgentMessage.serializer(), json)

fun eventOf(json: String): AgentEvent = TeaNodeJson.decodeFromString(AgentEvent.serializer(), json)

/** A stored message of a chat, as the server answers it. */
fun chatMessage(
    id: String,
    content: String,
    authorUserId: String,
    authorName: String,
    isMine: Boolean,
    at: String = "2030-01-01T10:00:00Z",
    authorKind: String = "person",
    extra: String = "",
): String =
    """{"id":"$id","createdAt":"$at","role":"${if (isMine) "user" else "assistant"}","content":"$content","authorKind":"$authorKind",""" +
        """"authorUserId":"$authorUserId","authorName":"$authorName","authorAvatarUrl":null,"authorAvatarColorIndex":4,""" +
        """"authorAgentId":"","isMine":$isMine,"editedAt":null,"isDeleted":false$extra}"""

/** A direct chat of person-a with person-b, as the list of conversations answers it, who has read nothing yet. */
val chatAnswer = """
    {"id":"conversation-1","kind":"chat","jobKind":null,"title":"","summary":"","lastAt":"2030-01-01T10:00:00Z","archivedAt":null,
     "goal":null,"goalState":null,"goalNote":null,
     "chat":{"chatId":"chat-1","chatKind":"direct","circleId":null,"title":"","isCircleChat":false,"unreadCount":2,"notificationLevel":"all",
       "isRemoved":false,
       "members":[
         {"userId":"user-a","username":"person-a","name":"Person A","avatarUrl":null,"avatarColorIndex":4,"memberRole":"member",
          "isDeletedAccount":false,"isRemoved":false,"readThroughMessageId":"","isAgentReading":false},
         {"userId":"user-b","username":"person-b","name":"","avatarUrl":"/api/v1/avatar/user/user-b?v=2","avatarColorIndex":7,"memberRole":"member",
          "isDeletedAccount":false,"isRemoved":false,"readThroughMessageId":"01J0000000000000000000002","isAgentReading":false}]}}
""".trimIndent()

