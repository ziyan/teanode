package com.teanode.app.ui.components

import android.content.Context
import android.text.format.DateUtils
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalContext

/**
 * RelativeTime writes when something happened as a person says it: "5 min. ago", "Yesterday", "Oct 3". Android words it
 * in the phone's language. Pass [nowEpochMS] to get the same text every time, as the screenshot tests do.
 */
object RelativeTime {
    fun text(@Suppress("UNUSED_PARAMETER") context: Context, thenEpochMS: Long, nowEpochMS: Long = System.currentTimeMillis()): String =
        DateUtils.getRelativeTimeSpanString(thenEpochMS, nowEpochMS, DateUtils.MINUTE_IN_MILLIS, DateUtils.FORMAT_ABBREV_RELATIVE).toString()
}

@Composable
fun relativeTimeText(thenEpochMS: Long, nowEpochMS: Long = System.currentTimeMillis()): String =
    RelativeTime.text(LocalContext.current, thenEpochMS, nowEpochMS)
