package com.teanode.kit.coding

/**
 * CodingDictation holds the rules for dictating into a coding session's field, as the iPhone's composer has them:
 * the words already in the field stay, but listening ends when sending starts, when the session starts asking a
 * question (a typed line would answer it by accident), and when the app is not in front.
 */
object CodingDictation {
    /** The place the server words its transcription prompt for: files, functions and commands. */
    const val PLACE = "coding"

    /** Whether the microphone is offered: the server offers voice, and the field is not off for a question. */
    fun isMicrophoneShown(sampleRate: Int?, isDisabled: Boolean): Boolean = sampleRate != null && !isDisabled

    /** Whether listening must end now. */
    fun shouldStop(isSending: Boolean, isDisabled: Boolean, isInFront: Boolean): Boolean = isSending || isDisabled || !isInFront
}
