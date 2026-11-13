package com.teanode.kit.knowledge

import com.teanode.kit.TeaNodeJson
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class KnowledgeTest {
    private fun row(path: String, kind: String = "topic", pinned: Boolean = false, children: Int = 0): KnowledgeRow = TeaNodeJson.decodeFromString(
        KnowledgeRow.serializer(),
        """{"node":{"id":"$path","path":"$path","kind":"$kind","name":"","summary":"About $path","pinned":$pinned},"hint":"","children":$children}""",
    )

    @Test
    fun rowsPutSelfAndPinnedFirstThenWhatOpensIntoMore() {
        val rows = listOf(
            row("garden"), row("projects", kind = "folder", children = 3), row("self", kind = "person"),
            row("music", pinned = true), row("cooking", children = 2), row("travel"),
        )
        assertEquals(listOf("self", "music", "projects", "cooking", "garden", "travel"), KnowledgeRow.ordered(rows).mapNotNull { ordered -> ordered.node?.path })
        assertEquals("Garden-planner", row("projects/garden-planner").node?.shownName)
        assertEquals("projects", row("projects/garden-planner").node?.parentPath)
        assertEquals("About garden", row("garden").line)
    }

    @Test
    fun aFactsFilesAreTheAttachmentsItCites() {
        val page = TeaNodeJson.decodeFromString(
            KnowledgePage.serializer(),
            """
            {"node":null,"folded":[],"children":[],"contact":null,
             "facts":[{"id":"f1","number":1,"kind":"reflection","text":"Plans slip in spring.","happenedAt":null,"inferred":true,
               "evidence":[{"kind":"dream","id":"d1","quote":"reflection: pattern"},{"kind":"memory","id":"","quote":"projects/garden#3"},{"kind":"document","id":"doc7[page 2]","quote":""}]}],
             "attachments":[{"documentId":"doc7","name":"plan.jpg","contentType":"image/jpeg","channel":"photos","path":"/api/v1/agent/documents/doc7/file"},
                            {"documentId":"doc8","name":"other.pdf","contentType":"application/pdf","channel":"","path":""}]}
            """,
        )
        val fact = page.presentFacts.first()
        assertEquals("pattern", fact.reflectionKind)
        assertEquals(listOf("projects/garden#3"), fact.citations)
        assertEquals(listOf("plan.jpg"), page.attachments(fact).map { attachment -> attachment.name })
    }

    @Test
    fun whenAFactWasTrueIsAYearAMonthOrADay() {
        assertTrue(KnowledgeWords.isHappenedValid(""))
        assertTrue(KnowledgeWords.isHappenedValid("2023"))
        assertTrue(KnowledgeWords.isHappenedValid("2023-06"))
        assertTrue(KnowledgeWords.isHappenedValid("2023-06-14"))
        assertFalse(KnowledgeWords.isHappenedValid("June 2023"))
        assertFalse(KnowledgeWords.isHappenedValid("2023-6"))
    }
}
