package com.teanode.app.feed

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.kit.feed.FeedAgendaEntry
import com.teanode.kit.feed.FeedAgendaKind
import com.teanode.kit.feed.FeedListRow
import com.teanode.kit.feed.FeedPicture
import com.teanode.kit.feed.FeedWeather

/** The icon of a symbol a row, an hour or a condition names, or of [fallback] when it names nothing. */
@Composable
private fun SymbolIcon(symbol: String, fallback: String, tint: Color, size: androidx.compose.ui.unit.Dp) {
    when (val picture = FeedPicture.of(symbol.ifEmpty { fallback })) {
        is FeedPicture.Symbol -> Icon(picture.kind.icon(), contentDescription = null, tint = tint, modifier = Modifier.size(size))
        is FeedPicture.Emoji -> Text(picture.text)
    }
}

/** A list card's rows, each with its symbol, title, detail and meta, told apart by a rule. A row with an address or a record opens it. */
@Composable
fun FeedListRows(rows: List<FeedListRow>, actions: FeedActions, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth()) {
        for ((index, row) in rows.withIndex()) {
            if (index > 0) HorizontalDivider()
            val address = row.linkAddress
            Row(
                Modifier.fillMaxWidth().then(if (row.isOpenable) Modifier.clickable(role = Role.Button) { if (address != null) actions.followLink(address) } else Modifier).padding(vertical = 8.dp),
                horizontalArrangement = Arrangement.spacedBy(10.dp),
                verticalAlignment = Alignment.Top,
            ) {
                Column(Modifier.widthIn(min = 24.dp).padding(top = 2.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                    SymbolIcon(row.rowSymbol, "doc.text", MaterialTheme.colorScheme.onSurfaceVariant, 20.dp)
                }
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(row.rowTitle, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.SemiBold)
                    if (row.rowDetail.isNotEmpty()) Text(row.rowDetail, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    if (row.rowMeta.isNotEmpty()) Text(row.rowMeta, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                if (row.isOpenable) Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null, tint = MaterialTheme.colorScheme.outline, modifier = Modifier.padding(top = 2.dp).size(20.dp))
            }
        }
    }
}

/** A weather card: the temperature now, large, with the condition's symbol, the words and the day's high and low, and a strip of the hours ahead. */
@Composable
fun FeedWeather(weather: FeedWeather, modifier: Modifier = Modifier) {
    val highLowText = run {
        val high = weather.shown(weather.highTemperature)
        val low = weather.shown(weather.lowTemperature)
        when {
            high.isNotEmpty() && low.isNotEmpty() -> stringResource(R.string.feed_weather_high_low, high, low)
            high.isNotEmpty() -> stringResource(R.string.feed_weather_high, high)
            low.isNotEmpty() -> stringResource(R.string.feed_weather_low, low)
            else -> ""
        }
    }
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        if (weather.placeName.isNotEmpty()) Text(weather.placeName, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
            SymbolIcon(weather.conditionSymbol, "cloud.sun", MaterialTheme.colorScheme.onSurface, 36.dp)
            Text(weather.shown(weather.currentTemperature), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.SemiBold)
            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                if (weather.conditionText.isNotEmpty()) Text(weather.conditionText, style = MaterialTheme.typography.bodyMedium)
                if (highLowText.isNotEmpty()) Text(highLowText, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        if (weather.hourlyForecast.isNotEmpty()) {
            Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(vertical = 2.dp), horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                for (hour in weather.hourlyForecast) {
                    Column(Modifier.widthIn(min = 44.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        Text(hour.hourText(), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        SymbolIcon(hour.forecastSymbol, "cloud", MaterialTheme.colorScheme.onSurface, 24.dp)
                        Text(weather.shown(hour.forecastTemperature), style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.SemiBold)
                        if (hour.precipitationChancePercent > 0) {
                            val description = stringResource(R.string.feed_rain_chance, hour.precipitationChancePercent)
                            Text("${hour.precipitationChancePercent}%", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.semantics { contentDescription = description })
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun FeedAgendaKind.words(): String = stringResource(
    when (this) {
        FeedAgendaKind.Meeting -> R.string.feed_agenda_meeting
        FeedAgendaKind.Focus -> R.string.feed_agenda_focus
        FeedAgendaKind.Task -> R.string.feed_agenda_task
        FeedAgendaKind.Break -> R.string.feed_agenda_break
    },
)

/** A day plan: a vertical timeline of entries, each with its start and end, a marker colored by its kind, its title, its kind in words and its detail. */
@Composable
fun FeedAgenda(entries: List<FeedAgendaEntry>, actions: FeedActions, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        for (entry in entries) {
            val address = entry.linkAddress
            val kindWords = entry.kind.words()
            val markerColor = when (entry.kind) {
                FeedAgendaKind.Meeting -> MaterialTheme.colorScheme.primary
                FeedAgendaKind.Focus -> MaterialTheme.colorScheme.onSurface
                FeedAgendaKind.Task -> MaterialTheme.colorScheme.outline
                FeedAgendaKind.Break -> MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.5f)
            }
            Row(
                Modifier.fillMaxWidth().height(IntrinsicSize.Min).then(if (entry.recordReference != null) Modifier.clickable(role = Role.Button) { if (address != null) actions.followLink(address) } else Modifier),
                horizontalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                Column(Modifier.widthIn(min = 62.dp).width(IntrinsicSize.Max), horizontalAlignment = Alignment.End) {
                    entry.startText()?.let { text -> Text(text, style = MaterialTheme.typography.labelMedium, fontWeight = FontWeight.SemiBold, softWrap = false, textAlign = TextAlign.End) }
                    entry.endText()?.let { text -> Text(text, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, softWrap = false, textAlign = TextAlign.End) }
                }
                Column(Modifier.width(4.dp).fillMaxHeight().background(markerColor, RoundedCornerShape(50))) {}
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(entry.agendaTitle, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.SemiBold)
                    Text(
                        if (entry.agendaDetail.isEmpty()) kindWords else "$kindWords · ${entry.agendaDetail}",
                        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}
