package com.teanode.app.feed

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.kit.AppSection
import com.teanode.kit.feed.AgentFeedEvents
import com.teanode.kit.feed.agentFeed
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay

/** How often the unread count is read when no change has come: the live connection can be down. */
private const val FEED_BADGE_POLL_MS = 60_000L

/**
 * Keeps the red count on the Feed tab right whichever part is open, while the app is in front: it is read when the app
 * comes forward, every minute, and set at once by each change the server reports. The Feed screen sets it too, from
 * what it shows.
 */
@Composable
fun FeedBadge(isOffered: Boolean) {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val lifecycleOwner = LocalLifecycleOwner.current
    LaunchedEffect(connection, isOffered) {
        if (!isOffered) return@LaunchedEffect
        if (session.isDemo) {
            // Demo mode never connects: the count its cards have, so the tab can be photographed with its badge.
            session.setBadgeCount(AppSection.Feed, demoFeedCards.count { card -> !card.isRead })
            return@LaunchedEffect
        }
        lifecycleOwner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            coroutineScope {
                launch {
                    while (true) {
                        try {
                            AgentFeedEvents.changes(connection).collect { changed -> session.setBadgeCount(AppSection.Feed, changed.unreadCount) }
                        } catch (failure: CancellationException) {
                            throw failure
                        } catch (_: Throwable) {
                            // Opened again after the pause.
                        }
                        delay(FEED_BADGE_POLL_MS)
                    }
                }
                while (true) {
                    runCatching { connection.agentFeed(first = 1) }.onSuccess { page -> session.setBadgeCount(AppSection.Feed, page.unreadCount) }
                    delay(FEED_BADGE_POLL_MS)
                }
            }
        }
    }
}
