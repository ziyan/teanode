package com.teanode.kit.knowledge

import com.teanode.kit.TeaNodeJson
import com.teanode.kit.calendar.fakeConnection
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class KnowledgeSharingTest {
    private fun root(owner: String, path: String, name: String = ""): SharedKnowledgeRoot = TeaNodeJson.decodeFromString(
        SharedKnowledgeRoot.serializer(),
        """{"ownerUserId":"$owner-id","ownerUsername":"$owner","ownerName":"","path":"$path","name":"$name","nodeKind":"topic","opening":"About it.","isQuoteShared":false}""",
    )

    private fun named(owner: String, name: String): SharedKnowledgeRoot = TeaNodeJson.decodeFromString(
        SharedKnowledgeRoot.serializer(),
        """{"ownerUserId":"$owner-id","ownerUsername":"$owner","ownerName":"$name","path":"people/a-grandparent","name":"","nodeKind":"person","opening":"","isQuoteShared":false}""",
    )

    @Test
    fun theRootsAreGroupedUnderWhoSharedThemInTheOrderGiven() {
        val groups = SharedKnowledgeOwnerGroup.grouped(
            listOf(root("ada", "people/a-grandparent", name = "A Grandparent"), root("ada", "projects/garden"), root("ben", "projects/boat")),
        )
        assertEquals(listOf("ada", "ben"), groups.map { group -> group.ownerName })
        assertEquals(listOf("people/a-grandparent", "projects/garden"), groups[0].roots.map { found -> found.path })
        assertEquals(listOf("A Grandparent", "projects/garden"), groups[0].roots.map { found -> found.shownName })
    }

    @Test
    fun aShareReachesItsPageAndThePagesUnderItAtASlash() {
        val share = TeaNodeJson.decodeFromString(
            KnowledgeShare.serializer(),
            """{"id":"s","path":"people/a","pageName":"A","granteeUserId":"u","granteeUsername":"ben","granteeName":"","granteeCircleId":null,"granteeCircleName":"","isQuoteShared":true}""",
        )
        assertTrue(share.covers("people/a"))
        assertTrue(share.covers("people/a/x"))
        assertFalse(share.covers("people/ab"))
        assertFalse(share.covers("people"))
        assertFalse(share.isWithCircle)
        assertEquals("ben", share.granteeDisplayName)
    }

    @Test
    fun aPageCitesAFactWithItsOwner() {
        val page = TeaNodeJson.decodeFromString(
            SharedKnowledgePage.serializer(),
            """
            {"ownerUserId":"u","ownerUsername":"ada","ownerName":"Ada Example","ownerAvatarUrl":null,"ownerAvatarColorIndex":2,
             "path":"people/a-grandparent","name":"A Grandparent","nodeKind":"person","aliases":[],"opening":"Lives by the sea.",
             "isQuoteShared":false,
             "facts":[{"number":3,"factKind":"fact","text":"Next appointment is Tuesday.","happenedAt":null,"isInferred":false,
                       "evidenceSource":"from Ada Example's mail","evidenceQuote":""}, null],
             "edges":[{"fromPath":"people/a-grandparent","toPath":"people/a-grandparent/doctor","relation":"knows","isProposed":false}],
             "children":[{"path":"people/a-grandparent/doctor","name":"The doctor","opening":""}]}
            """,
        )
        assertEquals(1, page.presentFacts.size)
        assertEquals("people/a-grandparent#3 (shared by Ada Example)", page.citation(page.presentFacts[0]))
        assertEquals(listOf("people/a-grandparent/doctor"), page.presentEdges.map { edge -> edge.toPath })
        assertEquals(listOf("The doctor"), page.presentChildren.map { child -> child.name })
    }

    @Test
    fun aSharedCitationInAnswerWordsIsLinkedToItsOwnersPage() {
        val usernames = SharedCitations.usernames(listOf(named("ada", "Ada Example"), named("ada", "Ada Example"), named("ben", "Ben Example")))
        assertEquals(mapOf("ada example" to "ada", "ben example" to "ben"), usernames)
        // Two people who share a display name cannot be told apart by it.
        assertNull(SharedCitations.usernames(listOf(named("ada", "Ada Example"), named("ada2", "Ada Example")))["ada example"])

        val words = "It is on people/a-grandparent#3 (shared by Ada Example), at three."
        assertTrue(SharedCitations.hasCitation(words))
        assertFalse(SharedCitations.hasCitation("It is on people/a-grandparent#3."))
        assertEquals(
            "It is on [people/a-grandparent#3 (shared by Ada Example)](memory:people/a-grandparent#3@ada), at three.",
            SharedCitations.linking(words, usernames),
        )
        // An owner not known, and a citation already made a link, stay as written.
        assertEquals(words, SharedCitations.linking(words, mapOf("ben example" to "ben")))
        val linked = "[people/a-grandparent#3 (shared by Ada Example)](memory:people/a-grandparent#3@ada)"
        assertEquals(linked, SharedCitations.linking(linked, usernames))
        // Two in one answer are each linked.
        assertEquals(
            "[a/b#1 (shared by Ada Example)](memory:a/b#1@ada) and [c/d#2 (shared by Ben Example)](memory:c/d#2@ben)",
            SharedCitations.linking("a/b#1 (shared by Ada Example) and c/d#2 (shared by Ben Example)", usernames),
        )
    }

    @Test
    fun theDocumentsAskForTheOperationsByName() {
        for ((document, operation) in listOf(
            KnowledgeDocuments.SHARES to "ListAgentPageShares", KnowledgeDocuments.SHARE_PAGE to "ShareAgentPage",
            KnowledgeDocuments.UNSHARE_PAGE to "UnshareAgentPage", KnowledgeDocuments.PREVIEW_SHARE to "PreviewAgentPageShare",
            KnowledgeDocuments.SHARED_ROOTS to "ListSharedAgentPages", KnowledgeDocuments.SHARED_PAGE to "ReadSharedAgentPage",
            KnowledgeDocuments.SEARCH_SHARED to "SearchSharedAgentPages",
        )) {
            assertTrue(document.contains(operation), operation)
        }
    }

    @Test
    fun aSearchOfWhatIsSharedSaysWhoseEachHitIsAndWhetherItIsAFact() = runBlocking {
        val body = """
            {"data":{"SearchSharedAgentPages":{"hasMoreHits":true,"hits":[
              {"ownerUserId":"ada-id","ownerUsername":"ada","ownerName":"Ada Example","path":"people/a-grandparent","name":"A Grandparent","opening":"Lives by the sea.","factNumber":0,"text":""},
              {"ownerUserId":"ada-id","ownerUsername":"ada","ownerName":"","path":"projects/garden","name":"","opening":"","factNumber":3,"text":"Next appointment is Tuesday."}]}}}
        """
        val (connection, fake) = fakeConnection { body }
        val search = connection.searchSharedKnowledge("appointment", first = 5)
        assertTrue(search.hasMoreHits)
        assertEquals(listOf(false, true), search.hits.map { hit -> hit.isFact })
        assertEquals(listOf("A Grandparent", "projects/garden"), search.hits.map { hit -> hit.shownName })
        assertEquals(listOf("Ada Example", "ada"), search.hits.map { hit -> hit.shownOwner })
        assertEquals(listOf("Lives by the sea.", "Next appointment is Tuesday."), search.hits.map { hit -> hit.shownText })
        val sent = fake.sent.last().body!!.toString(Charsets.UTF_8)
        assertTrue(sent.contains("\"query\":\"appointment\"") && sent.contains("\"first\":5"), sent)
    }

    @Test
    fun aPreviewListsThePagesItReachesAndTheFactsItKeepsBack() = runBlocking {
        val body = """
            {"data":{"PreviewAgentPageShare":{"page":{"ownerUserId":"ada-id","ownerUsername":"ada","ownerName":"Ada Example","ownerAvatarUrl":null,"ownerAvatarColorIndex":1,
              "path":"people/a-grandparent","name":"A Grandparent","nodeKind":"person","aliases":[],"opening":"Lives by the sea.","isQuoteShared":false,
              "facts":[],"edges":[],"children":[]},
              "reachedPageCount":4,"isReachedPageCountCut":false,"reachedPagePaths":["people/a-grandparent","people/a-grandparent/doctor"],"withheldFactCount":3}}}
        """
        val (connection, _) = fakeConnection { body }
        val preview = connection.previewKnowledgeShare("people/a-grandparent", "ben-id", null, false)
        assertEquals(4, preview.reachedPageCount)
        assertEquals(listOf("people/a-grandparent", "people/a-grandparent/doctor"), preview.reachedPagePaths)
        assertEquals(3, preview.withheldFactCount)
        assertTrue(KnowledgeDocuments.PREVIEW_SHARE.contains("reachedPagePaths withheldFactCount"))
    }
}
