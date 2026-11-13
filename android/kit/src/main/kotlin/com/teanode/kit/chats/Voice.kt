package com.teanode.kit.chats

import com.teanode.kit.Connection
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** VoiceAvailability is whether the person may call their agent, the rate the server takes speech at, and the voices answers may be read in. */
@Serializable
data class VoiceAvailability(
    val isVoiceAvailable: Boolean = false,
    val sampleRate: Int? = null,
    /** Whether the server can prepare a recording sent as a voice message in a chat; null from a server that does not know them. */
    val isVoiceMessageAvailable: Boolean? = null,
    /** The voice answers are read in: the person's choice, or the server's. */
    val speechVoice: String? = null,
    /** The voices the person may choose. */
    val speechVoices: List<String>? = null,
    /** The server's voice, which an empty choice means. */
    val serverSpeechVoice: String? = null,
)

@Serializable
private class VoiceAnswer(@SerialName("ReadAgentVoice") val voice: VoiceAvailability)

suspend fun Connection.voiceAvailability(): VoiceAvailability = execute(ChatDocuments.VOICE, VoiceAnswer.serializer()).voice

/**
 * SpokenText turns an answer written for the screen into what is said: code, pictures and formatting left out, and
 * the text cut into sentences, each spoken as soon as it is whole.
 */
object SpokenText {
    /** The words of a Markdown answer, as they are read out: no code blocks, no formatting marks, a link's words without its address, no hidden lines. */
    fun of(markdown: String): String {
        var text = Suggestions.split(markdown).text
        // Code is for the screen.
        text = text.replace(Regex("""```[\s\S]*?(```|$)"""), " ")
        text = HiddenMarkers.removed(text)
        // A picture says nothing; a link says its words.
        text = text.replace(Regex("""!\[[^\]]*\]\([^)]*\)"""), " ")
        text = text.replace(Regex("""\[([^\]]*)\]\([^)]*\)"""), "$1")
        text = text.replace(Regex("""`([^`]*)`"""), "$1")
        // Formatting marks, headings, list marks, quotes and table rules.
        text = text.replace(Regex("""(\*\*|__|\*|~~)"""), "")
        text = text.replace(Regex("""(?m)^\s{0,3}(#{1,6}|>|[-*+]|\d{1,3}[.)])\s+"""), "")
        text = text.replace(Regex("""(?m)^\s*\|?[\s:|-]+\|?\s*$"""), "")
        text = text.replace("|", ", ")
        return text.replace(Regex("""\s+"""), " ").trim()
    }

    /** The sentences found at the start of a text, and how much of it they take. */
    data class Sentences(val sentences: List<String>, val consumed: Int)

    /** The whole sentences at the start of text being spoken as it is written, and how much of it they take; what is left is not finished. */
    fun sentences(text: String): Sentences {
        val sentences = mutableListOf<String>()
        var consumed = 0
        val current = StringBuilder()
        var index = 0
        while (index < text.length) {
            val character = text[index]
            current.append(character)
            index += 1
            val isEnd = character in ".!?。！？" || character == '\n'
            val isFollowed = if (index == text.length) false else text[index].isWhitespace() || character in "。！？"
            if (isEnd && (isFollowed || character == '\n')) {
                val sentence = current.toString().trim()
                if (sentence.isNotEmpty()) sentences.add(sentence)
                consumed = index
                current.clear()
            }
        }
        return Sentences(sentences, consumed)
    }
}
