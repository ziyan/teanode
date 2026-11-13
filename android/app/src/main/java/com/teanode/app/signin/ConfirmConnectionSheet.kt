package com.teanode.app.signin

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.PhonelinkSetup
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.PendingConnection

/**
 * ConfirmConnectionSheet asks whether this is the right server and the right account before the phone keeps the
 * sign-in: a link names the server it connects to, so one sent by somebody else could name theirs.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ConfirmConnectionSheet(pending: PendingConnection, onAnswer: (isConfirmed: Boolean) -> Unit) {
    ModalBottomSheet(
        onDismissRequest = { onAnswer(false) },
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
    ) {
        Column(
            Modifier.fillMaxWidth().padding(horizontal = 24.dp).padding(bottom = 24.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Icon(Icons.Outlined.PhonelinkSetup, contentDescription = null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(40.dp))
            Text(
                stringResource(R.string.signin_confirm_title, pending.account.server.host),
                style = MaterialTheme.typography.headlineSmall, textAlign = TextAlign.Center,
            )
            Text(
                stringResource(R.string.signin_confirm_body, pending.account.username),
                color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center,
            )
            Button(onClick = { onAnswer(true) }, modifier = Modifier.fillMaxWidth().height(48.dp)) { Text(stringResource(R.string.signin_connect)) }
            OutlinedButton(onClick = { onAnswer(false) }, modifier = Modifier.fillMaxWidth().height(48.dp)) { Text(stringResource(R.string.signin_not_this_one)) }
        }
    }
}
