package com.teanode.app.feed

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckBoxOutlineBlank
import androidx.compose.material.icons.outlined.FreeBreakfast
import androidx.compose.material.icons.outlined.Groups
import androidx.compose.material.icons.outlined.Psychology
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.kit.feed.FeedAgendaEntry
import com.teanode.kit.feed.FeedAgendaKind
import com.teanode.kit.feed.FeedListRow
import com.teanode.kit.feed.FeedPicture
import com.teanode.kit.feed.FeedPictureKind
import com.teanode.kit.feed.FeedWeatherContent
import com.teanode.kit.feed.feedLinkAddress

/** The icon of a symbol a row, an hour or a condition names; the newspaper when it names nothing. */
@Composable
private fun SymbolIcon(symbol: String, tint: Color, modifier: Modifier = Modifier) {
    when (val picture = FeedPicture.of(symbol)) {
        is FeedPicture.Symbol -> Icon(picture.kind.icon(), contentDescription = null, tint = tint, modifier = modifier)
        is FeedPicture.Emoji -> Text(picture.text, modifier = modifier)
    }
}

/** A list card's rows: an icon, the title, a line of detail and the source or place and time; a row with an address or a record opens it. */
@Composable
fun FeedListRows(rows: List<FeedListRow>, actions: FeedActions, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth()) {
        for (row in rows) {
            val address = row.linkAddress
            Row(
                Modifier.fillMaxWidth().then(if (address != null) Modifier.clickable(role = Role.Button) { actions.followLink(address) } else Modifier).padding(vertical = 8.dp),
                horizontalArrangement = Arrangement.spacedBy(12.dp),
                verticalAlignment = Alignment.Top,
            ) {
                Box(Modifier.size(32.dp).background(MaterialTheme.colorScheme.surfaceContainerHighest, CircleShape), contentAlignment = Alignment.Center) {
                    SymbolIcon(row.rowSymbol, MaterialTheme.colorScheme.onSurfaceVariant, Modifier.size(18.dp))
                }
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(row.rowTitle, style = MaterialTheme.typography.bodyLarge, fontWeight = FontWeight.Medium, maxLines = 3, overflow = TextOverflow.Ellipsis)
                    if (row.rowDetail.isNotEmpty()) Text(row.rowDetail, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 3, overflow = TextOverflow.Ellipsis)
                    if (row.rowMeta.isNotEmpty()) Text(row.rowMeta, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.outline, maxLines = 2, overflow = TextOverflow.Ellipsis)
                }
            }
        }
    }
}

/** A weather card: the temperature large, the condition, the day's high and low, and the hours ahead in a strip that scrolls sideways. */
@Composable
fun FeedWeather(weather: FeedWeatherContent, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(16.dp), verticalAlignment = Alignment.CenterVertically) {
            if (weather.currentTemperature != null) {
                Text(weather.currentText + weather.temperatureUnit, style = MaterialTheme.typography.displayMedium)
            }
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                    if (weather.conditionSymbol.isNotEmpty()) SymbolIcon(weather.conditionSymbol, MaterialTheme.colorScheme.primary, Modifier.size(24.dp))
                    Text(weather.conditionText, style = MaterialTheme.typography.titleMedium)
                }
                if (weather.placeName.isNotEmpty()) Text(weather.placeName, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                if (weather.highTemperature != null && weather.lowTemperature != null) {
                    Text(
                        stringResource(R.string.feed_weather_high_low, weather.degrees(weather.highTemperature), weather.degrees(weather.lowTemperature)),
                        style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
        if (weather.hourlyForecast.isNotEmpty()) {
            Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                for (hour in weather.hourlyForecast) {
                    Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        Text(hour.hourLabel, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        SymbolIcon(hour.forecastSymbol, MaterialTheme.colorScheme.primary, Modifier.size(24.dp))
                        Text(weather.degrees(hour.forecastTemperature), style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
                        hour.precipitationChancePercent?.takeIf { percent -> percent > 0 }?.let { percent ->
                            Text("$percent%", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.tertiary)
                        }
                    }
                }
            }
        }
    }
}

private fun FeedAgendaKind.icon(): ImageVector = when (this) {
    FeedAgendaKind.Meeting -> Icons.Outlined.Groups
    FeedAgendaKind.Focus -> Icons.Outlined.Psychology
    FeedAgendaKind.Task -> Icons.Outlined.CheckBoxOutlineBlank
    FeedAgendaKind.Break -> Icons.Outlined.FreeBreakfast
    FeedAgendaKind.Other -> FeedPictureKind.Time.icon()
}

@Composable
private fun FeedAgendaKind.words(): String? = when (this) {
    FeedAgendaKind.Meeting -> stringResource(R.string.feed_agenda_meeting)
    FeedAgendaKind.Focus -> stringResource(R.string.feed_agenda_focus)
    FeedAgendaKind.Task -> stringResource(R.string.feed_agenda_task)
    FeedAgendaKind.Break -> stringResource(R.string.feed_agenda_break)
    FeedAgendaKind.Other -> null
}

/** A day's plan as a timeline: the start (and end) time at the left, a line with a dot per entry, and the entry's title and detail. */
@Composable
fun FeedAgenda(entries: List<FeedAgendaEntry>, actions: FeedActions, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth()) {
        for ((index, entry) in entries.withIndex()) {
            val address = entry.recordReference?.feedLinkAddress
            val isLast = index == entries.lastIndex
            Row(
                Modifier.fillMaxWidth().height(IntrinsicSize.Min).then(if (address != null) Modifier.clickable(role = Role.Button) { actions.followLink(address) } else Modifier),
                horizontalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                Column(Modifier.width(IntrinsicSize.Max).padding(top = 2.dp)) {
                    Text(entry.startText, style = MaterialTheme.typography.labelLarge, fontWeight = FontWeight.SemiBold, maxLines = 1, softWrap = false)
                    if (entry.endText.isNotEmpty()) Text(entry.endText, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.outline, maxLines = 1, softWrap = false)
                }
                Column(Modifier.fillMaxHeight().width(14.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                    Box(Modifier.padding(top = 6.dp).size(10.dp).background(if (entry.kind == FeedAgendaKind.Break) MaterialTheme.colorScheme.outline else MaterialTheme.colorScheme.primary, CircleShape))
                    if (!isLast) Box(Modifier.width(2.dp).weight(1f).background(MaterialTheme.colorScheme.outlineVariant))
                }
                Column(Modifier.weight(1f).padding(bottom = if (isLast) 0.dp else 12.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.Top) {
                        val words = entry.kind.words()
                        Icon(entry.kind.icon(), contentDescription = words, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(top = 4.dp).size(16.dp))
                        Text(entry.agendaTitle, style = MaterialTheme.typography.bodyLarge, fontWeight = FontWeight.Medium, modifier = Modifier.weight(1f))
                    }
                    if (entry.agendaDetail.isNotEmpty()) Text(entry.agendaDetail, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
    }
}
