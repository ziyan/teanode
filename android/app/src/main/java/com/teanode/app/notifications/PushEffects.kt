package com.teanode.app.notifications

import android.Manifest
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.ContextCompat
import com.teanode.app.TeaNodeApplication
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.kit.Connection
import kotlinx.coroutines.launch

/**
 * PushEffects is what the signed-in app starts for push and calls, as the iPhone app does after connecting and each
 * time it starts: it asks to send notifications (Android 13 and later ask), registers the phone with the server,
 * follows the calls of the person's chats, and says what a notification that could not be opened says.
 */
@Composable
fun PushEffects(connection: Connection) {
    val context = LocalContext.current
    val push = (context.applicationContext as TeaNodeApplication).push
    val scope = rememberCoroutineScope()
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) {
        // Whatever the answer, the phone registers: told nothing when refused, as an iPhone is.
        scope.launch { push.registrar.setUp(connection) }
    }
    LaunchedEffect(connection.accountKey) {
        val isAskNeeded = Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) != android.content.pm.PackageManager.PERMISSION_GRANTED
        if (isAskNeeded) launcher.launch(Manifest.permission.POST_NOTIFICATIONS) else push.registrar.setUp(connection)
    }
    LaunchedEffect(connection.accountKey) { push.calls.follow(connection) }
    val notice by NotificationOpening.notice.collectAsState()
    val snackbar = LocalSnackbar.current
    LaunchedEffect(notice) {
        notice?.let { text ->
            NotificationOpening.consumeNotice()
            snackbar.showProblem(text)
        }
    }
}
