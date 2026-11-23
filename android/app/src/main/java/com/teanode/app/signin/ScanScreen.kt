package com.teanode.app.signin

import android.Manifest
import android.content.pm.PackageManager
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.ImageProxy
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.CameraAlt
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import com.teanode.app.R
import com.teanode.app.ui.components.EmptyState
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

/** ScanScreen is the camera looking for the dashboard's code. It asks for the camera when it opens. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ScanScreen(onFound: (String) -> Unit, onCancel: () -> Unit) {
    val context = LocalContext.current
    var isAllowed by remember { mutableStateOf(ContextCompat.checkSelfPermission(context, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED) }
    var hasAsked by remember { mutableStateOf(isAllowed) }
    val askForCamera = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        isAllowed = granted
        hasAsked = true
    }
    LaunchedEffect(Unit) { if (!isAllowed) askForCamera.launch(Manifest.permission.CAMERA) }
    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.signin_scan_code)) },
                navigationIcon = {
                    IconButton(onClick = onCancel) { Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = stringResource(R.string.common_cancel)) }
                },
            )
        },
    ) { contentPadding ->
        Box(Modifier.fillMaxSize().padding(contentPadding)) {
            if (isAllowed) {
                CameraPreview(onFound, Modifier.fillMaxSize())
            } else if (hasAsked) {
                EmptyState(
                    icon = Icons.Outlined.CameraAlt,
                    title = stringResource(R.string.signin_camera_unavailable),
                    description = stringResource(R.string.signin_camera_unavailable_hint),
                )
            }
        }
    }
}

@Composable
private fun CameraPreview(onFound: (String) -> Unit, modifier: Modifier) {
    val context = LocalContext.current
    val lifecycleOwner = LocalLifecycleOwner.current
    val executor = remember { Executors.newSingleThreadExecutor() }
    val isFound = remember { AtomicBoolean(false) }
    val decoder = remember { QrCodeDecoder() }
    DisposableEffect(Unit) { onDispose { executor.shutdown() } }
    AndroidView(
        modifier = modifier,
        factory = { viewContext ->
            val previewView = PreviewView(viewContext)
            val providerFuture = ProcessCameraProvider.getInstance(viewContext)
            providerFuture.addListener({
                val provider = providerFuture.get()
                val preview = Preview.Builder().build().also { built -> built.surfaceProvider = previewView.surfaceProvider }
                val analysis = ImageAnalysis.Builder().setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST).build()
                analysis.setAnalyzer(executor) { frame: ImageProxy ->
                    try {
                        val plane = frame.planes[0]
                        val bytes = ByteArray(plane.buffer.remaining()).also { copy -> plane.buffer.get(copy) }
                        val text = decoder.decode(bytes, frame.width, frame.height, plane.rowStride)
                        // The first code found is the answer; later frames of the same code say nothing more.
                        if (text != null && isFound.compareAndSet(false, true)) previewView.post { onFound(text) }
                    } finally {
                        frame.close()
                    }
                }
                provider.unbindAll()
                provider.bindToLifecycle(lifecycleOwner, CameraSelector.DEFAULT_BACK_CAMERA, preview, analysis)
            }, ContextCompat.getMainExecutor(context))
            previewView
        },
    )
}
