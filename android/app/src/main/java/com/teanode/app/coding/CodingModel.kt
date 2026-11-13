package com.teanode.app.coding

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.teanode.kit.Connection
import com.teanode.kit.coding.CodingComputer
import com.teanode.kit.coding.HerdrList
import com.teanode.kit.coding.HerdrSession
import com.teanode.kit.coding.carryingFailedComputersFrom
import com.teanode.kit.coding.computers
import com.teanode.kit.coding.herdrSessions
import com.teanode.kit.coding.removing
import com.teanode.kit.coding.upserting
import kotlinx.coroutines.CancellationException
import java.util.WeakHashMap

/**
 * CodingModel is the list of the person's coding sessions, shared by the list and the session on screen, which keeps the
 * sessions' states. One lives for each connection, so a session opened from the list reads the list the list holds.
 */
class CodingModel(val connection: Connection) {
    var list: HerdrList? by mutableStateOf(null)
        private set

    /** Why the list could not be read, while there is none to show. */
    var loadFailure: Throwable? by mutableStateOf(null)
        private set

    /** Reads the list again; what was known stays when it cannot be read. */
    suspend fun load() {
        try {
            val read = connection.herdrSessions()
            // A computer that did not answer this time keeps the sessions last known for it, so a session open on screen
            // does not vanish for one slow answer.
            list = read.carryingFailedComputersFrom(list)
            loadFailure = null
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (failure: Throwable) {
            loadFailure = failure
        }
    }

    fun session(id: String): HerdrSession? = list?.sessions?.firstOrNull { session -> session.id == id }

    /** Puts a session as the server just answered it in the list, so a change shows before the list is read again. */
    fun upsert(session: HerdrSession) {
        list = list?.upserting(session)
    }

    fun remove(id: String) {
        list = list?.removing(id)
    }

    /** Every computer with its workspaces, by name. */
    val computers: List<CodingComputer> get() = list?.computers() ?: emptyList()

    val computerNames: List<String> get() = list?.computerNames ?: emptyList()

    companion object {
        private val models = WeakHashMap<Connection, CodingModel>()

        fun of(connection: Connection): CodingModel = synchronized(models) { models.getOrPut(connection) { CodingModel(connection) } }
    }
}

/** What opens one coding session: the computer and the pane, since no two computers' panes are told apart by the pane's id alone. */
fun codingSessionRoute(computer: String, paneId: String): String =
    "coding/session/${android.net.Uri.encode(computer)}/${android.net.Uri.encode(paneId)}"

/** The model of the connection in use, with the list read when it has not been (a screen opened from a link has no list yet). */
@androidx.compose.runtime.Composable
internal fun rememberCodingModel(connection: Connection): CodingModel {
    val model = androidx.compose.runtime.remember(connection) { CodingModel.of(connection) }
    androidx.compose.runtime.LaunchedEffect(model) { if (model.list == null) model.load() }
    return model
}
