package com.teanode.app.calls

import androidx.compose.runtime.Composable
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.setValue
import kotlinx.coroutines.delay

/** What time the call screens take it to be; null is the phone's own clock. A screenshot test fixes it so a timer reads the same every time. */
val LocalCallClockMS = compositionLocalOf<Long?> { null }

/** The time now in milliseconds, ticking every second, or the fixed time of [LocalCallClockMS]. */
@Composable
fun rememberCallNowMS(): Long {
    val fixed = LocalCallClockMS.current
    if (fixed != null) return fixed
    var nowMS by remember { mutableLongStateOf(System.currentTimeMillis()) }
    LaunchedEffect(Unit) {
        while (true) {
            delay(1_000)
            nowMS = System.currentTimeMillis()
        }
    }
    return nowMS
}
