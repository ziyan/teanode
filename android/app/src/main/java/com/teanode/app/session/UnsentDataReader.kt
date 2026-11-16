package com.teanode.app.session

import android.content.Context
import com.teanode.app.recordings.RecordingJournal
import com.teanode.kit.Account
import com.teanode.kit.location.LocationOutbox
import com.teanode.kit.session.UnsentData
import java.io.File

/** Reads what is waiting on the phone for an account: recordings not fully uploaded, location reports not sent, a mail not sent. */
object UnsentDataReader {
    fun read(context: Context, account: Account): UnsentData {
        val accountKey = account.accountKey
        val recordingCount = runCatching { UnsentData.unfinishedRecordingCount(RecordingJournal(context).recordings(accountKey)) }.getOrDefault(0)
        val locationCount = runCatching {
            val outbox = LocationOutbox.decoded(File(context.filesDir, "location/" + LocationOutbox.fileName(accountKey)).readText())
            if (outbox.accountKey == accountKey) outbox.points.size + outbox.visits.size + outbox.activities.size else 0
        }.getOrDefault(0)
        val hasMail = context.getSharedPreferences("mail", Context.MODE_PRIVATE).getString("mail-pending-send.${account.username}", null).orEmpty().isNotEmpty()
        return UnsentData(recordingCount, locationCount, hasMail)
    }
}
