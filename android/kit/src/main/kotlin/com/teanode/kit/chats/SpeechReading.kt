package com.teanode.kit.chats

import com.teanode.kit.Connection
import com.teanode.kit.HTTPRequest
import com.teanode.kit.TeaNodeError
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.stringValue
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

/** The documents and calls that read a message aloud in the server's voice. */
object SpeechDocuments {
    const val SPEECH_PIECES = """
        query (${'$'}messageText: String!) { ReadSpeechPieces(messageText: ${'$'}messageText) }
        """
}

@Serializable
private class SpeechPiecesAnswer(@SerialName("ReadSpeechPieces") val pieces: List<String>)

/** A message cut into the pieces the server reads one at a time. */
suspend fun Connection.speechPieces(messageText: String): List<String> =
    execute(SpeechDocuments.SPEECH_PIECES, SpeechPiecesAnswer.serializer(), mapOf("messageText" to JsonPrimitive(messageText))).pieces

/** A piece of a message in the voice the person chose, as a WAV file. It is paid for like any speech. */
suspend fun Connection.speechAudio(answerText: String): ByteArray {
    val body = JsonObject(mapOf("answerText" to JsonPrimitive(answerText))).toString().toByteArray()
    val response = send(HTTPRequest(account.server.endpoint("/api/v1/agent/voice/read").toString(), "POST", mapOf("Content-Type" to "application/json"), body))
    if (response.statusCode != 200) {
        val reason = runCatching { (TeaNodeJson.parseToJsonElement(response.text) as JsonObject)["error"]?.stringValue }.getOrNull()
        throw TeaNodeError.Refused(reason ?: "The message could not be read aloud (${response.statusCode}).")
    }
    return response.body
}
