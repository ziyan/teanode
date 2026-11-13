package com.teanode.kit.ideas

import com.teanode.kit.Connection
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive

/** The ideas' GraphQL: the same text as the iPhone app's (ios/TeaNodeKit Ideas.swift and Operations.swift). */
object IdeaDocuments {
    const val ALL_IDEAS = """query (${'$'}language: String) {
  ListAgentIdeas(language: ${'$'}language) {
    ideas { id ideaKind ideaCategory emoji headline body openingRequest suggestionReason ideaStatus expiredReason startedConversationId createdAt startedAt closedAt }
    ideaCategories { ideaCategory emojis }
  }
}"""
    const val SET_IDEA_STATUS = """mutation (${'$'}ideaId: String!, ${'$'}ideaStatus: String!) { SetAgentIdeaStatus(ideaId: ${'$'}ideaId, ideaStatus: ${'$'}ideaStatus) { id ideaStatus } }"""
}

/** IdeaEntry is one idea as the Ideas page lists it: one found for the person in their own mail and memory, or one of the catalog's, and what became of it. */
@Serializable
data class IdeaEntry(
    val id: String,
    /** "personal" or "catalog". */
    val ideaKind: String,
    val ideaCategory: String? = null,
    val emoji: String? = null,
    val headline: String,
    val body: String? = null,
    val openingRequest: String? = null,
    /** What prompted a personal idea. */
    val suggestionReason: String? = null,
    /** open, started, done, dismissed or expired. */
    val ideaStatus: String,
    val expiredReason: String? = null,
    val startedConversationId: String? = null,
    val createdAt: String,
    val startedAt: String? = null,
    val closedAt: String? = null,
) {
    val isOpen: Boolean get() = ideaStatus == "open"

    /** Whether a closed idea may be opened again: an expired catalog idea comes back only when it expired because it was used. */
    val canRestore: Boolean
        get() {
            if (ideaStatus == "open") return false
            return !(ideaKind == "catalog" && ideaStatus == "expired" && expiredReason != "already_used")
        }
}

/** IdeaList is every idea and the catalog's categories, in their order. */
@Serializable
data class IdeaList(val ideas: List<IdeaEntry>, val ideaCategories: List<Category>) {
    @Serializable
    data class Category(val ideaCategory: String, val emojis: List<String>)

    data class CategoryIdeas(val ideaCategory: String, val ideas: List<IdeaEntry>)

    /** The open ideas found for the person. */
    val forYou: List<IdeaEntry> get() = ideas.filter { idea -> idea.isOpen && idea.ideaKind == "personal" }

    /** The open catalog ideas, by category in the catalog's order. */
    val byCategory: List<CategoryIdeas>
        get() = ideaCategories.mapNotNull { category ->
            val shown = ideas.filter { idea -> idea.isOpen && idea.ideaKind == "catalog" && idea.ideaCategory == category.ideaCategory }
            if (shown.isEmpty()) null else CategoryIdeas(category.ideaCategory, shown)
        }

    /** What became of the rest, the latest first. */
    val history: List<IdeaEntry>
        get() = ideas.filter { idea -> !idea.isOpen }
            .sortedByDescending { idea -> idea.closedAt ?: idea.startedAt ?: idea.createdAt }
}

@Serializable
private class AllIdeasAnswer(val ListAgentIdeas: IdeaList)

@Serializable
private class IdeaStatusAnswer(val SetAgentIdeaStatus: JsonElement? = null)

/** Every idea, open or not, in the person's language. */
suspend fun Connection.allIdeas(language: String): IdeaList =
    execute<AllIdeasAnswer>(IdeaDocuments.ALL_IDEAS, mapOf("language" to JsonPrimitive(language))).ListAgentIdeas

/** Says what became of an idea: done, dismissed, or open again. */
suspend fun Connection.setIdeaStatus(ideaId: String, ideaStatus: String) {
    execute<IdeaStatusAnswer>(IdeaDocuments.SET_IDEA_STATUS, mapOf("ideaId" to JsonPrimitive(ideaId), "ideaStatus" to JsonPrimitive(ideaStatus)))
}
