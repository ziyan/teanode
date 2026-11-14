package com.teanode.kit

/**
 * A reason the server or the system gave, written as the sentence a screen shows: trimmed, with a capital first letter
 * and a full stop when it ends without one. They come as phrases such as "photo storage is unavailable". A text that is
 * already a sentence is returned as it is.
 */
fun String.asReasonSentence(): String {
    val trimmed = trim()
    if (trimmed.isEmpty()) return trimmed
    val capitalized = trimmed.replaceFirstChar { first -> first.uppercase() }
    return if (".!?。！？…".contains(capitalized.last())) capitalized else "$capitalized."
}
