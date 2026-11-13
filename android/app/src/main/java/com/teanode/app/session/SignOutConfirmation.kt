package com.teanode.app.session

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import com.teanode.app.R
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.kit.Account
import com.teanode.kit.session.UnsentData
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * The question before signing out. Signing out deletes what the phone kept for the account, so anything the server does
 * not have yet is named, and the sign-out then takes the words "Sign out anyway".
 */
@Composable
fun SignOutConfirmation(account: Account?, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    val context = LocalContext.current
    var unsent by remember(account) { mutableStateOf<UnsentData?>(null) }
    LaunchedEffect(account) { unsent = if (account == null) UnsentData() else withContext(Dispatchers.IO) { UnsentDataReader.read(context, account) } }
    // The count is read in a moment; the question waits for it so the words are right the first time they show.
    val found = unsent ?: return
    SignOutConfirmationContent(found, onConfirm, onDismiss)
}

@Composable
fun SignOutConfirmationContent(unsent: UnsentData, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    if (unsent.isEmpty) {
        ConfirmationDialog(
            title = stringResource(R.string.settings_sign_out_title), confirmLabel = stringResource(R.string.settings_sign_out), isDestructive = true,
            text = stringResource(R.string.settings_sign_out_detail), onConfirm = onConfirm, onDismiss = onDismiss,
        )
        return
    }
    val lines = buildList {
        if (unsent.unfinishedRecordingCount > 0) add(pluralStringResource(R.plurals.settings_unsent_recordings, unsent.unfinishedRecordingCount, unsent.unfinishedRecordingCount))
        if (unsent.unsentLocationReportCount > 0) add(pluralStringResource(R.plurals.settings_unsent_locations, unsent.unsentLocationReportCount, unsent.unsentLocationReportCount))
        if (unsent.hasUnsentMail) add(stringResource(R.string.settings_unsent_mail))
    }
    ConfirmationDialog(
        title = stringResource(R.string.settings_sign_out_title), confirmLabel = stringResource(R.string.settings_sign_out_anyway), isDestructive = true,
        text = lines.joinToString("\n") + "\n\n" + stringResource(R.string.settings_unsent_deleted), onConfirm = onConfirm, onDismiss = onDismiss,
    )
}
