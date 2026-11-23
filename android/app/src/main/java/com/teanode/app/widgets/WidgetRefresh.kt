package com.teanode.app.widgets

import android.content.Context
import androidx.glance.appwidget.updateAll
import com.teanode.kit.notifications.WidgetRefreshPolicy
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch

/**
 * WidgetRefresh draws the widgets again when what they show may have changed, as the iPhone app reloads their
 * timelines: a push arrived (a message, a question or approval, an event or mail), the app was left (after the person
 * changed an event, read mail or answered the agent), or the account in use changed. Between those the system draws
 * them every half hour. A widget nobody put on a screen is not drawn at all.
 */
object WidgetRefresh {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val policy = WidgetRefreshPolicy()

    /** Draws every widget again, at most once per few seconds; [isForced] for a change of account, which is never held back. */
    fun all(context: Context, isForced: Boolean = false) {
        if (!policy.shouldRefresh(System.currentTimeMillis(), isForced)) return
        val applicationContext = context.applicationContext
        scope.launch {
            runCatching { AgentWidget().updateAll(applicationContext) }
            runCatching { WaitingWidget().updateAll(applicationContext) }
            runCatching { NextWidget().updateAll(applicationContext) }
        }
    }
}
