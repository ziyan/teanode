package com.teanode.app.knowledge

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.teanode.app.ui.components.SnackbarController
import com.teanode.kit.Connection
import com.teanode.kit.knowledge.KnowledgeNeighbours
import com.teanode.kit.knowledge.KnowledgePage
import com.teanode.kit.knowledge.KnowledgeRecordLink
import com.teanode.kit.knowledge.KnowledgeRevision
import com.teanode.kit.knowledge.knowledgeHistory
import com.teanode.kit.knowledge.knowledgeNeighbours
import com.teanode.kit.knowledge.knowledgePage
import com.teanode.kit.knowledge.knowledgeRecordLinks

/** KnowledgePageModel is one page and its changes: each reads the page again, and says what it did (ios KnowledgeViews.swift, KnowledgePageModel). */
class KnowledgePageModel(val connection: Connection, pagePath: String, private val snackbar: SnackbarController) {
    var pagePath: String by mutableStateOf(pagePath)
        private set
    var page: KnowledgePage? by mutableStateOf(null)
        private set
    var neighbours: KnowledgeNeighbours? by mutableStateOf(null)
        private set
    var history: List<KnowledgeRevision>? by mutableStateOf(null)
        private set
    var isMissing: Boolean by mutableStateOf(false)
        private set
    var problem: Throwable? by mutableStateOf(null)
        private set

    /** The record links past the first part the page read, as they are asked for. */
    private var laterRecordLinks: List<KnowledgeRecordLink> by mutableStateOf(emptyList())

    /** The records the page is linked to that have been read so far. */
    val recordLinks: List<KnowledgeRecordLink> get() = page?.recordLinks.orEmpty().filterNotNull() + laterRecordLinks

    val recordLinkCount: Int get() = page?.recordLinkCount ?: 0

    /** Reads the next part of a page with more links than the page carries. */
    suspend fun loadMoreRecordLinks() {
        runCatching { connection.knowledgeRecordLinks(pagePath, first = 50, offset = recordLinks.size) }.getOrNull()?.let { more -> laterRecordLinks = laterRecordLinks + more }
    }

    suspend fun load() {
        try {
            val read = connection.knowledgePage(pagePath)
            page = read
            laterRecordLinks = emptyList()
            isMissing = read?.node == null
            neighbours = runCatching { connection.knowledgeNeighbours(pagePath) }.getOrNull()
            problem = null
        } catch (failure: kotlinx.coroutines.CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            problem = failure
        }
    }

    suspend fun loadHistory() {
        history = runCatching { connection.knowledgeHistory(pagePath) }.getOrDefault(emptyList())
    }

    /** Runs a change and reads the page again; false when it was refused. */
    suspend fun act(said: String, work: suspend () -> Unit): Boolean = try {
        work()
        snackbar.show(said)
        load()
        true
    } catch (failure: kotlinx.coroutines.CancellationException) {
        throw failure
    } catch (failure: Throwable) {
        snackbar.showError(failure)
        false
    }

    /** The page now lives at another path, after it was moved or merged. */
    fun moved(newPath: String) {
        pagePath = newPath
    }
}
