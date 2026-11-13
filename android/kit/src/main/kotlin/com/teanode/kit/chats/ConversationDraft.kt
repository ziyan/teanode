package com.teanode.kit.chats

import java.util.Base64

/**
 * ConversationDraft identifies drafts by both account and conversation.
 *
 * A key is "draft.account.", the account, a dot, and the conversation, each encoded so that no dot can appear inside
 * one: every draft of an account starts with the same prefix, and signing out of the account removes them all. The main
 * conversation is "main"; a named one is "c." and its id, so the two can never be taken for each other.
 */
object ConversationDraft {
    fun storageKey(accountKey: String, conversationId: String?): String {
        val conversationPart = if (conversationId == null) "main" else "c.${encoded(conversationId)}"
        return accountPrefix(accountKey) + conversationPart
    }

    /** Whether a stored key is a draft of this account, so signing out of it can remove them all. */
    fun isDraftOf(accountKey: String, storedKey: String): Boolean = storedKey.startsWith(accountPrefix(accountKey))

    fun accountPrefix(accountKey: String): String = "draft.account.${encoded(accountKey)}."

    /** Base64 with no dots in it, so a part cannot run into the next. */
    private fun encoded(text: String): String = Base64.getEncoder().encodeToString(text.toByteArray(Charsets.UTF_8))
}
