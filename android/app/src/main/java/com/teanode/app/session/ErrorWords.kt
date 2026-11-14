package com.teanode.app.session

import android.content.Context
import com.teanode.app.R
import com.teanode.kit.TeaNodeError
import com.teanode.kit.asReasonSentence
import com.teanode.kit.refusalReason
import kotlinx.coroutines.CancellationException
import java.io.IOException

/** ErrorWords says what went wrong in the person's language, in words that tell them what to do next. */
class ErrorWords(private val context: Context) {
    /** What to show for an error from the server. */
    fun describe(error: Throwable): String = when (error) {
        is TeaNodeError.SignedOut -> context.getString(R.string.error_signed_out)
        is TeaNodeError.ConnectCodeSpent -> context.getString(R.string.error_connect_code_spent)
        is TeaNodeError.UnreadableAnswer -> context.getString(R.string.error_unreadable_answer)
        is TeaNodeError.RecordingDeleted -> context.getString(R.string.error_recording_deleted)
        is TeaNodeError.Refused -> error.reason.asReasonSentence()
        is IOException -> context.getString(R.string.error_server_unreachable)
        else -> (error.message ?: error.toString()).asReasonSentence()
    }

    /** Why a read failed, in words that say what to do next: the server's own reason when it answered and refused, else the connection advice. */
    fun loadProblem(error: Throwable): String = error.refusalReason(orSay = context.getString(R.string.common_check_connection)).asReasonSentence()

    companion object {
        /**
         * Whether an error is a request called off: leaving a screen or pulling to refresh again cancels what was being
         * read. Nothing went wrong, so nothing is shown.
         */
        fun isCancellation(error: Throwable): Boolean = error is CancellationException
    }
}
