package com.teanode.app.ideas

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.graphQLField
import java.time.Instant

private fun daysAgo(days: Long): String = Instant.now().minusSeconds(days * 86_400).toString()

private fun idea(
    id: String,
    kind: String,
    category: String?,
    emoji: String,
    headline: String,
    body: String?,
    reason: String?,
    status: String = "open",
    expiredReason: String? = null,
    conversationId: String? = null,
    closedDays: Long? = null,
): String =
    """{"id":"$id","ideaKind":"$kind","ideaCategory":${category?.let { value -> "\"$value\"" }},"emoji":"$emoji","headline":"$headline",
        "body":${body?.let { value -> "\"$value\"" }},"openingRequest":"Help me with this idea.","suggestionReason":${reason?.let { value -> "\"$value\"" }},
        "ideaStatus":"$status","expiredReason":${expiredReason?.let { value -> "\"$value\"" }},"startedConversationId":${conversationId?.let { value -> "\"$value\"" }},
        "createdAt":"${daysAgo(20)}","startedAt":${if (conversationId != null) "\"${daysAgo(5)}\"" else "null"},"closedAt":${closedDays?.let { days -> "\"${daysAgo(days)}\"" }}}"""

/** What demo mode answers for Ideas: ideas found for the person, the catalog by category, and a history of every kind. */
val ideasDemoAnswerers: List<DemoAnswerer> = listOf(
    graphQLField("StartAgentIdea", """{"conversation":{"id":"conversation-idea"},"openingRequest":"Help me with this idea."}"""),
    graphQLField("SetAgentIdeaStatus", """{"id":"idea-1","ideaStatus":"dismissed"}"""),
    graphQLField("ListAgentIdeas") {
        """{"ideas":[
            ${idea("p1", "personal", null, "✈️", "Plan the trip to the lake house", "Three emails this week mention a long weekend in June.", "Found in your mail from Sam and the calendar entry on June 12.")},
            ${idea("p2", "personal", null, "🧾", "Sort the year's receipts", "Fourteen receipts in your mail are not filed yet.", "Found in your mail.")},
            ${idea("c1", "catalog", "assistant", "🧠", "Teach your agent what you like", "Tell it three things it should remember about how you work.", null)},
            ${idea("c2", "catalog", "home", "🛠️", "Make a list of what to fix at home", "Walk through each room and let your agent keep the list.", null)},
            ${idea("c3", "catalog", "money", "💰", "Check what you pay every month", "Your agent can list the subscriptions it finds in your mail.", null)},
            ${idea("h1", "catalog", "fun", "🎵", "Build a playlist for the week", null, null, status = "started", conversationId = "conversation-old", closedDays = 4)},
            ${idea("h2", "personal", null, "📞", "Call the school about the trip", null, null, status = "done", closedDays = 8)},
            ${idea("h3", "catalog", "travel", "🧳", "Pack for a weekend away", null, null, status = "dismissed", closedDays = 12)},
            ${idea("h4", "catalog", "work", "🗂️", "Tidy the shared folder", null, null, status = "expired", expiredReason = "already_used", closedDays = 20)}],
          "ideaCategories":[{"ideaCategory":"assistant","emojis":["🧠"]},{"ideaCategory":"home","emojis":["🛠️"]},{"ideaCategory":"money","emojis":["💰"]},
            {"ideaCategory":"fun","emojis":["🎵"]},{"ideaCategory":"travel","emojis":["🧳"]},{"ideaCategory":"work","emojis":["🗂️"]}]}"""
    },
)
