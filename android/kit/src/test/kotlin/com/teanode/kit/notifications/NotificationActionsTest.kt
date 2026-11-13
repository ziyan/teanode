package com.teanode.kit.notifications

import com.teanode.kit.TeaNodeJson
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class NotificationActionsTest {
    private fun content(json: String) = TeaNodeJson.decodeFromString(NotificationContent.serializer(), json)

    private val keptAccounts = listOf("https://mail.example.com|sam", "https://mail.example.com|robin")

    private fun press(info: NotificationInfo, action: String, typed: String? = null, kept: List<String> = keptAccounts) =
        NotificationPress.decide(info, action, typed, kept)

    @Test
    fun eachKindHasItsButtons() {
        fun ids(json: String) = NotificationCategories.buttons(content(json)).map { button -> button.actionId }
        assertEquals(listOf("approve", "deny"), ids("""{"notificationKind":"approval","title":"t","body":"b"}"""))
        assertEquals(listOf("reply", "open"), ids("""{"notificationKind":"message","title":"t","body":"b"}"""))
        assertEquals(listOf("reply", "open"), ids("""{"notificationKind":"chat","title":"t","body":"b"}"""))
        assertEquals(listOf("open"), ids("""{"notificationKind":"circle","title":"t","body":"b"}"""))
        assertEquals(listOf("open"), ids("""{"notificationKind":"location","title":"t","body":"b"}"""))
        assertEquals(listOf("open"), ids("""{"notificationKind":"shopping","title":"t","body":"b"}"""))
        assertEquals(listOf("open"), ids("""{"notificationKind":"skill_share_call","title":"t","body":"b"}"""))
        assertEquals(listOf("open"), ids("""{"notificationKind":"herdr_question","title":"t","body":"b"}"""))
        assertEquals(emptyList<String>(), ids("""{"notificationKind":"test","title":"t","body":"b"}"""))
        // Any other kind is a message.
        assertEquals(listOf("reply", "open"), ids("""{"notificationKind":"photo","title":"t","body":"b"}"""))
    }

    @Test
    fun theEveningQuestionHasFiveMoodButtonsThatNeedNoUnlock() {
        val buttons = NotificationCategories.buttons(content("""{"notificationKind":"journalEveningQuestion","title":"t","body":"b","journalDay":"2026-10-26"}"""))
        assertEquals((1..5).map { level -> "journalMood$level" }, buttons.map { button -> button.actionId })
        assertEquals(
            listOf(FixedButtonWords.Awful, FixedButtonWords.Low, FixedButtonWords.Okay, FixedButtonWords.Good, FixedButtonWords.Great),
            buttons.map { button -> button.fixedWords },
        )
        assertTrue(buttons.none { button -> button.requiresUnlock || button.doesOpenApp })
        assertEquals(3, NotificationCategories.journalMoodLevel("journalMood3"))
        assertNull(NotificationCategories.journalMoodLevel("journalMood6"))
        assertNull(NotificationCategories.journalMoodLevel("journalMood0"))
        assertNull(NotificationCategories.journalMoodLevel("approve"))
    }

    @Test
    fun approvingNeedsTheUnlockedPhoneAndDenyingIsDestructive() {
        val buttons = NotificationCategories.buttons(content("""{"notificationKind":"approval","title":"t","body":"b"}"""))
        assertTrue(buttons.all { button -> button.requiresUnlock })
        assertEquals(listOf(false, true), buttons.map { button -> button.isDestructive })
    }

    @Test
    fun aQuestionShowsItsChoicesUpToFour() {
        val json = """{"notificationKind":"question","title":"t","body":"Which day?","runId":"r1","callId":"c1","buttons":[
            {"label":"Friday","buttonAction":"answer:1"},{"label":"Saturday","buttonAction":"answer:2"},{"label":"Sunday","buttonAction":"answer:3"},
            {"label":"Reply","buttonAction":"reply"},{"label":"Open","buttonAction":"open"}]}"""
        val buttons = NotificationCategories.buttons(content(json))
        assertEquals(listOf("answer:1", "answer:2", "answer:3", "reply"), buttons.map { button -> button.actionId })
        assertEquals(listOf("Friday", "Saturday", "Sunday", null), buttons.map { button -> button.literalLabel })
        assertTrue(buttons.last().isTextInput)
        // The same choices share a name, and other choices do not.
        val choices = content(json).buttons!!
        assertEquals(NotificationCategories.questionCategoryName(choices), NotificationCategories.questionCategoryName(choices))
        assertNotEquals(NotificationCategories.questionCategoryName(choices), NotificationCategories.questionCategoryName(choices.reversed()))
        assertTrue(NotificationCategories.questionCategoryName(choices).startsWith("question-"))
    }

    @Test
    fun kindsAreGroupedIntoChannels() {
        assertEquals(NotificationChannelKind.Chats, NotificationChannelKind.of("chat"))
        assertEquals(NotificationChannelKind.Places, NotificationChannelKind.of("shopping"))
        assertEquals(NotificationChannelKind.Agent, NotificationChannelKind.of("approval"))
        assertEquals(NotificationChannelKind.Journal, NotificationChannelKind.of("journalEveningQuestion"))
        assertEquals(NotificationChannelKind.Other, NotificationChannelKind.of("something-new"))
        assertEquals(NotificationChannelKind.entries.size, NotificationChannelKind.entries.map { kind -> kind.channelId }.toSet().size)
    }

    @Test
    fun theInfoOfAShownNotificationKeepsWhatItsButtonsAnswer() {
        val json = """{"notificationKind":"question","title":"t","body":"Which day?","runId":"r1","callId":"c1","conversationId":"conv","buttons":[
            {"label":"Friday","buttonAction":"answer:1"},{"label":"Reply","buttonAction":"reply"}]}"""
        val info = NotificationInfo.of(content(json), "https://mail.example.com|sam")
        assertEquals(mapOf("answer:1" to "Friday"), info.answers)
        assertEquals("r1", info.runId)
        assertEquals("conv", info.conversationId)
        val again = TeaNodeJson.decodeFromString(NotificationInfo.serializer(), TeaNodeJson.encodeToString(NotificationInfo.serializer(), info))
        assertEquals(info, again)
    }

    @Test
    fun openingGoesWhereTheNotificationIsAbout() {
        fun destination(json: String) = (press(NotificationInfo.of(content(json), keptAccounts[0]), "default") as NotificationPress.Open).destination
        assertEquals(NotificationDestination.Circles, destination("""{"notificationKind":"circle","title":"t","body":"b"}"""))
        assertEquals(NotificationDestination.Location("user-2"), destination("""{"notificationKind":"location","locationUserId":"user-2","title":"t","body":"b"}"""))
        assertEquals(NotificationDestination.Location(null), destination("""{"notificationKind":"location","title":"t","body":"b"}"""))
        assertEquals(NotificationDestination.Shopping("Example Market", null), destination("""{"notificationKind":"shopping","shoppingStoreName":"Example Market","shoppingListId":"l1","title":"t","body":"b"}"""))
        assertEquals(NotificationDestination.Shopping(null, "l1"), destination("""{"notificationKind":"shopping","shoppingListId":"l1","title":"t","body":"b"}"""))
        assertEquals(NotificationDestination.Coding("laptop", "p3"), destination("""{"notificationKind":"herdr_question","computer":"laptop","paneId":"p3","title":"t","body":"b"}"""))
        assertEquals(NotificationDestination.CodingHome, destination("""{"notificationKind":"herdr_question","title":"t","body":"b"}"""))
        assertEquals(NotificationDestination.JournalDay("2026-10-26"), destination("""{"notificationKind":"journalEveningQuestion","journalDay":"2026-10-26","title":"t","body":"b"}"""))
        assertEquals(NotificationDestination.Conversation("conv"), destination("""{"notificationKind":"message","conversationId":"conv","title":"t","body":"b"}"""))
        assertEquals(NotificationDestination.Main, destination("""{"notificationKind":"message","title":"t","body":"b"}"""))
    }

    @Test
    fun openingSwitchesToTheAccountItCameFrom() {
        val info = NotificationInfo.of(content("""{"notificationKind":"message","conversationId":"conv","title":"t","body":"b"}"""), keptAccounts[1])
        val opened = press(info, "open") as NotificationPress.Open
        assertEquals(keptAccounts[1], opened.accountKey)
    }

    @Test
    fun aNotificationFromAnAccountThatIsGoneSaysSo() {
        val info = NotificationInfo.of(content("""{"notificationKind":"message","title":"t","body":"b"}"""), "https://mail.example.com|gone")
        assertEquals(NotificationPress.UnknownAccount(isOpening = true, doesNameAccount = true), press(info, "default"))
        assertEquals(NotificationPress.UnknownAccount(isOpening = false, doesNameAccount = true), press(info, "approve"))
    }

    @Test
    fun aNotificationThatDoesNotSayItsAccountCanOnlyBeTheOneAccount() {
        val info = NotificationInfo.of(content("""{"notificationKind":"approval","runId":"r1","callId":"c1","title":"t","body":"b"}"""), "")
        assertEquals(NotificationPress.UnknownAccount(isOpening = false, doesNameAccount = false), press(info, "approve"))
        assertEquals(NotificationPress.Resolve("r1", "c1", true), press(info, "approve", kept = listOf(keptAccounts[0])))
    }

    @Test
    fun approvingAndDenyingResolveTheConfirmation() {
        val info = NotificationInfo.of(content("""{"notificationKind":"approval","runId":"r1","callId":"c1","title":"t","body":"b"}"""), keptAccounts[0])
        assertEquals(NotificationPress.Resolve("r1", "c1", true), press(info, "approve"))
        assertEquals(NotificationPress.Resolve("r1", "c1", false), press(info, "deny"))
    }

    @Test
    fun aReplyAnswersAQuestionOrSpeaksInTheConversation() {
        val question = NotificationInfo.of(content("""{"notificationKind":"question","runId":"r1","callId":"c1","conversationId":"conv","title":"t","body":"b"}"""), keptAccounts[0])
        assertEquals(NotificationPress.AnswerQuestion("r1", "c1", "Friday"), press(question, "reply", "  Friday "))
        val message = NotificationInfo.of(content("""{"notificationKind":"message","conversationId":"conv","title":"t","body":"b"}"""), keptAccounts[0])
        assertEquals(NotificationPress.Say("conv", "Thanks", isChat = false), press(message, "reply", "Thanks"))
        val chat = NotificationInfo.of(content("""{"notificationKind":"chat","conversationId":"conv","title":"t","body":"b"}"""), keptAccounts[0])
        assertEquals(NotificationPress.Say("conv", "Thanks", isChat = true), press(chat, "reply", "Thanks"))
        val main = NotificationInfo.of(content("""{"notificationKind":"message","title":"t","body":"b"}"""), keptAccounts[0])
        assertEquals(NotificationPress.Say(null, "Thanks", isChat = false), press(main, "reply", "Thanks"))
        // Nothing typed, nothing said.
        assertEquals(NotificationPress.None, press(message, "reply", "   "))
        assertEquals(NotificationPress.None, press(message, "reply", null))
    }

    @Test
    fun aChoiceAnswersWithItsWords() {
        val json = """{"notificationKind":"question","title":"t","body":"b","runId":"r1","callId":"c1","buttons":[{"label":"Friday","buttonAction":"answer:1"}]}"""
        val info = NotificationInfo.of(content(json), keptAccounts[0])
        assertEquals(NotificationPress.AnswerQuestion("r1", "c1", "Friday"), press(info, "answer:1"))
        assertEquals(NotificationPress.None, press(info, "answer:9"))
    }

    @Test
    fun aMoodButtonSetsThatDaysMood() {
        val info = NotificationInfo.of(content("""{"notificationKind":"journalEveningQuestion","journalDay":"2026-10-26","title":"t","body":"b"}"""), keptAccounts[0])
        assertEquals(NotificationPress.SetMood("2026-10-26", 4), press(info, "journalMood4"))
        assertEquals(NotificationPress.None, press(info, "journalMood9"))
        val noDay = NotificationInfo.of(content("""{"notificationKind":"journalEveningQuestion","journalDay":"yesterday","title":"t","body":"b"}"""), keptAccounts[0])
        assertEquals(NotificationPress.None, press(noDay, "journalMood4"))
    }

    @Test
    fun aDayIsAnExistingDayWrittenAsIsoText() {
        assertTrue(NotificationPress.isDayText("2026-10-26"))
        assertTrue(NotificationPress.isDayText("2028-02-29"))
        assertFalse(NotificationPress.isDayText("2026-02-29"))
        assertFalse(NotificationPress.isDayText("2026-1-5"))
        assertFalse(NotificationPress.isDayText(""))
    }
}
