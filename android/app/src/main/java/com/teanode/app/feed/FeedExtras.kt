package com.teanode.app.feed

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Videocam
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.drawText
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.teanode.app.R
import com.teanode.kit.feed.AgentFeedChartBlock
import com.teanode.kit.feed.AgentFeedChartSeries
import com.teanode.kit.feed.AgentFeedItemView
import com.teanode.kit.feed.AgentFeedMediaView
import com.teanode.kit.feed.AgentFeedSeriesRole

/** The green of a cleared problem: dark enough on the light card, light enough on the dark one. */
@Composable
fun feedSuccessColor(): Color = if (isFeedDark()) Color(0xFF7BD79A) else Color(0xFF1B6E3A)

/** The chip in a resolved card's header: a check and "Resolved 06:41", the time in the person's own zone. */
@Composable
fun FeedResolvedChip(item: AgentFeedItemView, modifier: Modifier = Modifier) {
    val timeText = item.resolvedTimeText()
    val words = if (timeText.isEmpty()) stringResource(R.string.feed_resolved) else stringResource(R.string.feed_resolved_chip, timeText)
    val color = feedSuccessColor()
    Surface(modifier, shape = CircleShape, color = color.copy(alpha = 0.14f), contentColor = color) {
        Row(Modifier.padding(horizontal = 8.dp, vertical = 3.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            Icon(Icons.Outlined.CheckCircle, contentDescription = null, modifier = Modifier.size(with(LocalDensity.current) { 14.sp.toDp() }))
            Text(words, style = MaterialTheme.typography.labelMedium, fontWeight = FontWeight.SemiBold, maxLines = 1)
        }
    }
}

/** The note a resolved card carries, as one muted line under its body. */
@Composable
fun FeedResolutionNote(item: AgentFeedItemView) {
    if (item.resolutionNote.isNotEmpty()) Text(item.resolutionNote, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

/** What an answered card shows instead of itself: its title, one muted line (the choice and what came of it) and "Show the card". */
@Composable
fun FeedFoldedAnswer(item: AgentFeedItemView, onShow: () -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(item.title, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
        if (item.foldedAnswerText.isNotEmpty()) Text(item.foldedAnswerText, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        TextButton(onClick = onShow, contentPadding = PaddingValues(start = 0.dp, top = 12.dp, end = 12.dp, bottom = 12.dp)) { Text(stringResource(R.string.feed_show_card)) }
    }
}

/**
 * A card's still from one of the person's cameras: the picture across the card's width, tapping it opens the recording of
 * that moment when there is one; under it the caption, "<camera> · 14:02:11" on the camera's own clock (its zone is named
 * when it is not the phone's), why this camera, and "Watch live" when the camera's live view is known.
 */
@Composable
fun FeedCameraStill(media: AgentFeedMediaView, actions: FeedActions, modifier: Modifier = Modifier) {
    val playbackAddress = media.playbackAddress
    val liveAddress = media.liveAddress
    val cameraLine = listOf(media.cameraLabel, media.capturedAtText()).filter { part -> part.isNotEmpty() }
    val description = listOf(stringResource(R.string.feed_media_camera_still), media.caption, cameraLine.joinToString(", "), media.reasonText).filter { part -> part.isNotEmpty() }.joinToString(". ")
    val pressHint = stringResource(R.string.feed_still_opens_recording)
    Column(modifier.fillMaxWidth()) {
        Box(Modifier.fillMaxWidth().aspectRatio(media.singleAspectRatio).background(MaterialTheme.colorScheme.surfaceContainerHighest)) {
            FeedMediaImage(
                media.imagePath, description,
                if (playbackAddress != null) Modifier.fillMaxSize().clickable(role = Role.Button, onClickLabel = pressHint) { actions.followLink(playbackAddress) } else Modifier.fillMaxSize(),
            )
        }
        Column(Modifier.padding(start = 16.dp, end = 16.dp, top = 10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            if (media.caption.isNotEmpty()) Text(media.caption, style = MaterialTheme.typography.bodyMedium)
            if (cameraLine.isNotEmpty()) {
                Text(cameraLine.joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (media.reasonText.isNotEmpty()) Text(media.reasonText, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            if (liveAddress != null) {
                OutlinedButton(onClick = { actions.followLink(liveAddress) }, modifier = Modifier.padding(top = 4.dp)) {
                    Icon(Icons.Outlined.Videocam, contentDescription = null, modifier = Modifier.size(18.dp))
                    Text(stringResource(R.string.feed_watch_live), modifier = Modifier.padding(start = 6.dp))
                }
            }
        }
    }
}

/** The charts of a card, each with its title and a small muted line saying where and when the numbers were read. */
@Composable
fun FeedCharts(charts: List<AgentFeedChartBlock>) {
    for (chart in charts) FeedChart(chart)
}

@Composable
fun FeedChart(chart: AgentFeedChartBlock, modifier: Modifier = Modifier) {
    val description = remember(chart) { chart.description() }
    // One node for a screen reader: the title, the numbers and where they were read, not the pieces of the drawing.
    Column(modifier.fillMaxWidth().clearAndSetSemantics { contentDescription = description }, verticalArrangement = Arrangement.spacedBy(6.dp)) {
        val rangeText = if (chart.isBars) "" else chart.rangeText()
        if (chart.chartTitle.isNotEmpty()) Text(chart.chartTitle, style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold)
        // The numbers the frame spans, since the lines run over its rules.
        if (rangeText.isNotEmpty()) Text(rangeText, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        if (chart.isBars) FeedBarsChart(chart) else FeedSeriesChart(chart)
        if (chart.provenance.isNotEmpty()) Text(chart.provenance, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** The numbers the frame of a series chart spans, "1,150 to 2,500 USD"; empty when no line has a number. */
private fun AgentFeedChartBlock.rangeText(): String {
    val known = series.flatMap { line -> line.values.filterNotNull() }
    if (known.isEmpty()) return ""
    val low = FeedNumbers.text(known.min())
    val high = FeedNumbers.text(known.max())
    val unitText = if (unit.isEmpty()) "" else " $unit"
    return if (low == high) high + unitText else "$low \u2192 $high$unitText"
}

/** The color and stroke of one line: a reference is solid in the text color, a projection dashed, a measured line in the accent. */
private class SeriesLook(val color: Color, val isDashed: Boolean)

@Composable
private fun seriesLooks(series: List<AgentFeedChartSeries>): List<SeriesLook> {
    val ink = MaterialTheme.colorScheme.onSurface
    val accent = MaterialTheme.colorScheme.primary
    val second = MaterialTheme.colorScheme.tertiary
    var measuredCount = 0
    return series.map { line ->
        when (line.role) {
            AgentFeedSeriesRole.Reference -> SeriesLook(ink, false)
            AgentFeedSeriesRole.Projection -> SeriesLook(accent, true)
            AgentFeedSeriesRole.Measured -> SeriesLook(if (measuredCount++ == 0) accent else second, false)
        }
    }
}

private val PLOT_HEIGHT = 132.dp

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun FeedSeriesChart(chart: AgentFeedChartBlock) {
    val looks = seriesLooks(chart.series)
    val gridColor = MaterialTheme.colorScheme.outlineVariant
    val labelColor = MaterialTheme.colorScheme.onSurfaceVariant
    val ruleColor = MaterialTheme.colorScheme.outline
    val measurer = rememberTextMeasurer()
    val labelStyle = MaterialTheme.typography.labelSmall.copy(color = labelColor)
    val markerStyle = MaterialTheme.typography.labelSmall.copy(color = MaterialTheme.colorScheme.onSurface, fontWeight = FontWeight.SemiBold)
    val known = chart.series.flatMap { line -> line.values.filterNotNull() }
    val lowest = known.minOrNull() ?: 0.0
    val highest = known.maxOrNull() ?: 1.0
    // A flat line gets a little room either side, so it is drawn in the middle and not on the frame.
    val span = if (highest - lowest < 1e-9) maxOf(1.0, Math.abs(highest) * 0.1) else highest - lowest
    val bottom = if (highest - lowest < 1e-9) lowest - span / 2 else lowest
    val top = bottom + span
    val markerLines = chart.placedMarkers
    val density = LocalDensity.current
    val labelHeightPixels = remember(labelStyle, density.fontScale) { measurer.measure("0", labelStyle).size.height }
    val topInset = if (markerLines.isEmpty()) 0f else labelHeightPixels * 1.4f
    val bottomInset = labelHeightPixels * 1.6f
    val plotHeightPixels = with(density) { PLOT_HEIGHT.toPx() }
    val heightDp = with(density) { (plotHeightPixels + topInset + bottomInset).toDp() }
    Canvas(Modifier.fillMaxWidth().height(heightDp)) {
        val plotTop = topInset
        val plotBottom = size.height - bottomInset
        val plotHeight = plotBottom - plotTop
        val count = chart.xLabels.size
        val horizontalPadding = 6.dp.toPx()
        val stepWidth = (size.width - horizontalPadding * 2) / (count - 1)
        fun xAt(position: Int) = horizontalPadding + stepWidth * position
        fun yAt(value: Double) = plotBottom - ((value - bottom) / (top - bottom)).toFloat() * plotHeight
        // Three rules across, the top and bottom ones named with the range of the numbers.
        for (fraction in listOf(0f, 0.5f, 1f)) {
            val y = plotTop + plotHeight * fraction
            drawLine(gridColor, Offset(0f, y), Offset(size.width, y), strokeWidth = 1.dp.toPx())
        }
        for ((position, marker) in markerLines) {
            val x = xAt(position)
            drawLine(ruleColor, Offset(x, plotTop), Offset(x, plotBottom), strokeWidth = 1.5.dp.toPx())
            val layout = measurer.measure(marker.markerLabel, markerStyle, maxLines = 1, overflow = TextOverflow.Ellipsis)
            drawText(layout, topLeft = Offset((x - layout.size.width / 2f).coerceIn(0f, (size.width - layout.size.width).coerceAtLeast(0f)), 0f))
        }
        for ((index, line) in chart.series.withIndex()) drawSeriesLine(line, looks[index], ::xAt, ::yAt)
        // The axis: the first and last label, and the middle one when there is room for it.
        val axisIndexes = if (count >= 5) listOf(0, count / 2, count - 1) else listOf(0, count - 1)
        for (position in axisIndexes) {
            val layout = measurer.measure(chart.xLabels[position], labelStyle, maxLines = 1, overflow = TextOverflow.Ellipsis)
            val x = (xAt(position) - layout.size.width / 2f).coerceIn(0f, (size.width - layout.size.width).coerceAtLeast(0f))
            drawText(layout, topLeft = Offset(x, plotBottom + labelHeightPixels * 0.35f))
        }
    }
    if (chart.hasLegend) {
        FlowRow(horizontalArrangement = Arrangement.spacedBy(14.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            for ((index, line) in chart.series.withIndex()) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    Canvas(Modifier.width(18.dp).height(8.dp)) {
                        drawLine(looks[index].color, Offset(0f, size.height / 2), Offset(size.width, size.height / 2), strokeWidth = 2.dp.toPx(), cap = StrokeCap.Round, pathEffect = if (looks[index].isDashed) dashEffect() else null)
                    }
                    Text(line.seriesName, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
    }
}

private fun DrawScope.dashEffect(): PathEffect = PathEffect.dashPathEffect(floatArrayOf(5.dp.toPx(), 4.dp.toPx()))

/** One line over the plot: pieces of it are drawn between the numbers it has, and a gap where a number is missing. */
private fun DrawScope.drawSeriesLine(line: AgentFeedChartSeries, look: SeriesLook, xAt: (Int) -> Float, yAt: (Double) -> Float) {
    val strokeWidth = 2.5.dp.toPx()
    val path = Path()
    var isPenDown = false
    val points = ArrayList<Offset>()
    for ((position, value) in line.values.withIndex()) {
        if (value == null) {
            isPenDown = false
            continue
        }
        val point = Offset(xAt(position), yAt(value))
        points.add(point)
        if (isPenDown) path.lineTo(point.x, point.y) else path.moveTo(point.x, point.y)
        isPenDown = true
    }
    drawPath(path, look.color, style = Stroke(strokeWidth, cap = StrokeCap.Round, pathEffect = if (look.isDashed) dashEffect() else null))
    // A number on its own, with gaps either side, would not show as a line, so every number gets a dot.
    for (point in points) drawCircle(look.color, radius = strokeWidth * if (look.isDashed) 0.7f else 0.9f, center = point)
}

@Composable
private fun FeedBarsChart(chart: AgentFeedChartBlock) {
    val bars = chart.orderedBars
    val largest = bars.maxOf { bar -> bar.barValue }.takeIf { value -> value > 0 } ?: 1.0
    val track = MaterialTheme.colorScheme.surfaceContainerHighest
    val accent = MaterialTheme.colorScheme.primary
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        for (bar in bars) {
            val valueText = FeedNumbers.text(bar.barValue).let { number -> if (chart.unit.isEmpty()) number else stringResource(R.string.feed_chart_unit_value, number, chart.unit) }
            Column(verticalArrangement = Arrangement.spacedBy(3.dp)) {
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    Text(bar.barLabel, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
                    Text(valueText, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Bold)
                }
                Box(Modifier.fillMaxWidth().height(8.dp).clip(RoundedCornerShape(4.dp)).background(track)) {
                    Box(Modifier.fillMaxHeight().fillMaxWidth((bar.barValue / largest).toFloat().coerceIn(0.02f, 1f)).clip(RoundedCornerShape(4.dp)).background(accent))
                }
            }
        }
    }
}

/** Numbers as the charts write them. */
object FeedNumbers {
    fun text(value: Double): String = AgentFeedChartBlock.trimmed(value)
}
