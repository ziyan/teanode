package com.teanode.app.chats

import android.annotation.SuppressLint
import android.content.ClipData
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.gestures.detectTransformGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.InsertDriveFile
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material.icons.outlined.AttachFile
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.GraphicEq
import androidx.compose.material.icons.outlined.Image
import androidx.compose.material.icons.outlined.OpenInNew
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.core.content.FileProvider
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection
import androidx.compose.runtime.collectAsState
import com.teanode.app.session.LocalConnectionOrNull
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.openWebPage
import com.teanode.app.ui.components.rememberServerPicture
import com.teanode.kit.Connection
import com.teanode.kit.chats.AgentAttachment
import com.teanode.kit.chats.AgentFile
import com.teanode.kit.chats.download
import com.teanode.kit.chats.picturePath
import com.teanode.kit.chats.shareAddress
import kotlinx.coroutines.launch
import java.io.File

/** The conversation an answer belongs to, through which the pictures it shows are read. */
val LocalPictureConversationId = staticCompositionLocalOf<String?> { null }

/** Where a link tapped in a message goes: the in-app browser for a web page, the system for the rest (LinkOpening). */
val LocalLinkOpener = staticCompositionLocalOf<(String) -> Unit> { { } }

/** The text a size in bytes is shown as: 2.0 MB. */
fun byteCountText(size: Int): String = when {
    size >= 1_000_000 -> "%.1f MB".format(size / 1_000_000.0)
    size >= 1_000 -> "%.0f KB".format(size / 1_000.0)
    else -> "$size B"
}

/** The route of the recordings part that saves a chat's audio or video attachment as a recording. */
fun recordingImportRoute(attachmentId: String, name: String, contentType: String, size: Int): String =
    Routes.child(AppSection.Recordings, "import/${Uri.encode(attachmentId)}?name=${Uri.encode(name)}&type=${Uri.encode(contentType)}&size=$size")

/** Whether a file can be saved as a recording, and the person has recordings. */
@Composable
fun canBeSavedAsRecording(contentType: String): Boolean {
    val facts by LocalAppSession.current.facts.collectAsState()
    return (contentType.startsWith("audio/") || contentType.startsWith("video/")) && facts.isAvailable(AppSection.Recordings)
}

/** Writes a file the server keeps into the cache and hands it to another app, which is what Share does. */
suspend fun shareServerFile(context: Context, connection: Connection, path: String, name: String, contentType: String) {
    val bytes = connection.download(path)
    val file = File(ChatFilesCache.directory(context), ChatFilesCache.safeName(name))
    file.writeBytes(bytes)
    val address = FileProvider.getUriForFile(context, "${context.packageName}.chatfiles", file)
    val intent = Intent(Intent.ACTION_SEND).apply {
        type = contentType.ifEmpty { "application/octet-stream" }
        putExtra(Intent.EXTRA_STREAM, address)
        clipData = ClipData.newRawUri(name, address)
        addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
    }
    context.startActivity(Intent.createChooser(intent, null).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
}

/** Opens a file the server keeps in whatever shows it: the address the server signs for a few hours opens without a sign-in. */
suspend fun openServerFile(context: Context, connection: Connection, attachmentId: String) {
    openWebPage(context, connection.shareAddress(attachmentId))
}

/**
 * RemotePicture is a picture the server keeps, read with the app's sign-in; tapping it opens it full screen, to zoom
 * and share, unless the card around it says what a tap does instead.
 */
@Composable
fun RemotePicture(
    path: String,
    name: String,
    modifier: Modifier = Modifier,
    maxHeightDp: Int = 360,
    onOpen: (() -> Unit)? = null,
    originalFilePath: String? = null,
) {
    val picture = rememberServerPicture(path)
    var isMissing by remember(path) { mutableStateOf(false) }
    var isOpen by remember { mutableStateOf(false) }
    LaunchedEffect(path, picture) {
        if (picture == null) {
            kotlinx.coroutines.delay(8_000)
            isMissing = true
        }
    }
    Box(modifier) {
        when {
            picture != null -> Image(
                picture, contentDescription = name, contentScale = ContentScale.Fit,
                modifier = Modifier.heightIn(max = maxHeightDp.dp).clip(RoundedCornerShape(12.dp)).clickable { if (onOpen != null) onOpen() else isOpen = true },
            )
            isMissing -> Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Icon(Icons.Outlined.Image, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(name, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
            }
            else -> Box(Modifier.size(width = 120.dp, height = 90.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator(Modifier.size(24.dp)) }
        }
    }
    if (isOpen && picture != null) PictureLightbox(path, name, originalFilePath ?: path, onClose = { isOpen = false })
}

/** PictureLightbox shows a picture full screen on black, to pinch and drag, with a way to share it and a way back. */
@Composable
fun PictureLightbox(path: String, name: String, sharePath: String, onClose: () -> Unit) {
    val picture = rememberServerPicture(path)
    var scale by remember { mutableFloatStateOf(1f) }
    var offsetX by remember { mutableFloatStateOf(0f) }
    var offsetY by remember { mutableFloatStateOf(0f) }
    val context = LocalContext.current
    val connection = LocalConnectionOrNull.current
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    Dialog(onDismissRequest = onClose, properties = DialogProperties(usePlatformDefaultWidth = false)) {
        Box(Modifier.fillMaxSize().background(Color.Black)) {
            if (picture != null) {
                Image(
                    picture, contentDescription = name, contentScale = ContentScale.Fit,
                    modifier = Modifier.fillMaxSize()
                        .pointerInput(Unit) {
                            detectTapGestures(onDoubleTap = {
                                scale = if (scale > 1f) 1f else 3f
                                offsetX = 0f
                                offsetY = 0f
                            })
                        }
                        .pointerInput(Unit) {
                            detectTransformGestures { _, pan, zoom, _ ->
                                scale = (scale * zoom).coerceIn(1f, 6f)
                                if (scale > 1f) {
                                    offsetX += pan.x
                                    offsetY += pan.y
                                } else {
                                    offsetX = 0f
                                    offsetY = 0f
                                }
                            }
                        }
                        .graphicsLayer(scaleX = scale, scaleY = scale, translationX = offsetX, translationY = offsetY),
                )
            }
            Row(Modifier.align(Alignment.TopEnd).padding(8.dp)) {
                if (connection != null) {
                    IconButton(onClick = {
                        scope.launch {
                            runCatching { shareServerFile(context, connection, sharePath, name.ifEmpty { "picture.jpg" }, "image/jpeg") }
                                .onFailure { failure -> snackbar.showError(failure) }
                        }
                    }) { Icon(Icons.Outlined.Share, contentDescription = stringResource(R.string.chats_share), tint = Color.White) }
                }
                IconButton(onClick = onClose) { Icon(Icons.Outlined.Close, contentDescription = stringResource(R.string.chats_close), tint = Color.White) }
            }
        }
    }
}

/** A picture on a line of its own in an answer, read through the server, which fetches only addresses a tool showed the agent. */
@Composable
fun AnswerPicture(alt: String, address: String, link: String?, conversationId: String? = null) {
    val picturedConversationId = conversationId ?: LocalPictureConversationId.current
    val openLink = LocalLinkOpener.current
    if (picturedConversationId != null && address.lowercase().startsWith("http")) {
        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            RemotePicture(picturePath(picturedConversationId, address), alt.ifEmpty { "picture" }, maxHeightDp = 240)
            if (link != null) {
                Text(alt.ifEmpty { link }, color = MaterialTheme.colorScheme.primary, style = MaterialTheme.typography.bodySmall, modifier = Modifier.clickable { openLink(link) })
            }
        }
    } else {
        val target = link ?: address
        Text(alt.ifEmpty { address }, color = MaterialTheme.colorScheme.primary, modifier = Modifier.clickable { openLink(target) })
    }
}

/** What a file the person attached shows: a picture, or a chip with its name and size that opens it. */
@Composable
fun AttachmentView(attachment: AgentAttachment, modifier: Modifier = Modifier) {
    val connection = LocalConnectionOrNull.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    val path = "/api/v1/agent/attachments/${attachment.id}"
    if (attachment.isRemoved == true) {
        // The message is kept and its file went with the account.
        Chip(Icons.Outlined.AttachFile, stringResource(R.string.chats_file_removed), modifier)
    } else if (attachment.contentType.startsWith("image/")) {
        RemotePicture(path, attachment.name, modifier.widthIn(max = 200.dp), maxHeightDp = 160)
    } else {
        val navigator = LocalAppNavigator.current
        val canSave = canBeSavedAsRecording(attachment.contentType)
        var isMenuOpen by remember { mutableStateOf(false) }
        val open = { if (connection != null) scope.launch { runCatching { openServerFile(context, connection, attachment.id) }.onFailure { failure -> snackbar.showError(failure) } } }
        Box {
            Chip(Icons.Outlined.AttachFile, "${attachment.name} · ${byteCountText(attachment.size)}", modifier.clickable { if (canSave) isMenuOpen = true else open() })
            DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                DropdownMenuItem(text = { Text(stringResource(R.string.chats_open)) }, leadingIcon = { Icon(Icons.Outlined.OpenInNew, null) }, onClick = { isMenuOpen = false; open() })
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.chats_save_as_recording)) }, leadingIcon = { Icon(Icons.Outlined.GraphicEq, null) },
                    onClick = { isMenuOpen = false; navigator.navigate(recordingImportRoute(attachment.id, attachment.name, attachment.contentType, attachment.size)) },
                )
            }
        }
    }
}

@Composable
private fun Chip(icon: androidx.compose.ui.graphics.vector.ImageVector, text: String, modifier: Modifier = Modifier) {
    Row(
        modifier.clip(CircleShape).background(MaterialTheme.colorScheme.onSurface.copy(alpha = 0.08f)).padding(horizontal = 10.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Icon(icon, contentDescription = null, modifier = Modifier.size(16.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(text, style = MaterialTheme.typography.bodySmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/**
 * FileCard is a file the agent handed over: a picture to look at, or any other file to open. The card is the file's
 * name and size, the file and its caption; a tap opens it, and what else it offers is in the menu at its corner.
 */
@Composable
fun FileCard(file: AgentFile, modifier: Modifier = Modifier) {
    val connection = LocalConnection.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    var isMenuOpen by remember { mutableStateOf(false) }
    val clipboard = LocalClipboardManager.current
    val navigator = LocalAppNavigator.current
    val canSaveAsRecording = canBeSavedAsRecording(file.contentType)
    CardSurface(modifier = modifier.widthIn(max = 420.dp)) {
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Row(
                    Modifier.weight(1f).clickable(enabled = !file.isImage) { scope.launch { runCatching { openServerFile(context, connection, file.attachmentId) }.onFailure { failure -> snackbar.showError(failure) } } },
                    verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    Icon(Icons.Outlined.AttachFile, contentDescription = null, modifier = Modifier.size(16.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                    Text(file.name, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                    if (file.size > 0) Text("· ${byteCountText(file.size)}", style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    if (!file.isImage) Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null, tint = MaterialTheme.colorScheme.outline)
                }
                Box {
                    IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.OpenInNew, contentDescription = stringResource(R.string.chats_file_options)) }
                    DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.chats_open)) }, leadingIcon = { Icon(Icons.Outlined.OpenInNew, null) },
                            onClick = {
                                isMenuOpen = false
                                scope.launch { runCatching { openServerFile(context, connection, file.attachmentId) }.onFailure { failure -> snackbar.showError(failure) } }
                            },
                        )
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.chats_share)) }, leadingIcon = { Icon(Icons.Outlined.Share, null) },
                            onClick = {
                                isMenuOpen = false
                                scope.launch { runCatching { shareServerFile(context, connection, file.path, file.name, file.contentType) }.onFailure { failure -> snackbar.showError(failure) } }
                            },
                        )
                        if (canSaveAsRecording) {
                            DropdownMenuItem(
                                text = { Text(stringResource(R.string.chats_save_as_recording)) }, leadingIcon = { Icon(Icons.Outlined.GraphicEq, null) },
                                onClick = {
                                    isMenuOpen = false
                                    navigator.navigate(recordingImportRoute(file.attachmentId, file.name, file.contentType, file.size))
                                },
                            )
                        }
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.chats_copy_link)) }, leadingIcon = { Icon(Icons.Outlined.ContentCopy, null) },
                            onClick = {
                                isMenuOpen = false
                                scope.launch {
                                    runCatching { connection.shareAddress(file.attachmentId) }.onSuccess { address -> clipboard.setText(AnnotatedString(address)) }
                                        .onFailure { failure -> snackbar.showError(failure) }
                                }
                            },
                        )
                    }
                }
            }
            if (file.isImage) RemotePicture(file.path, file.name, maxHeightDp = 320)
            if (file.caption.isNotEmpty()) Text(file.caption, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

/**
 * ArtifactCard is a page or drawing or document the agent made: a document draws inline, a page runs in a web view
 * that has no sign-in, given an address that opens without one and the theme to draw in.
 */
@SuppressLint("SetJavaScriptEnabled")
@Composable
fun ArtifactCard(file: AgentFile, modifier: Modifier = Modifier) {
    val connection = LocalConnection.current
    val isDark = MaterialTheme.colorScheme.background.let { color -> (color.red + color.green + color.blue) / 3 < 0.5f }
    var address by remember(file.attachmentId, isDark) { mutableStateOf<String?>(null) }
    var markdown by remember(file.attachmentId) { mutableStateOf<String?>(null) }
    var isFailed by remember(file.attachmentId) { mutableStateOf(false) }
    LaunchedEffect(file.attachmentId, isDark) {
        try {
            if (file.artifactKind == "markdown") {
                markdown = connection.download(file.path).decodeToString()
            } else {
                val shared = connection.shareAddress(file.attachmentId)
                address = shared.substringBefore('#') + "#theme=${if (isDark) "dark" else "light"}"
            }
        } catch (_: Exception) {
            isFailed = true
        }
    }
    CardSurface(modifier = modifier.fillMaxWidth()) {
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Icon(Icons.Outlined.AutoAwesome, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(file.name.ifEmpty { stringResource(R.string.chats_untitled) }, style = MaterialTheme.typography.titleSmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            Box(Modifier.fillMaxWidth().height(320.dp).clip(RoundedCornerShape(12.dp))) {
                val markdownText = markdown
                val pageAddress = address
                when {
                    markdownText != null -> Box(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) { MarkdownView(markdownText, Modifier.padding(4.dp)) }
                    pageAddress != null -> AndroidView(
                        factory = { context ->
                            WebView(context).apply {
                                // An artifact is an interactive page the agent wrote, so its scripts run. They hold no token of the person's
                                // (the signed address is the only credential), and the page cannot read the phone's files or load plain http.
                                settings.javaScriptEnabled = true
                                settings.allowFileAccess = false
                                settings.allowContentAccess = false
                                settings.mixedContentMode = android.webkit.WebSettings.MIXED_CONTENT_NEVER_ALLOW
                                webViewClient = object : WebViewClient() {
                                    override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                                        // The page itself and frames inside it from the same server stay; a link the person taps opens in the browser.
                                        val target = request.url
                                        val shown = Uri.parse(pageAddress)
                                        if (target.host == shown.host && target.scheme == shown.scheme) return false
                                        if (request.hasGesture() && (target.scheme == "https" || target.scheme == "http")) openWebPage(context, target.toString())
                                        return true
                                    }
                                }
                                loadUrl(pageAddress)
                            }
                        },
                        modifier = Modifier.fillMaxSize(),
                    )
                    isFailed -> Text(stringResource(R.string.chats_artifact_failed), Modifier.padding(12.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    else -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                }
            }
        }
    }
}

/** The files a tool handed the person, drawn as cards under the tool's line. */
@Composable
fun ToolFiles(toolResult: String, toolName: String) {
    val files = remember(toolResult, toolName) { AgentFile.of(toolResult, toolName) }
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        for (file in files) {
            if (file.isArtifact) ArtifactCard(file) else FileCard(file)
        }
    }
}

/** A file or picture chosen to go with the next message, with its progress while it is sent and a way to drop it. */
@Composable
fun PendingFileChip(file: PendingFile, isUploading: Boolean, isUploaded: Boolean, onRemove: () -> Unit) {
    Row(
        Modifier.clip(RoundedCornerShape(14.dp)).background(MaterialTheme.colorScheme.surfaceContainerHigh).padding(start = 6.dp, end = 2.dp, top = 4.dp, bottom = 4.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        val preview = file.preview
        if (preview != null) {
            Image(preview.asImageBitmap(), contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.size(36.dp).clip(RoundedCornerShape(8.dp)))
        } else {
            Icon(Icons.AutoMirrored.Outlined.InsertDriveFile, contentDescription = null, modifier = Modifier.size(24.dp))
        }
        Column(Modifier.widthIn(max = 150.dp)) {
            Text(file.name, maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodySmall)
            Text(byteCountText(file.data.size), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        when {
            isUploading && !isUploaded -> CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
            isUploaded -> Icon(Icons.Outlined.Check, contentDescription = null, modifier = Modifier.size(18.dp), tint = MaterialTheme.colorScheme.primary)
            else -> IconButton(onClick = onRemove, modifier = Modifier.size(28.dp)) {
                Icon(Icons.Outlined.Close, contentDescription = stringResource(R.string.chats_remove_file, file.name), modifier = Modifier.size(16.dp))
            }
        }
    }
}

/** A picture still being read from the library, drawn as a quiet placeholder. */
@Composable
fun LoadingFileChip() {
    Box(Modifier.size(44.dp).clip(RoundedCornerShape(12.dp)).background(MaterialTheme.colorScheme.surfaceContainerHigh), contentAlignment = Alignment.Center) {
        CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
    }
}
