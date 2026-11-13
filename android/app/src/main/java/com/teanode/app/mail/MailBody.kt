package com.teanode.app.mail

import android.annotation.SuppressLint
import android.content.Intent
import android.net.Uri
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.viewinterop.AndroidView
import com.teanode.kit.Connection
import com.teanode.kit.mail.MailWords
import kotlinx.coroutines.runBlocking
import java.io.ByteArrayInputStream

/**
 * The page a message's HTML is shown in: scripts off, nothing loads from the sender, pictures held back unless the
 * person asked for them, and darkened in the dark theme unless they asked for the original look.
 */
fun mailDocument(html: String, mailId: String, isShowingPictures: Boolean, isDarkened: Boolean): String {
    val shown = if (isShowingPictures) MailWords.showingRemoteImages(html, mailId) else html
    val darkening = if (isDarkened) "html{filter:invert(1) hue-rotate(180deg)} img,video,picture,[style*=\"background-image\"]{filter:invert(1) hue-rotate(180deg)}" else ""
    return """
        <!doctype html><html><head><meta charset="utf-8">
        <meta name="viewport" content="width=device-width, initial-scale=1">
        <meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data: https://teanode-mail.invalid; style-src 'unsafe-inline'; font-src data:">
        <style>
        html,body{margin:0;padding:0;background:#fff;color:#111}
        #teanode-content{font:16px sans-serif;line-height:1.45;word-wrap:break-word;overflow-x:auto}
        img{max-width:100%;height:auto} table{max-width:100%} pre{white-space:pre-wrap}
        $darkening
        </style></head><body><div id="teanode-content">$shown</div></body></html>
    """.trimIndent()
}

/** The address the page asks the server's pictures through: a name no real site has, which the view answers itself. */
private const val PICTURE_HOST = "teanode-mail.invalid"

/**
 * MailBody is a message's HTML in a web view with scripts off. Every picture it asks for is read by the app as the
 * person, from this server only; a link opens in the phone's browser.
 */
@SuppressLint("SetJavaScriptEnabled")
@Composable
fun MailBody(html: String, mailId: String, connection: Connection, isShowingPictures: Boolean, isOriginalLook: Boolean) {
    val isDarkened = isSystemInDarkTheme() && !isOriginalLook
    val document = mailDocument(html.replace("src=\"/api/v1/", "src=\"https://$PICTURE_HOST/api/v1/"), mailId, isShowingPictures, isDarkened)
    var heightPixels by remember { mutableIntStateOf(0) }
    val density = LocalDensity.current
    AndroidView(
        modifier = Modifier.fillMaxWidth().height(with(density) { (if (heightPixels == 0) 60 * density.density else heightPixels.toFloat()).toDp() }),
        factory = { context ->
            WebView(context).apply {
                settings.javaScriptEnabled = false
                settings.allowFileAccess = false
                settings.allowContentAccess = false
                settings.blockNetworkLoads = false
                setBackgroundColor(android.graphics.Color.WHITE)
                webViewClient = object : WebViewClient() {
                    override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? {
                        val address = request.url
                        if (address.host == PICTURE_HOST) {
                            val path = address.encodedPath.orEmpty() + (address.encodedQuery?.let { query -> "?$query" } ?: "")
                            val answer = runCatching { runBlocking { connection.get(path) } }.getOrNull()
                            if (answer != null && answer.statusCode == 200) {
                                return WebResourceResponse("image/*", null, ByteArrayInputStream(answer.body))
                            }
                        }
                        // Nothing else loads: not a sender's picture, not a tracker.
                        return if (address.scheme == "data") null else WebResourceResponse("text/plain", "utf-8", ByteArrayInputStream(ByteArray(0)))
                    }

                    override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                        val target = request.url
                        if (target.scheme == "http" || target.scheme == "https" || target.scheme == "mailto") {
                            runCatching { view.context.startActivity(Intent(Intent.ACTION_VIEW, target).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) }
                        }
                        return true
                    }

                    override fun onPageFinished(view: WebView, url: String?) {
                        view.post { heightPixels = (view.contentHeight * view.resources.displayMetrics.density).toInt() + 8 }
                    }
                }
            }
        },
        update = { view ->
            // A recomposition that changed nothing must not load the page again.
            if (view.tag != document) {
                view.tag = document
                view.loadDataWithBaseURL("https://$PICTURE_HOST/", document, "text/html", "utf-8", null)
            }
        },
    )
}
