package com.teanode.app.chats

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.LinearGradient
import android.graphics.Paint
import android.graphics.Shader
import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.DemoRequest
import com.teanode.app.demo.DemoResponse
import com.teanode.app.demo.graphQLField
import com.teanode.app.demo.text
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.chats.AgentAttachment
import com.teanode.kit.chats.AgentBackgroundCommand
import com.teanode.kit.chats.AgentConversation
import com.teanode.kit.chats.AgentIdea
import com.teanode.kit.chats.AgentInteraction
import com.teanode.kit.chats.AgentMessage
import com.teanode.kit.chats.AgentMessageReaction
import com.teanode.kit.chats.AgentReference
import com.teanode.kit.chats.AgentTodo
import com.teanode.kit.chats.AgentToolCall
import com.teanode.kit.chats.AgentUsage
import com.teanode.kit.chats.AgentWaiting
import com.teanode.kit.chats.AgentWaitingInteraction
import com.teanode.kit.chats.ChatCallMemberRecord
import com.teanode.kit.chats.ChatCallRecord
import com.teanode.kit.chats.ChatMember
import com.teanode.kit.chats.ChatPerson
import com.teanode.kit.chats.ChatSummary
import com.teanode.kit.chats.LinkPreview
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import java.io.ByteArrayOutputStream
import java.time.Instant

// What demo mode answers for Chats. Everything here is invented: the people, the places and the words. The ids name the
// conversation each screen photographs: demo-main (the main chat), demo-cards (approvals, a question and a coding
// session's question), demo-empty (nothing said yet, with ideas), demo-long (a message too long for its bubble),
// demo-goal, demo-run, demo-sam (a direct chat), demo-group (a group with agents).

private fun hoursAgo(hours: Number): String = Instant.now().minusSeconds((hours.toDouble() * 3600).toLong()).toString()

private fun message(
    id: String, role: String, content: String, hours: Number, toolCalls: List<AgentToolCall>? = null, toolCallId: String? = null,
    attachments: List<AgentAttachment>? = null, reactions: List<AgentMessageReaction>? = null, usage: AgentUsage? = null, references: List<AgentReference>? = null,
    name: String? = null,
): AgentMessage = AgentMessage(
    id = id, createdAt = hoursAgo(hours), role = role, content = content, toolCalls = toolCalls, toolCallId = toolCallId, attachments = attachments,
    reactions = reactions, usage = usage, references = references, name = name,
)

private val demoMembers = listOf(
    ChatMember("demo-user", "robin", "Robin", memberRole = "admin", isAgentActive = true, agentId = "agent-robin", agentName = "Agent", isAgentAskableByOthers = false, readThroughMessageId = "m9"),
    ChatMember("user-sam", "sam", "Sam Rivera", avatarColorIndex = 3, readThroughMessageId = "c6", isAgentAskableByOthers = true, agentId = "agent-sam", agentName = "Pip", isAgentActive = true, isAgentReading = true),
    ChatMember("user-alex", "alex", "Alex Chen", avatarColorIndex = 5, readThroughMessageId = "c4"),
)

private fun chatOf(kind: String, title: String?, members: List<ChatMember>, unreadCount: Int = 0, level: String = "all") = ChatSummary(
    chatId = "chat-$kind", chatKind = kind, title = title, members = members, unreadCount = unreadCount, notificationLevel = level,
)

private val conversations: List<AgentConversation> = listOf(
    AgentConversation("demo-main", "main", title = null, summary = null, lastAt = hoursAgo(0.2)),
    AgentConversation("demo-sam", "chat", title = null, summary = null, lastAt = hoursAgo(0.5), chat = chatOf("direct", null, demoMembers.take(2), unreadCount = 2)),
    AgentConversation("demo-group", "chat", title = "Weekend plans", summary = null, lastAt = hoursAgo(5), chat = chatOf("group", "Weekend plans", demoMembers, level = "mentions")),
    AgentConversation("demo-cards", "named", title = "Planning the trip", summary = "Flights, a hotel near the station, and a day in the mountains.", lastAt = hoursAgo(3)),
    AgentConversation("demo-empty", "named", title = null, summary = null, lastAt = hoursAgo(30)),
    AgentConversation("demo-long", "named", title = "A long instruction", summary = "A message too long for its bubble.", lastAt = hoursAgo(50)),
    AgentConversation("demo-goal", "goal", title = "File the quarterly invoices", summary = null, lastAt = hoursAgo(8), goal = "File the quarterly invoices with the accountant. Check each total against the bank first.", goalState = "waiting", goalNote = "Two invoices have no total. Tell me which month they belong to."),
    AgentConversation("demo-run", "run", title = "Morning mail check", summary = "What the agent did on its own.", lastAt = hoursAgo(20)),
)

private val archivedConversations: List<AgentConversation> = listOf(
    AgentConversation("demo-old", "named", title = "Birthday ideas", summary = "A list of ten.", lastAt = hoursAgo(400), archivedAt = hoursAgo(300)),
)

private fun json(value: List<AgentConversation>): String = TeaNodeJson.encodeToString(ListSerializer(AgentConversation.serializer()), value)

private val parkPicture = AgentAttachment("att-park", "park.jpg", "image/jpeg", 120_000)

private val tableAnswer = """
    Tomorrow looks calm. Here is what is on:

    | Time | What | Where |
    |---|---|---|
    | 09:00 | Dentist | Elm Street |
    | 13:30 | Lunch with Sam | The corner cafe |
    | 16:00 | Call about the invoices | Phone |

    You have a **free evening**. See the [city guide](https://www.example.com/guide?utm_source=mail&lang=en) for a place to eat, or run `make dinner` if you want me to book one.

    - Leave by 08:30 for the dentist
    - Take the umbrella

    ```
    Dentist: Elm Street 12
    Phone: +1 555 0100
    ```
    <!--suggestions:["Book dinner","Move lunch","That is all"]-->
""".trimIndent()

private fun mainMessages(): List<AgentMessage> = listOf(
    message("m1", "user", "What is on my calendar tomorrow?", 3.0),
    message("m2", "assistant", "", 2.99, toolCalls = listOf(AgentToolCall("call-1", "calendar_read", """{"day":"tomorrow"}"""))),
    message("m3", "tool", """{"events":3}""", 2.98, toolCallId = "call-1"),
    message(
        "m4", "assistant", tableAnswer, 2.97, reactions = listOf(AgentMessageReaction("❤️", "person")),
        usage = AgentUsage(4200, 380, 0.0132),
    ),
    message("m5", "user", "Show me the park near the dentist.", 0.4),
    message("m6", "assistant", "Here it is. It has a pond and a long path.\n\n![The park](https://example.com/park.jpg)\n\nThe photo above was taken last spring.", 0.39, reactions = listOf(AgentMessageReaction("👍", "person"), AgentMessageReaction("🎉", "agent"))),
    message(
        "m7", "assistant", "", 0.38,
        toolCalls = listOf(AgentToolCall("call-2", "share_file", "{}")),
    ),
    message(
        "m8", "tool",
        """{"files":[{"attachment_id":"att-notes","name":"trip-notes.pdf","content_type":"application/pdf","size":482113,"url":"/api/v1/agent/attachments/att-notes","caption":"The notes you asked for."}]}""",
        0.37, toolCallId = "call-2",
    ),
    message("m9", "user", "Thanks. Remind me to leave early.", 0.3, attachments = listOf(AgentAttachment("att-receipt", "receipt.pdf", "application/pdf", 20_480))),
    message("m10", "assistant", "Done. I will remind you at 08:15.", 0.29, usage = AgentUsage(900, 40, 0.0021)),
    message("m11", "note", "", 0.28, name = "goal_set").copy(content = "Plan the weekend trip"),
)

private val sentenceBlock = "This is the instruction the agent was given. It goes on for a long time and says what to check, what to skip and when to stop. "

private fun cardsMessages(): List<AgentMessage> = listOf(
    message("k1", "user", "Book the hotel near the station and tell my sister.", 3.5),
    message("k2", "assistant", "I found two hotels. Which one should I book?", 3.49),
    message(
        "k3", "note",
        """{"computer":"studio","paneId":"w1:p1","questionFingerprint":"f1","herdrSession":{"paneName":"site","codingAgentKind":"claude","question":{"questionFingerprint":"f1","questionText":"Bash command\nRun the build's tests\nmake -C build test && make -C build lint\nDo you want to proceed?","isMultipleChoice":false,"options":[{"optionNumber":1,"optionLabel":"Yes","optionDescription":"Run it once","herdrOptionKind":"choice"},{"optionNumber":2,"optionLabel":"Yes, and do not ask again","optionDescription":"For this command","herdrOptionKind":"choice"},{"optionNumber":3,"optionLabel":"No","optionDescription":"","herdrOptionKind":"choice"}]}}}""",
        3.4, name = "herdr_asked",
    ),
    message(
        "k4", "note",
        """{"computer":"studio","paneId":"w1:p2","questionFingerprint":"f0","herdrSession":{"paneName":"docs","codingAgentKind":"codex","question":{"questionFingerprint":"f0","questionText":"Which file should I edit?","isMultipleChoice":false,"options":[{"optionNumber":1,"optionLabel":"README","herdrOptionKind":"choice"}]}}}""",
        3.6, name = "herdr_asked",
    ),
    message("k5", "note", """{"computer":"studio","paneId":"w1:p2","questionFingerprint":"f0","answerText":"1. README"}""", 3.55, name = "herdr_answered"),
    message("k6", "user", "[goal done] goal-1 Invoices filed", 3.3),
    message("k7", "note", "you asked it to stop", 3.2, name = "stopped"),
    message("k8", "note", "The earlier chat was folded into a note about the hotels and the dates.", 3.1, name = "compacted").copy(role = "compaction"),
)

private val cardsInteractions = listOf(
    AgentInteraction("run-cards", "approve-1", "approval", "mail_send", "Send the booking confirmation to **Sam Rivera**?", createdAt = hoursAgo(0.1)),
    AgentInteraction("run-cards", "approve-2", "approval", "files_delete", "Delete the folder `old-trip`? This cannot be undone.", toolRisk = "destructive", createdAt = hoursAgo(0.09)),
    AgentInteraction("run-cards", "ask-1", "question", "ask_user", "Which hotel should I book?", listOf("The Linden, near the station", "The Harbor, by the water"), createdAt = hoursAgo(0.08)),
)

private fun chatMessage(
    id: String, role: String, content: String, hours: Number, authorKind: String, authorUserId: String, authorName: String, isMine: Boolean,
    ownerName: String? = null, agentId: String? = null, extra: (AgentMessage) -> AgentMessage = { each -> each },
): AgentMessage = extra(
    AgentMessage(
        id = id, createdAt = hoursAgo(hours), role = role, content = content, authorKind = authorKind, authorUserId = authorUserId, authorName = authorName,
        authorOwnerName = ownerName, authorAgentId = agentId ?: "", authorAvatarColorIndex = 3, isMine = isMine,
    ),
)

private fun samMessages(): List<AgentMessage> = listOf(
    chatMessage("c1", "assistant", "Are we still on for dinner on Friday?", 26.0, "person", "user-sam", "Sam Rivera", false),
    chatMessage("c2", "user", "Yes! I booked the table for seven.", 25.9, "person", "demo-user", "Robin", true),
    chatMessage("c3", "assistant", "Perfect. Take a look at https://www.example.com/menu?utm_source=chat&id=7 before we go.", 25.8, "person", "user-sam", "Sam Rivera", false),
    chatMessage("c4", "assistant", "", 3.0, "person", "user-sam", "Sam Rivera", false) { each ->
        each.copy(
            isVoiceMessage = true, transcriptText = "Running ten minutes late, save me a seat.", transcriptState = "done",
            attachments = listOf(AgentAttachment("voice-1", "Voice message.m4a", "audio/mp4", 9000, audioDurationMS = 7000, audioWaveform = "KFJ7oL7S29fHrYxlPTdZcoSMjIR3ZlRDNCkuMTIwLSspKCgoKSkoLTQ+SVVfZmll")),
        )
    },
    chatMessage("c5", "user", "No problem, I will order a starter.", 2.9, "person", "demo-user", "Robin", true) { each ->
        each.copy(references = listOf(AgentReference.reply("c4", "assistant", "Running ten minutes late, save me a seat.")), reactions = listOf(AgentMessageReaction("👍", "person", reactorUserId = "user-sam", reactorName = "Sam Rivera")))
    },
    chatMessage("c6", "assistant", "Sounds good, thank you! @Robin did you see the photos?", 0.6, "person", "user-sam", "Sam Rivera", false) { each ->
        each.copy(mentionedUserIds = listOf("demo-user"), editedAt = hoursAgo(0.5))
    },
    chatMessage("c7", "assistant", "", 0.5, "person", "user-sam", "Sam Rivera", false) { each -> each.copy(isDeleted = true) },
    chatMessage("c8", "assistant", "", 0.4, "person", "demo-user", "Robin", true) { each ->
        each.copy(callId = "call-1", role = "user", call = ChatCallRecord(id = "call-1", startedAt = hoursAgo(0.4), endedAt = hoursAgo(0.35), endReason = "hangup", members = listOf(ChatCallMemberRecord("demo-user"), ChatCallMemberRecord("user-sam"))))
    },
)

private fun groupMessages(): List<AgentMessage> = listOf(
    chatMessage("g1", "assistant", "I will bring the board games.", 9.0, "person", "user-alex", "Alex Chen", false),
    chatMessage("g2", "user", "I can drive. @all who needs a lift?", 8.9, "person", "demo-user", "Robin", true) { each -> each.copy(isAllMentioned = true) },
    chatMessage("g3", "assistant", "Both of us. @Pip what is the weather like on Saturday?", 8.8, "person", "user-sam", "Sam Rivera", false),
    chatMessage("g4", "assistant", "Saturday looks dry, with a high of 21 degrees and a light wind. Sunday may rain after lunch.", 8.7, "agent", "user-sam", "Pip", false, ownerName = "Sam Rivera", agentId = "agent-sam"),
    chatMessage("g5", "assistant", "That settles it: we go on Saturday.", 8.6, "agent", "demo-user", "Agent", true, ownerName = "Robin", agentId = "agent-robin"),
)

private fun longMessages(): List<AgentMessage> = listOf(
    message("l1", "user", (1..80).joinToString("\n") { line -> "Step $line: $sentenceBlock" }, 50.0),
    message("l2", "assistant", "Understood. I will start with the first twenty steps and tell you when they are done.", 49.9),
)

private fun goalMessages(): List<AgentMessage> = listOf(
    message("o1", "user", "[goal check-in] goal-1 File the quarterly invoices", 8.2),
    message("o2", "assistant", "Two invoices have no total. Which month do they belong to?", 8.1),
)

private fun runMessages(): List<AgentMessage> = listOf(
    message("r1", "user", "[schedule] Check the mail every morning", 20.0),
    message("r2", "assistant", "", 19.9, toolCalls = listOf(AgentToolCall("call-9", "mail_search", """{"query":"unread"}"""))),
    message("r3", "tool", """{"threads":2}""", 19.8, toolCallId = "call-9"),
    message("r4", "assistant", "Two new threads. Neither needs an answer today.", 19.7),
)

private fun messagesOf(conversationId: String?): List<AgentMessage> = when (conversationId) {
    null, "demo-main" -> mainMessages()
    "demo-cards" -> cardsMessages()
    "demo-sam" -> samMessages()
    "demo-group" -> groupMessages()
    "demo-long" -> longMessages()
    "demo-goal" -> goalMessages()
    "demo-run" -> runMessages()
    else -> emptyList()
}

private fun conversationOf(conversationId: String?): AgentConversation =
    conversations.firstOrNull { found -> found.id == (conversationId ?: "demo-main") } ?: archivedConversations.firstOrNull { found -> found.id == conversationId }
        ?: AgentConversation(conversationId ?: "demo-main", "named", title = null, summary = null, lastAt = null)

private val demoTodos = listOf(AgentTodo("t1", "Find two hotels near the station", hoursAgo(3.4)), AgentTodo("t2", "Ask which one to book", null), AgentTodo("t3", "Send the confirmation to Sam", null))

private fun conversationAnswer(request: DemoRequest): DemoResponse? {
    if (!Regex("""\bReadAgentConversation\b""").containsMatchIn(request.document)) return null
    val conversationId = request.variables.text("conversationId")
    val found = conversationOf(conversationId)
    val interactions = if (conversationId == "demo-cards") cardsInteractions else emptyList()
    val messages = messagesOf(conversationId)
    val read = JsonObject(
        mapOf(
            "conversation" to TeaNodeJson.encodeToJsonElement(AgentConversation.serializer(), found),
            "actingAs" to JsonNull,
            "messages" to TeaNodeJson.encodeToJsonElement(ListSerializer(AgentMessage.serializer()), messages),
            "total" to JsonPrimitive(messages.size),
            "todos" to TeaNodeJson.encodeToJsonElement(ListSerializer(AgentTodo.serializer()), if (conversationId == "demo-cards") demoTodos else emptyList()),
        ),
    )
    val answer = JsonObject(
        mapOf(
            "ReadAgentConversation" to read,
            "ListAgentInteractions" to TeaNodeJson.encodeToJsonElement(ListSerializer(AgentInteraction.serializer()), interactions),
        ),
    )
    return DemoResponse("""{"data":$answer}""")
}

private fun conversationsAnswer(request: DemoRequest): DemoResponse? {
    if (!Regex("""\bListAgentConversations\b""").containsMatchIn(request.document)) return null
    val isArchived = (request.variables["archived"] as? JsonPrimitive)?.content == "true"
    val query = request.variables.text("query")
    val listed = when {
        !query.isNullOrEmpty() -> (conversations + archivedConversations).filter { found ->
            (found.title ?: "").contains(query, ignoreCase = true) || (found.summary ?: "").contains(query, ignoreCase = true) || found.chat?.members.orEmpty().any { member -> member.name.orEmpty().contains(query, ignoreCase = true) }
        }
        isArchived -> archivedConversations
        else -> conversations
    }
    return DemoResponse("""{"data":{"ListAgentConversations":${json(listed)}}}""")
}

private val statusAnswer = DemoAnswerer { request ->
    if (!request.document.contains("ReadAgentTab")) return@DemoAnswerer null
    DemoResponse(
        """{"data":{"ReadAgentTab":{"attached":true,"title":"Example Shop","url":"https://www.example.com/shop"},"ReadAgentComputers":{"computers":[{"name":"studio","system":"Linux","description":"The desk computer"}]},"ReadAgent":{"budget":{"used":184000,"limit":300000,"resetsAt":"${Instant.now().plusSeconds(3 * 3600)}","cost":1.42,"costLimit":0,"currency":"USD"},"timezone":"UTC"}}}""",
    )
}

private val demoCommands = listOf(
    AgentBackgroundCommand("studio", "cmd-1", "npm run build -- --watch", "/work/site", "demo-main", hoursAgo(0.3), null, true),
)

private fun linkPreviewAnswer(request: DemoRequest): DemoResponse? {
    if (!Regex("""\bReadLinkPreviews\b""").containsMatchIn(request.document)) return null
    val urls = (request.variables["urls"] as? JsonArray)?.map { element -> (element as JsonPrimitive).content } ?: emptyList()
    val previews = urls.map { address ->
        if (address.contains("menu")) {
            LinkPreview(address, address.substringBefore('?') + "?id=7", "example.com/menu?id=7", true, "Tonight's menu", "Seasonal dishes, a short wine list and a quiet corner table.", "The Corner Table", "/api/v1/link-preview/image/menu", 600, 400)
        } else {
            LinkPreview(address, address, address, false)
        }
    }
    return DemoResponse("""{"data":{"ReadLinkPreviews":${TeaNodeJson.encodeToString(ListSerializer(LinkPreview.serializer()), previews)}}}""")
}

/** A picture for the answers that show one: a soft green gradient, drawn on the spot so no file is kept. */
private fun demoPicture(): ByteArray {
    val bitmap = Bitmap.createBitmap(600, 400, Bitmap.Config.ARGB_8888)
    val paint = Paint().apply { shader = LinearGradient(0f, 0f, 600f, 400f, Color.rgb(120, 190, 140), Color.rgb(40, 110, 90), Shader.TileMode.CLAMP) }
    Canvas(bitmap).drawRect(0f, 0f, 600f, 400f, paint)
    val output = ByteArrayOutputStream()
    bitmap.compress(Bitmap.CompressFormat.PNG, 100, output)
    return output.toByteArray()
}

private val pictureAnswer = DemoAnswerer { request ->
    if (request.path.contains("/picture") || request.path.contains("/link-preview/image") || request.path.contains("/api/v1/agent/attachments/att-park")) DemoResponse(demoPicture()) else null
}

private val people = listOf(
    ChatPerson("user-sam", "sam", "Sam Rivera", null, 3), ChatPerson("user-alex", "alex", "Alex Chen", null, 5), ChatPerson("user-kai", "kai", null, null, 1),
)

/**
 * What demo mode answers for Chats: one answer per document the screens send, so every screen and state can be reached
 * and photographed without a server. Most specific first.
 */
val chatsDemoAnswerers: List<DemoAnswerer> = listOf(
    statusAnswer,
    pictureAnswer,
    DemoAnswerer { request -> conversationAnswer(request) },
    DemoAnswerer { request -> conversationsAnswer(request) },
    DemoAnswerer { request -> linkPreviewAnswer(request) },
    graphQLField("ReadAgentWaiting", TeaNodeJson.encodeToString(AgentWaiting.serializer(), AgentWaiting(2, listOf(AgentWaitingInteraction("w1", "demo-cards", "approval", "Send the booking confirmation?", hoursAgo(0.1)), AgentWaitingInteraction("w2", "demo-goal", "question", "Which month?", hoursAgo(8.0)))))),
    graphQLField("ReadAgentVoice", """{"isVoiceAvailable":true,"sampleRate":24000,"isVoiceMessageAvailable":true}"""),
    graphQLField("ListAgentIdeas", """{"ideas":${TeaNodeJson.encodeToString(ListSerializer(AgentIdea.serializer()), listOf(AgentIdea("i1", "🧳", "Plan a weekend away", "Find a place, check the weather and list what to pack.", "Plan a weekend away"), AgentIdea("i2", "📬", "Catch up on mail", "Summarize what arrived since yesterday morning.", "Catch me up on my mail"), AgentIdea("i3", "🛒", "Make this week's shopping list", null, "Make this week's shopping list")))}}"""),
    graphQLField("MarkAgentIdeasShown", "true"),
    graphQLField("StartAgentIdea", """{"conversation":{"id":"demo-empty"},"openingRequest":"Plan a weekend away"}"""),
    graphQLField("ListAgentBackgroundCommands", TeaNodeJson.encodeToString(ListSerializer(AgentBackgroundCommand.serializer()), demoCommands)),
    graphQLField("ListCirclePeople", TeaNodeJson.encodeToString(ListSerializer(ChatPerson.serializer()), people)),
    graphQLField("OpenDirectChat", """{"id":"demo-sam"}"""),
    graphQLField("CreateGroupChat", """{"id":"demo-group"}"""),
    graphQLField("AskAgent", """{"runId":"run-demo","conversationId":"demo-main"}"""),
    graphQLField("StartAgentConversation", """{"id":"demo-empty","kind":"named","title":null,"summary":null,"lastAt":null}"""),
    graphQLField("ReactToAgentMessage", "true"),
    graphQLField("MarkChatRead", "true"),
    graphQLField("ResolveAgentConfirmation", "true"),
    graphQLField("AnswerAgentQuestion", "true"),
    graphQLField("AnswerAgentHerdrQuestion", """{"isAnswerAccepted":true,"answeredWith":"1. Yes"}"""),
    graphQLField("ReportAgentPresence", "true"),
    graphQLField("StopAgentRun", "true"),
    graphQLField("UpdateAgentConversation", """{"id":"demo-main"}"""),
    graphQLField("DeleteAgentConversation", "true"),
    graphQLField("SetAgentMainConversation", """{"id":"demo-main"}"""),
    graphQLField("SetChatNotificationLevel", """{"id":"demo-sam"}"""),
    graphQLField("SetChatAgentReading", """{"id":"demo-sam"}"""),
    graphQLField("SetChatAgentAskableByOthers", """{"id":"demo-sam"}"""),
    graphQLField("SetChatTyping", "true"),
    graphQLField("RenameChat", """{"id":"demo-group"}"""),
    graphQLField("AddChatMembers", """{"id":"demo-group"}"""),
    graphQLField("RemoveChatMember", """{"id":"demo-group"}"""),
    graphQLField("LeaveChat", "true"),
    graphQLField("EditChatMessage", """{"id":"c6"}"""),
    graphQLField("DeleteChatMessage", "true"),
    graphQLField("TranscribeChatVoiceMessage", """{"id":"c4","transcriptState":"done","transcriptText":"Running ten minutes late, save me a seat."}"""),
    // The agent's identity: its name, and that it is on. The head's budget rides on the same field.
    graphQLField("ReadAgent", """{"agent":{"id":"agent-robin","enabled":true,"name":"Robin's agent"},"budget":null,"timezone":"UTC"}"""),
)

/** The references the main conversation's next message may hold, for the screenshot of the composer with a reply above it. */
val demoReplyReference: AgentReference = AgentReference.reply("m4", "assistant", "Tomorrow looks calm. Here is what is on.")

/** The list of people used by the sheets' screenshots. */
val demoPeople: List<ChatPerson> = people
