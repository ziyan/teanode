package com.teanode.app.calendar

import android.content.Context
import android.content.ContextWrapper
import androidx.activity.ComponentActivity
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.LocalSnackbar

private fun Context.findActivity(): ComponentActivity? {
    var current: Context? = this
    while (current is ContextWrapper) {
        if (current is ComponentActivity) return current
        current = current.baseContext
    }
    return null
}

/**
 * The calendar's model for the account in use. It lives with the activity, so the month view, a day opened from it, the
 * event editor and the screens of calendars and sharing all read the same months and the same change in flight.
 */
@Composable
fun rememberCalendarModel(): CalendarModel {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val owner = checkNotNull(context.findActivity()) { "the calendar is drawn outside an activity" }
    val applicationContext = context.applicationContext
    val model: CalendarModel = viewModel(
        viewModelStoreOwner = owner,
        key = "calendar-${connection.accountKey}",
        factory = viewModelFactory {
            initializer {
                CalendarModel(connection, applicationContext, applicationContext.getSharedPreferences("calendar", Context.MODE_PRIVATE))
            }
        },
    )
    SideEffect {
        model.attach(snackbar) { failure -> session.describe(failure) }
    }
    return model
}
