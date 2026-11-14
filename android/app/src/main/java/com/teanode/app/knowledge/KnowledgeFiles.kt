package com.teanode.app.knowledge

import android.content.ActivityNotFoundException
import android.content.ClipData
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.Color as AndroidColor
import android.graphics.pdf.PdfRenderer
import android.os.ParcelFileDescriptor
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.OpenInNew
import androidx.compose.material.icons.outlined.PictureAsPdf
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.core.content.FileProvider
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.kit.Connection
import com.teanode.kit.chats.download
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File

/** Where the files a page's facts were read from are written before another app or the PDF renderer reads them. */
private fun cachedFile(context: Context, name: String): File {
    val directory = File(context.cacheDir, "chat-files").also { made -> made.mkdirs() }
    return File(directory, name.ifEmpty { "file" }.replace("/", "-"))
}

/**
 * What a file a fact was read from shows as: a PDF as its first page (a tap reads every page), and any other file,
 * such as an office document, as a row that opens it in whatever app on the phone shows it.
 */
@Composable
fun KnowledgeFilePreview(path: String, name: String, contentType: String, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val couldNotOpen = stringResource(R.string.knowledge_could_not_open_file)
    val isPdf = contentType == "application/pdf" || name.endsWith(".pdf", ignoreCase = true)
    var pages by remember(path) { mutableStateOf<List<Bitmap>?>(null) }
    var isReading by remember(path) { mutableStateOf(isPdf) }
    var isShowingAll by remember(path) { mutableStateOf(false) }
    LaunchedEffect(path, isPdf) {
        if (!isPdf) return@LaunchedEffect
        pages = withContext(Dispatchers.IO) {
            runCatching { renderPdfPages(context, connection, path, name) }.getOrNull()
        }
        isReading = false
    }
    val readPages = pages
    if (isPdf && readPages != null && readPages.isNotEmpty()) {
        PdfPreviewCard(name, readPages, modifier) { isShowingAll = true }
        if (isShowingAll) PdfViewerDialog(name, readPages) { isShowingAll = false }
    } else {
        FileRow(name, if (isPdf) Icons.Outlined.PictureAsPdf else Icons.Outlined.Description, isBusy = isReading, modifier) {
            scope.launch {
                try {
                    openInAnotherApp(context, connection, path, name, contentType)
                } catch (error: kotlinx.coroutines.CancellationException) {
                    throw error
                } catch (_: Throwable) {
                    snackbar.showProblem(couldNotOpen)
                }
            }
        }
    }
}

@Composable
private fun FileRow(name: String, icon: androidx.compose.ui.graphics.vector.ImageVector, isBusy: Boolean, modifier: Modifier, onOpen: () -> Unit) {
    Row(modifier.fillMaxWidth().clickable(enabled = !isBusy, onClick = onOpen).padding(vertical = 6.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Icon(icon, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
        Text(name, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.primary, modifier = Modifier.weight(1f))
        Icon(Icons.Outlined.OpenInNew, contentDescription = stringResource(R.string.knowledge_open_in_another_app), tint = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** The first page of a PDF, with its name and how many pages it has. */
@Composable
fun PdfPreviewCard(name: String, pages: List<Bitmap>, modifier: Modifier = Modifier, onOpen: () -> Unit) {
    Column(modifier.fillMaxWidth().clickable(onClick = onOpen).padding(vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Image(
            pages.first().asImageBitmap(), contentDescription = name, contentScale = ContentScale.Fit, alignment = Alignment.CenterStart,
            modifier = Modifier.fillMaxWidth().heightIn(max = 220.dp).background(androidx.compose.ui.graphics.Color.White),
        )
        Text(
            name + ", " + androidx.compose.ui.res.pluralStringResource(R.plurals.knowledge_page_count, pages.size, pages.size),
            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.primary,
        )
    }
}

/** Every page of a PDF, one under another, in a full screen window. */
@Composable
fun PdfViewerDialog(name: String, pages: List<Bitmap>, onClose: () -> Unit) {
    Dialog(onDismissRequest = onClose, properties = DialogProperties(usePlatformDefaultWidth = false)) {
        Surface(Modifier.fillMaxSize()) {
            Column(Modifier.fillMaxSize()) {
                Row(Modifier.fillMaxWidth().padding(start = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(name, style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f), maxLines = 1)
                    IconButton(onClick = onClose) { Icon(Icons.Outlined.Close, contentDescription = stringResource(R.string.knowledge_close)) }
                }
                LazyColumn(Modifier.fillMaxSize(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    items(pages.size) { index ->
                        Image(
                            pages[index].asImageBitmap(), contentDescription = null, contentScale = ContentScale.FillWidth,
                            modifier = Modifier.fillMaxWidth().background(androidx.compose.ui.graphics.Color.White),
                        )
                    }
                }
            }
        }
    }
}

/** At most this many pages of a PDF are drawn, so a long document does not fill the phone's memory. */
private const val PDF_PAGE_LIMIT = 30

/** Draws the pages of a PDF the server keeps, as bitmaps about a thousand pixels wide. */
private suspend fun renderPdfPages(context: Context, connection: Connection, path: String, name: String): List<Bitmap> {
    val file = cachedFile(context, name)
    file.writeBytes(connection.download(path))
    ParcelFileDescriptor.open(file, ParcelFileDescriptor.MODE_READ_ONLY).use { descriptor ->
        PdfRenderer(descriptor).use { renderer ->
            return (0 until minOf(renderer.pageCount, PDF_PAGE_LIMIT)).map { index ->
                renderer.openPage(index).use { page ->
                    val width = 1000
                    val height = (width.toFloat() * page.height / page.width).toInt().coerceAtLeast(1)
                    Bitmap.createBitmap(width, height, Bitmap.Config.ARGB_8888).also { bitmap ->
                        bitmap.eraseColor(AndroidColor.WHITE)
                        page.render(bitmap, null, null, PdfRenderer.Page.RENDER_MODE_FOR_DISPLAY)
                    }
                }
            }
        }
    }
}

/** Writes the file into the cache and asks the phone to show it; when no app shows that kind of file, to share it instead. */
private suspend fun openInAnotherApp(context: Context, connection: Connection, path: String, name: String, contentType: String) {
    val file = cachedFile(context, name)
    withContext(Dispatchers.IO) { file.writeBytes(connection.download(path)) }
    val address = FileProvider.getUriForFile(context, "${context.packageName}.chatfiles", file)
    val mime = contentType.ifEmpty { "application/octet-stream" }
    val view = Intent(Intent.ACTION_VIEW).setDataAndType(address, mime).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
    try {
        context.startActivity(view)
    } catch (_: ActivityNotFoundException) {
        val send = Intent(Intent.ACTION_SEND).apply {
            type = mime
            putExtra(Intent.EXTRA_STREAM, address)
            clipData = ClipData.newRawUri(name, address)
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        }
        context.startActivity(Intent.createChooser(send, null).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    }
}
