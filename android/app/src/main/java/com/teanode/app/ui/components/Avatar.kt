package com.teanode.app.ui.components

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.util.LruCache
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.teanode.app.session.LocalConnectionOrNull
import com.teanode.app.ui.theme.AvatarPalette
import com.teanode.kit.AvatarColor
import com.teanode.kit.Monogram
import androidx.compose.ui.graphics.Color

/**
 * AvatarView is a person: their picture when they have one, otherwise the first letter of their name on the color the
 * server chose for them, as the dashboard draws them.
 *
 * @param avatarUrl the picture's path on the server, from the person's `avatarUrl`; null when they have none.
 * @param colorIndex the color the server chose. When it has not said, one is worked out from the name, which is the
 *   same function the server runs over an id.
 */
@Composable
fun AvatarView(name: String, avatarUrl: String?, colorIndex: Int?, modifier: Modifier = Modifier, size: Dp = 40.dp) {
    val picture = rememberServerPicture(avatarUrl?.let { path -> sizedPicturePath(path, with(LocalDensity.current) { size.toPx() }) })
    val isDark = MaterialTheme.colorScheme.background.luminance() < 0.5f
    Box(
        modifier = modifier.size(size).clip(CircleShape).clearAndSetSemantics {},
        contentAlignment = Alignment.Center,
    ) {
        if (picture != null) {
            Image(picture, contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.size(size))
        } else {
            Box(
                Modifier.size(size).background(AvatarPalette.color(AvatarColor.index(colorIndex, name), isDark)),
                contentAlignment = Alignment.Center,
            ) {
                Text(Monogram.letter(name), color = Color.White, fontSize = monogramSize(size), style = MaterialTheme.typography.titleMedium)
            }
        }
    }
}

private fun monogramSize(size: Dp): TextUnit = (size.value * 0.46f).sp

/** The server draws a picture at 64 or 512 pixels square; the small one serves everything up to 64 pixels on screen. */
fun sizedPicturePath(avatarUrl: String, pixels: Float): String {
    val renderedPixels = if (pixels <= 64) 64 else 512
    return avatarUrl + (if (avatarUrl.contains("?")) "&" else "?") + "size=$renderedPixels"
}

private object PictureCache {
    val bitmaps = object : LruCache<String, Bitmap>(24 * 1024 * 1024) {
        override fun sizeOf(key: String, bitmap: Bitmap): Int = bitmap.byteCount
    }
}

/**
 * A picture kept on the server, read with the person's token and decoded; null while it loads, when the path is null,
 * or when the server has none. The address carries the picture's version, so a new picture is a new address.
 */
@Composable
fun rememberServerPicture(path: String?): androidx.compose.ui.graphics.ImageBitmap? {
    val connection = LocalConnectionOrNull.current
    var picture by remember(path, connection) { mutableStateOf<Bitmap?>(null) }
    LaunchedEffect(path, connection) {
        if (path.isNullOrEmpty() || connection == null) return@LaunchedEffect
        val cacheKey = "${connection.accountKey}$path"
        PictureCache.bitmaps.get(cacheKey)?.let { cached ->
            picture = cached
            return@LaunchedEffect
        }
        val decoded = runCatching {
            val response = connection.get(path)
            if (response.statusCode == 200) BitmapFactory.decodeByteArray(response.body, 0, response.body.size) else null
        }.getOrNull()
        if (decoded != null) {
            PictureCache.bitmaps.put(cacheKey, decoded)
            picture = decoded
        }
    }
    return picture?.asImageBitmap()
}
