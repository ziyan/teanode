package com.teanode.kit.knowledge

import com.teanode.kit.Connection
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive

/** The memory graph's GraphQL: the same text as the iPhone app's (ios/TeaNodeKit Knowledge*.swift). */
object KnowledgeDocuments {
    private const val NODE_FIELDS = "id path kind name summary aliases pinned dormant contactId overview overviewWrittenAt modifiedAt"
    private const val RECORD_LINK_FIELDS =
        "id nodePath nodeName nodeKind recordKind recordId recordRelation linkStatus passageStartMS passageEndMS transcriptRunId passageCount note happenedAt createdAt recordTitle recordHappenedAt dashboardPath isUnavailable"
    private const val SHARED_PAGE_FIELDS = """ownerUserId ownerUsername ownerName ownerAvatarUrl ownerAvatarColorIndex
path name nodeKind aliases opening isQuoteShared
facts { number factKind text happenedAt isInferred evidenceSource evidenceQuote }
edges { fromPath toPath relation isProposed }
children { path name opening }"""

    const val CHILDREN = """query (${'$'}path: String!, ${'$'}first: Int, ${'$'}offset: Int) {
  AgentGraphChildren(path: ${'$'}path, first: ${'$'}first, offset: ${'$'}offset) {
    rows { node { $NODE_FIELDS } hint children }
    total
  }
}"""
    const val SEARCH = """query (${'$'}query: String!, ${'$'}first: Int, ${'$'}offset: Int) {
  SearchAgentGraph(query: ${'$'}query, first: ${'$'}first, offset: ${'$'}offset) {
    nodes { $NODE_FIELDS }
    facts { fact { id number text } path name }
    moreNodeCount isMoreNodeCountLowerBound moreFactCount isMoreFactCountLowerBound nextOffset
  }
}"""
    const val PAGE = """query (${'$'}path: String!) {
  AgentGraphPage(path: ${'$'}path) {
    node { $NODE_FIELDS }
    facts { id number kind text happenedAt inferred evidence { kind id quote } audiences }
    folded { into fact { id number text } }
    children { $NODE_FIELDS }
    attachments { documentId name contentType channel path }
    contact { id name emails organization }
    recordLinks { $RECORD_LINK_FIELDS }
    recordLinkCount
    evidenceRecords { factId evidenceIndex recordKind recordId passageStartMS passageEndMS transcriptRunId recordTitle dashboardPath isUnavailable }
  }
}"""
    const val HISTORY = """query (${'$'}path: String!, ${'$'}first: Int) {
  ListAgentPageHistory(path: ${'$'}path, first: ${'$'}first) { revision actor change before createdAt }
}"""
    const val NEIGHBOURS = """query (${'$'}path: String!) {
  AgentGraphNeighbours(path: ${'$'}path) {
    node { $NODE_FIELDS }
    parent { $NODE_FIELDS }
    neighbours { node { $NODE_FIELDS } relation outward note weight status }
    children links
  }
}"""
    const val SAVE_NODE = """mutation (${'$'}path: String!, ${'$'}kind: String, ${'$'}name: String, ${'$'}summary: String, ${'$'}aliases: [String!], ${'$'}pinned: Boolean) {
  SaveAgentNode(path: ${'$'}path, kind: ${'$'}kind, name: ${'$'}name, summary: ${'$'}summary, aliases: ${'$'}aliases, pinned: ${'$'}pinned) { id path }
}"""
    const val SAVE_FACT = """mutation (${'$'}path: String!, ${'$'}number: Int, ${'$'}kind: String, ${'$'}text: String!, ${'$'}happened: String, ${'$'}audiences: [String!]) {
  SaveAgentFact(path: ${'$'}path, number: ${'$'}number, kind: ${'$'}kind, text: ${'$'}text, happened: ${'$'}happened, audiences: ${'$'}audiences) { id number }
}"""
    const val MOVE_NODE = """mutation (${'$'}path: String!, ${'$'}under: String!) { MoveAgentNode(path: ${'$'}path, under: ${'$'}under) { id path } }"""
    const val MERGE_NODES = """mutation (${'$'}path: String!, ${'$'}into: String!) { MergeAgentNodes(path: ${'$'}path, into: ${'$'}into) { id path } }"""
    const val MOVE_FACT = """mutation (${'$'}path: String!, ${'$'}number: Int!, ${'$'}to: String!) { MoveAgentFact(path: ${'$'}path, number: ${'$'}number, to: ${'$'}to) { id number } }"""
    const val LINK_NODES = """mutation (${'$'}path: String!, ${'$'}to: String!, ${'$'}relation: String!, ${'$'}note: String) {
  LinkAgentNodes(path: ${'$'}path, to: ${'$'}to, relation: ${'$'}relation, note: ${'$'}note)
}"""
    const val UNLINK_NODES = """mutation (${'$'}path: String!, ${'$'}to: String!, ${'$'}relation: String!) {
  UnlinkAgentNodes(path: ${'$'}path, to: ${'$'}to, relation: ${'$'}relation)
}"""
    const val DELETE_FACT = """mutation (${'$'}path: String!, ${'$'}number: Int!) { DeleteAgentFact(path: ${'$'}path, number: ${'$'}number) }"""
    const val DELETE_NODE = """mutation (${'$'}path: String!) { DeleteAgentNode(path: ${'$'}path) }"""
    const val RECALL = """query (${'$'}question: String!) { RecallAgentMemory(question: ${'$'}question) { pages { path facts { number text } } } }"""
    const val SEARCH_DOCUMENTS = """query (${'$'}query: String!, ${'$'}first: Int, ${'$'}offset: Int, ${'$'}sourceId: String, ${'$'}directory: String) {
  SearchAgentDocuments(query: ${'$'}query, first: ${'$'}first, offset: ${'$'}offset, sourceId: ${'$'}sourceId, directory: ${'$'}directory) {
    passages { documentId externalId title author source happenedAt number text }
    definitions { symbol kind documentId title }
    directories { directory sourceId source passageCount }
    meaningful moreCount isMoreCountLowerBound nextOffset
  }
}"""
    const val READ_DOCUMENT = """query (${'$'}documentId: String!, ${'$'}from: Int) {
  ReadAgentDocument(documentId: ${'$'}documentId, from: ${'$'}from) { documentId title author source happenedAt text total next }
}"""
    const val SOURCES = """query { ListAgentKnowledgeSources { id name } }"""

    const val READ_RECORD_LINKS = """query (${'$'}recordKind: String!, ${'$'}recordId: String!, ${'$'}passageStartMS: Int64, ${'$'}passageEndMS: Int64) {
  ReadRecordMemoryLinks(recordKind: ${'$'}recordKind, recordId: ${'$'}recordId, passageStartMS: ${'$'}passageStartMS, passageEndMS: ${'$'}passageEndMS) { $RECORD_LINK_FIELDS }
}"""
    const val LIST_RECORD_LINKS = """query (${'$'}path: String!, ${'$'}first: Int, ${'$'}offset: Int) {
  ListAgentRecordLinks(path: ${'$'}path, first: ${'$'}first, offset: ${'$'}offset) {
    recordLinks { $RECORD_LINK_FIELDS }
    recordLinkCount nextOffset
  }
}"""
    const val LINK_RECORD = """mutation (${'$'}path: String!, ${'$'}recordKind: String!, ${'$'}recordId: String!, ${'$'}recordRelation: String!, ${'$'}passageStartMS: Int64, ${'$'}passageEndMS: Int64, ${'$'}transcriptRunId: String) {
  LinkAgentRecord(path: ${'$'}path, recordKind: ${'$'}recordKind, recordId: ${'$'}recordId, recordRelation: ${'$'}recordRelation, passageStartMS: ${'$'}passageStartMS, passageEndMS: ${'$'}passageEndMS, transcriptRunId: ${'$'}transcriptRunId) { id }
}"""
    const val UNLINK_RECORD = """mutation (${'$'}path: String!, ${'$'}recordKind: String!, ${'$'}recordId: String!, ${'$'}recordRelation: String, ${'$'}passageStartMS: Int64, ${'$'}passageEndMS: Int64) {
  UnlinkAgentRecord(path: ${'$'}path, recordKind: ${'$'}recordKind, recordId: ${'$'}recordId, recordRelation: ${'$'}recordRelation, passageStartMS: ${'$'}passageStartMS, passageEndMS: ${'$'}passageEndMS)
}"""
    const val CONFIRM_RECORD = """mutation (${'$'}path: String!, ${'$'}recordKind: String!, ${'$'}recordId: String!, ${'$'}recordRelation: String) {
  ConfirmAgentRecord(path: ${'$'}path, recordKind: ${'$'}recordKind, recordId: ${'$'}recordId, recordRelation: ${'$'}recordRelation)
}"""
    const val SET_CONTACT = """mutation (${'$'}path: String!, ${'$'}contactId: String) { SetAgentNodeContact(path: ${'$'}path, contactId: ${'$'}contactId) }"""

    /** The person's own address books and their contacts, for choosing the card a page is about (ios Calendar.swift addressBooks and contacts). */
    const val ADDRESS_BOOKS = """query { ListAddressBooks { id name contacts } }"""
    const val CONTACTS = """query (${'$'}addressBookId: String!, ${'$'}query: String, ${'$'}first: Int) {
  ListContacts(addressBookId: ${'$'}addressBookId, query: ${'$'}query, first: ${'$'}first) {
    id name organization emails phones hasPhoto addresses { written } accountUserId
  }
}"""

    const val SHARES = """query { ListAgentPageShares { id path pageName granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName isQuoteShared } }"""
    const val SHARE_PAGE = """mutation (${'$'}path: String!, ${'$'}granteeUserId: String, ${'$'}granteeCircleId: String, ${'$'}isQuoteShared: Boolean) {
  ShareAgentPage(path: ${'$'}path, granteeUserId: ${'$'}granteeUserId, granteeCircleId: ${'$'}granteeCircleId, isQuoteShared: ${'$'}isQuoteShared) { id }
}"""
    const val UNSHARE_PAGE = """mutation (${'$'}shareId: String!) { UnshareAgentPage(shareId: ${'$'}shareId) }"""
    const val PREVIEW_SHARE = """query (${'$'}path: String!, ${'$'}granteeUserId: String, ${'$'}granteeCircleId: String, ${'$'}isQuoteShared: Boolean) {
  PreviewAgentPageShare(path: ${'$'}path, granteeUserId: ${'$'}granteeUserId, granteeCircleId: ${'$'}granteeCircleId, isQuoteShared: ${'$'}isQuoteShared) {
    page { $SHARED_PAGE_FIELDS }
    reachedPageCount isReachedPageCountCut reachedPagePaths withheldFactCount
  }
}"""
    const val SHARED_ROOTS = """query { ListSharedAgentPages {
  ownerUserId ownerUsername ownerName ownerAvatarUrl ownerAvatarColorIndex path name nodeKind opening isQuoteShared
} }"""
    const val SEARCH_SHARED = """query (${'$'}query: String!, ${'$'}first: Int) {
  SearchSharedAgentPages(query: ${'$'}query, first: ${'$'}first) {
    hasMoreHits hits { ownerUserId ownerUsername ownerName path name opening factNumber text }
  }
}"""
    const val SHARED_PAGE = """query (${'$'}ownerUserId: String!, ${'$'}path: String!) {
  ReadSharedAgentPage(ownerUserId: ${'$'}ownerUserId, path: ${'$'}path) { $SHARED_PAGE_FIELDS }
}"""
}

@Serializable
private class ChildrenAnswer(val AgentGraphChildren: KnowledgeRowPage)

@Serializable
private class SearchAnswer(val SearchAgentGraph: KnowledgeSearch)

@Serializable
private class PageAnswer(val AgentGraphPage: KnowledgePage? = null)

@Serializable
private class HistoryAnswer(val ListAgentPageHistory: List<KnowledgeRevision?>)

@Serializable
private class NeighboursAnswer(val AgentGraphNeighbours: KnowledgeNeighbours? = null)

@Serializable
private class SavedNode(val id: String)

@Serializable
private class SaveNodeAnswer(val SaveAgentNode: SavedNode? = null)

@Serializable
private class SavedFact(val number: Int)

@Serializable
private class SaveFactAnswer(val SaveAgentFact: SavedFact)

@Serializable
private class MovedNode(val path: String)

@Serializable
private class MoveNodeAnswer(val MoveAgentNode: MovedNode? = null)

@Serializable
private class MergeNodesAnswer(val MergeAgentNodes: MovedNode? = null)

@Serializable
private class MovedFact(val number: Int)

@Serializable
private class MoveFactAnswer(val MoveAgentFact: MovedFact? = null)

@Serializable
private class AnyAnswer(
    val LinkAgentNodes: JsonElement? = null,
    val UnlinkAgentNodes: JsonElement? = null,
    val DeleteAgentFact: JsonElement? = null,
    val DeleteAgentNode: JsonElement? = null,
)

@Serializable
private class RecallAnswer(val RecallAgentMemory: KnowledgeRecall)

@Serializable
private class SearchDocumentsAnswer(val SearchAgentDocuments: DocumentSearch)

@Serializable
private class ReadDocumentAnswer(val ReadAgentDocument: DocumentExtract? = null)

@Serializable
private class SourcesAnswer(val ListAgentKnowledgeSources: List<KnowledgeSource?>)

@Serializable
private class RecordLinksAnswer(val ReadRecordMemoryLinks: List<KnowledgeRecordLink?>)

@Serializable
private class RecordLinkPart(val recordLinks: List<KnowledgeRecordLink?>)

@Serializable
private class ListRecordLinksAnswer(val ListAgentRecordLinks: RecordLinkPart)

@Serializable
private class LinkedRecord(val id: String)

@Serializable
private class LinkRecordAnswer(val LinkAgentRecord: LinkedRecord? = null)

@Serializable
private class RecordAnswer(
    val ConfirmAgentRecord: Int? = null,
    val UnlinkAgentRecord: Boolean? = null,
    val SetAgentNodeContact: Boolean? = null,
)

/** KnowledgeContactChoice is a contact card as far as choosing the one a page is about needs it. */
@Serializable
data class KnowledgeContactChoice(
    val id: String,
    val name: String? = null,
    val organization: String? = null,
    val emails: List<String>? = null,
) {
    /** What the list calls them: their name, or else their first address. */
    val shownName: String get() = name?.takeIf { text -> text.isNotEmpty() } ?: emails?.firstOrNull().orEmpty()
}

@Serializable
private class AddressBookChoice(val id: String)

@Serializable
private class AddressBooksAnswer(val ListAddressBooks: List<AddressBookChoice>)

@Serializable
private class ContactsAnswer(val ListContacts: List<KnowledgeContactChoice>)

@Serializable
private class SharesAnswer(val ListAgentPageShares: List<KnowledgeShare>)

@Serializable
private class SharedShare(val id: String)

@Serializable
private class ShareAnswer(val ShareAgentPage: SharedShare)

@Serializable
private class UnshareAnswer(val UnshareAgentPage: Boolean? = null)

@Serializable
private class PreviewAnswer(val PreviewAgentPageShare: KnowledgeSharePreview)

@Serializable
private class SharedRootsAnswer(val ListSharedAgentPages: List<SharedKnowledgeRoot>)

@Serializable
private class SearchSharedAnswer(val SearchSharedAgentPages: SharedKnowledgeSearch)

@Serializable
private class SharedPageAnswer(val ReadSharedAgentPage: SharedKnowledgePage)

private fun number(value: Number): JsonElement = JsonPrimitive(value)

private fun text(value: String?): JsonElement = if (value == null) JsonNull else JsonPrimitive(value)

private fun strings(values: List<String>): JsonElement = JsonArray(values.map { value -> JsonPrimitive(value) })

suspend fun Connection.knowledgeChildren(path: String, first: Int = 50, offset: Int = 0): KnowledgeRowPage =
    execute<ChildrenAnswer>(
        KnowledgeDocuments.CHILDREN, mapOf("path" to JsonPrimitive(path), "first" to number(first), "offset" to number(offset)),
    ).AgentGraphChildren

suspend fun Connection.searchKnowledge(words: String, first: Int = 60, offset: Int = 0): KnowledgeSearch =
    execute<SearchAnswer>(
        KnowledgeDocuments.SEARCH, mapOf("query" to JsonPrimitive(words), "first" to number(first), "offset" to number(offset)),
    ).SearchAgentGraph

/** A page whole, or null when there is no page at the path. */
suspend fun Connection.knowledgePage(path: String): KnowledgePage? =
    execute<PageAnswer>(KnowledgeDocuments.PAGE, mapOf("path" to JsonPrimitive(path))).AgentGraphPage

suspend fun Connection.knowledgeHistory(path: String, first: Int = 50): List<KnowledgeRevision> =
    execute<HistoryAnswer>(KnowledgeDocuments.HISTORY, mapOf("path" to JsonPrimitive(path), "first" to number(first))).ListAgentPageHistory.filterNotNull()

suspend fun Connection.knowledgeNeighbours(path: String): KnowledgeNeighbours? =
    execute<NeighboursAnswer>(KnowledgeDocuments.NEIGHBOURS, mapOf("path" to JsonPrimitive(path))).AgentGraphNeighbours

/** Saves a page's name, summary, other names and pin. The summary is always written, an empty one clearing it; the other names only when given. */
suspend fun Connection.saveKnowledgeNode(path: String, name: String, summary: String, aliases: List<String>?, isPinned: Boolean?) {
    val variables = HashMap<String, JsonElement>()
    variables["path"] = JsonPrimitive(path)
    variables["name"] = JsonPrimitive(name)
    variables["summary"] = JsonPrimitive(summary)
    if (aliases != null) variables["aliases"] = strings(aliases)
    if (isPinned != null) variables["pinned"] = JsonPrimitive(isPinned)
    execute<SaveNodeAnswer>(KnowledgeDocuments.SAVE_NODE, variables)
}

/** Adds a fact, or changes one by its number. Returns its number, which for a new fact may be an older one's with the same words. */
suspend fun Connection.saveKnowledgeFact(path: String, number: Int?, kind: String, text: String, happened: String, audiences: List<String>?): Int {
    val variables = HashMap<String, JsonElement>()
    variables["path"] = JsonPrimitive(path)
    variables["kind"] = JsonPrimitive(kind)
    variables["text"] = JsonPrimitive(text)
    variables["happened"] = JsonPrimitive(happened)
    if (number != null) variables["number"] = number(number)
    if (audiences != null) variables["audiences"] = strings(audiences)
    return execute<SaveFactAnswer>(KnowledgeDocuments.SAVE_FACT, variables).SaveAgentFact.number
}

/** Files a page under another; returns its new path. */
suspend fun Connection.moveKnowledgeNode(path: String, under: String): String? =
    execute<MoveNodeAnswer>(KnowledgeDocuments.MOVE_NODE, mapOf("path" to JsonPrimitive(path), "under" to JsonPrimitive(under))).MoveAgentNode?.path

/** Folds a page into another; returns the path that is left. */
suspend fun Connection.mergeKnowledgeNodes(path: String, into: String): String? =
    execute<MergeNodesAnswer>(KnowledgeDocuments.MERGE_NODES, mapOf("path" to JsonPrimitive(path), "into" to JsonPrimitive(into))).MergeAgentNodes?.path

suspend fun Connection.moveKnowledgeFact(path: String, number: Int, to: String) {
    execute<MoveFactAnswer>(KnowledgeDocuments.MOVE_FACT, mapOf("path" to JsonPrimitive(path), "number" to number(number), "to" to JsonPrimitive(to)))
}

suspend fun Connection.linkKnowledgeNodes(path: String, to: String, relation: String, note: String) {
    execute<AnyAnswer>(
        KnowledgeDocuments.LINK_NODES,
        mapOf("path" to JsonPrimitive(path), "to" to JsonPrimitive(to), "relation" to JsonPrimitive(relation), "note" to JsonPrimitive(note)),
    )
}

suspend fun Connection.unlinkKnowledgeNodes(path: String, to: String, relation: String) {
    execute<AnyAnswer>(
        KnowledgeDocuments.UNLINK_NODES, mapOf("path" to JsonPrimitive(path), "to" to JsonPrimitive(to), "relation" to JsonPrimitive(relation)),
    )
}

/** Forgets a fact; the agent is shown it as what not to keep. */
suspend fun Connection.deleteKnowledgeFact(path: String, number: Int) {
    execute<AnyAnswer>(KnowledgeDocuments.DELETE_FACT, mapOf("path" to JsonPrimitive(path), "number" to number(number)))
}

/** Forgets a page and every page under it. */
suspend fun Connection.deleteKnowledgeNode(path: String) {
    execute<AnyAnswer>(KnowledgeDocuments.DELETE_NODE, mapOf("path" to JsonPrimitive(path)))
}

suspend fun Connection.recallMemory(question: String): KnowledgeRecall =
    execute<RecallAnswer>(KnowledgeDocuments.RECALL, mapOf("question" to JsonPrimitive(question))).RecallAgentMemory

suspend fun Connection.searchDocuments(words: String, sourceId: String?, directory: String?, first: Int = 20, offset: Int = 0): DocumentSearch {
    val variables = hashMapOf<String, JsonElement>("query" to JsonPrimitive(words), "first" to number(first), "offset" to number(offset))
    if (!sourceId.isNullOrEmpty()) variables["sourceId"] = JsonPrimitive(sourceId)
    if (!directory.isNullOrEmpty()) variables["directory"] = JsonPrimitive(directory)
    return execute<SearchDocumentsAnswer>(KnowledgeDocuments.SEARCH_DOCUMENTS, variables).SearchAgentDocuments
}

/** A slice of a document from a character on; null when it is no longer kept. */
suspend fun Connection.readDocument(documentId: String, from: Int): DocumentExtract? =
    execute<ReadDocumentAnswer>(KnowledgeDocuments.READ_DOCUMENT, mapOf("documentId" to JsonPrimitive(documentId), "from" to number(from))).ReadAgentDocument

suspend fun Connection.knowledgeSources(): List<KnowledgeSource> =
    execute<SourcesAnswer>(KnowledgeDocuments.SOURCES).ListAgentKnowledgeSources.filterNotNull()

/** The pages of memory that link to one record. For a passage of a recording the range narrows it to the passages that overlap it. */
suspend fun Connection.recordMemoryLinks(recordKind: String, recordId: String, passageStartMS: Long? = null, passageEndMS: Long? = null): List<KnowledgeRecordLink> {
    val variables = hashMapOf<String, JsonElement>("recordKind" to JsonPrimitive(recordKind), "recordId" to JsonPrimitive(recordId))
    if (passageStartMS != null) variables["passageStartMS"] = number(passageStartMS)
    if (passageEndMS != null) variables["passageEndMS"] = number(passageEndMS)
    return execute<RecordLinksAnswer>(KnowledgeDocuments.READ_RECORD_LINKS, variables).ReadRecordMemoryLinks.filterNotNull()
}

/** A part of a page's record links, for a page with more than it read. */
suspend fun Connection.knowledgeRecordLinks(path: String, first: Int = 50, offset: Int = 0): List<KnowledgeRecordLink> =
    execute<ListRecordLinksAnswer>(
        KnowledgeDocuments.LIST_RECORD_LINKS, mapOf("path" to JsonPrimitive(path), "first" to number(first), "offset" to number(offset)),
    ).ListAgentRecordLinks.recordLinks.filterNotNull()

/** States a link: what the person asked for, and so what confirms a guess the agent made. */
suspend fun Connection.linkKnowledgeRecord(path: String, link: KnowledgeRecordLink) {
    val variables = hashMapOf<String, JsonElement>(
        "path" to JsonPrimitive(path), "recordKind" to JsonPrimitive(link.recordKind), "recordId" to JsonPrimitive(link.recordId),
        "recordRelation" to JsonPrimitive(link.recordRelation),
    )
    if (link.recordKind == "recording_passage") {
        variables["passageStartMS"] = number(link.passageStartMS)
        variables["passageEndMS"] = number(link.passageEndMS)
        if (link.transcriptRunId.isNotEmpty()) variables["transcriptRunId"] = JsonPrimitive(link.transcriptRunId)
    }
    execute<LinkRecordAnswer>(KnowledgeDocuments.LINK_RECORD, variables)
}

/**
 * States the agent's guesses at a page's links to one record: every passage of a recording the row stands for, or the
 * one link of any other kind. Stating a link again is how the person confirms it.
 */
suspend fun Connection.confirmKnowledgeRecord(path: String, link: KnowledgeRecordLink) {
    execute<RecordAnswer>(
        KnowledgeDocuments.CONFIRM_RECORD,
        mapOf(
            "path" to JsonPrimitive(path), "recordKind" to JsonPrimitive(link.recordKind), "recordId" to JsonPrimitive(link.recordId),
            "recordRelation" to JsonPrimitive(link.recordRelation),
        ),
    )
}

/** Takes one relation of a page to a record away. The automatic passes do not make it again. */
suspend fun Connection.unlinkKnowledgeRecord(path: String, link: KnowledgeRecordLink) {
    // A row of a recording stands for every passage the page is linked to in that relation, so no range is named and all of them go.
    execute<RecordAnswer>(
        KnowledgeDocuments.UNLINK_RECORD,
        mapOf(
            "path" to JsonPrimitive(path), "recordKind" to JsonPrimitive(link.recordKind), "recordId" to JsonPrimitive(link.recordId),
            "recordRelation" to JsonPrimitive(link.recordRelation),
        ),
    )
}

/** Names the card a person's page is about, from the person's own address books; an empty identifier clears it. */
suspend fun Connection.setKnowledgeContact(path: String, contactId: String) {
    execute<RecordAnswer>(
        KnowledgeDocuments.SET_CONTACT, mapOf("path" to JsonPrimitive(path), "contactId" to if (contactId.isEmpty()) JsonNull else JsonPrimitive(contactId)),
    )
}

/** The shares the person made of their own pages. */
suspend fun Connection.knowledgeShares(): List<KnowledgeShare> = execute<SharesAnswer>(KnowledgeDocuments.SHARES).ListAgentPageShares

/**
 * Shares one of the person's pages, and everything under it, with a person or a circle (one of the two identifiers), or
 * changes whether the quotes are shared when it is shared with them already.
 */
suspend fun Connection.shareKnowledgePage(path: String, granteeUserId: String?, granteeCircleId: String?, isQuoteShared: Boolean) {
    execute<ShareAnswer>(
        KnowledgeDocuments.SHARE_PAGE,
        mapOf(
            "path" to JsonPrimitive(path), "granteeUserId" to text(granteeUserId), "granteeCircleId" to text(granteeCircleId),
            "isQuoteShared" to JsonPrimitive(isQuoteShared),
        ),
    )
}

/** Takes a share back. The reader loses the page at once: nothing was copied. */
suspend fun Connection.unshareKnowledgePage(shareId: String) {
    execute<UnshareAnswer>(KnowledgeDocuments.UNSHARE_PAGE, mapOf("shareId" to JsonPrimitive(shareId)))
}

/** Exactly what a person or a circle would be shown of a page if it were shared with them, made before there is a share. */
suspend fun Connection.previewKnowledgeShare(path: String, granteeUserId: String?, granteeCircleId: String?, isQuoteShared: Boolean): KnowledgeSharePreview =
    execute<PreviewAnswer>(
        KnowledgeDocuments.PREVIEW_SHARE,
        mapOf(
            "path" to JsonPrimitive(path), "granteeUserId" to text(granteeUserId), "granteeCircleId" to text(granteeCircleId),
            "isQuoteShared" to JsonPrimitive(isQuoteShared),
        ),
    ).PreviewAgentPageShare

/** The roots other people shared with the person, by who shared them. */
suspend fun Connection.sharedKnowledgeRoots(): List<SharedKnowledgeRoot> = execute<SharedRootsAnswer>(KnowledgeDocuments.SHARED_ROOTS).ListSharedAgentPages

/** Finds words in the pages other people shared, only within what is shared with the person. Each hit says whose it is, and the answer says when more matched than it gives back. */
suspend fun Connection.searchSharedKnowledge(words: String, first: Int? = null): SharedKnowledgeSearch =
    execute<SearchSharedAnswer>(
        KnowledgeDocuments.SEARCH_SHARED, mapOf("query" to JsonPrimitive(words), "first" to if (first == null) JsonNull else number(first)),
    ).SearchSharedAgentPages

/** One page another person shared, or a page under one. A page not shared with the person is not found. */
suspend fun Connection.sharedKnowledgePage(ownerUserId: String, path: String): SharedKnowledgePage =
    execute<SharedPageAnswer>(
        KnowledgeDocuments.SHARED_PAGE, mapOf("ownerUserId" to JsonPrimitive(ownerUserId), "path" to JsonPrimitive(path)),
    ).ReadSharedAgentPage

/** The cards in the person's own address books that match some words (all of them for none), at most 30 from each book. */
suspend fun Connection.contactChoices(words: String): List<KnowledgeContactChoice> {
    val found = ArrayList<KnowledgeContactChoice>()
    for (book in execute<AddressBooksAnswer>(KnowledgeDocuments.ADDRESS_BOOKS).ListAddressBooks) {
        val variables = hashMapOf<String, JsonElement>("addressBookId" to JsonPrimitive(book.id), "first" to number(30))
        if (words.isNotEmpty()) variables["query"] = JsonPrimitive(words)
        found += execute<ContactsAnswer>(KnowledgeDocuments.CONTACTS, variables).ListContacts
    }
    return found
}
