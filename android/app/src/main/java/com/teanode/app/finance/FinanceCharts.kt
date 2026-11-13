package com.teanode.app.finance

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.size
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

// The charts of Finance, drawn with a canvas: a donut for where money went, bars for a month's income and spending, and
// a line for a running total. The iPhone app's shared chart parts (ChartParts.swift) are the model; these are the
// finance part of them, in `com.teanode.app.finance` since the shared `charts` package was not on the branch yet.

/** The colors the slices and series are drawn in, the same in light and dark mode so a legend stays readable. */
internal object FinancePalette {
    private val light = listOf(
        Color(0xFF2E7D32), Color(0xFF1565C0), Color(0xFFEF6C00), Color(0xFF6A1B9A), Color(0xFF00838F), Color(0xFFC62828), Color(0xFF9E9D24), Color(0xFF4E342E),
    )
    private val dark = listOf(
        Color(0xFF81C784), Color(0xFF64B5F6), Color(0xFFFFB74D), Color(0xFFBA68C8), Color(0xFF4DD0E1), Color(0xFFE57373), Color(0xFFDCE775), Color(0xFFA1887F),
    )

    val size: Int get() = light.size

    @Composable
    fun color(index: Int): Color = (if (MaterialTheme.colorScheme.background.luminance() < 0.5f) dark else light)[index % light.size]
}

/** One slice of a donut. */
internal class DonutSlice(val id: String, val value: Double, val color: Color)

@Composable
internal fun DonutChart(slices: List<DonutSlice>, selectedId: String?, modifier: Modifier = Modifier, diameter: Dp = 180.dp, centerContent: @Composable () -> Unit = {}) {
    val trackColor = MaterialTheme.colorScheme.surfaceVariant
    Box(modifier.size(diameter), contentAlignment = androidx.compose.ui.Alignment.Center) {
        Canvas(Modifier.size(diameter)) {
            val strokeWidth = size.minDimension * 0.16f
            val inset = strokeWidth / 2
            val arcSize = Size(size.width - strokeWidth, size.height - strokeWidth)
            val total = slices.sumOf { slice -> slice.value }.takeIf { sum -> sum > 0 } ?: 1.0
            drawCircle(trackColor, radius = arcSize.width / 2, style = Stroke(strokeWidth))
            var start = -90f
            for (slice in slices) {
                val sweep = (slice.value / total * 360).toFloat()
                val isSelected = selectedId == null || selectedId == slice.id
                drawArc(
                    slice.color.copy(alpha = if (isSelected) 1f else 0.35f), start, maxOf(sweep - 1.5f, 0.5f), false,
                    Offset(inset, inset), arcSize, style = Stroke(strokeWidth),
                )
                start += sweep
            }
        }
        centerContent()
    }
}

/** A month's income and spending side by side, one pair of bars for each month given. */
internal class BarPair(val label: String, val first: Double, val second: Double)

@Composable
internal fun PairedBarChart(pairs: List<BarPair>, firstColor: Color, secondColor: Color, modifier: Modifier = Modifier, height: Dp = 140.dp) {
    val baseline = MaterialTheme.colorScheme.outlineVariant
    Canvas(modifier.fillMaxWidth().height(height)) {
        val peak = pairs.maxOfOrNull { pair -> maxOf(pair.first, pair.second) }?.takeIf { value -> value > 0 } ?: 1.0
        val slot = size.width / maxOf(pairs.size, 1)
        val barWidth = slot * 0.32f
        drawLine(baseline, Offset(0f, size.height), Offset(size.width, size.height), 1.dp.toPx())
        pairs.forEachIndexed { index, pair ->
            val left = slot * index + slot * 0.14f
            val firstHeight = (size.height * (pair.first / peak)).toFloat()
            val secondHeight = (size.height * (pair.second / peak)).toFloat()
            drawRect(firstColor, Offset(left, size.height - firstHeight), Size(barWidth, firstHeight))
            drawRect(secondColor, Offset(left + barWidth + 2.dp.toPx(), size.height - secondHeight), Size(barWidth, secondHeight))
        }
    }
}

/** One series of a line chart: the points, the color and whether it is drawn dashed (the month it is compared with). */
internal class LineSeries(val values: List<Double>, val color: Color, val isDashed: Boolean = false)

@Composable
internal fun LineChart(series: List<LineSeries>, modifier: Modifier = Modifier, height: Dp = 140.dp, isZeroIncluded: Boolean = false) {
    val baseline = MaterialTheme.colorScheme.outlineVariant
    Canvas(modifier.fillMaxWidth().height(height)) {
        val all = series.flatMap { each -> each.values }
        if (all.isEmpty()) return@Canvas
        val low = if (isZeroIncluded) minOf(0.0, all.min()) else all.min()
        val high = all.max()
        val span = if (high - low < 1e-9) 1.0 else high - low
        val longest = series.maxOf { each -> each.values.size }
        drawLine(baseline, Offset(0f, size.height), Offset(size.width, size.height), 1.dp.toPx())
        for (each in series) {
            if (each.values.isEmpty()) continue
            val path = Path()
            each.values.forEachIndexed { index, value ->
                val x = if (longest <= 1) size.width / 2 else size.width * index / (longest - 1)
                val y = size.height - (size.height * ((value - low) / span)).toFloat()
                if (index == 0) path.moveTo(x, y) else path.lineTo(x, y)
            }
            drawPath(
                path, each.color,
                style = Stroke(
                    2.dp.toPx(),
                    pathEffect = if (each.isDashed) androidx.compose.ui.graphics.PathEffect.dashPathEffect(floatArrayOf(10f, 8f)) else null,
                ),
            )
        }
    }
}
