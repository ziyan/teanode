package com.teanode.app.chats

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.Image
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.ui.components.rememberServerPicture
import com.teanode.kit.chats.LinkPreview
import com.teanode.kit.chats.LinkText

/**
 * The card of the first link in a message's words, under the message: the page's picture, its site, its title and a
 * line. It is asked for from the server, which fetched the page, never this phone.
 */
@Composable
fun LinkPreviewBelow(
    conversationId: String?,
    text: String,
    isMarkdown: Boolean,
    environment: ChatsEnvironment,
    isReady: Boolean = true,
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    val link = if (conversationId != null && isReady) LinkText.firstLink(text, isMarkdown) else null
    var preview by remember(link, conversationId) { mutableStateOf<LinkPreview?>(null) }
    LaunchedEffect(link, conversationId) {
        preview = if (link != null && conversationId != null) environment.linkPreview(link, conversationId) else null
    }
    val shown = preview ?: if (link != null && conversationId != null) environment.answeredLinkPreview(link, conversationId) else null
    Column(modifier, verticalArrangement = androidx.compose.foundation.layout.Arrangement.spacedBy(6.dp)) {
        content()
        if (shown != null && shown.hasCard) LinkPreviewCard(shown)
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
fun LinkPreviewCard(preview: LinkPreview) {
    val openLink = LocalLinkOpener.current
    val clipboard = LocalClipboardManager.current
    Surface(
        Modifier.widthIn(max = 320.dp).fillMaxWidth().clip(RoundedCornerShape(18.dp)).combinedClickable(
            onClick = { openLink(preview.cleanedUrl) }, onLongClick = { clipboard.setText(AnnotatedString(preview.cleanedUrl)) },
        ),
        shape = RoundedCornerShape(18.dp), color = MaterialTheme.colorScheme.surfaceContainerHigh, border = BorderStroke(0.5.dp, MaterialTheme.colorScheme.outlineVariant),
    ) {
        Column {
            preview.imageUrl?.let { imageUrl ->
                val picture = rememberServerPicture(imageUrl)
                val ratio = if ((preview.imageWidthPixels ?: 0) > 0 && (preview.imageHeightPixels ?: 0) > 0) preview.imageWidthPixels!!.toFloat() / preview.imageHeightPixels!! else 1.91f
                if (picture != null) {
                    Image(picture, contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxWidth().aspectRatio(ratio).heightIn(max = 200.dp))
                }
            }
            Column(Modifier.fillMaxWidth().padding(12.dp), verticalArrangement = androidx.compose.foundation.layout.Arrangement.spacedBy(2.dp), horizontalAlignment = Alignment.Start) {
                if (preview.siteName.isNotEmpty()) Text(preview.siteName, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
                if (preview.title.isNotEmpty()) Text(preview.title, style = MaterialTheme.typography.titleSmall, maxLines = 2, overflow = TextOverflow.Ellipsis)
                if (preview.description.isNotEmpty()) Text(preview.description, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}
