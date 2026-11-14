package com.teanode.app.location

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.*
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.AvatarView
import com.teanode.kit.location.LocationPauseChoice
import com.teanode.kit.location.LocationPlace
import com.teanode.kit.location.LocationShareLevel
import com.teanode.kit.location.PersonLocation
import kotlinx.coroutines.launch

/** Who the person chosen on the map is, the way to mute their notices, and the way back to everyone. */
@Composable
fun LocationChosenPersonHeader(person: PersonLocation, close: () -> Unit, mute: (LocationPauseChoice) -> Unit, unmute: () -> Unit) {
    val text = rememberLocationText()
    var isMuteMenuOpen by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp).padding(bottom = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            AvatarView(person.displayName, person.avatarUrl, person.avatarColorIndex, size = 36.dp)
            Column(Modifier.weight(1f).padding(start = 12.dp)) {
                Text(person.displayName, style = MaterialTheme.typography.titleMedium)
                Text(text.whereText(person), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Box {
                IconButton(onClick = { isMuteMenuOpen = true }) { Icon(Icons.Outlined.NotificationsOff, contentDescription = stringResource(R.string.location_mute_notifications), tint = MaterialTheme.colorScheme.primary) }
                DropdownMenu(expanded = isMuteMenuOpen, onDismissRequest = { isMuteMenuOpen = false }) {
                    LocationPauseChoice.entries.forEach { choice ->
                        DropdownMenuItem(text = { Text(text.muteChoiceTitle(choice)) }, onClick = { isMuteMenuOpen = false; mute(choice) })
                    }
                }
            }
            IconButton(onClick = close) { Icon(Icons.Outlined.Close, contentDescription = stringResource(R.string.location_close)) }
        }
        val mute = person.notificationMute
        if (mute != null && person.isNotificationMuted()) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Outlined.NotificationsOff, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(text.mutedUntil(mute), Modifier.weight(1f).padding(horizontal = 6.dp), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                TextButton(onClick = unmute) {
                    Icon(Icons.Outlined.Notifications, contentDescription = null)
                    Text(stringResource(R.string.location_unmute), Modifier.padding(start = 6.dp))
                }
            }
        }
    }
}

/** The visits and places of the person chosen: their day when they share their history, else a line saying only where they are is shared. */
@Composable
fun LocationChosenPersonVisits(state: PersonVisitsState, person: PersonLocation, onOpenPlace: (LocationPlace) -> Unit, onOpenPersonTravel: ((String) -> Unit)? = null) {
    if (person.level == LocationShareLevel.History) {
        LocationPersonVisitsList(state, onOpenPlace, onOpenTravel = onOpenPersonTravel?.let { open -> { open(person.userId) } })
    } else {
        Text(stringResource(R.string.location_shares_where_now_only, person.displayName), Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** One day of somebody sharing with the person: the day's visits, pauses and trips with a way to move between days, and their places. */
@Composable
fun LocationPersonVisitsList(state: PersonVisitsState, onOpenPlace: (LocationPlace) -> Unit, onOpenTravel: (() -> Unit)? = null) {
    val text = rememberLocationText()
    val scope = rememberCoroutineScope()
    LaunchedEffect(state) { if (state.readDay == null) state.load() }
    LazyColumn(Modifier.fillMaxWidth()) {
        item { Text(text.dayTitle(state.day), Modifier.padding(16.dp), style = MaterialTheme.typography.titleMedium) }
        item {
            Row(Modifier.padding(horizontal = 8.dp)) {
                TextButton(onClick = { scope.launch { state.move(-1) } }) {
                    Icon(Icons.AutoMirrored.Outlined.KeyboardArrowLeft, contentDescription = null)
                    Text(stringResource(R.string.location_previous_day))
                }
                if (state.canMoveLater) {
                    TextButton(onClick = { scope.launch { state.move(1) } }) {
                        Text(stringResource(R.string.location_next_day))
                        Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null)
                    }
                }
            }
        }
        val problem = state.problemText
        if (problem != null) {
            item { Text(problem, Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
        } else if (state.entries.isEmpty()) {
            item {
                if (state.isLoading) Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                else Text(stringResource(R.string.location_no_visits_this_day), Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        items(state.entries, key = { entry -> entry.id }) { entry -> LocationTimelineRow(entry) }
        if (onOpenTravel != null) {
            item {
                com.teanode.app.ui.components.TeaNodeRow(
                    headline = stringResource(R.string.location_travel), leadingIcon = Icons.AutoMirrored.Outlined.DirectionsWalk, onClick = onOpenTravel,
                )
            }
        }
        if (state.places.isNotEmpty()) {
            item { com.teanode.app.ui.components.TeaNodeSectionHeader(stringResource(R.string.location_places)) }
            items(state.places, key = { place -> place.id }) { place -> LocationPlaceRow(place) }
        }
        item { OpenStreetMapCredit() }
    }
    text.hashCode()
    FontWeight.Normal.hashCode()
}
