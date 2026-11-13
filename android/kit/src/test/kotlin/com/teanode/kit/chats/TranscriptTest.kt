package com.teanode.kit.chats

import com.teanode.kit.TeaNodeJson
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import com.teanode.kit.chats.TranscriptLine.Kind

private fun message(id: String, createdAt: String, role: String, content: String, name: String? = null, toolCallId: String? = null, toolCalls: List<AgentToolCall>? = null) =
    AgentMessage(id = id, createdAt = createdAt, role = role, content = content, name = name, toolCallId = toolCallId, toolCalls = toolCalls)

private fun herdrAsked(agentKind: String, questionText: String, options: String): String =
    """{"computer":"a laptop","paneId":"w1:p1","questionFingerprint":"f1","herdrSession":{"paneName":"site","codingAgentKind":"$agentKind","question":{"questionFingerprint":"f1","questionText":"$questionText","isMultipleChoice":false,"options":[$options]}}}"""

class TranscriptTest {
    @Test
    fun storedMessagesBecomeLines() {
        val transcript = Transcript(
            messages = listOf(
                message("1", "2026-10-10T09:00:00Z", "user", "What is on today?"),
                message("2", "2026-10-10T09:00:01Z", "assistant", "", toolCalls = listOf(AgentToolCall("c1", "calendar", "{}"))),
                message("3", "2026-10-10T09:00:02Z", "tool", "Nothing.", toolCallId = "c1"),
                message("4", "2026-10-10T09:00:03Z", "assistant", "Nothing at all."),
                message("5", "2026-10-10T10:00:00Z", "user", "[alert] mail showed something"),
            ),
            interactions = listOf(AgentInteraction("r", "c2", "approval", "mail_send", "Send the reply?")),
        )
        assertEquals(listOf(Kind.Person, Kind.Tool, Kind.Answer, Kind.CheckIn, Kind.Approval), transcript.lines.map { line -> line.kind })
        assertEquals("Nothing.", transcript.lines[1].text)
        assertEquals("calendar", transcript.lines[1].detail)
        assertEquals(CheckInOrigin.Alert.marker, transcript.lines[3].detail)
    }

    @Test
    fun anAnswerStreamsAndACardIsSettled() {
        val transcript = Transcript()
        transcript.apply(AgentEvent(kind = "text", runId = "r", sequence = 1, text = "Hel"))
        transcript.apply(AgentEvent(kind = "text", runId = "r", sequence = 2, text = "lo"))
        assertEquals(1, transcript.lines.size)
        assertEquals("Hello", transcript.lines[0].text)
        assertTrue(transcript.lines[0].isStreaming)
        transcript.apply(AgentEvent(kind = "message", runId = "r", sequence = 3, text = "Hello."))
        assertEquals("Hello.", transcript.lines[0].text)
        assertFalse(transcript.lines[0].isStreaming)

        val question = AgentEvent(kind = "question", runId = "r", sequence = 4, text = "Tomorrow\nFriday", callId = "c", note = "When?")
        transcript.apply(question)
        transcript.apply(question)
        assertEquals(2, transcript.lines.size, "a replayed event added a second card")
        assertEquals(listOf("Tomorrow", "Friday"), transcript.lines[1].choices)
        transcript.apply(AgentEvent(kind = "tool_result", runId = "r", sequence = 5, text = "Friday", callId = "c"))
        assertTrue(transcript.lines[1].isDone)
        assertEquals("Friday", transcript.lines[1].resolution)
    }

    @Test
    fun whatThisAppSentIsShownOnce() {
        val transcript = Transcript()
        transcript.addSent("Is it raining?")
        transcript.apply(AgentEvent(kind = "asked", runId = "r", sequence = 1, text = "Is it raining?"))
        transcript.apply(AgentEvent(kind = "asked", runId = "s", sequence = 1, text = "Asked on the dashboard."))
        transcript.apply(AgentEvent(kind = "asked", runId = "t", sequence = 1, text = "[alert] a parcel arrived"))
        assertEquals(listOf(Kind.Person, Kind.Person, Kind.CheckIn), transcript.lines.map { line -> line.kind })
        assertEquals("r", transcript.lines[0].runId)
    }

    @Test
    fun aCodingSessionsQuestionIsACardWithItsOptions() {
        val asked = herdrAsked(
            "claude", "Deploy now?",
            """{"optionNumber":1,"optionLabel":"Yes","herdrOptionKind":"choice"},{"optionNumber":2,"optionLabel":"Type something","herdrOptionKind":"freeText"}""",
        )
        val answered = """{"computer":"a laptop","paneId":"w1:p1","questionFingerprint":"f1","answerText":"1. Yes"}"""
        val messages = mutableListOf(
            message("1", "2026-10-10T09:00:00Z", "user", "[herdr question] a laptop w1:p1 f1\nNobody asked for this."),
            message("2", "2026-10-10T09:00:01Z", "assistant", "**Claude Code** in site asks:\n\n> Deploy now?"),
            message("3", "2026-10-10T09:00:01Z", "note", asked, name = "herdr_asked"),
        )
        var transcript = Transcript(messages)
        assertEquals(listOf(Kind.CodingQuestion), transcript.lines.map { line -> line.kind })
        assertEquals(listOf("Yes"), transcript.lines[0].choices)
        assertEquals("site", transcript.lines[0].codingQuestion?.sessionName)
        assertEquals(transcript.lines[0].id, transcript.openQuestion?.id)

        messages.add(message("4", "2026-10-10T09:02:00Z", "note", answered, name = "herdr_answered"))
        transcript = Transcript(messages)
        assertTrue(transcript.lines[0].isDone)
        assertEquals("1. Yes", transcript.lines[0].resolution)
        assertNull(transcript.openQuestion)
    }

    @Test
    fun aCodingSessionsQuestionFoldsWhenTheFeedSaysItWent() {
        val asked = herdrAsked(
            "codex", "Run the tests?",
            """{"optionNumber":1,"optionLabel":"Yes","herdrOptionKind":"choice"},{"optionNumber":2,"optionLabel":"No","herdrOptionKind":"choice"}""",
        )
        val transcript = Transcript(listOf(message("1", "2026-10-10T09:00:01Z", "note", asked, name = "herdr_asked")))
        assertEquals("a laptop", transcript.lines[0].codingQuestion?.computer)
        assertFalse(transcript.lines[0].isDone)

        fun answeredNote(fingerprint: String, answerText: String?) = AgentEvent(
            kind = "note", runId = "", sequence = 0, noteKind = "herdr_answered",
            noteDetail = """{"computer":"a laptop","paneId":"w1:p1","questionFingerprint":"$fingerprint"${if (answerText != null) ""","answerText":"$answerText"""" else ""}}""",
        )
        // Another question's note leaves this card alone.
        transcript.apply(answeredNote("f2", "No"))
        assertFalse(transcript.lines[0].isDone)
        // Answered at the keyboard: the card folds with the answer, live.
        transcript.apply(answeredNote("f1", "1. Yes"))
        assertTrue(transcript.lines[0].isDone)
        assertEquals("1. Yes", transcript.lines[0].resolution)
        assertNull(transcript.openQuestion)
        assertEquals(1, transcript.lines.size, "the note was drawn as a line of its own")
        // A note that knows no answer keeps the one this app gave.
        transcript.settle(transcript.lines[0].id, "2. No")
        transcript.apply(answeredNote("f1", null))
        assertEquals("2. No", transcript.lines[0].resolution)
        // A card settled here, then confirmed: an answer that later fails does not open it again.
        transcript.reopen(transcript.lines[0].id)
        assertTrue(transcript.lines[0].isDone, "a confirmed card was opened again")
    }

    @Test
    fun aCardAnsweredHereOpensAgainOnlyIfNothingElseSettledIt() {
        val asked = herdrAsked("codex", "Run the tests?", """{"optionNumber":1,"optionLabel":"Yes","herdrOptionKind":"choice"}""")
        val transcript = Transcript(listOf(message("1", "2026-10-10T09:00:01Z", "note", asked, name = "herdr_asked")))
        val cardId = transcript.lines.first().id
        // Settled as soon as it was tapped; the answer did not go through, so it opens again.
        transcript.settleAwaiting(cardId, "Yes")
        transcript.reopen(cardId)
        assertFalse(transcript.lines[0].isDone)
        assertEquals("", transcript.lines[0].resolution)
        // Settled when tapped, then answered at the computer while the answer was on its way: it stays answered.
        transcript.settleAwaiting(cardId, "Yes")
        transcript.apply(
            AgentEvent(
                kind = "note", runId = "", sequence = 0, noteKind = "herdr_answered",
                noteDetail = """{"computer":"a laptop","paneId":"w1:p1","questionFingerprint":"f1","answerText":"1. Yes"}""",
            ),
        )
        transcript.reopen(cardId)
        assertTrue(transcript.lines[0].isDone, "an answer from the computer was undone")
        assertEquals("1. Yes", transcript.lines[0].resolution)
    }

    @Test
    fun aReplayedTurnIsNotDrawnTwice() {
        // Read back after the feed dropped: the person's message and a tool call are stored; the feed then replays the turn from its start.
        val transcript = Transcript(
            listOf(
                message("1", "2026-10-10T09:00:00Z", "user", "Plan tomorrow."),
                message("2", "2026-10-10T09:00:01Z", "assistant", "", toolCalls = listOf(AgentToolCall("c1", "calendar", "{}"))),
            ),
        )
        transcript.apply(AgentEvent(kind = "asked", runId = "r", sequence = 1, text = "Plan tomorrow."))
        transcript.apply(AgentEvent(kind = "tool_call", runId = "r", sequence = 2, tool = "calendar", callId = "c1"))
        assertEquals(listOf(Kind.Person, Kind.Tool), transcript.lines.map { line -> line.kind })
    }

    @Test
    fun aLongTurnReadBackMidwayIsDrawnOnceWhenItReplays() {
        val messages = mutableListOf(
            message("1", "2026-10-10T09:00:00Z", "user", "Archive the notices."),
            message("2", "2026-10-10T09:00:01Z", "assistant", "I will check the senders first."),
        )
        for (index in 0 until 10) {
            messages.add(message("t$index", "2026-10-10T09:00:02Z", "assistant", "", toolCalls = listOf(AgentToolCall("c$index", "mail_search", "{}"))))
        }
        messages.add(message("n", "2026-10-10T09:00:03Z", "compaction", "The note."))
        val transcript = Transcript(messages)
        transcript.apply(AgentEvent(kind = "asked", runId = "r", sequence = 0, text = "Archive the notices."))
        transcript.apply(AgentEvent(kind = "text", runId = "r", sequence = 1, text = "I will check "))
        transcript.apply(AgentEvent(kind = "text", runId = "r", sequence = 2, text = "the senders first."))
        transcript.apply(AgentEvent(kind = "message", runId = "r", sequence = 3, text = "I will check the senders first."))
        transcript.apply(AgentEvent(kind = "tool_call", runId = "r", sequence = 4, tool = "mail_search", callId = "c0"))
        assertEquals(1, transcript.lines.count { line -> line.kind == Kind.Person })
        assertEquals(listOf("I will check the senders first."), transcript.lines.filter { line -> line.kind == Kind.Answer }.map { line -> line.text })
        assertEquals(listOf(Kind.Person, Kind.Answer, Kind.Tool), transcript.lines.map { line -> line.kind })
    }

    @Test
    fun aTurnWrittenIntoWhileItRanIsDrawnOnceWhenItReplays() {
        val transcript = Transcript(
            listOf(
                message("0", "2026-10-03T09:00:00Z", "user", "Import the statement."),
                message("0a", "2026-10-03T09:00:05Z", "assistant", "Imported."),
                message("1", "2026-10-10T09:00:00Z", "user", "Import the statement."),
                message("2", "2026-10-10T09:00:04Z", "assistant", "Importing the visible months."),
                message("3", "2026-10-10T09:00:09Z", "user", "And the other bank."),
            ),
        )
        transcript.apply(AgentEvent(kind = "asked", runId = "r1", sequence = 0, at = "2026-10-10T09:00:00.123Z", text = "Import the statement."))
        transcript.apply(AgentEvent(kind = "message", runId = "r1", sequence = 1, text = "Importing the visible months."))
        transcript.apply(AgentEvent(kind = "asked", runId = "r2", sequence = 0, at = "2026-10-10T09:00:08Z", text = "And the other bank."))
        assertEquals(
            listOf("Import the statement.", "Imported.", "Import the statement.", "Importing the visible months.", "And the other bank."),
            transcript.lines.map { line -> line.text },
        )
        assertEquals("1", transcript.lines[2].id, "the turn's stored opening was not the one matched")
        assertEquals("r1", transcript.lines[2].runId)
    }

    @Test
    fun aTurnOfTheAgentsOwnWaitingOnAQuestionKeepsTheQuestionLastWhenItReplays() {
        val checkIn = "[speaking first] a few questions."
        val messages = listOf(
            message("0", "2026-10-10T08:00:00Z", "assistant", "Good morning."),
            message("1", "2026-10-10T09:00:00Z", "user", checkIn),
            message("2", "2026-10-10T09:00:01Z", "assistant", "I'd like to check a few things.", toolCalls = listOf(AgentToolCall("c1", "ask_user", "{}"))),
            message("3", "2026-10-10T09:00:02Z", "tool", "Yes", toolCallId = "c1"),
            message("4", "2026-10-10T09:00:03Z", "assistant", "", toolCalls = listOf(AgentToolCall("c2", "ask_user", "{}"))),
            message("5", "2026-10-10T09:00:04Z", "tool", "Yes", toolCallId = "c2"),
            message("6", "2026-10-10T09:00:05Z", "assistant", "", toolCalls = listOf(AgentToolCall("c3", "ask_user", "{}"))),
        )
        val open = AgentInteraction("r", "c3", "question", "ask_user", "Anything new?", listOf("No"), createdAt = "2026-10-10T09:00:05Z")
        val transcript = Transcript(messages, listOf(open))
        assertEquals("Anything new?", transcript.lines.last().text)
        transcript.apply(AgentEvent(kind = "asked", runId = "r", sequence = 0, at = "2026-10-10T09:00:00.200Z", text = checkIn))
        transcript.apply(AgentEvent(kind = "message", runId = "r", sequence = 1, text = "I'd like to check a few things."))
        for ((sequence, callId) in listOf(2 to "c1", 5 to "c2")) {
            transcript.apply(AgentEvent(kind = "tool_call", runId = "r", sequence = sequence, tool = "ask_user", callId = callId))
            transcript.apply(AgentEvent(kind = "question", runId = "r", sequence = sequence + 1, text = "Yes\nNo", callId = callId, note = "Right?"))
            transcript.apply(AgentEvent(kind = "tool_result", runId = "r", sequence = sequence + 2, text = "Yes", callId = callId))
        }
        transcript.apply(AgentEvent(kind = "tool_call", runId = "r", sequence = 8, tool = "ask_user", callId = "c3"))
        transcript.apply(AgentEvent(kind = "question", runId = "r", sequence = 9, text = "No", callId = "c3", note = "Anything new?"))
        val shown = transcript.lines.filter { line -> line.kind != Kind.Tool }
        assertEquals(listOf("Good morning.", checkIn, "I'd like to check a few things.", "Right?", "Right?", "Anything new?"), shown.map { line -> line.text })
        assertEquals("Anything new?", transcript.openQuestion?.text)
    }

    @Test
    fun aWeeklyCheckInDoesNotTakeLastWeeksTurnWithIt() {
        val checkIn = "[schedule] the weekly review."
        val transcript = Transcript(
            listOf(message("1", "2026-10-03T09:00:00Z", "user", checkIn), message("2", "2026-10-03T09:00:05Z", "assistant", "Last week's review.")),
        )
        transcript.apply(AgentEvent(kind = "asked", runId = "r2", sequence = 0, at = "2026-10-10T09:00:00Z", text = checkIn))
        assertTrue(transcript.lines.any { line -> line.text == "Last week's review." })
    }

    @Test
    fun aFileSentFromThisAppKeepsItsFileWhenTheTurnStarts() {
        val transcript = Transcript()
        transcript.addSent("Here it is.", attachments = listOf(AgentAttachment("a1", "receipt.jpg", "image/jpeg", 10)))
        transcript.apply(AgentEvent(kind = "asked", runId = "r", sequence = 0, text = "Here it is."))
        assertEquals(1, transcript.lines.size)
        assertEquals(listOf("a1"), transcript.lines[0].attachments.map { attachment -> attachment.id })
        assertEquals("r", transcript.lines[0].runId)
    }

    @Test
    fun suggestionsAreOfferedAndNotShown() {
        val split = Suggestions.split("Shall I book it?\n<!--suggestions:[\"Yes\",\"Not yet\"]-->")
        assertEquals("Shall I book it?", split.text)
        assertEquals(listOf("Yes", "Not yet"), split.suggestions)
        assertEquals("Working on it", Suggestions.split("Working on it <!--sugg").text)
        val transcript = Transcript(listOf(message("1", "2026-10-10T09:00:00Z", "assistant", "Shall I book it?\n<!--suggestions:[\"Yes\",\"Not yet\"]-->")))
        assertEquals(listOf("Yes", "Not yet"), transcript.offeredSuggestions)
    }

    @Test
    fun aTurnsUsageIsOnItsLastAnswer() {
        val first = message("2", "2026-10-10T09:00:01Z", "assistant", "Looking.").copy(usage = AgentUsage(100, 10, 0.01))
        val second = message("3", "2026-10-10T09:00:02Z", "assistant", "Done.").copy(usage = AgentUsage(200, 20, 0.02))
        val transcript = Transcript(listOf(message("1", "2026-10-10T09:00:00Z", "user", "Go."), first, second))
        assertNull(transcript.lines[1].usage)
        assertEquals(300, transcript.lines[2].usage?.promptTokens)
    }

    @Test
    fun aQueuedTurnSaysSoUntilItBegins() {
        val transcript = Transcript()
        transcript.apply(AgentEvent(kind = "note", runId = "r2", sequence = 1, noteKind = "queued"))
        assertEquals(true, transcript.lines.firstOrNull()?.isPending)
        transcript.apply(AgentEvent(kind = "asked", runId = "r2", sequence = 2, text = "Next thing"))
        assertEquals(listOf(Kind.Person), transcript.lines.map { line -> line.kind })
    }

    @Test
    fun serverTimesAreRead() {
        assertNotNull(ServerTime.instant("2026-10-10T17:58:04.123456789-04:00"))
        assertNotNull(ServerTime.instant("2026-10-10T21:58:04Z"))
        assertNull(ServerTime.instant("yesterday"))
        assertTrue(ServerTime.isLater("2026-10-10T21:58:04.5Z", "2026-10-10T21:58:04Z"))
        assertFalse(ServerTime.isLater("2026-10-10T21:58:04Z", "2026-10-10T21:58:04Z"))
        assertTrue(ServerTime.isLater("2026-10-10T21:58:04Z", "not a time"))
        assertFalse(ServerTime.isLater("not a time", "2026-10-10T21:58:04Z"))
    }

    @Test
    fun earlierHistoryKeepsStreamingAnswersAndUnconfirmedMessages() {
        val recent = message("recent", "", "user", "A question")
        val older = message("older", "", "user", "An earlier question")
        val transcript = Transcript(listOf(recent))
        transcript.apply(AgentEvent(kind = "text", runId = "active-turn", sequence = 1, text = "An answer in progress"))
        transcript.addSent("A message being sent")
        val liveLines = transcript.lines
        transcript.prepend(listOf(older, recent), earlierCount = 1)
        assertEquals("older", transcript.lines.first().messageId)
        assertEquals(liveLines, transcript.lines.drop(1))
        transcript.prepend(listOf(older), earlierCount = 1)
        assertEquals(liveLines.size + 1, transcript.lines.size)
    }

    @Test
    fun earlierHistoryKeepsReactionsAndSettledCards() {
        val recent = message("recent", "", "assistant", "An answer")
        val transcript = Transcript(listOf(recent))
        transcript.react("recent", "👍", "person")
        transcript.apply(AgentEvent(kind = "confirmation", runId = "active-turn", sequence = 1, tool = "a-tool", callId = "approval-call"))
        transcript.settle(transcript.lines.last().id, "Approved")
        val liveLines = transcript.lines
        transcript.prepend(listOf(message("older", "", "user", "Earlier"), recent), earlierCount = 1)
        assertEquals(liveLines, transcript.lines.drop(1))
    }

    @Test
    fun toolResultsAreMatchedAcrossHistoryPages() {
        val toolCall = message("older", "", "assistant", "", toolCalls = listOf(AgentToolCall("tool-call", "a-tool", "{}")))
        val toolResult = message("recent", "", "tool", "The tool's answer", toolCallId = "tool-call")
        val transcript = Transcript(listOf(toolResult))
        transcript.prepend(listOf(toolCall, toolResult), earlierCount = 1)
        assertEquals(1, transcript.lines.size)
        assertEquals("The tool's answer", transcript.lines.first().text)
        assertTrue(transcript.lines.first().isDone)
    }

    @Test
    fun earlierHistoryDoesNotBringBackWhatTheFeedTookAway() {
        val older = message("older", "", "user", "Earlier")
        val recent = message("recent", "", "assistant", "An answer the replay redraws")
        val transcript = Transcript(listOf(recent))
        transcript.remove("recent")
        assertTrue(transcript.lines.isEmpty())
        transcript.prepend(listOf(older, recent), earlierCount = 1)
        assertEquals(listOf("older"), transcript.lines.map { line -> line.id })
    }

    @Test
    fun aRefusedRequestRemovesOnlyItsPendingMessage() {
        val transcript = Transcript()
        val firstLineId = transcript.addSent("First request")
        val secondLineId = transcript.addSent("Second request")
        transcript.withdrawSent(firstLineId)
        assertEquals(listOf(secondLineId), transcript.lines.map { line -> line.id })
    }

    @Test
    fun aFeedConfirmationKeepsOtherPendingMessages() {
        val transcript = Transcript()
        val firstLineId = transcript.addSent("First request")
        val secondLineId = transcript.addSent("Second request")
        transcript.apply(AgentEvent(kind = "asked", runId = "first-turn", sequence = 1, text = "First request"))
        assertEquals(listOf(firstLineId, secondLineId), transcript.lines.map { line -> line.id })
        assertEquals("first-turn", transcript.lines[0].runId)
        transcript.withdrawSent(firstLineId)
        assertEquals(2, transcript.lines.size)
        transcript.withdrawSent(secondLineId)
        assertEquals(listOf(firstLineId), transcript.lines.map { line -> line.id })
    }

    @Test
    fun aFeedConfirmationDoesNotRemoveAnAnswerAlreadyArriving() {
        val transcript = Transcript()
        transcript.addSent("A request")
        transcript.apply(AgentEvent(kind = "text", runId = "a-turn", sequence = 2, text = "An answer"))
        transcript.apply(AgentEvent(kind = "asked", runId = "a-turn", sequence = 1, text = "A request"))
        assertEquals(listOf("A request", "An answer"), transcript.lines.map { line -> line.text })
    }

    @Test
    fun interleavedAnswersKeepTheirOwnTextAndCompletion() {
        val transcript = Transcript()
        transcript.apply(AgentEvent(kind = "text", runId = "first-turn", sequence = 1, text = "First"))
        transcript.apply(AgentEvent(kind = "text", runId = "second-turn", sequence = 1, text = "Second"))
        transcript.apply(AgentEvent(kind = "text", runId = "first-turn", sequence = 2, text = " answer"))
        transcript.apply(AgentEvent(kind = "message", runId = "first-turn", sequence = 3, text = "First answer."))
        assertEquals(listOf("First answer.", "Second"), transcript.lines.map { line -> line.text })
        assertFalse(transcript.lines[0].isStreaming)
        assertTrue(transcript.lines[1].isStreaming)
        transcript.apply(AgentEvent(kind = "message", runId = "second-turn", sequence = 2, text = "Second answer."))
        assertEquals(listOf("First answer.", "Second answer."), transcript.lines.map { line -> line.text })
        assertFalse(transcript.lines[1].isStreaming)
    }

    @Test
    fun theSuggestionsLineIsNotShownWhileItIsWritten() {
        val transcript = Transcript()
        transcript.apply(AgentEvent(kind = "text", runId = "r", sequence = 1, text = "Booked for Friday."))
        for ((index, piece) in listOf("\n<!--sugg", "estions:[\"Thanks\",", "\"Not now\"]-->").withIndex()) {
            transcript.apply(AgentEvent(kind = "text", runId = "r", sequence = 2 + index, text = piece))
            assertEquals("Booked for Friday.", transcript.lines.last().text)
        }
        transcript.apply(AgentEvent(kind = "message", runId = "r", sequence = 5, text = "Booked for Friday.\n<!--suggestions:[\"Thanks\",\"Not now\"]-->"))
        assertEquals("Booked for Friday.", transcript.lines.last().text)
        assertEquals(listOf("Thanks", "Not now"), transcript.offeredSuggestions)
    }

    @Test
    fun reactionsAreReadWithTheirMessagesAndReplacedAndRemovedByEvents() {
        val messages = messagesOf(
            """{"id":"m1","createdAt":"2026-10-11T09:00:00Z","role":"user","content":"Thanks, that helps."}""",
            """{"id":"m2","createdAt":"2026-10-11T09:00:05Z","role":"assistant","content":"Glad to.",
              "reactions":[{"emoji":"👍","reactorKind":"person","reactedAt":"2026-10-11T09:01:00Z"}]}""",
        )
        val transcript = Transcript(messages)
        fun line(messageId: String) = transcript.lines.first { candidate -> candidate.messageId == messageId }
        assertEquals("👍", line("m2").reaction("person"))
        assertNull(line("m2").reaction("agent"))
        transcript.apply(AgentEvent(kind = "reacted", runId = "", sequence = 0, messageId = "m1", emoji = "❤️", reactorKind = "agent"))
        assertEquals("❤️", line("m1").reaction("agent"))
        transcript.apply(AgentEvent(kind = "reacted", runId = "", sequence = 0, messageId = "m2", emoji = "🎉", reactorKind = "person"))
        assertEquals(1, line("m2").reactions.size)
        assertEquals("🎉", line("m2").reaction("person"))
        transcript.apply(AgentEvent(kind = "reacted", runId = "", sequence = 0, messageId = "m2", emoji = "", reactorKind = "person"))
        assertNull(line("m2").reaction("person"))
    }

    @Test
    fun aReactedEventDecodesFromTheFeed() {
        val event = eventOf("""{"kind":"reacted","runId":"","sequence":0,"messageId":"m2","emoji":"👍","reactorKind":"person"}""")
        assertEquals("m2", event.messageId)
        assertEquals("👍", event.emoji)
        assertEquals("person", event.reactorKind)
    }

    @Test
    fun aRecordingReferenceIsNamedByItsTitleAndSentWithItsKeys() {
        val reference = AgentReference.recording("recording-1", "Quarterly planning")
        assertEquals("recording-1", reference.recordingId)
        assertEquals("Quarterly planning", reference.label)
        assertFalse(reference.isReply)
        val sent = TeaNodeJson.parseToJsonElement(TeaNodeJson.encodeToString(AgentReference.serializer(), reference)).toString()
        assertTrue(sent.contains(""""recordingId":"recording-1""""))
        assertTrue(sent.contains(""""recordingTitle":"Quarterly planning""""))
    }

    @Test
    fun theHeadlineOfAToolApprovalNamesTheToolAndWhatItIsFor() {
        fun question(text: String) = CodingQuestion("a laptop", "w1:p1", "f1", "claude", "site", text, false, emptyList())
        val approval = question(
            "Bash command\nRun the build's tests\nmake -C build test && make -C build lint\n⚠ Claude Code will automatically deny this request in 0:12, to avoid blocking progress on an unattended session\nDo you want to proceed?",
        )
        assertEquals("Bash command · Run the build's tests", approval.headline)
        assertFalse(approval.shownText.contains("automatically deny"))
        assertTrue(approval.shownText.contains("make -C build test"))
        assertEquals("Which fruit should we pick?", question("Fruit\nWhich fruit should we pick?").headline)
        assertEquals("Which fruit should we pick?", question("Which fruit should we pick?").headline)
        val long = question("word ".repeat(60) + "?")
        assertEquals(158, long.headline.length)
        assertTrue(long.headline.endsWith("…"))
    }

    @Test
    fun filesAToolHandsOverAreRead() {
        val shared = """<untrusted-data>{"attachment_id":"a1","name":"receipt.jpg","content_type":"image/jpeg","size":2048,"url":"/api/v1/agent/attachments/a1","caption":"The receipt"}</untrusted-data>"""
        assertEquals(listOf("receipt.jpg"), AgentFile.of(shared).map { file -> file.name })
        assertEquals(true, AgentFile.of(shared).firstOrNull()?.isImage)
        val artifact = """{"artifact_id":"b2","title":"Spending","kind":"html","url":"/api/v1/agent/attachments/b2"}"""
        assertEquals("html", AgentFile.of(artifact).firstOrNull()?.artifactKind)
        val elsewhere = """{"attachment_id":"c3","name":"x","url":"https://example.com/x"}"""
        assertTrue(AgentFile.of(elsewhere).isEmpty())
    }

    @Test
    fun aPageSentToTheMainChatIsShownAsAnArtifact() {
        val sent = """{"attachment_id":"d4","name":"notes.html","content_type":"text/html","size":40,"url":"/api/v1/agent/attachments/d4","sent_by":"Example Coder"}"""
        assertEquals("html", AgentFile.of(sent).firstOrNull()?.artifactKind)
        val handed = """{"attachment_id":"d5","name":"notes.html","content_type":"text/html","size":40,"url":"/api/v1/agent/attachments/d5"}"""
        assertEquals("", AgentFile.of(handed).firstOrNull()?.artifactKind)
        assertEquals(CheckInOrigin.FileSent, CheckInOrigin.of("[file sent] Example Coder sent the person notes.html"))
    }

    @Test
    fun aFinishedGoalIsAnOwnTurnAndNotThePersonsWords() {
        assertEquals(CheckInOrigin.GoalDone, CheckInOrigin.of("[goal done] goal-1 Invoices filed"))
        assertEquals(CheckInOrigin.GoalNeedsYou, CheckInOrigin.of("[goal needs you] goal-1 Invoices filed"))
    }

    @Test
    fun aPhotoTheAgentShowedKeepsItsIdOnlyFromThePhotoTool() {
        val shown = """{"files":[{"photo_id":"photo-1_A","attachment_id":"a1","name":"IMG_0001.jpg","content_type":"image/jpeg","size":12,"url":"/api/v1/agent/attachments/a1"}]}"""
        assertEquals("photo-1_A", AgentFile.of(shown, "photo").firstOrNull()?.photoId)
        assertNull(AgentFile.of(shown, "share_file").firstOrNull()?.photoId)
        assertNull(AgentFile.of(shown).firstOrNull()?.photoId)
        for (badId in listOf("../settings", "a/b", "a?b", "", "x".repeat(65))) {
            val result = """{"files":[{"photo_id":"$badId","attachment_id":"a1","name":"x.jpg","url":"/api/v1/agent/attachments/a1"}]}"""
            val files = AgentFile.of(result, "photo")
            assertEquals(1, files.size)
            assertNull(files.firstOrNull()?.photoId, badId)
        }
    }

    @Test
    fun aPictureOnItsOwnLineIsABlock() {
        assertEquals(
            listOf(MarkdownBlock.Paragraph("Here it is:"), MarkdownBlock.Image("The park", "https://example.com/park.jpg", null)),
            MarkdownBlock.parse("Here it is:\n\n![The park](https://example.com/park.jpg)"),
        )
        assertEquals(
            "/api/v1/agent/conversations/c1/picture?url=https%3A%2F%2Fexample.com%2Fa%20b.jpg",
            picturePath("c1", "https://example.com/a b.jpg"),
        )
    }

    @Test
    fun aTableAndARuleAreRead() {
        val text = "Two stops:\n\n| Time | Place |\n|---|:---:|\n| 10:00 | Park |\n| 11:00 | Lunch |\n\n---\nDone."
        assertEquals(
            listOf(
                MarkdownBlock.Paragraph("Two stops:"),
                MarkdownBlock.Table(listOf("Time", "Place"), listOf(listOf("10:00", "Park"), listOf("11:00", "Lunch"))),
                MarkdownBlock.Rule,
                MarkdownBlock.Paragraph("Done."),
            ),
            MarkdownBlock.parse(text),
        )
    }

    @Test
    fun anAnswersBlocksAreRead() {
        val text = "**Claude Code** asks:\n\n> What now?\n> Pick one.\n\n1. Stop\n2. Wait here,\n   then go on\n\n- one\n- two\n\n## Next\n```\nmake test\n```"
        assertEquals(
            listOf(
                MarkdownBlock.Paragraph("**Claude Code** asks:"),
                MarkdownBlock.Quote("What now?\nPick one."),
                MarkdownBlock.ListBlock(listOf(MarkdownBlock.ListItem("1.", "Stop"), MarkdownBlock.ListItem("2.", "Wait here, then go on"))),
                MarkdownBlock.ListBlock(listOf(MarkdownBlock.ListItem("•", "one"), MarkdownBlock.ListItem("•", "two"))),
                MarkdownBlock.Heading("Next"),
                MarkdownBlock.Code("make test"),
            ),
            MarkdownBlock.parse(text),
        )
    }

    @Test
    fun anAnswerIsSaidWithoutItsMarks() {
        val said = SpokenText.of("## Tomorrow\n\n- **9:00** Swim. See [the map](https://example.com).\n\n```\ncode\n```\nDone.\n<!--suggestions:[\"Yes\",\"No\"]-->")
        assertEquals("Tomorrow 9:00 Swim. See the map. Done.", said)
        assertEquals("It is sunny. Bring a hat.", SpokenText.of("It is sunny. <!-- kept: 2 --> Bring a hat.\n<!--suggestions:[\"Yes\",\"No\"]-->"))
    }

    @Test
    fun onlyWholeSentencesAreSpoken() {
        val text = "It is sunny. Bring a hat! And a coa"
        val split = SpokenText.sentences(text)
        assertEquals(listOf("It is sunny.", "Bring a hat!"), split.sentences)
        assertEquals("And a coa", text.substring(split.consumed).trim())
    }
}
