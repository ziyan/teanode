package com.teanode.app.photos

import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.photos.PhotoBackupRecord
import com.teanode.kit.photos.PhotoBackupRefusals
import com.teanode.kit.photos.PhotoBackupRefusedFile

/**
 * The files backup left alone (ios/TeaNode/Sources/PhotoBackupRefusedFilesView.swift): those the server refused as often as
 * backup tries, which can be tried again or skipped, and those skipped, which stay out until tried again. [previewRecords]
 * draws given records (the screenshots).
 */
@Composable
fun PhotoBackupFilesScreen(onBack: () -> Unit, previewRecords: Map<String, PhotoBackupRecord>? = null) {
    val context = LocalContext.current
    val connection = LocalConnection.current
    val settings = remember(connection) { PhotoBackupSettings(context, connection.accountKey) }
    var records by remember { mutableStateOf(previewRecords?.toMutableMap() ?: settings.records()) }
    fun change(work: (MutableMap<String, PhotoBackupRecord>) -> Unit) {
        val updated = records.toMutableMap().also(work)
        records = updated
        if (previewRecords == null) { settings.saveRecords(updated); PhotoBackupScheduler.backUpNow(context, settings.usesMobileData) }
    }
    val refused = PhotoBackupRefusals.refused(records)
    val skipped = PhotoBackupRefusals.skipped(records)
    TeaNodeScreen(title = stringResource(R.string.photos_backup_files_title), onBack = onBack) { padding ->
        if (refused.isEmpty() && skipped.isEmpty()) {
            EmptyState(Icons.Outlined.CheckCircle, stringResource(R.string.photos_backup_files_empty), modifier = Modifier.padding(padding), description = stringResource(R.string.photos_backup_files_empty_text))
        } else {
            LazyColumn(Modifier.fillMaxSize().padding(padding)) {
                if (refused.isNotEmpty()) {
                    item {
                        TeaNodeSectionHeader(stringResource(R.string.photos_backup_files_refused))
                        Text(stringResource(R.string.photos_backup_files_refused_text), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp))
                    }
                    items(refused, key = { file -> file.id }) { file ->
                        FileRow(file, primaryLabel = R.string.photos_try_again, onPrimary = { change { map -> PhotoBackupRefusals.tryAgain(map, file.localIdentifier, file.role) } }, secondaryLabel = R.string.photos_backup_files_skip, onSecondary = { change { map -> PhotoBackupRefusals.skip(map, file.localIdentifier, file.role, System.currentTimeMillis()) } })
                    }
                }
                if (skipped.isNotEmpty()) {
                    item {
                        TeaNodeSectionHeader(stringResource(R.string.photos_backup_files_skipped))
                        Text(stringResource(R.string.photos_backup_files_skipped_text), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp))
                    }
                    items(skipped, key = { file -> "skipped-" + file.id }) { file ->
                        FileRow(file, primaryLabel = R.string.photos_try_again, onPrimary = { change { map -> PhotoBackupRefusals.tryAgain(map, file.localIdentifier, file.role) } })
                    }
                }
            }
        }
    }
}

@Composable
private fun FileRow(file: PhotoBackupRefusedFile, primaryLabel: Int, onPrimary: () -> Unit, secondaryLabel: Int? = null, onSecondary: () -> Unit = {}) {
    TeaNodeRow(
        headline = file.filename, supporting = file.refusalReason?.takeIf { reason -> reason.isNotEmpty() && !file.isSkipped },
        trailing = {
            Row {
                TextButton(onClick = onPrimary) { Text(stringResource(primaryLabel)) }
                if (secondaryLabel != null) TextButton(onClick = onSecondary) { Text(stringResource(secondaryLabel)) }
            }
        },
    )
}
