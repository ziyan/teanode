package com.teanode.app.charts

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * ChartPalette is the colors a chart's series take, in the iPhone app's order (blue, orange, purple, cyan, pink, yellow,
 * indigo, brown), so the same kind of data is the same color on both. Shared by Health's charts and Finance's.
 */
object ChartPalette {
    val seriesColors: List<Color> = listOf(
        Color(0xFF3478F6), Color(0xFFFF9500), Color(0xFFAF52DE), Color(0xFF32ADE6), Color(0xFFFF2D55), Color(0xFFFFCC00), Color(0xFF5856D6), Color(0xFFA2845E),
    )

    /** The color of what the chart has no series for (the rest). */
    val restColor: Color = Color(0xFF8E8E93)

    fun seriesColor(index: Int): Color = seriesColors[index % seriesColors.size]

    /** How opaque a mark is: all of them when none is chosen, and the chosen one alone at full strength. */
    fun markOpacity(isChosen: Boolean, isAnyChosen: Boolean): Float = if (!isAnyChosen || isChosen) 1f else 0.3f
}

/** A small round swatch of a series' color. */
@Composable
fun ChartSwatch(color: Color, modifier: Modifier = Modifier) {
    Box(modifier.size(10.dp).background(color, CircleShape))
}

/** One entry of a chart's key: a color and what it stands for. */
data class ChartKeyEntry(val label: String, val color: Color)

/** The key of a chart: its entries in a row that wraps when there is not room. */
@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
fun ChartKey(entries: List<ChartKeyEntry>, modifier: Modifier = Modifier) {
    FlowRow(modifier, horizontalArrangement = Arrangement.spacedBy(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        entries.forEach { entry ->
            Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                ChartSwatch(entry.color)
                Text(entry.label, Modifier.padding(start = 6.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

/** One figure of a chart's callout: an optional label with its color, and a value. */
data class ChartFigure(val label: String? = null, val valueText: String, val color: Color? = null)

/** What a touch on a chart says: the day, week or month, and its figures. */
@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
fun ChartCallout(title: String, figures: List<ChartFigure>, modifier: Modifier = Modifier) {
    Column(
        modifier.background(MaterialTheme.colorScheme.surfaceContainerHigh, RoundedCornerShape(10.dp)).border(0.5.dp, MaterialTheme.colorScheme.outlineVariant, RoundedCornerShape(10.dp))
            .padding(horizontal = 10.dp, vertical = 6.dp),
    ) {
        Text(title, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            figures.forEach { figure ->
                Column {
                    if (figure.label != null) {
                        Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                            figure.color?.let { color -> ChartSwatch(color, Modifier.padding(end = 4.dp)) }
                            Text(figure.label, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                    Text(figure.valueText, style = MaterialTheme.typography.bodyMedium.copy(fontWeight = FontWeight.SemiBold, fontSize = 14.sp))
                }
            }
        }
    }
}
