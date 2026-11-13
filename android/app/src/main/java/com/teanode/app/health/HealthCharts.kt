package com.teanode.app.health

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextMeasurer
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.drawText
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.teanode.app.charts.ChartCallout
import com.teanode.app.charts.ChartKey
import com.teanode.app.charts.ChartPalette
import kotlin.math.max
import kotlin.math.min

/** The chart of a kind of Health data: bars, a night's stages stacked, or a level with its band. A touch on a period names it. */
@Composable
fun HealthChartView(plan: HealthChartPlan, selectedKey: String?, onSelect: (String?) -> Unit, modifier: Modifier = Modifier) {
    val selectedPlot = plan.plots.firstOrNull { plot -> plot.periodKey == selectedKey }
    Column(modifier.fillMaxWidth()) {
        if (plan.shape == HealthChartPlan.Shape.Stages) ChartKey(plan.stageKeyEntries, Modifier.padding(bottom = 8.dp))
        Box(Modifier.fillMaxWidth()) {
            HealthChartCanvas(plan, selectedKey, onSelect)
            if (selectedPlot != null) ChartCallout(selectedPlot.titleText, selectedPlot.figures, Modifier.align(Alignment.TopStart))
        }
    }
}

@Composable
private fun HealthChartCanvas(plan: HealthChartPlan, selectedKey: String?, onSelect: (String?) -> Unit) {
    val measurer = rememberTextMeasurer()
    val density = LocalDensity.current
    val axisColor = MaterialTheme.colorScheme.outlineVariant
    val labelColor = MaterialTheme.colorScheme.onSurfaceVariant
    val labelStyle = TextStyle(fontSize = 11.sp, color = labelColor)
    val description = plan.plots.joinToString("; ") { plot -> plot.titleText + ": " + plot.spokenText }
    val leftPadding = with(density) { 44.dp.toPx() }
    val bottomPadding = with(density) { 24.dp.toPx() }
    val topPadding = with(density) { 8.dp.toPx() }
    Canvas(
        Modifier.fillMaxWidth().height(220.dp).semantics { contentDescription = description }
            .pointerInput(plan) {
                detectTapGestures { tap -> onSelect(keyAt(plan, tap.x, size.width.toFloat(), leftPadding)?.takeIf { key -> key != selectedKey }) }
            }
            .pointerInput(plan) {
                detectDragGestures(onDragEnd = {}, onDrag = { change, _ -> onSelect(keyAt(plan, change.position.x, size.width.toFloat(), leftPadding)) })
            },
    ) {
        val chartWidth = size.width - leftPadding
        val chartHeight = size.height - bottomPadding - topPadding
        val keys = plan.periodKeys
        if (keys.isEmpty()) return@Canvas
        val slot = chartWidth / keys.size
        fun xOf(key: String): Float = leftPadding + keys.indexOf(key) * slot + slot / 2

        // The vertical range: from zero for what adds up, from the lowest reading for a level.
        val (low, high) = verticalRange(plan)
        fun yOf(value: Double): Float = (topPadding + chartHeight - ((value - low) / (high - low)).toFloat() * chartHeight)

        // The horizontal grid and its numbers.
        for (tick in 0..4) {
            val value = low + (high - low) * tick / 4
            val y = yOf(value)
            drawLine(axisColor, Offset(leftPadding, y), Offset(size.width, y), strokeWidth = 1f)
            drawLabel(measurer, plan.axisText(value), labelStyle, Offset(0f, y - 7.dp.toPx()))
        }
        for (key in plan.labelledKeys) {
            val label = plan.axisLabel(key)
            val measured = measurer.measure(label, labelStyle)
            val x = (xOf(key) - measured.size.width / 2f).coerceIn(leftPadding, size.width - measured.size.width)
            drawText(measured, topLeft = Offset(x, size.height - bottomPadding + 4.dp.toPx()))
        }
        val color = ChartPalette.seriesColor(0)
        when (plan.shape) {
            HealthChartPlan.Shape.Bars -> plan.plots.forEach { plot ->
                val amount = plot.amountValue ?: return@forEach
                val barWidth = max(2f, slot * 0.7f)
                val top = yOf(amount)
                drawRect(
                    color.copy(alpha = ChartPalette.markOpacity(plot.periodKey == selectedKey, selectedKey != null)), Offset(xOf(plot.periodKey) - barWidth / 2, top),
                    Size(barWidth, yOf(low) - top),
                )
            }
            HealthChartPlan.Shape.Stages -> {
                val barWidth = max(2f, slot * 0.7f)
                plan.stageSegments.groupBy { segment -> segment.periodKey }.forEach { (key, segments) ->
                    var bottom = yOf(0.0)
                    segments.forEach { segment ->
                        val top = bottom - (segment.hours / (high - low)).toFloat() * chartHeight
                        drawRect(
                            HealthChartPlan.color(segment.stage).copy(alpha = ChartPalette.markOpacity(key == selectedKey, selectedKey != null)),
                            Offset(xOf(key) - barWidth / 2, top), Size(barWidth, bottom - top),
                        )
                        bottom = top
                    }
                }
            }
            HealthChartPlan.Shape.Level -> {
                val band = Path()
                val withBand = plan.plots.filter { plot -> plot.lowestValue != null && plot.highestValue != null }
                if (withBand.size > 1) {
                    withBand.forEachIndexed { index, plot -> if (index == 0) band.moveTo(xOf(plot.periodKey), yOf(plot.highestValue!!)) else band.lineTo(xOf(plot.periodKey), yOf(plot.highestValue!!)) }
                    withBand.asReversed().forEach { plot -> band.lineTo(xOf(plot.periodKey), yOf(plot.lowestValue!!)) }
                    band.close()
                    drawPath(band, color.copy(alpha = 0.2f))
                }
                val line = Path()
                var isStarted = false
                plan.plots.forEach { plot ->
                    val amount = plot.amountValue ?: return@forEach
                    if (!isStarted) { line.moveTo(xOf(plot.periodKey), yOf(amount)); isStarted = true } else line.lineTo(xOf(plot.periodKey), yOf(amount))
                }
                drawPath(line, color, style = Stroke(width = 2.dp.toPx(), cap = StrokeCap.Round))
                plan.plots.firstOrNull { plot -> plot.periodKey == selectedKey }?.amountValue?.let { amount -> drawCircle(color, 5.dp.toPx(), Offset(xOf(selectedKey!!), yOf(amount))) }
            }
        }
        selectedKey?.let { key -> drawLine(axisColor, Offset(xOf(key), topPadding), Offset(xOf(key), topPadding + chartHeight), strokeWidth = 1.5f) }
    }
}

private fun DrawScope.drawLabel(measurer: TextMeasurer, text: String, style: TextStyle, topLeft: Offset) {
    drawText(measurer.measure(text, style), topLeft = topLeft)
}

private fun keyAt(plan: HealthChartPlan, x: Float, width: Float, leftPadding: Float): String? {
    val keys = plan.periodKeys
    if (keys.isEmpty()) return null
    val slot = (width - leftPadding) / keys.size
    val index = ((x - leftPadding) / slot).toInt().coerceIn(0, keys.size - 1)
    // Only a period that has something drawn can be chosen.
    return keys[index].takeIf { key -> plan.plots.any { plot -> plot.periodKey == key } }
}

/** The range the vertical axis covers: from zero for what adds up, a little around the readings for a level. */
private fun verticalRange(plan: HealthChartPlan): Pair<Double, Double> {
    return when (plan.shape) {
        HealthChartPlan.Shape.Level -> {
            val lowest = plan.plots.mapNotNull { plot -> plot.lowestValue ?: plot.amountValue }.minOrNull() ?: 0.0
            val highest = plan.plots.mapNotNull { plot -> plot.highestValue ?: plot.amountValue }.maxOrNull() ?: 1.0
            val padding = max((highest - lowest) * 0.1, 0.5)
            (lowest - padding) to (highest + padding)
        }
        HealthChartPlan.Shape.Stages -> 0.0 to max(1.0, plan.stageSegments.groupBy { segment -> segment.periodKey }.values.maxOfOrNull { segments -> segments.sumOf { segment -> segment.hours } } ?: 1.0) * 1.05
        HealthChartPlan.Shape.Bars -> 0.0 to max(1.0, plan.plots.mapNotNull { plot -> plot.amountValue }.maxOrNull() ?: 1.0) * 1.05
    }.let { (low, high) -> if (high - low < 1e-9) low to low + 1 else low to min(high, Double.MAX_VALUE) }
}
