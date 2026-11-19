package com.teanode.kit.feed

import com.teanode.kit.chats.AgentReference

/**
 * The address the app's link opener follows for a record a card points at (a mail, a recording, a memory page, a
 * finance transaction), or null for a record the app has no page for, which is shown but not opened.
 */
val AgentReference.feedLinkAddress: String?
    get() = when {
        !itemId.isNullOrEmpty() -> "mail:$itemId"
        !recordingId.isNullOrEmpty() -> "/recordings/$recordingId"
        !path.isNullOrEmpty() -> "memory:$path"
        !financeTransactionId.isNullOrEmpty() -> "/finance/transactions"
        else -> null
    }
