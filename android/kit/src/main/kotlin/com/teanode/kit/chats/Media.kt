package com.teanode.kit.chats

import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeError
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.get
import com.teanode.kit.stringValue
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.doubleOrNull
import java.net.URLEncoder

/** AgentFile is a file the agent handed the person: one a tool shared (the share_file tool, or a skill's files), or an artifact it made. */
data class AgentFile(
    val attachmentId: String,
    val name: String,
    val contentType: String,
    val size: Int,
    val caption: String,
    /** For an artifact: "html", "svg" or "markdown"; empty for a file. */
    val artifactKind: String,
    /** For a picture the photo tool showed: the photo it is, which the app opens in its photo viewer. Null for every other file. */
    val photoId: String?,
) {
    val isArtifact: Boolean get() = artifactKind.isNotEmpty()
    val isImage: Boolean get() = contentType.startsWith("image/")
    val isVideo: Boolean get() = contentType.startsWith("video/")
    val isAudio: Boolean get() = contentType.startsWith("audio/")

    /** The address it is read from on the server. */
    val path: String get() = "/api/v1/agent/attachments/$attachmentId"

    companion object {
        /**
         * The files a tool's result hands the person. A result names its file by the address it is served from on this
         * server; one naming any other address is not a file of this conversation and is never fetched. The photo of a
         * file is read only from the photo tool's result, and only when it is shaped like an id of the server's: any
         * other tool's answer is somebody else's text.
         */
        fun of(toolResult: String, tool: String = ""): List<AgentFile> {
            var text = toolResult.trim()
            if (text.startsWith("<untrusted-data>") && text.endsWith("</untrusted-data>")) {
                text = text.removePrefix("<untrusted-data>").removeSuffix("</untrusted-data>")
            }
            val value = try {
                TeaNodeJson.parseToJsonElement(text)
            } catch (_: Exception) {
                return emptyList()
            }
            val files = value["files"]
            val entries = if (files is JsonArray) files.toList() else listOf(value)
            return entries.mapNotNull { entry ->
                val artifactId = entry["artifact_id"]?.stringValue
                val artifactAddress = entry["url"]?.stringValue
                if (artifactId != null && artifactAddress != null && artifactAddress == "/api/v1/agent/attachments/$artifactId") {
                    val kind = entry["kind"]?.stringValue ?: "html"
                    return@mapNotNull AgentFile(
                        artifactId, entry["title"]?.stringValue.orEmpty(), if (kind == "markdown") "text/markdown" else "text/html", 0, "", kind, null,
                    )
                }
                val attachmentId = entry["attachment_id"]?.stringValue ?: return@mapNotNull null
                val address = entry["url"]?.stringValue ?: return@mapNotNull null
                if (address != "/api/v1/agent/attachments/$attachmentId") return@mapNotNull null
                val size = (entry["size"] as? JsonPrimitive)?.doubleOrNull?.toInt() ?: 0
                val contentType = entry["content_type"]?.stringValue.orEmpty()
                // A page sent to the main chat is shown as an artifact is, which is how the server serves it: in its sandbox.
                val isSentPage = !entry["sent_by"]?.stringValue.isNullOrEmpty() &&
                    (contentType.lowercase().startsWith("text/html") || contentType.lowercase().startsWith("image/svg+xml"))
                AgentFile(
                    attachmentId, entry["name"]?.stringValue.orEmpty(), contentType, size, entry["caption"]?.stringValue.orEmpty(),
                    if (isSentPage) "html" else "", if (tool == "photo") entry["photo_id"]?.stringValue?.let { text -> photoId(text) } else null,
                )
            }
        }

        /** The id when it is shaped like a photo's (letters, digits, "-" and "_", up to 64), and null otherwise. */
        fun photoId(text: String): String? {
            val bytes = text.toByteArray(Charsets.UTF_8)
            if (bytes.size !in 1..64) return null
            val isShaped = bytes.all { byte ->
                val code = byte.toInt()
                code in 48..57 || code in 65..90 || code in 97..122 || code == 45 || code == 95
            }
            return if (isShaped) text else null
        }
    }
}


@Serializable
private class ShareAddressAnswer(@SerialName("ShareAgentAttachment") val path: String)

/** Reads a file the server keeps for this account: an attachment, a picture an answer shows. */
suspend fun Connection.download(path: String): ByteArray {
    val response = get(path)
    if (response.statusCode != 200) throw TeaNodeError.Refused("The file could not be read (${response.statusCode}).")
    return response.body
}

/** An address for an attachment that opens without signing in, for a few hours: for a player or a web view that cannot send the token. */
suspend fun Connection.shareAddress(attachmentId: String): String {
    val path = execute(ChatDocuments.SHARE_ATTACHMENT, ShareAddressAnswer.serializer(), mapOf("attachmentId" to JsonPrimitive(attachmentId))).path
    return account.server.endpoint(path).toString()
}

/**
 * Where a picture an answer shows is read from: through the server, which fetches only addresses a tool showed the
 * agent in this conversation, so the phone never loads what a model wrote.
 */
fun picturePath(conversationId: String, address: String): String {
    val encoded = URLEncoder.encode(address, Charsets.UTF_8).replace("+", "%20").replace("*", "%2A")
    return "/api/v1/agent/conversations/$conversationId/picture?url=$encoded"
}
