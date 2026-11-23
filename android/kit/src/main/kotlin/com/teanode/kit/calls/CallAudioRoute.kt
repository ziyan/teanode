package com.teanode.kit.calls

/**
 * ChatCallAudioRoute is where a call's sound goes now: the phone's earpiece, its speaker, headphones on a wire, a
 * Bluetooth headset, or something else the phone offers, as the call's audio button shows it.
 */
sealed class ChatCallAudioRoute {
    data object Phone : ChatCallAudioRoute()

    data object Speaker : ChatCallAudioRoute()

    data object Headphones : ChatCallAudioRoute()

    data class Bluetooth(val deviceName: String) : ChatCallAudioRoute()

    data class Other(val deviceName: String) : ChatCallAudioRoute()

    /** Whether the sound is private to the person: a call is moved to the speaker by itself only when it is not. */
    val isPrivate: Boolean get() = this is Headphones || this is Bluetooth

    companion object {
        /** The order the routes are offered in. */
        fun order(route: ChatCallAudioRoute): Int = when (route) {
            Phone -> 0
            Speaker -> 1
            Headphones -> 2
            is Bluetooth -> 3
            is Other -> 4
        }

        /**
         * Whether a call that starts with the camera on goes to the speaker: a picture is looked at, not held to the
         * ear, unless something private is connected.
         */
        fun shouldMoveToSpeaker(current: ChatCallAudioRoute): Boolean = current == Phone
    }
}
