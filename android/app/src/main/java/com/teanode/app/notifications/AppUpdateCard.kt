package com.teanode.app.notifications

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.requiredSize
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material3.IconButton
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
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
import com.teanode.app.feed.FEED_CARD_CORNER
import com.teanode.app.feed.feedCardColor
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

/** The newer app [AppUpdateHost] reads, for the feed's own card; null when there is none or it was put off. */
@Composable
fun rememberOfferedAppUpdate(): Pair<AppUpdate?, () -> Unit> {
    val context = LocalContext.current
    val connection = LocalConnectionOrNull.current
    val model = (context.applicationContext as? TeaNodeApplication)?.push?.appUpdates ?: return null to {}
    LaunchedEffect(connection?.accountKey) { connection?.let { signedIn -> model.refresh(signedIn) } }
    val update by model.updateToOffer.collectAsState(initial = null)
    return update to { model.dismiss() }
}

/**
 * FeedAppUpdateCard offers an update at the top of the feed, above every card, until the person installs it or says
 * not now (ios FeedAppUpdateCard). It is drawn apart from the agent's cards: the app's own icon, a wash of the brand
 * color and a border in it, and one wide button, because it is the app speaking, not the agent.
 */
@Composable
fun FeedAppUpdateCard(update: AppUpdate, onInstall: () -> Unit, onDismiss: () -> Unit, modifier: Modifier = Modifier) {
    val brand = MaterialTheme.colorScheme.primary
    val shape = RoundedCornerShape(FEED_CARD_CORNER)
    Box(
        modifier.fillMaxWidth().clip(shape)
            .background(feedCardColor())
            .background(Brush.linearGradient(listOf(brand.copy(alpha = 0.22f), brand.copy(alpha = 0.04f))))
            .border(1.dp, brand.copy(alpha = 0.5f), shape),
    ) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            // The header leaves room for the close button in the corner; the install button below runs the card's width.
            Row(Modifier.padding(end = 28.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                // The logo, the leaf alone as on the iPhone's card: the launcher's foreground layer draws it in the middle
                // 66 of its 108 units, so it is drawn larger than the box and the box keeps the leaf's size.
                Box(Modifier.size(56.dp), contentAlignment = Alignment.Center) {
                    Image(painterResource(R.drawable.ic_launcher_foreground), contentDescription = null, modifier = Modifier.requiredSize(92.dp))
                }
                Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(stringResource(R.string.notifications_update_new_version), style = MaterialTheme.typography.labelLarge, color = brand)
                    Text(stringResource(R.string.notifications_update_title, update.versionText), style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
                    Text(stringResource(R.string.notifications_update_ready_short), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            Button(onClick = onInstall, modifier = Modifier.fillMaxWidth()) {
                Icon(Icons.Outlined.Download, contentDescription = null, modifier = Modifier.size(18.dp))
                Spacer(Modifier.width(8.dp))
                Text(stringResource(R.string.notifications_update_install))
            }
        }
        IconButton(onClick = onDismiss, modifier = Modifier.align(Alignment.TopEnd)) {
            Icon(Icons.Outlined.Close, contentDescription = stringResource(R.string.notifications_update_not_now), tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(20.dp))
        }
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
