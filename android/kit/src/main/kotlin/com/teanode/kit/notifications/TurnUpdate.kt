package com.teanode.kit.notifications

import com.teanode.kit.Connection
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonPrimitive

/**
 * TurnUpdate is where a turn the person left running has come to, as the server tells the phone while the app is out of
 * front: the Android side of the iPhone app's Live Activity update. The phone asks the push relay for a registration of
 * push type liveactivity (from the same Firebase token as its other two) and gives it to the server with the run; the
 * server then sends a data message of push kind `turn` at each change. Like a Live Activity's, it carries one word and
 * nothing the turn says, since Google can read it: the app writes the notification from the word and from the agent's
 * name it kept when it registered.
 */
data class TurnUpdate(
    /** working, waiting, done or failed. */
    val activityState: String,
    /** Whether the turn is over, so the notification no longer stays. */
    val isActivityEnded: Boolean,
    /** When the turn came to this state, on the server's clock, in seconds since 1970; it orders the updates. */
    val activityChangedAtEpochSeconds: Long,
) {
    companion object {
        const val DATA_ACTIVITY_STATE = "activityState"
        const val DATA_IS_ACTIVITY_ENDED = "isActivityEnded"
        const val DATA_ACTIVITY_CHANGED_AT = "activityChangedAt"

        /** The words of a state the server may send; anything else is not shown. */
        val activityStates: Set<String> = setOf("working", "waiting", "done", "failed")

        /** Reads the data of a `turn` message; null when it is not one a notification can be made from. */
        fun parse(data: Map<String, String>): TurnUpdate? {
            val activityState = data[DATA_ACTIVITY_STATE]?.takeIf { state -> state in activityStates } ?: return null
            return TurnUpdate(
                activityState = activityState,
                isActivityEnded = data[DATA_IS_ACTIVITY_ENDED] == "true",
                activityChangedAtEpochSeconds = data[DATA_ACTIVITY_CHANGED_AT]?.toLongOrNull() ?: 0L,
            )
        }
    }

    /**
     * Whether this update is to be shown after the one last shown, which came at [lastChangedAtEpochSeconds] (zero
     * when none did): Firebase may deliver two in the other order, and the later state must not be overwritten.
     */
    fun isNewerThan(lastChangedAtEpochSeconds: Long): Boolean = activityChangedAtEpochSeconds >= lastChangedAtEpochSeconds
}

object TurnDocuments {
    /** The same document as the iPhone app's. */
    const val REGISTER_LIVE_ACTIVITY = """
        mutation (${'$'}runId: String!, ${'$'}pushRegistration: String!) { RegisterAgentLiveActivity(runId: ${'$'}runId, pushRegistration: ${'$'}pushRegistration) }
        """
}

@Serializable
private class RegisterLiveActivityAnswer(@SerialName("RegisterAgentLiveActivity") val isRegistered: Boolean)

/** Has the server follow a turn and tell this phone, through [pushRegistration], where it comes to. */
suspend fun Connection.registerLiveActivity(runId: String, pushRegistration: String) {
    execute(
        TurnDocuments.REGISTER_LIVE_ACTIVITY, RegisterLiveActivityAnswer.serializer(),
        mapOf("runId" to JsonPrimitive(runId), "pushRegistration" to JsonPrimitive(pushRegistration)),
    )
}
