package com.teanode.app.notifications

import android.app.NotificationManager
import android.content.Context
import androidx.core.app.NotificationCompat
import com.teanode.app.R
import com.teanode.app.session.ErrorWords
import com.teanode.kit.Connection
import com.teanode.kit.notifications.NotificationCategories
import com.teanode.kit.notifications.NotificationDocuments
import com.teanode.kit.notifications.NotificationInfo
import com.teanode.kit.notifications.NotificationPress
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive

/**
 * NotificationActionRunner carries out what a button pressed on a notification means ([NotificationPress]), as the
 * account the notification came from. It is the Android side of `AppModel.perform(_ pressed:)`.
 */
class NotificationActionRunner(private val context: Context, private val push: PushSystem) {
    class Outcome(val isDone: Boolean)

    suspend fun perform(info: NotificationInfo, actionId: String, typedText: String?): Outcome {
        val kept = push.accountStore.accounts().accounts.map { account -> account.accountKey }
        return when (val press = NotificationPress.decide(info, actionId, typedText, kept)) {
            is NotificationPress.UnknownAccount -> {
                tell(if (press.doesNameAccount) context.getString(R.string.notifications_account_gone) else context.getString(R.string.notifications_account_unknown))
                Outcome(isDone = false)
            }
            is NotificationPress.Open -> Outcome(isDone = false)
            NotificationPress.None -> Outcome(isDone = false)
            else -> {
                val connection = push.connection(info.accountKey.ifEmpty { kept.single() }) ?: return Outcome(isDone = false)
                try {
                    carryOut(connection, press)
                    Outcome(isDone = true)
                } catch (error: Exception) {
                    // A reply to a person in a chat went to the chat, not to the agent.
                    val reason = ErrorWords(context).describe(error)
                    tell(
                        when {
                            info.notificationKind == "chat" -> context.getString(R.string.notifications_did_not_reach_chat, reason)
                            info.notificationKind == NotificationCategories.JOURNAL_EVENING_QUESTION -> context.getString(R.string.notifications_did_not_reach_journal, reason)
                            else -> context.getString(R.string.notifications_did_not_reach_agent, reason)
                        },
                    )
                    Outcome(isDone = false)
                }
            }
        }
    }

    private suspend fun carryOut(connection: Connection, press: NotificationPress) {
        when (press) {
            is NotificationPress.Resolve -> connection.execute(
                NotificationDocuments.RESOLVE_CONFIRMATION, JsonElement.serializer(),
                mapOf("runId" to JsonPrimitive(press.runId), "callId" to JsonPrimitive(press.callId), "approve" to JsonPrimitive(press.isApproved)),
            )
            is NotificationPress.AnswerQuestion -> connection.execute(
                NotificationDocuments.ANSWER_QUESTION, JsonElement.serializer(),
                mapOf("runId" to JsonPrimitive(press.runId), "callId" to JsonPrimitive(press.callId), "answer" to JsonPrimitive(press.answer)),
            )
            is NotificationPress.Say -> connection.execute(
                NotificationDocuments.ASK, JsonElement.serializer(),
                mapOf(
                    "conversationId" to (press.conversationId?.let { id -> JsonPrimitive(id) } ?: JsonNull),
                    "message" to JsonPrimitive(press.text),
                    "surface" to JsonPrimitive(TYPED_SURFACE),
                    "drawerId" to JsonPrimitive(push.drawerId()),
                    "attachmentIds" to JsonArray(emptyList()),
                ),
            )
            is NotificationPress.SetMood -> connection.execute(
                NotificationDocuments.SET_JOURNAL_MOOD, JsonElement.serializer(),
                mapOf("day" to JsonPrimitive(press.day), "moodLevel" to JsonPrimitive(press.moodLevel)),
            )
            else -> Unit
        }
    }

    /** Says something where a notification's button was: nobody is looking at the app. */
    private fun tell(text: String) {
        val manager = context.getSystemService(NotificationManager::class.java) ?: return
        val notification = NotificationCompat.Builder(context, NotificationChannels.PROBLEMS_CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(context.getString(R.string.app_name))
            .setContentText(text)
            .setStyle(NotificationCompat.BigTextStyle().bigText(text))
            .setAutoCancel(true)
            .build()
        manager.notify(text.hashCode(), notification)
    }

    private companion object {
        /** The surface the app names on its turns (internal/agent/surface.go); the chats part names the same. */
        const val TYPED_SURFACE = "ios"
    }
}
