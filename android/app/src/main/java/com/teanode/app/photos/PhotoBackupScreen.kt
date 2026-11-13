package com.teanode.app.photos

import android.content.Intent
import android.net.Uri
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.kit.photos.PhotoBackupFailure
import com.teanode.kit.photos.PhotoBackupPhase
import com.teanode.kit.photos.PhotoBackupSpokenDuration
import com.teanode.kit.photos.PhotoBackupSpokenDurationUnit
import com.teanode.kit.photos.PhotoBackupStatus
import com.teanode.kit.photos.PhotoLibrary
import com.teanode.kit.photos.photoLibrary
import com.teanode.kit.photos.setPhotoReading
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/**
 * Photo backup (ios/TeaNode/Sources/PhotoBackupView.swift): a switch to copy the phone's pictures and videos to the
 * server, what is done and what is left, the problems and what to do about them, mobile data, pausing, and the space used.
 * [statusOverride] draws a given status (the demo and the screenshots); a real screen reads it from the worker's progress.
 */
@Composable
fun PhotoBackupScreen(onBack: () -> Unit, statusOverride: PhotoBackupStatus? = null, initialIsEnabled: Boolean? = null, onOpenFiles: () -> Unit = {}) {
    val context = LocalContext.current
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val settings = remember(connection) { PhotoBackupSettings(context, connection.accountKey) }
    var isEnabled by remember { mutableStateOf(initialIsEnabled ?: settings.isEnabled) }
    var isPaused by remember { mutableStateOf(settings.isPaused) }
    var usesMobileData by remember { mutableStateOf(settings.usesMobileData) }
    var includesHidden by remember { mutableStateOf(settings.includesHiddenFolders) }
    var isTurningOff by remember { mutableStateOf(false) }
    var library by remember { mutableStateOf<PhotoLibrary?>(null) }
    var isReading by remember { mutableStateOf(false) }
    val liveStatus by PhotoBackupProgress.status.collectAsState()
    val status = statusOverride ?: if (isEnabled) liveStatus.copy(phase = if (isPaused) PhotoBackupPhase.Paused else if (liveStatus.phase == PhotoBackupPhase.Off) PhotoBackupPhase.Idle else liveStatus.phase) else PhotoBackupStatus()
    LaunchedEffect(connection) { library = runCatching { connection.photoLibrary() }.getOrNull() }
    isReading = library?.isPhotoReadingEnabled == true

    val permissionLauncher = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
        if (MediaStoreLibrary.hasFullAccess(context)) {
            settings.isEnabled = true; isEnabled = true
            PhotoBackupScheduler.schedule(context, usesMobileData); PhotoBackupScheduler.backUpNow(context, usesMobileData)
        } else {
            PhotoBackupProgress.reset(PhotoBackupStatus(phase = PhotoBackupPhase.NeedsAccess(MediaStoreLibrary.hasLimitedAccess(context))))
            settings.isEnabled = true; isEnabled = true
        }
    }

    fun turnOn() {
        if (MediaStoreLibrary.hasFullAccess(context)) {
            settings.isEnabled = true; isEnabled = true
            PhotoBackupScheduler.schedule(context, usesMobileData); PhotoBackupScheduler.backUpNow(context, usesMobileData)
        } else {
            permissionLauncher.launch(MediaStoreLibrary.permissions)
        }
    }

    TeaNodeScreen(title = stringResource(R.string.photos_backup_title), onBack = onBack) { padding ->
        Column(Modifier.fillMaxSize().padding(padding).verticalScroll(rememberScrollState())) {
            Text(stringResource(R.string.photos_backup_explain), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(16.dp))
            SwitchRow(stringResource(R.string.photos_backup_toggle), isEnabled) { isOn ->
                if (isOn) turnOn() else isTurningOff = true
            }
            if (isEnabled) {
                TeaNodeSectionHeader(stringResource(R.string.photos_backup_status))
                StatusBlock(status)
                if (status.phase is PhotoBackupPhase.NeedsAccess) {
                    Row(Modifier.padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Button(onClick = { permissionLauncher.launch(MediaStoreLibrary.permissions) }) { Text(stringResource(R.string.photos_backup_allow_access)) }
                        OutlinedButton(onClick = { context.startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.fromParts("package", context.packageName, null))) }) { Text(stringResource(R.string.photos_backup_open_settings)) }
                    }
                }
                if (status.phase == PhotoBackupPhase.NeedsSignIn) {
                    Button(onClick = { settings.forgetCredential(); PhotoBackupScheduler.backUpNow(context, usesMobileData) }, modifier = Modifier.padding(horizontal = 16.dp)) { Text(stringResource(R.string.photos_backup_sign_in_again)) }
                }
                Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Button(onClick = { PhotoBackupScheduler.backUpNow(context, usesMobileData) }, enabled = !isPaused) { Text(stringResource(R.string.photos_backup_now)) }
                    OutlinedButton(onClick = {
                        isPaused = !isPaused; settings.isPaused = isPaused
                        if (!isPaused) PhotoBackupScheduler.backUpNow(context, usesMobileData)
                    }) { Text(stringResource(if (isPaused) R.string.photos_backup_resume else R.string.photos_backup_pause)) }
                }
                if (isPaused) Text(stringResource(R.string.photos_backup_paused_text), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp))
                if (status.refusedFileCount > 0 || status.skippedFileCount > 0) TextButton(onClick = onOpenFiles, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.photos_backup_files_title)) }
                if (status.refusedFileCount > 0) {
                    Text(pluralStringResource(R.plurals.photos_backup_refused, status.refusedFileCount, status.refusedFileCount), color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp))
                }
                if (status.skippedFileCount > 0) Text(pluralStringResource(R.plurals.photos_backup_skipped, status.skippedFileCount, status.skippedFileCount), style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(horizontal = 16.dp))
                status.lastFailure?.let { failure ->
                    Text(stringResource(R.string.photos_backup_last_problem), style = MaterialTheme.typography.titleSmall, modifier = Modifier.padding(start = 16.dp, top = 8.dp))
                    Text(failureWords(failure), style = MaterialTheme.typography.bodyMedium, modifier = Modifier.padding(horizontal = 16.dp))
                }
                HorizontalDivider(Modifier.padding(vertical = 12.dp))
                SwitchRow(stringResource(R.string.photos_backup_mobile_data), usesMobileData) { isOn ->
                    usesMobileData = isOn; settings.usesMobileData = isOn; PhotoBackupScheduler.schedule(context, isOn)
                }
                SwitchRow(stringResource(R.string.photos_backup_hidden), includesHidden) { isOn -> includesHidden = isOn; settings.includesHiddenFolders = isOn; if (isEnabled) PhotoBackupScheduler.backUpNow(context, usesMobileData) }
                Text(stringResource(R.string.photos_backup_hidden_text), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp))
                SwitchRow(stringResource(R.string.photos_backup_read_new), isReading) { isOn ->
                    scope.launch { try { connection.setPhotoReading(isOn); isReading = isOn } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) } }
                }
                Text(stringResource(R.string.photos_backup_read_new_text), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp))
                Text(stringResource(R.string.photos_backup_wifi_text), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(16.dp))
                library?.let { read ->
                    TeaNodeSectionHeader(stringResource(R.string.photos_backup_your_library))
                    TeaNodeRow(headline = stringResource(R.string.photos_backup_space_used), trailing = { Text(bytesText(read.usedBytes) + if (read.maximumAccountBytes > 0) " / " + bytesText(read.maximumAccountBytes) else "") })
                    if (read.maximumFileBytes > 0) TeaNodeRow(headline = stringResource(R.string.photos_backup_largest_file), trailing = { Text(bytesText(read.maximumFileBytes)) })
                    status.lastPassFinishedEpochMS?.let { finished -> TeaNodeRow(headline = stringResource(R.string.photos_backup_last_finished), trailing = { Text(relativeTimeText(finished)) }) }
                    Text(stringResource(R.string.photos_backup_limits_text), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(16.dp))
                }
            }
        }
    }
    if (isTurningOff) {
        ConfirmationDialog(
            title = stringResource(R.string.photos_backup_turn_off_title), confirmLabel = stringResource(R.string.photos_backup_turn_off), isDestructive = true,
            text = stringResource(R.string.photos_backup_turn_off_text),
            onConfirm = { isTurningOff = false; settings.isEnabled = false; isEnabled = false; PhotoBackupScheduler.cancel(context); settings.forgetCredential() }, onDismiss = { isTurningOff = false },
        )
    }
}

@Composable
private fun SwitchRow(label: String, isChecked: Boolean, onChange: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(label, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
        Switch(checked = isChecked, onCheckedChange = onChange)
    }
}

@Composable
private fun StatusBlock(status: PhotoBackupStatus) {
    Column(Modifier.padding(horizontal = 16.dp, vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        val headline = when (val phase = status.phase) {
            PhotoBackupPhase.Off -> R.string.photos_backup_off
            PhotoBackupPhase.Idle -> R.string.photos_backup_phase_idle
            PhotoBackupPhase.Listing -> R.string.photos_backup_phase_listing
            PhotoBackupPhase.Uploading -> R.string.photos_backup_phase_uploading
            PhotoBackupPhase.WaitingForNetwork -> R.string.photos_backup_phase_network
            PhotoBackupPhase.WaitingForServer -> R.string.photos_backup_phase_server
            is PhotoBackupPhase.NeedsAccess -> if (phase.isLimited) R.string.photos_backup_phase_limited else R.string.photos_backup_phase_access
            PhotoBackupPhase.NeedsSignIn -> R.string.photos_backup_phase_sign_in
            PhotoBackupPhase.Paused -> R.string.photos_backup_phase_paused
        }
        Text(stringResource(headline), style = MaterialTheme.typography.titleMedium)
        if (status.libraryItemCount > 0) {
            Text(stringResource(R.string.photos_backup_count, status.backedUpItemCount, status.libraryItemCount))
            LinearProgressIndicator(progress = { status.backedUpItemCount.toFloat() / status.libraryItemCount }, modifier = Modifier.fillMaxWidth())
        }
        status.estimatedSecondsLeft?.let { seconds ->
            val spoken = PhotoBackupSpokenDuration.ofSeconds(seconds)
            val plural = when (spoken.unit) {
                PhotoBackupSpokenDurationUnit.Minutes -> R.plurals.photos_backup_left_minutes
                PhotoBackupSpokenDurationUnit.Hours -> R.plurals.photos_backup_left_hours
                PhotoBackupSpokenDurationUnit.Days -> R.plurals.photos_backup_left_days
            }
            Text(pluralStringResource(plural, spoken.count, spoken.count), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
internal fun failureWords(failure: PhotoBackupFailure): String = when (failure) {
    PhotoBackupFailure.SignInRefused -> stringResource(R.string.photos_failure_sign_in)
    PhotoBackupFailure.ServerDidNotSayWhichPhoto -> stringResource(R.string.photos_failure_no_photo_id)
    is PhotoBackupFailure.FileRefused -> failure.serverReason ?: stringResource(R.string.photos_failure_refused)
    is PhotoBackupFailure.ServerReason -> failure.reason
    PhotoBackupFailure.ServerOutOfSpace -> stringResource(R.string.photos_failure_out_of_space)
    PhotoBackupFailure.FileIsEmpty -> stringResource(R.string.photos_failure_empty)
    PhotoBackupFailure.FileIsNotInLibrary -> stringResource(R.string.photos_failure_not_in_library)
    PhotoBackupFailure.FileTookTooLongToRead -> stringResource(R.string.photos_failure_too_long)
    is PhotoBackupFailure.SystemDescription -> failure.description
}

internal fun bytesText(bytes: Long): String {
    val units = listOf("B", "KB", "MB", "GB", "TB")
    var amount = bytes.toDouble()
    var index = 0
    while (amount >= 1024 && index < units.size - 1) { amount /= 1024; index += 1 }
    return if (index == 0) "$bytes B" else "%.1f %s".format(amount, units[index])
}
