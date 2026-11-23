package com.teanode.kit.knowledge

import kotlinx.serialization.Serializable

// Pages of the agent's memory shared with other people, and the pages other people shared. The same operations the
// dashboard, the command line and the memory tool use. What a reader is shown is decided on the server
// (internal/agent/shared_memory.go); nothing here changes a shared page.

/** SharedKnowledgeFact is one fact of a shared page, numbered as its owner numbers it, with where the owner's agent read it and, when the owner chose to share them, the words. */
@Serializable
data class SharedKnowledgeFact(
    val number: Int,
    val factKind: String,
    val text: String,
    val happenedAt: String? = null,
    val isInferred: Boolean,
    val evidenceSource: String? = null,
    val evidenceQuote: String? = null,
)

/** SharedKnowledgeEdge is a link between two shared pages. */
@Serializable
data class SharedKnowledgeEdge(val fromPath: String, val toPath: String, val relation: String, val isProposed: Boolean)

/** SharedKnowledgeChild is a page under a shared page. */
@Serializable
data class SharedKnowledgeChild(val path: String, val name: String, val opening: String? = null)

/** SharedKnowledgeRoot is the root of a subtree another person shared. */
@Serializable
data class SharedKnowledgeRoot(
    val ownerUserId: String,
    val ownerUsername: String,
    val ownerName: String,
    val ownerAvatarUrl: String? = null,
    val ownerAvatarColorIndex: Int? = null,
    val path: String,
    val name: String,
    val nodeKind: String,
    val opening: String,
    val isQuoteShared: Boolean,
) {
    val id: String get() = "$ownerUserId|$path"

    /** The name it is listed by: its own, or its path. */
    val shownName: String get() = name.ifEmpty { path }

    /** Who shared it: their name, or their username when they have none. */
    val shownOwner: String get() = ownerName.ifEmpty { ownerUsername }
}

/** SharedKnowledgeOwnerGroup is the roots one person shared. */
data class SharedKnowledgeOwnerGroup(
    val ownerUserId: String,
    val ownerName: String,
    val ownerAvatarUrl: String?,
    val ownerAvatarColorIndex: Int?,
    val roots: List<SharedKnowledgeRoot>,
) {
    val id: String get() = ownerUserId

    companion object {
        /** The roots under whoever shared them, in the order the server listed them (by owner, then path). */
        fun grouped(roots: List<SharedKnowledgeRoot>): List<SharedKnowledgeOwnerGroup> =
            roots.groupBy { root -> root.ownerUserId }.map { (ownerUserId, owned) ->
                val first = owned.first()
                SharedKnowledgeOwnerGroup(ownerUserId, first.shownOwner, first.ownerAvatarUrl, first.ownerAvatarColorIndex, owned)
            }
    }
}

/** SharedKnowledgeSearch is what a search of the shared pages found, and whether more matched than were given back. */
@Serializable
data class SharedKnowledgeSearch(val hits: List<SharedKnowledgeHit>, val hasMoreHits: Boolean)

/**
 * SharedKnowledgeHit is a page, or a fact of a page, that a search of what other people shared found, with whose it is.
 * A fact has its number as the owner numbers them and what it says; a page has an opening.
 */
@Serializable
data class SharedKnowledgeHit(
    val ownerUserId: String,
    val ownerUsername: String,
    val ownerName: String,
    val path: String,
    val name: String,
    val opening: String,
    val factNumber: Int,
    val text: String,
) {
    val id: String get() = "$ownerUserId/$path/$factNumber"
    val isFact: Boolean get() = factNumber > 0
    val shownName: String get() = name.ifEmpty { path }
    val shownOwner: String get() = ownerName.ifEmpty { ownerUsername }

    /** What the hit says: the fact, or the page's opening. */
    val shownText: String get() = if (isFact) text else opening
}

/** SharedKnowledgePage is a page another person shared, as the person is shown it. */
@Serializable
data class SharedKnowledgePage(
    val ownerUserId: String,
    val ownerUsername: String,
    val ownerName: String,
    val ownerAvatarUrl: String? = null,
    val ownerAvatarColorIndex: Int? = null,
    val path: String,
    val name: String,
    val nodeKind: String,
    val aliases: List<String>,
    val opening: String,
    val isQuoteShared: Boolean,
    val facts: List<SharedKnowledgeFact?>,
    val edges: List<SharedKnowledgeEdge?>,
    val children: List<SharedKnowledgeChild?>,
) {
    val shownName: String get() = name.ifEmpty { path }
    val shownOwner: String get() = ownerName.ifEmpty { ownerUsername }
    val presentFacts: List<SharedKnowledgeFact> get() = facts.filterNotNull()
    val presentEdges: List<SharedKnowledgeEdge> get() = edges.filterNotNull()
    val presentChildren: List<SharedKnowledgeChild> get() = children.filterNotNull()

    /** How a fact of this page is cited: the path and number, and whose it is. */
    fun citation(fact: SharedKnowledgeFact): String = "$path#${fact.number} (shared by $shownOwner)"
}

/** KnowledgeShare is one share the person made of one of their own pages. */
@Serializable
data class KnowledgeShare(
    val id: String,
    val path: String,
    val pageName: String? = null,
    val granteeUserId: String? = null,
    val granteeUsername: String? = null,
    val granteeName: String? = null,
    val granteeCircleId: String? = null,
    val granteeCircleName: String? = null,
    val isQuoteShared: Boolean,
) {
    /** Whether it is with a circle rather than a person. */
    val isWithCircle: Boolean get() = granteeCircleId != null

    /** Who it is with: the person's name, or their username when they have no name, or the circle's name. */
    val granteeDisplayName: String
        get() {
            if (isWithCircle) return granteeCircleName.orEmpty()
            if (!granteeName.isNullOrEmpty()) return granteeName
            return granteeUsername.orEmpty()
        }

    /** Whether this share reaches the page at the path: the page itself, or one above it, at a slash ("people/a" reaches people/a/x and not people/ab). */
    fun covers(pagePath: String): Boolean = pagePath == path || pagePath.startsWith("$path/")
}

/**
 * KnowledgeSharePreview is what a share would show, and which pages it would reach: the first of their paths, by path,
 * of reachedPageCount in all, and how many facts on them stay private for their evidence.
 */
@Serializable
data class KnowledgeSharePreview(
    val page: SharedKnowledgePage,
    val reachedPageCount: Int,
    val isReachedPageCountCut: Boolean,
    val reachedPagePaths: List<String>,
    val withheldFactCount: Int,
)

/**
 * SharedCitations turns a fact of a shared page cited in an answer's words, "people/a-grandparent#3 (shared by Ada)",
 * into a link to the page: [that](memory:people/a-grandparent#3@ada), which the app opens as the owner's shared page.
 * The words name the owner by display name, so the username comes from the pages shared with the person.
 */
object SharedCitations {
    private val pattern = Regex("([a-z0-9][a-z0-9-]*(?:/[a-z0-9][a-z0-9-]*)*)#(\\d{1,6}) \\(shared by ([^)\\n]+)\\)")

    /** Whether the words cite a fact of a shared page. */
    fun hasCitation(text: String): Boolean = pattern.containsMatchIn(text)

    /** Owner display name (lower case) to username, for the people who shared pages. A name two people share is left out: a link could open either. */
    fun usernames(roots: List<SharedKnowledgeRoot>): Map<String, String> {
        val found = HashMap<String, String>()
        val ambiguous = HashSet<String>()
        for (root in roots) {
            val name = root.ownerName.trim().lowercase()
            if (name.isEmpty() || name in ambiguous) continue
            val known = found[name]
            if (known != null && known != root.ownerUsername) {
                found.remove(name)
                ambiguous.add(name)
                continue
            }
            found[name] = root.ownerUsername
        }
        return found
    }

    /** The words with each shared citation whose owner is known made a link. One the agent already made a link of, or whose owner is not known, is left as it was. */
    fun linking(text: String, usernames: Map<String, String>): String {
        if (usernames.isEmpty()) return text
        val linked = StringBuilder(text)
        // From the end, so an earlier replacement does not move a later range.
        for (match in pattern.findAll(text).toList().reversed()) {
            val whole = match.range
            if (whole.first > 0 && linked[whole.first - 1] == '[') continue
            if (whole.last + 1 < linked.length && linked[whole.last + 1] == ']') continue
            val written = match.value
            val owner = match.groupValues[3].trim().lowercase()
            val username = usernames[owner]
            if (username.isNullOrEmpty()) continue
            if (!username.all { character -> character.isLetterOrDigit() || character in "._~-" }) continue
            if (written.contains("[") || written.contains("]")) continue
            linked.replace(whole.first, whole.last + 1, "[$written](memory:${match.groupValues[1]}#${match.groupValues[2]}@$username)")
        }
        return linked.toString()
    }
}
