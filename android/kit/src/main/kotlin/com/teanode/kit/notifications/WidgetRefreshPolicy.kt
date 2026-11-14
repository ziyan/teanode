package com.teanode.kit.notifications

/**
 * WidgetRefreshPolicy says when the widgets are drawn again because something they show changed (a message arrived,
 * the app was left after the person changed an event, a sign-in changed): the Android side of the iPhone app's
 * `WidgetCenter.shared.reloadAllTimelines()`. Each drawing reads the server, so a burst of pushes is one reading, not
 * one per push. A change of account is never held back, since the widgets would show the other account's.
 */
class WidgetRefreshPolicy(private val minimumIntervalMS: Long = 15_000L) {
    private var lastRefreshEpochMS: Long = 0L

    /** Whether to draw the widgets now; says yes at most once per interval, unless [isForced]. */
    @Synchronized
    fun shouldRefresh(nowEpochMS: Long, isForced: Boolean = false): Boolean {
        if (!isForced && nowEpochMS - lastRefreshEpochMS < minimumIntervalMS && nowEpochMS >= lastRefreshEpochMS) return false
        lastRefreshEpochMS = nowEpochMS
        return true
    }
}
