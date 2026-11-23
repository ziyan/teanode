package com.teanode.app.mail

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.teanode.kit.mail.MailFilter
import com.teanode.kit.mail.MailThread
import com.teanode.kit.mail.mailThreads

/** ThreadListModel is one list's conversations, a page at a time, narrowed as the person chose. */
class ThreadListModel(val place: MailPlace, startingFilter: MailFilter = MailFilter()) {
    var threads by mutableStateOf(emptyList<MailThread>())
        private set
    var total by mutableStateOf(0L)
        private set
    var isLoading by mutableStateOf(false)
        private set
    var isLoaded by mutableStateOf(false)
        private set
    var failure by mutableStateOf<Throwable?>(null)
        private set
    var filter by mutableStateOf(startingFilter)
    private var generation = 0

    /** Whether the list spans every folder: Starred, Priority, or a search everywhere. */
    val isAcrossFolders: Boolean
        get() = when (place) {
            MailPlace.Starred, MailPlace.Priority -> true
            else -> filter.isEverywhere && filter.isNarrowing
        }

    suspend fun load(mail: MailModel, isMore: Boolean = false) {
        generation += 1
        val mine = generation
        isLoading = true
        var asked = filter
        var folderId: String? = null
        var mailboxId: String? = null
        when (place) {
            is MailPlace.Folder -> if (isAcrossFolders) mailboxId = mail.mailbox?.id else folderId = place.folderId
            MailPlace.Starred -> {
                asked = asked.copy(isStarredOnly = true)
                mailboxId = mail.mailbox?.id
            }
            MailPlace.Priority -> mailboxId = mail.mailbox?.id
            MailPlace.Subscriptions -> {
                isLoading = false
                return
            }
        }
        try {
            val page = mail.connection.mailThreads(folderId, mailboxId, asked, isPriority = place == MailPlace.Priority, first = PAGE_SIZE, offset = if (isMore) threads.size else 0)
            if (mine != generation) return
            val fresh = page.threads.filterNotNull()
            threads = if (isMore) threads + fresh.filter { row -> threads.none { existing -> existing.id == row.id } } else fresh
            total = page.total
            isLoaded = true
            failure = null
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            if (mine != generation) return
            if (isLoaded) mail.snackbar?.showError(failed) else failure = failed
        } finally {
            if (mine == generation) isLoading = false
        }
    }

    /** What acting on a row means: in a folder, every message of the conversation there; across folders, the one message shown. */
    fun target(thread: MailThread): MailTarget? {
        val item = thread.item ?: return null
        if (isAcrossFolders) return MailTarget.of(listOf(item))
        return MailTarget(thread.itemIds.ifEmpty { listOf(item.id) }, emptyMap(), item.folderId)
    }

    fun target(chosen: List<MailThread>): MailTarget? {
        val targets = chosen.mapNotNull { thread -> target(thread) }
        val first = targets.firstOrNull() ?: return null
        return targets.drop(1).fold(first) { joined, other -> joined.joined(other) }
    }

    fun remove(removed: Set<String>) {
        val before = threads.size
        threads = threads.filter { thread -> !removed.contains(thread.id) }
        total = maxOf(0, total - (before - threads.size))
    }

    fun patch(ids: Set<String>, isSeen: Boolean? = null, isFlagged: Boolean? = null) {
        threads = threads.map { thread -> if (ids.contains(thread.id)) thread.with(isSeen, isFlagged) else thread }
    }

    companion object {
        const val PAGE_SIZE = 50
    }
}
