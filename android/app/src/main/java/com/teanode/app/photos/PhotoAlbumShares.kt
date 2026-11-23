package com.teanode.app.photos

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Groups
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.photos.PhotoAlbum
import com.teanode.kit.photos.PhotoAlbumShare
import com.teanode.kit.photos.PhotoShareDraft
import com.teanode.kit.photos.PhotoShareLevel
import com.teanode.kit.photos.PhotoShareTarget
import com.teanode.kit.photos.PhotoShareTargets
import com.teanode.kit.photos.photoAlbumShares
import com.teanode.kit.photos.photoAlbums
import com.teanode.kit.photos.photoShareTargets
import com.teanode.kit.photos.removePhotoAlbumShare
import com.teanode.kit.photos.sharePhotoAlbum
import com.teanode.kit.photos.updatePhotoAlbumShare
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

private class SharesRead(val album: PhotoAlbum, val shares: List<PhotoAlbumShare>, val targets: PhotoShareTargets)

/** Who an album is shared with, what each may do, and sharing it with somebody else (ios/TeaNode/Sources/PhotoSharingViews.swift). */
@Composable
fun AlbumSharesScreen(photoAlbumId: String, onBack: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val read = rememberLoad(photoAlbumId) {
        SharesRead(
            connection.photoAlbums().first { candidate -> candidate.id == photoAlbumId }, connection.photoAlbumShares(photoAlbumId),
            runCatching { connection.photoShareTargets(null) }.getOrDefault(PhotoShareTargets(emptyList(), emptyList())),
        )
    }
    var isAdding by remember { mutableStateOf(false) }
    var removing by remember { mutableStateOf<PhotoAlbumShare?>(null) }
    var draft by remember { mutableStateOf(PhotoShareDraft()) }

    fun fail(failure: Throwable) { if (failure is CancellationException) throw failure else snackbar.showError(failure) }

    TeaNodeScreen(
        title = stringResource(R.string.photos_share_album), onBack = onBack,
        floatingActionButton = { FloatingActionButton(onClick = { draft = PhotoShareDraft(); isAdding = true }) { Icon(Icons.Outlined.Add, contentDescription = stringResource(R.string.photos_share)) } },
    ) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            LoadedContent(read) { loaded ->
                val available = loaded.targets.available(loaded.shares)
                LazyColumn(Modifier.fillMaxSize()) {
                    item { TeaNodeSectionHeader(stringResource(R.string.photos_shared_with)) }
                    if (loaded.shares.isEmpty()) item { Text(stringResource(R.string.photos_not_shared_yet), modifier = Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
                    items(loaded.shares.size) { index ->
                        val share = loaded.shares[index]
                        ShareRow(
                            share,
                            onLevel = { level -> scope.launch { try { connection.updatePhotoAlbumShare(share.id, level, share.isLocationIncluded, share.isFaceNamesIncluded); read.refresh() } catch (failure: Throwable) { fail(failure) } } },
                            onLocation = { included -> scope.launch { try { connection.updatePhotoAlbumShare(share.id, share.knownShareLevel, included, share.isFaceNamesIncluded); read.refresh() } catch (failure: Throwable) { fail(failure) } } },
                            onFaces = { included -> scope.launch { try { connection.updatePhotoAlbumShare(share.id, share.knownShareLevel, share.isLocationIncluded, included); read.refresh() } catch (failure: Throwable) { fail(failure) } } },
                            onStop = { removing = share },
                        )
                    }
                    item {
                        Text(stringResource(R.string.photos_share_stays_note), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(16.dp))
                    }
                }
                if (isAdding) {
                    AddShareDialog(
                        available, draft, onDraft = { draft = it },
                        onShare = { scope.launch { try { connection.sharePhotoAlbum(photoAlbumId, draft); isAdding = false; read.refresh() } catch (failure: Throwable) { fail(failure) } } },
                        onDismiss = { isAdding = false },
                    )
                }
            }
        }
    }
    removing?.let { share ->
        ConfirmationDialog(
            title = stringResource(R.string.photos_stop_sharing), confirmLabel = stringResource(R.string.photos_stop_sharing), isDestructive = true,
            text = stringResource(R.string.photos_stop_sharing_text, share.granteeDisplayName),
            onConfirm = { removing = null; scope.launch { try { connection.removePhotoAlbumShare(share.id); read.refresh() } catch (failure: Throwable) { fail(failure) } } },
            onDismiss = { removing = null },
        )
    }
}

@Composable
private fun ShareRow(share: PhotoAlbumShare, onLevel: (PhotoShareLevel) -> Unit, onLocation: (Boolean) -> Unit, onFaces: (Boolean) -> Unit, onStop: () -> Unit) {
    Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
        TeaNodeRow(
            headline = share.granteeDisplayName,
            supporting = stringResource(if (share.knownShareLevel == PhotoShareLevel.Contribute) R.string.photos_level_contribute else R.string.photos_level_view),
            leading = { if (share.isWithCircle) Icon(Icons.Outlined.Groups, contentDescription = null) else AvatarView(share.granteeDisplayName, share.granteeAvatarUrl, share.granteeAvatarColorIndex) },
            onClick = { onLevel(if (share.knownShareLevel == PhotoShareLevel.View) PhotoShareLevel.Contribute else PhotoShareLevel.View) },
        )
        androidx.compose.foundation.layout.Row(Modifier.padding(start = 56.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(R.string.photos_share_include_location), modifier = Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium)
            Switch(checked = share.isLocationIncluded, onCheckedChange = onLocation)
        }
        androidx.compose.foundation.layout.Row(Modifier.padding(start = 56.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(R.string.photos_share_include_faces), modifier = Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium)
            Switch(checked = share.isFaceNamesIncluded, onCheckedChange = onFaces)
        }
        TextButton(onClick = onStop, modifier = Modifier.padding(start = 44.dp)) { Text(stringResource(R.string.photos_stop_sharing), color = MaterialTheme.colorScheme.error) }
    }
}

@Composable
private fun AddShareDialog(targets: PhotoShareTargets, draft: PhotoShareDraft, onDraft: (PhotoShareDraft) -> Unit, onShare: () -> Unit, onDismiss: () -> Unit) {
    androidx.compose.material3.AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.photos_share_album)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState())) {
                if (targets.circles.isEmpty() && targets.people.isEmpty()) {
                    Text(stringResource(R.string.photos_share_nobody))
                } else {
                    if (targets.circles.isNotEmpty()) {
                        Text(stringResource(R.string.photos_share_with_circle), style = MaterialTheme.typography.titleSmall)
                        for (circle in targets.circles) {
                            TeaNodeRow(
                                headline = circle.name, leadingIcon = Icons.Outlined.Groups,
                                trailing = { if (draft.target == PhotoShareTarget.Circle(circle.id)) Text("✓") },
                                onClick = { onDraft(draft.copy(target = PhotoShareTarget.Circle(circle.id))) },
                            )
                        }
                    }
                    if (targets.people.isNotEmpty()) {
                        Text(stringResource(R.string.photos_share_with_person), style = MaterialTheme.typography.titleSmall, modifier = Modifier.padding(top = 8.dp))
                        for (person in targets.people) {
                            TeaNodeRow(
                                headline = person.displayName, leading = { AvatarView(person.displayName, person.avatarUrl, person.avatarColorIndex, size = 32.dp) },
                                trailing = { if (draft.target == PhotoShareTarget.Person(person.userId)) Text("✓") },
                                onClick = { onDraft(draft.copy(target = PhotoShareTarget.Person(person.userId))) },
                            )
                        }
                    }
                    Text(stringResource(R.string.photos_share_what_they_can_do), style = MaterialTheme.typography.titleSmall, modifier = Modifier.padding(top = 8.dp))
                    for (level in PhotoShareLevel.entries) {
                        TeaNodeRow(
                            headline = stringResource(if (level == PhotoShareLevel.Contribute) R.string.photos_level_contribute else R.string.photos_level_view),
                            trailing = { if (draft.photoShareLevel == level) Text("✓") }, onClick = { onDraft(draft.copy(photoShareLevel = level)) },
                        )
                    }
                }
            }
        },
        confirmButton = { TextButton(onClick = onShare, enabled = draft.canSubmit) { Text(stringResource(R.string.photos_share)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
