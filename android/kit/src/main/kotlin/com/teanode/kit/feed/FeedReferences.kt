package com.teanode.kit.feed

import com.teanode.kit.chats.AgentReference

/**
 * The address the app's link opener follows for a record a list row or an agenda entry points at, as the iPhone opens
 * one: a mail, a page of the agent's knowledge, a finance transaction. A reference naming anything else opens nothing.
 */
val AgentReference.feedLinkAddress: String?
    get() = when {
        !itemId.isNullOrEmpty() -> "mail:$itemId"
        !path.isNullOrEmpty() -> "memory:$path"
        !financeTransactionId.isNullOrEmpty() -> "/finance/transactions"
        else -> null
    }
