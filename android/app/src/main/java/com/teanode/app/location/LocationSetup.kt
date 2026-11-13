package com.teanode.app.location

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.*
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.kit.location.LocationShareLevel
import kotlinx.coroutines.launch

/** Setting Location up the first time, in place of the lists: rows that each finish in one tap and show as done when they are. */
@Composable
fun LocationSetupList(model: LocationModel, isShowingPeople: Boolean, onOpenPeople: () -> Unit, onShowSharing: () -> Unit, onOpenHomeAddress: () -> Unit) {
    val recording = LocalLocationRecording.current
    val permission by recording.permission.collectAsState()
    val isMotionAuthorized by recording.isMotionAuthorized.collectAsState()
    val text = rememberLocationText()
    val scope = rememberCoroutineScope()
    var isWorking by remember { mutableStateOf(false) }
    val setup = model.setup
    val canLocate = permission == LocationPermissionState.Always || permission == LocationPermissionState.WhileUsing
    fun run(work: suspend () -> Unit) {
        isWorking = true
        scope.launch {
            work()
            isWorking = false
        }
    }
    LazyColumn(Modifier.fillMaxWidth()) {
        if (isShowingPeople) {
            item { LocationSharingBannerCard(model = model, onShowSharing = onShowSharing) }
            if (model.location != null || model.people.isNotEmpty()) item { LocationPeopleCard(model = model, onOpenPeople = onOpenPeople) }
        }
        item { Text(stringResource(R.string.location_set_up_location), Modifier.padding(16.dp), style = MaterialTheme.typography.titleMedium) }
        item {
            LocationSetupRow(
                stringResource(R.string.location_allow_location), stringResource(R.string.location_allow_location_detail), Icons.Outlined.LocationOn,
                isDone = setup.isPermissionDone, isEnabled = !isWorking,
            ) { recording.requestPermission() }
        }
        if (recording.isMotionAvailable) {
            item {
                LocationSetupRow(
                    stringResource(R.string.location_allow_motion), stringResource(if (canLocate) R.string.location_allow_motion_detail else R.string.location_allow_location_first),
                    Icons.AutoMirrored.Outlined.DirectionsWalk, isDone = isMotionAuthorized, isEnabled = !isWorking && canLocate,
                ) { recording.requestMotionPermission() }
            }
        }
        item {
            LocationSetupRow(
                stringResource(R.string.location_this_is_home), stringResource(if (canLocate) R.string.location_this_is_home_detail else R.string.location_allow_location_first),
                Icons.Outlined.Home, isDone = setup.isHomeDone, isEnabled = !isWorking && canLocate,
            ) { run { model.markHome() } }
        }
        item {
            LocationSetupRow(
                stringResource(R.string.location_type_my_address), stringResource(R.string.location_type_my_address_detail), Icons.Outlined.Search, isDone = setup.isHomeDone,
                isEnabled = !isWorking, onClick = onOpenHomeAddress,
            )
        }
        items(setup.familyCircles, key = { circle -> circle.id }) { circle ->
            val isShared = setup.isShared(circle)
            Column {
                LocationSetupRow(
                    title = if (isShared) stringResource(R.string.location_shared_with, circle.name) else stringResource(R.string.location_share_history_with, circle.name),
                    detail = if (isShared) text.levelTitle(LocationShareLevel.History) else text.levelExplanation(LocationShareLevel.History),
                    icon = Icons.Outlined.People, isDone = isShared, isEnabled = !isWorking,
                ) { run { model.shareWithFamily(circle, LocationShareLevel.History) } }
                if (!isShared) {
                    TextButton(onClick = { run { model.shareWithFamily(circle, LocationShareLevel.Current) } }, enabled = !isWorking, modifier = Modifier.padding(horizontal = 56.dp)) {
                        Icon(Icons.Outlined.NearMe, contentDescription = null)
                        Text(stringResource(R.string.location_share_current_with, circle.name), Modifier.padding(start = 8.dp))
                    }
                }
            }
        }
        item { Text(stringResource(R.string.location_setup_footer), Modifier.padding(16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        item {
            TextButton(onClick = { model.dismissSetup() }, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.location_not_now)) }
            Text(stringResource(R.string.location_not_now_footer), Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
fun LocationSetupRow(title: String, detail: String, icon: ImageVector, isDone: Boolean, isEnabled: Boolean = true, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().then(if (!isDone && isEnabled) Modifier.clickable(onClick = onClick) else Modifier).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            if (isDone) Icons.Outlined.CheckCircle else icon, contentDescription = null,
            tint = if (isDone) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.primary, modifier = Modifier.width(28.dp),
        )
        Column(Modifier.weight(1f).padding(start = 12.dp)) {
            Text(title, style = MaterialTheme.typography.bodyLarge, color = if (isDone || !isEnabled) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface)
            Text(detail, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}
