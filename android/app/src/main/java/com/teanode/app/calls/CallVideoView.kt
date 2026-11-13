package com.teanode.app.calls

import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.viewinterop.AndroidView
import org.webrtc.RendererCommon
import org.webrtc.SurfaceViewRenderer

/**
 * CallVideoView draws a track of pictures. Compose has nothing that draws WebRTC's frames, so this is the one platform
 * view of a call: WebRTC's own renderer, filling its tile.
 *
 * @param isMirrored the person's own picture is drawn mirrored, as in a mirror.
 */
@Composable
fun CallVideoView(track: CallVideoTrack, modifier: Modifier = Modifier, isMirrored: Boolean = false) {
    val webRtcTrack = (track as? WebRtcVideoTrack)?.track ?: return
    val renderer = remember { arrayOfNulls<SurfaceViewRenderer>(1) }
    AndroidView(
        modifier = modifier,
        factory = { context ->
            SurfaceViewRenderer(context).also { view ->
                view.init(WebRtcFactory.eglBase.eglBaseContext, null)
                view.setScalingType(RendererCommon.ScalingType.SCALE_ASPECT_FILL)
                view.setEnableHardwareScaler(true)
                renderer[0] = view
            }
        },
        update = { view -> view.setMirror(isMirrored) },
    )
    DisposableEffect(webRtcTrack) {
        val view = renderer[0]
        view?.let { webRtcTrack.addSink(it) }
        onDispose {
            view?.let { webRtcTrack.removeSink(it) }
        }
    }
    DisposableEffect(Unit) {
        onDispose { renderer[0]?.release() }
    }
}
