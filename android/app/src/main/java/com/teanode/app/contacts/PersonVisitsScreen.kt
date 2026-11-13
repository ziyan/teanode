package com.teanode.app.contacts

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowLeft
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material.icons.outlined.Place
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.mail.MailTimes
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.Connection
import com.teanode.kit.contacts.PersonPlace
import com.teanode.kit.contacts.PersonVisit
import com.teanode.kit.contacts.personPlaces
import com.teanode.kit.contacts.personVisits
import com.teanode.kit.mail.ServerTime
import java.time.LocalDate
import java.time.ZoneId

/**
 * The page the card's Location section opens for somebody who shares where they have been: their visits for a day, with a
 * way back and forth by the day, and their places, to read and not to change.
 */
@Composable
fun PersonVisitsScreen(connection: Connection, userId: String, onBack: () -> Unit, today: LocalDate = LocalDate.now()) {
    var day by remember { mutableStateOf(today) }
    var visits by remember { mutableStateOf(emptyList<PersonVisit>()) }
    var places by remember { mutableStateOf(emptyList<PersonPlace>()) }
    var isLoading by remember { mutableStateOf(true) }
    var failure by remember { mutableStateOf<Throwable?>(null) }
    var attempt by remember { mutableStateOf(0) }
    val zone = ZoneId.systemDefault()

    LaunchedEffect(day, attempt) {
        isLoading = true
        try {
            val from = ServerTime.startOfDayText(day, zone)
            val until = ServerTime.startOfDayText(day.plusDays(1), zone)
            visits = connection.personVisits(userId, from, until)
            places = connection.personPlaces(userId)
            failure = null
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            failure = failed
        } finally {
            isLoading = false
        }
    }

    TeaNodeScreen(title = stringResource(R.string.contacts_visits_and_places), onBack = onBack) { contentPadding ->
        val problem = failure
        if (problem != null && visits.isEmpty() && places.isEmpty()) {
            ErrorState(problem.message.orEmpty(), onRetry = { failure = null; attempt += 1 }, modifier = Modifier.padding(contentPadding))
            return@TeaNodeScreen
        }
        LazyColumn(Modifier.padding(contentPadding).fillMaxSize()) {
            item {
                Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.SpaceBetween) {
                    IconButton(onClick = { day = day.minusDays(1) }) { Icon(Icons.AutoMirrored.Outlined.KeyboardArrowLeft, stringResource(R.string.contacts_previous_day)) }
                    Text(MailTimes.wholeDate(day.atStartOfDay(zone).toInstant()), style = MaterialTheme.typography.titleMedium)
                    IconButton(onClick = { day = day.plusDays(1) }, enabled = day.isBefore(today)) { Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, stringResource(R.string.contacts_next_day)) }
                }
            }
            if (visits.isEmpty()) {
                item {
                    Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) {
                        if (isLoading) CircularProgressIndicator() else Text(stringResource(R.string.contacts_no_visits_this_day), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
            items(visits, key = { visit -> visit.id }) { visit ->
                val arrived = ServerTime.instant(visit.arrivedAt)
                val departed = ServerTime.instant(visit.departedAt)
                TeaNodeRow(
                    headline = visit.place.placeName.ifEmpty { stringResource(R.string.contacts_a_place) }, leadingIcon = Icons.Outlined.Place,
                    supporting = when {
                        arrived != null && departed != null -> MailTimes.timeOfDay(arrived) + " - " + MailTimes.timeOfDay(departed)
                        arrived != null -> stringResource(R.string.contacts_since_time, MailTimes.timeOfDay(arrived))
                        else -> ""
                    },
                )
            }
            if (places.isNotEmpty()) {
                item { TeaNodeSectionHeader(stringResource(R.string.contacts_places)) }
                items(places, key = { place -> "place-" + place.id }) { place ->
                    TeaNodeRow(headline = place.placeName.ifEmpty { place.addressText }, supporting = place.addressText.takeIf { address -> address.isNotEmpty() && address != place.placeName }, leadingIcon = Icons.Outlined.Place)
                }
            }
        }
    }
}
