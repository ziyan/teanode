package com.teanode.app.notifications

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.TeaNodeApplication
import com.teanode.app.session.LocalConnectionOrNull
import com.teanode.app.ui.components.openWebPage
import com.teanode.kit.notifications.AppUpdate

/**
 * AppUpdateHost reads whether a newer app is out when it comes into view, and offers it with [AppUpdateCard] until the
 * person installs it or says not now.
 */
@Composable
fun AppUpdateHost(modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val connection = LocalConnectionOrNull.current
    val model = (context.applicationContext as TeaNodeApplication).push.appUpdates
    LaunchedEffect(connection?.accountKey) { connection?.let { signedIn -> model.refresh(signedIn) } }
    val update by model.updateToOffer.collectAsState(initial = null)
    update?.let { offered ->
        AppUpdateCard(
            update = offered,
            onInstall = { openWebPage(context, offered.installUrl) },
            onDismiss = { model.dismiss() },
            modifier = modifier,
        )
    }
}

/** AppUpdateCard offers an update at the top of the chats, on the cards' surface, until the person installs it or says not now. */
@Composable
fun AppUpdateCard(update: AppUpdate, onInstall: () -> Unit, onDismiss: () -> Unit, modifier: Modifier = Modifier) {
    Card(
        modifier = modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainer),
    ) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Icon(Icons.Outlined.Download, contentDescription = null, modifier = Modifier.size(18.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(stringResource(R.string.notifications_update_available), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Text(stringResource(R.string.notifications_update_ready, update.versionText), style = MaterialTheme.typography.bodyLarge)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(onClick = onInstall) { Text(stringResource(R.string.notifications_update_install)) }
                OutlinedButton(onClick = onDismiss) { Text(stringResource(R.string.notifications_update_not_now)) }
            }
        }
    }
}
