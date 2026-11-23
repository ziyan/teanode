package com.teanode.app.journal

import android.Manifest
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Mic
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import com.teanode.app.R
import com.teanode.app.chats.Dictation
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.kit.chats.voiceAvailability

/**
 * A microphone button that puts what the person says at the end of a text, with the dictation Chats uses. It is not
 * drawn when the server does not offer dictation. [read] and [write] are the text's current value and its setter.
 */
@Composable
fun JournalDictationButton(read: () -> String, write: (String) -> Unit, isEnabled: Boolean = true) {
    val context = LocalContext.current
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val dictation = remember { Dictation(context) }
    var sampleRate by remember(connection) { mutableStateOf<Int?>(null) }
    LaunchedEffect(connection) {
        sampleRate = runCatching { connection.voiceAvailability() }.getOrNull()?.takeIf { voice -> voice.isVoiceAvailable }?.let { voice -> voice.sampleRate ?: 24_000 }
    }
    DisposableEffect(dictation) { onDispose { dictation.stop() } }
    LaunchedEffect(dictation.problemText) {
        dictation.problemText?.let { problem ->
            snackbar.showProblem(problem)
            dictation.problemText = null
        }
    }
    val microphonePermission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { isGranted ->
        if (!isGranted) snackbar.showProblem(context.getString(R.string.chats_microphone_not_allowed))
    }
    val rate = sampleRate ?: return
    IconButton(
        enabled = isEnabled,
        onClick = {
            if (!dictation.hasPermission()) microphonePermission.launch(Manifest.permission.RECORD_AUDIO)
            else dictation.toggle(read, write, Dictation.Place.Agent, connection, rate)
        },
    ) {
        Icon(
            Icons.Outlined.Mic, contentDescription = stringResource(if (dictation.isDictating) R.string.chats_stop_dictating else R.string.chats_dictate),
            tint = if (dictation.isDictating) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
