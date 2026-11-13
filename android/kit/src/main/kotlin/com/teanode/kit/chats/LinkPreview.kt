package com.teanode.kit.chats

import com.teanode.kit.Connection
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerialName
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import java.net.URI

// What the page behind a link says about itself, for the card under a message (ReadLinkPreviews). The server fetches
// the page and its picture, never this phone, and keeps both, so asking again is cheap. The picture is the server's
// copy, read from it like any other picture of an account.

/** LinkPreview is one link's preview. When `isAvailable` is false the page could not be read or had nothing to say, and the rest is empty. */
@Serializable
data class LinkPreview(
    /** The link as it was asked for. */
    val url: String,
    /** The link without its tracking parameters: what a tap opens. */
    val cleanedUrl: String = url,
    val displayText: String = url,
    val isAvailable: Boolean = false,
    val title: String = "",
    val description: String = "",
    /** What the page calls its site, or its host when it says nothing. */
    val siteName: String = "",
    /** The picture, on the person's own server; null when the page has none. */
    val imageUrl: String? = null,
    val imageWidthPixels: Int? = null,
    val imageHeightPixels: Int? = null,
) {
    /** Whether there is a card to draw: the page was read, its address is a web address, and there is a title or a line to put on it. */
    val hasCard: Boolean
        get() {
            if (!isAvailable) return false
            val scheme = try {
                URI(cleanedUrl).scheme?.lowercase()
            } catch (_: Exception) {
                null
            }
            return (scheme == "http" || scheme == "https") && (title.isNotEmpty() || description.isNotEmpty())
        }

    companion object {
        /** How many links one question may be about. */
        const val MOST_URLS = 10

        /**
         * The links whose cards the lines read so far show, the newest line's first and each once, to be asked for
         * before the lazy list draws those lines: a card that arrives after its message is drawn makes the message
         * taller under the person's finger. Each line is read as its view reads it: a person's words as they stand and
         * the agent's as Markdown, and none of a message still being written or sent, or taken back.
         */
        fun linksAhead(lines: List<TranscriptLine>): List<String> {
            val seen = mutableSetOf<String>()
            val links = mutableListOf<String>()
            for (line in lines.asReversed()) {
                val link = when {
                    line.kind == TranscriptLine.Kind.Person && !line.isBeingSent && !line.isDeleted -> LinkText.firstLink(line.text, isMarkdown = false)
                    line.kind == TranscriptLine.Kind.Answer && !line.isStreaming && !line.isDeleted -> LinkText.firstLink(line.text, isMarkdown = true)
                    else -> null
                }
                if (link != null && seen.add(link)) links.add(link)
            }
            return links
        }
    }
}

@Serializable
private class LinkPreviewsAnswer(@SerialName("ReadLinkPreviews") val previews: List<LinkPreview>)

/**
 * The preview of each link, in the order given, at most `LinkPreview.MOST_URLS` of them, for links in the words of a
 * conversation. The server previews a link only if its exact text is in a message of that conversation that the agent
 * did not write, so the links are sent as they stand in the messages, never cleaned.
 */
suspend fun Connection.linkPreviews(conversationId: String, urls: List<String>): List<LinkPreview> {
    val variables: Map<String, JsonElement> = mapOf(
        "conversationId" to JsonPrimitive(conversationId),
        "urls" to kotlinx.serialization.json.JsonArray(urls.map { url -> JsonPrimitive(url) }),
    )
    return execute(ChatDocuments.READ_LINK_PREVIEWS, LinkPreviewsAnswer.serializer(), variables).previews
}

/**
 * LinkPreviewLoader asks for the previews of the links of a screenful in one question for each conversation, ten at a
 * time, and never asks again for a link in a conversation it has asked for (until a question that failed has waited
 * out `failedForMS`). The server keeps what it read, but a conversation holds many links, and one question a link would
 * be one trip each.
 */
class LinkPreviewLoader(
    private val collectingForMS: Long = 60,
    private val failedForMS: Long = 60_000,
    private val load: suspend (conversationId: String, urls: List<String>) -> List<LinkPreview>,
) {
    private class Waiting(val conversationId: String, val url: String, val settle: CompletableDeferred<LinkPreview?>)

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val lock = Mutex()
    private val known = mutableMapOf<String, Deferred<LinkPreview?>>()
    private val waiting = mutableListOf<Waiting>()
    private var isCollecting = false

    private fun key(conversationId: String, url: String): String = conversationId + "\n" + url

    /** The preview of a link written in a conversation, or null when there is none to show. */
    suspend fun preview(conversationId: String, url: String): LinkPreview? {
        val asked = lock.withLock {
            known.getOrPut(key(conversationId, url)) {
                val settle = CompletableDeferred<LinkPreview?>()
                waiting.add(Waiting(conversationId, url, settle))
                if (!isCollecting) {
                    isCollecting = true
                    scope.launch { sendWaiting() }
                }
                settle
            }
        }
        return asked.await()
    }

    private suspend fun sendWaiting() {
        delay(collectingForMS)
        while (true) {
            // The oldest question's conversation goes first, with as many of its links as one question takes.
            val batch = lock.withLock {
                val first = waiting.firstOrNull()
                if (first == null) {
                    isCollecting = false
                    return
                }
                val taken = waiting.filter { each -> each.conversationId == first.conversationId }.take(LinkPreview.MOST_URLS)
                waiting.removeAll(taken.toSet())
                taken
            }
            val previews = try {
                load(batch.first().conversationId, batch.map { each -> each.url })
            } catch (_: Exception) {
                null
            }
            for ((index, asked) in batch.withIndex()) {
                asked.settle.complete(previews?.getOrNull(index))
                if (previews == null) forgetLater(key(asked.conversationId, asked.url))
            }
        }
    }

    /** A question that failed is asked again after a while, not at once. */
    private fun forgetLater(key: String) {
        scope.launch {
            delay(failedForMS)
            lock.withLock { known.remove(key) }
        }
    }
}
