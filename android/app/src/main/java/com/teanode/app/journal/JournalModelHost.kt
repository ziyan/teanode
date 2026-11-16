package com.teanode.app.journal

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

/** The journal's timeline model for the account in use; it lives with the activity, so the day and period screens read the same today and week start. */
@Composable
fun rememberJournalModel(): JournalModel {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val owner = checkNotNull(context.findActivity()) { "the journal is drawn outside an activity" }
    val applicationContext = context.applicationContext
    val model: JournalModel = viewModel(
        viewModelStoreOwner = owner,
        key = "journal-${connection.accountKey}",
        factory = viewModelFactory { initializer { JournalModel(connection, applicationContext) } },
    )
    SideEffect {
        model.snackbar = snackbar
        model.describeError = { failure -> session.describe(failure) }
    }
    return model
}
