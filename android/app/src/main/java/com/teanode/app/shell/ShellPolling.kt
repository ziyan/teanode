package com.teanode.app.shell

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.platform.LocalContext
import com.teanode.app.chats.ChatsEnvironment
import androidx.compose.runtime.collectAsState
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import com.teanode.app.coding.CodingModel
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.kit.AppSection
import com.teanode.kit.chats.reportPresence
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * ShownConversation is which conversation the person is looking at right now, for the presence the shell reports: the
 * conversation screen sets it while it is in front and clears it when it is not. An empty text is a conversation that is
 * not a chat (the main one and the side chats), which the server holds no messages back for.
 */
object ShownConversation {
    private val shownConversationId = MutableStateFlow<String?>(null)

    val conversationId: StateFlow<String?> = shownConversationId.asStateFlow()

    fun show(conversationId: String) {
        shownConversationId.value = conversationId
    }

    fun clear() {
        shownConversationId.value = null
    }
}

/** How often the shell tells the server where the person is, and asks whether a coding session is waiting (ios AppShell.refreshPresence). */
const val SHELL_POLL_MS = 30_000L

/**
 * What the shell does while the app is in front, whichever part is open (ios AppShell.refreshPresence and
 * refreshCodingPresence): every half minute it tells the server whether a chat is on screen, so a message in it is held
 * back from the phone only then, and it reads the chats for the badge on Chats, and it asks which coding sessions wait, so the badge on Coding is right while Coding is
 * not the part on screen.
 */
@Composable
fun ShellPolling(currentSection: AppSection?) {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val lifecycleOwner = LocalLifecycleOwner.current
    val context = LocalContext.current.applicationContext
    val facts by session.facts.collectAsState()
    val latestFacts by rememberUpdatedState(facts)
    val latestSection by rememberUpdatedState(currentSection)
    LaunchedEffect(connection) {
        if (session.isDemo) return@LaunchedEffect
        lifecycleOwner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (true) {
                val isChatOffered = latestFacts.services?.isAssistantEnabled == true || latestFacts.session?.holds("circle:use") == true
                if (isChatOffered) {
                    val shownId = ShownConversation.conversationId.value
                    runCatching { connection.reportPresence(shownId != null, shownId.orEmpty()) }
                }
                // The Chats list reads for itself while it is on screen; the badge is kept right from here the rest of the time.
                runCatching { ChatsEnvironment.of(context, connection, session).refreshChats() }
                delay(SHELL_POLL_MS)
            }
        }
    }
    LaunchedEffect(connection) {
        if (session.isDemo) return@LaunchedEffect
        lifecycleOwner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            val model = CodingModel.of(connection)
            while (true) {
                // The Coding list reads for itself while it is on screen.
                if (latestFacts.services?.isAssistantEnabled == true && latestSection != AppSection.Coding) {
                    model.load()
                    if (model.computerNames.isNotEmpty()) session.noteCodingComputers()
                    model.list?.let { list -> session.setBadgeCount(AppSection.Coding, list.sessions.count { entry -> entry.isAsking }) }
                }
                delay(SHELL_POLL_MS)
            }
        }
    }
}
