package com.teanode.app.calls

import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.res.stringResource
import com.teanode.app.R

/** The question a call asked for by an icon shortcut, a widget or a link has to be answered with before the microphone opens. */
@Composable
fun CallOfferDialog() {
    val isOffered by OpeningRequests.isCallOffered.collectAsState()
    if (!isOffered) return
    AlertDialog(
        onDismissRequest = { OpeningRequests.declineOffer() },
        title = { Text(stringResource(R.string.calls_call_agent)) },
        text = { Text(stringResource(R.string.calls_call_offer_body)) },
        confirmButton = { TextButton(onClick = { OpeningRequests.acceptOffer() }) { Text(stringResource(R.string.calls_call_agent)) } },
        dismissButton = { TextButton(onClick = { OpeningRequests.declineOffer() }) { Text(stringResource(android.R.string.cancel)) } },
    )
}
