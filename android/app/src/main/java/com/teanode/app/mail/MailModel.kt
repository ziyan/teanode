package com.teanode.app.mail

import android.content.Context
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.SnackbarController
import com.teanode.kit.Connection
import com.teanode.kit.mail.MailFolder
import com.teanode.kit.mail.MailFolderRow
import com.teanode.kit.mail.MailFolders
import com.teanode.kit.mail.MailItem
import com.teanode.kit.mail.MailMoved
import com.teanode.kit.mail.Mailbox
import com.teanode.kit.mail.MailboxEntry
import com.teanode.kit.mail.ServerTime
import com.teanode.kit.mail.agentCategories
import com.teanode.kit.mail.deleteMail
import com.teanode.kit.mail.mailboxes
import com.teanode.kit.mail.moveMail
import com.teanode.kit.mail.newestMailItems
import com.teanode.kit.mail.reportJunk
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import java.time.Instant
import java.util.WeakHashMap

/**
 * MailTarget is what an action acts on: the items, and where each came from, so the action can be undone. A
 * conversation's row in a folder stands for all its messages there, whose home is that folder.
 */
data class MailTarget(val itemIds: List<String>, val homes: Map<String, String> = emptyMap(), val home: String) {
    fun home(mailId: String): String = homes[mailId] ?: home

    /** The target of these and the others put together. */
    fun joined(other: MailTarget): MailTarget = MailTarget(itemIds + other.itemIds, other.homes + homes, home)

    companion object {
        fun of(items: List<MailItem>): MailTarget {
            val homes = LinkedHashMap<String, String>()
            for (item in items) homes.putIfAbsent(item.mailId, item.folderId)
            return MailTarget(items.map { item -> item.id }, homes, items.firstOrNull()?.folderId ?: "")
        }
    }
}

/** The folders for the list: pinned ones first, then the rest of the tree. */
data class MailRailRows(val pinned: List<MailFolderRow>, val rest: List<MailFolderRow>)

/**
 * MailModel is the person's mailboxes as the dashboard's rail keeps them: the one being read, its folders and counts,
 * read again after each change and whenever the app comes back. One lives for each account, so the screens of Mail
 * share what is read.
 */
class MailModel(val connection: Connection, val context: Context) {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val preferences = runCatching { context.getSharedPreferences("mail", Context.MODE_PRIVATE) }.getOrNull()
    private val currentMailboxKey = "mail-current-mailbox.${connection.account.username}"

    var entries by mutableStateOf(emptyList<MailboxEntry>())
        private set
    var isLoaded by mutableStateOf(false)
        private set
    var loadFailure by mutableStateOf<Throwable?>(null)
        private set
    var categories by mutableStateOf(emptyList<String>())
        private set

    /** Where the snackbar of the screen on show is: set by [rememberMailModel] on every composition. */
    var snackbar: SnackbarController? = null

    var currentMailboxId: String? by mutableStateOf(runCatching { preferences?.getString(currentMailboxKey, null) }.getOrNull())
        private set

    fun chooseMailbox(mailboxId: String?) {
        currentMailboxId = mailboxId
        runCatching { preferences?.edit()?.putString(currentMailboxKey, mailboxId)?.apply() }
    }

    val current: MailboxEntry? get() = entries.firstOrNull { entry -> entry.mailbox?.id == currentMailboxId } ?: entries.firstOrNull()
    val mailbox: Mailbox? get() = current?.mailbox
    val folders: List<MailFolder> get() = current?.presentFolders ?: emptyList()

    fun folderById(folderId: String): MailFolder? = entries.asSequence().flatMap { entry -> entry.presentFolders.asSequence() }.firstOrNull { folder -> folder.id == folderId }

    fun folderOfKind(kind: String): MailFolder? = MailFolders.folder(kind, folders)

    /** The folders for the list: pinned ones first in the order they were pinned, each with its children, then the rest of the tree. */
    val railRows: MailRailRows
        get() {
            val rows = MailFolders.rows(folders)
            val pinnedIds = folders.filter { folder -> folder.pinnedAt != null && folder.kind != "inbox" }.sortedBy { folder -> folder.pinnedAt ?: "" }.map { folder -> folder.id }
            val pinned = mutableListOf<MailFolderRow>()
            val taken = mutableSetOf<String>()
            for (pinnedId in pinnedIds) {
                if (taken.contains(pinnedId)) continue
                val start = rows.indexOfFirst { row -> row.folder.id == pinnedId }
                if (start < 0) continue
                val depth = rows[start].depth
                var index = start
                do {
                    pinned.add(MailFolderRow(rows[index].folder, rows[index].depth - depth))
                    taken.add(rows[index].folder.id)
                    index += 1
                } while (index < rows.size && rows[index].depth > depth)
            }
            return MailRailRows(pinned, rows.filter { row -> !taken.contains(row.folder.id) })
        }

    /** The agent's categories, or the fixed list when it has none. */
    val sortCategories: List<String>
        get() = categories.ifEmpty { listOf("personal", "work", "newsletter", "notification", "receipt", "promotion", "social", "invitation", "phishing", "junk", "other") }

    suspend fun load() {
        try {
            entries = connection.mailboxes()
            if (current?.mailbox?.id != currentMailboxId) chooseMailbox(current?.mailbox?.id)
            isLoaded = true
            loadFailure = null
            if (categories.isEmpty() && mailbox?.agent?.isSorting == true) {
                categories = runCatching { connection.agentCategories() }.getOrDefault(emptyList())
            }
        } catch (failure: kotlinx.coroutines.CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            if (!isLoaded) loadFailure = failure else snackbar?.showError(failure)
        }
    }

    /** Reads the mailboxes without waiting, for a screen that has no scope of its own to wait in. */
    fun reload() {
        scope.launch { load() }
    }

    /** Runs an action, says what it did, and reads the counts again. An undo is offered when the action has one. */
    suspend fun act(said: String, undo: (suspend () -> Unit)? = null, work: suspend () -> Unit): Boolean {
        return try {
            work()
            if (undo != null) {
                snackbar?.show(said, context.getString(R.string.mail_undo)) { scope.launch { undo() } }
            } else {
                snackbar?.show(said)
            }
            load()
            true
        } catch (failure: kotlinx.coroutines.CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            snackbar?.showProblem(context.getString(R.string.mail_that_did_not_work, failure.message.orEmpty()))
            load()
            false
        }
    }

    /** Moves messages, offering to move them back to where each came from. */
    suspend fun move(target: MailTarget, folderId: String, said: String): Boolean {
        var moved = emptyList<MailMoved>()
        return act(said, undo = { putBack(moved, target) }) { moved = connection.moveMail(target.itemIds, folderId) }
    }

    /** Moves messages to Trash, or out of it for good. From another folder it can be undone: what landed in Trash is found and moved home. */
    suspend fun delete(target: MailTarget, isForever: Boolean, said: String): Boolean {
        val trash = folderOfKind("trash")
        if (isForever || trash == null) return act(said) { connection.deleteMail(target.itemIds) }
        val since = Instant.now().minusSeconds(1)
        return act(said, undo = {
            val landed = landed(trash.id, since)
            putBack(landed, target)
        }) { connection.deleteMail(target.itemIds) }
    }

    /** Reports junk, or not junk; undone by reporting the other way and moving each message back to where it was. */
    suspend fun reportJunk(target: MailTarget, isNotJunk: Boolean, said: String): Boolean {
        val since = Instant.now().minusSeconds(1)
        val landingKind = if (isNotJunk) "inbox" else "junk"
        val returnKind = if (isNotJunk) "junk" else "inbox"
        return act(said, undo = {
            val landing = folderOfKind(landingKind)
            if (landing != null) {
                val landed = landed(landing.id, since)
                val undoneSince = Instant.now().minusSeconds(1)
                runCatching { connection.reportJunk(landed.map { moved -> moved.id }, !isNotJunk) }
                val back = folderOfKind(returnKind)
                if (back != null) {
                    val mailIds = landed.map { moved -> moved.mailId }.toSet()
                    val returned = landed(back.id, undoneSince).filter { moved -> mailIds.contains(moved.mailId) }
                    putBack(returned.filter { moved -> target.home(moved.mailId) != back.id }, target)
                }
                load()
            }
        }) { connection.reportJunk(target.itemIds, isNotJunk) }
    }

    /** The items that arrived in a folder since a time, as the dashboard finds what an action put there. */
    private suspend fun landed(folderId: String, since: Instant): List<MailMoved> {
        val newest = runCatching { connection.newestMailItems(folderId) }.getOrDefault(emptyList())
        return newest.filter { item ->
            val addedAt = ServerTime.instant(item.addedAt)
            addedAt != null && !addedAt.isBefore(since)
        }
    }

    /** Moves items back to the folders they came from, a folder at a time. */
    private suspend fun putBack(moved: List<MailMoved>, target: MailTarget) {
        for ((home, items) in moved.groupBy { item -> target.home(item.mailId) }) {
            if (home.isEmpty()) continue
            runCatching { connection.moveMail(items.map { item -> item.id }, home) }
        }
        snackbar?.show(context.getString(R.string.mail_undone))
        load()
    }

    companion object {
        private val models = WeakHashMap<Connection, MailModel>()

        /** The model of this account's mail, made once. */
        @Synchronized
        fun of(connection: Connection, context: Context): MailModel = models.getOrPut(connection) { MailModel(connection, context.applicationContext) }
    }
}

/** The mail of the account in use, read when the first screen needs it. */
@Composable
fun rememberMailModel(): MailModel {
    val connection = LocalConnection.current
    val context = LocalContext.current
    val snackbar = LocalSnackbar.current
    val model = remember(connection) { MailModel.of(connection, context) }
    SideEffect { model.snackbar = snackbar }
    return model
}
