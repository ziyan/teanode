package com.teanode.kit.knowledge

import kotlinx.serialization.Serializable

/** KnowledgeNode is a page of the agent's memory: a person, a project, a topic, or a folder of pages. */
@Serializable
data class KnowledgeNode(
    val id: String,
    val path: String,
    val kind: String,
    val name: String,
    val summary: String,
    val aliases: List<String>? = null,
    val pinned: Boolean? = null,
    val dormant: Boolean? = null,
    val contactId: String? = null,
    val overview: String? = null,
    val overviewWrittenAt: String? = null,
    val modifiedAt: String? = null,
) {
    /** The name it is shown by: its own, or the last part of its path with a capital. */
    val shownName: String
        get() {
            if (name.isNotEmpty()) return name
            val last = path.split("/").lastOrNull { part -> part.isNotEmpty() } ?: path
            return last.take(1).uppercase() + last.drop(1)
        }

    val isFolder: Boolean get() = kind == "folder"

    val isSelf: Boolean get() = path == "self"

    /** The path of the page it is filed under; empty for a root. */
    val parentPath: String
        get() {
            val cut = path.lastIndexOf('/')
            return if (cut < 0) "" else path.substring(0, cut)
        }
}

/** KnowledgeRow is a page in a folder's list, with what to say under it. */
@Serializable
data class KnowledgeRow(val node: KnowledgeNode? = null, val hint: String, val children: Int) {
    /** The line under its name: the hint, else the summary, at most 90 characters. */
    val line: String
        get() {
            val text = hint.ifEmpty { node?.summary.orEmpty() }
            return if (text.length > 90) text.take(89) + "…" else text
        }

    /** Whether a tap can go into it: a folder, or a page with pages under it. */
    val opensInto: Boolean get() = node?.isFolder == true || children > 0

    companion object {
        /** Self and pinned pages first, then those that open into more, then the rest, each group in the server's order. */
        fun ordered(rows: List<KnowledgeRow>): List<KnowledgeRow> {
            fun rank(row: KnowledgeRow): Int = when {
                row.node?.isSelf == true || row.node?.pinned == true -> 0
                row.opensInto -> 1
                else -> 2
            }
            return rows.withIndex().sortedWith(compareBy({ entry -> rank(entry.value) }, { entry -> entry.index })).map { entry -> entry.value }
        }
    }
}

@Serializable
data class KnowledgeRowPage(val rows: List<KnowledgeRow?>, val total: Int)

/** KnowledgeEvidence is where a fact came from. */
@Serializable
data class KnowledgeEvidence(val kind: String, val id: String, val quote: String)

/** KnowledgeFact is one thing the agent knows, numbered on its page. */
@Serializable
data class KnowledgeFact(
    val id: String,
    val number: Int,
    val kind: String,
    val text: String,
    val happenedAt: String? = null,
    val inferred: Boolean,
    val evidence: List<KnowledgeEvidence>,
    val audiences: List<String>? = null,
) {
    /** A reflection's own kind (pattern, tension, trend, risk, question), from the dream evidence that wrote it. */
    val reflectionKind: String?
        get() = evidence.firstOrNull { entry -> entry.kind == "dream" && entry.quote.startsWith(REFLECTION_PREFIX) }?.quote?.removePrefix(REFLECTION_PREFIX)

    /** The pages a reflection cites, as path#number. */
    val citations: List<String> get() = evidence.filter { entry -> entry.kind == "memory" && entry.quote.isNotEmpty() }.map { entry -> entry.quote }
}

private const val REFLECTION_PREFIX = "reflection: "

@Serializable
data class KnowledgeFoldedFact(val into: Int, val fact: Folded? = null) {
    @Serializable
    data class Folded(val id: String, val number: Int, val text: String)
}

/** KnowledgeAttachment is a file a fact came with. */
@Serializable
data class KnowledgeAttachment(val documentId: String, val name: String, val contentType: String, val channel: String, val path: String)

/** KnowledgePage is a page whole: its facts, the facts folded into others, the pages under it, the files and the contact it is. */
@Serializable
data class KnowledgePage(
    val node: KnowledgeNode? = null,
    val facts: List<KnowledgeFact?>,
    val folded: List<KnowledgeFoldedFact?>,
    val children: List<KnowledgeNode?>,
    val attachments: List<KnowledgeAttachment?>,
    val contact: Contact? = null,
    /** The person's own records the page is linked to: the first part of them. Missing from a server that does not know record links. */
    val recordLinks: List<KnowledgeRecordLink?>? = null,
    /** How many record links there are in all. */
    val recordLinkCount: Int? = null,
    /** The records the facts' evidence names, so a fact's source opens it. */
    val evidenceRecords: List<KnowledgeEvidenceRecord?>? = null,
) {
    @Serializable
    data class Contact(val id: String, val name: String, val emails: List<String>, val organization: String)

    val presentFacts: List<KnowledgeFact> get() = facts.filterNotNull()

    /** The records one fact's evidence names, in the order it names them. */
    fun records(fact: KnowledgeFact): List<KnowledgeEvidenceRecord> =
        (evidenceRecords ?: emptyList()).filterNotNull().filter { record -> record.factId == fact.id }.sortedBy { record -> record.evidenceIndex }

    /** The files a fact cites: its evidence ids, without any [...] part, matched to the page's attachments. */
    fun attachments(fact: KnowledgeFact): List<KnowledgeAttachment> {
        val cited = fact.evidence.map { entry -> entry.id.replace(Regex("\\[.*\\]"), "") }.toSet()
        return attachments.filterNotNull().filter { attachment -> attachment.documentId in cited }
    }
}

@Serializable
data class KnowledgeRevision(val revision: Int, val actor: String, val change: String, val before: String, val createdAt: String)

/** KnowledgeSearch is what a search found: pages, and facts with their pages, and how much more there is. */
@Serializable
data class KnowledgeSearch(
    val nodes: List<KnowledgeNode?>,
    val facts: List<FoundFact?>,
    val moreNodeCount: Int,
    val isMoreNodeCountLowerBound: Boolean,
    val moreFactCount: Int,
    val isMoreFactCountLowerBound: Boolean,
    /** Where the next page starts; zero on the last. */
    val nextOffset: Int,
) {
    @Serializable
    data class FoundFact(val fact: Fact? = null, val path: String, val name: String) {
        @Serializable
        data class Fact(val id: String, val number: Int, val text: String)
    }
}

/** KnowledgeNeighbours are a page's connections: what it is filed under, the pages it links to and from, and how many are left out. */
@Serializable
data class KnowledgeNeighbours(
    val node: KnowledgeNode? = null,
    val parent: KnowledgeNode? = null,
    val neighbours: List<Neighbour?>,
    val children: Int,
    val links: Int,
) {
    @Serializable
    data class Neighbour(
        val node: KnowledgeNode? = null,
        val relation: String,
        val outward: Boolean,
        val note: String,
        val weight: Double,
        /** "proposed" for a link the agent guessed. */
        val status: String,
    )
}

/** KnowledgeRecall is what the agent's memory brings up for a question. */
@Serializable
data class KnowledgeRecall(val pages: List<Page?>) {
    @Serializable
    data class Page(val path: String, val facts: List<Fact?>) {
        @Serializable
        data class Fact(val number: Int, val text: String)
    }
}

/** DocumentSearch is what a search of the agent's sources found. */
@Serializable
data class DocumentSearch(
    val passages: List<Passage?>,
    val definitions: List<Definition?>,
    val directories: List<Directory?>,
    val meaningful: Boolean,
    val moreCount: Int,
    val isMoreCountLowerBound: Boolean,
    val nextOffset: Int,
) {
    @Serializable
    data class Passage(
        val documentId: String,
        val externalId: String,
        val title: String,
        val author: String,
        val source: String,
        val happenedAt: String? = null,
        val number: Int,
        val text: String,
    )

    @Serializable
    data class Definition(val symbol: String, val kind: String, val documentId: String, val title: String)

    @Serializable
    data class Directory(val directory: String, val sourceId: String, val source: String, val passageCount: Int)
}

/** DocumentExtract is a slice of a document, read a part at a time. */
@Serializable
data class DocumentExtract(
    val documentId: String,
    val title: String,
    val author: String,
    val source: String,
    val happenedAt: String? = null,
    val text: String,
    val total: Int,
    /** Where the next slice starts; zero at the end. */
    val next: Int,
)

/**
 * KnowledgeSource is a place the agent reads, as far as the Knowledge screens need it: its id and name, which the
 * document search offers as a filter. The full form, with how often it is read and how far it has got, belongs to the
 * agent's upkeep settings.
 */
@Serializable
data class KnowledgeSource(val id: String, val name: String)

/** KnowledgeWords are the fixed vocabularies the page editor offers. */
object KnowledgeWords {
    val factKinds: List<String> = listOf("fact", "preference", "decision", "event", "howto")
    val audiences: List<String> = listOf("triage", "reply", "research", "summaries")
    val relations: List<String> = listOf(
        "part_of", "works_on", "member_of", "knows", "owns", "uses", "located_in", "related_to", "decided_in", "about", "depends_on",
    )

    private val happenedPattern = Regex("^\\d{4}(-\\d{2}(-\\d{2})?)?$")

    /** Whether a "when it was true" is one the server reads: a year, a month or a day. */
    fun isHappenedValid(text: String): Boolean = text.isEmpty() || happenedPattern.matches(text)
}
