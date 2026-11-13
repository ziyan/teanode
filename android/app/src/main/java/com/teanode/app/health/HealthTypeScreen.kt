package com.teanode.app.health

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.health.HealthPeriod
import com.teanode.kit.health.HealthSampleView
import com.teanode.kit.health.HealthTypeView
import com.teanode.kit.health.healthSamples
import com.teanode.kit.health.healthSummary
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/** One kind of Health data: its chart over a range, the average and total, and under them the person's own recent readings. */
@Composable
fun HealthTypeScreen(model: HealthModel, type: HealthTypeView, onBack: () -> Unit, onOpenRoute: (HealthSampleView) -> Unit) {
    val text = rememberHealthText()
    val session = LocalAppSession.current
    val scope = rememberCoroutineScope()
    var range by remember { mutableStateOf(HealthRange.ThirtyDays) }
    var period by remember { mutableStateOf(HealthPeriod.Day) }
    var isPeriodChosen by remember { mutableStateOf(false) }
    var plan by remember { mutableStateOf<HealthChartPlan?>(null) }
    var loadFailed by remember { mutableStateOf<String?>(null) }
    var rangeCutText by remember { mutableStateOf<String?>(null) }
    var selectedKey by remember { mutableStateOf<String?>(null) }
    var isMenuOpen by remember { mutableStateOf(false) }
    var isConfirmingErase by remember { mutableStateOf(false) }
    val name = model.name(type)

    fun mostDays(period: HealthPeriod): Int? = when (period) {
        HealthPeriod.Day -> 400
        HealthPeriod.Week -> 399 * 7
        HealthPeriod.Month -> null
    }
    suspend fun load() {
        val requestedPeriod = period
        val toDay = text.today()
        var fromDay = if (range == HealthRange.All) (type.firstDay ?: text.day(toDay, -364)).take(10) else text.day(toDay, 1 - range.dayCount)
        var mostPeriodCount: Int? = null
        mostDays(requestedPeriod)?.let { most ->
            val earliest = text.day(toDay, 1 - most)
            if (fromDay < earliest) {
                fromDay = earliest
                mostPeriodCount = if (requestedPeriod == HealthPeriod.Week) 399 else 400
            }
        }
        try {
            val summary = model.connection.healthSummary(type.healthType, fromDay, toDay, requestedPeriod, model.selectedUserId)
            plan = HealthChartPlan.make(summary, type, requestedPeriod, fromDay, toDay, text)
            rangeCutText = mostPeriodCount?.let { count -> text.rangeCut(requestedPeriod, count) }
            loadFailed = null
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            // A chart already drawn stays; only a first read that failed says so.
            if (plan == null) loadFailed = session.describeLoadProblem(error)
        }
    }
    LaunchedEffect(range, period) { load() }
    HealthNoticeEffect(model)

    TeaNodeScreen(
        title = name, onBack = onBack,
        actions = {
            if (model.isViewingOwn) {
                Box {
                    IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.health_more)) }
                    DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.health_erase_type, name), color = MaterialTheme.colorScheme.error) },
                            leadingIcon = { Icon(Icons.Outlined.Delete, contentDescription = null, tint = MaterialTheme.colorScheme.error) },
                            onClick = { isMenuOpen = false; isConfirmingErase = true },
                        )
                    }
                }
            }
        },
    ) { padding ->
        LazyColumn(Modifier.padding(padding).fillMaxSize()) {
            item {
                Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                        val choices = listOf(HealthRange.ThirtyDays to stringResource(R.string.health_range_30_days), HealthRange.NinetyDays to stringResource(R.string.health_range_90_days), HealthRange.Year to text.yearRangeTitle, HealthRange.All to stringResource(R.string.health_range_all))
                        choices.forEachIndexed { index, (choice, label) ->
                            SegmentedButton(
                                selected = choice == range, onClick = { range = choice; selectedKey = null; if (!isPeriodChosen) period = choice.defaultPeriod },
                                shape = SegmentedButtonDefaults.itemShape(index, choices.size),
                            ) { Text(label, maxLines = 1) }
                        }
                    }
                    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                        val choices = listOf(HealthPeriod.Day to stringResource(R.string.health_period_day), HealthPeriod.Week to stringResource(R.string.health_period_week), HealthPeriod.Month to stringResource(R.string.health_period_month))
                        choices.forEachIndexed { index, (choice, label) ->
                            SegmentedButton(
                                selected = choice == period, onClick = { isPeriodChosen = true; period = choice; selectedKey = null },
                                shape = SegmentedButtonDefaults.itemShape(index, choices.size),
                            ) { Text(label, maxLines = 1) }
                        }
                    }
                    val current = plan
                    when {
                        current != null && current.hasData -> {
                            val headline = current.figureRows.firstOrNull()
                            Column {
                                if (selectedKey == null && headline != null) {
                                    Text(headline.valueText, style = MaterialTheme.typography.headlineMedium.copy(fontWeight = FontWeight.SemiBold))
                                    Text(headline.label, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                } else {
                                    // The callout of a touched period takes the headline's place.
                                    Box(Modifier.padding(bottom = 0.dp))
                                }
                                HealthChartView(current, selectedKey, onSelect = { key -> selectedKey = key }, modifier = Modifier.padding(top = 8.dp))
                                rangeCutText?.let { cut -> Text(cut, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                            }
                        }
                        current != null -> Text(stringResource(R.string.health_nothing_recorded_in_range), color = MaterialTheme.colorScheme.onSurfaceVariant)
                        loadFailed != null -> {
                            Text(loadFailed.orEmpty(), color = MaterialTheme.colorScheme.onSurfaceVariant)
                            TextButton(onClick = { scope.launch { load() } }) { Text(stringResource(R.string.common_try_again)) }
                        }
                        else -> Box(Modifier.fillMaxWidth(), contentAlignment = androidx.compose.ui.Alignment.Center) { CircularProgressIndicator() }
                    }
                }
            }
            val current = plan
            if (current != null && current.hasData && current.figureRows.size > 1) {
                items(current.figureRows.drop(1), key = { row -> row.label }) { row ->
                    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp)) {
                        Text(row.label, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
                        Text(row.valueText, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
            if (model.isViewingOwn) healthReadings(model, type, onOpenRoute)
        }
    }
    if (isConfirmingErase) {
        ConfirmationDialog(
            title = stringResource(R.string.health_erase_type_title, name), text = stringResource(R.string.health_erase_type_explanation, name),
            confirmLabel = stringResource(R.string.health_erase_type, name), isDestructive = true, onDismiss = { isConfirmingErase = false },
            onConfirm = { isConfirmingErase = false; scope.launch { if (model.erase(type.healthType)) onBack() } },
        )
    }
}

/** The person's own recent readings of a kind; a workout with a route opens it. */
private fun androidx.compose.foundation.lazy.LazyListScope.healthReadings(model: HealthModel, type: HealthTypeView, onOpenRoute: (HealthSampleView) -> Unit) {
    item(key = "readings") { HealthReadingsSection(model, type, onOpenRoute) }
}

@Composable
private fun HealthReadingsSection(model: HealthModel, type: HealthTypeView, onOpenRoute: (HealthSampleView) -> Unit) {
    val text = rememberHealthText()
    val session = LocalAppSession.current
    val scope = rememberCoroutineScope()
    var samples by remember { mutableStateOf<List<HealthSampleView>>(emptyList()) }
    var nextOffset by remember { mutableStateOf<Int?>(null) }
    var hasLoaded by remember { mutableStateOf(false) }
    var isLoadingMore by remember { mutableStateOf(false) }
    var loadFailed by remember { mutableStateOf<String?>(null) }
    suspend fun loadFirstPage() {
        try {
            val page = model.connection.healthSamples(type.healthType, limitCount = PAGE_SIZE, offset = 0)
            samples = page.samples
            nextOffset = page.nextOffset
            loadFailed = null
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            if (samples.isEmpty()) loadFailed = session.describeLoadProblem(error)
        }
        hasLoaded = true
    }
    suspend fun loadMore() {
        val offset = nextOffset ?: return
        if (isLoadingMore) return
        isLoadingMore = true
        try {
            val page = runCatching { model.connection.healthSamples(type.healthType, limitCount = PAGE_SIZE, offset = offset) }.getOrNull() ?: return
            val known = samples.map { sample -> sample.id }.toSet()
            samples = samples + page.samples.filter { sample -> sample.id !in known }
            nextOffset = page.nextOffset
        } finally {
            isLoadingMore = false
        }
    }
    LaunchedEffect(type.healthType) { loadFirstPage() }
    Column(Modifier.fillMaxWidth()) {
        TeaNodeSectionHeader(stringResource(R.string.health_recent_readings))
        if (hasLoaded && samples.isEmpty() && loadFailed == null) Text(stringResource(R.string.health_no_readings_of_this_kind), Modifier.padding(horizontal = 16.dp, vertical = 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
        samples.forEach { sample ->
            val detail = (sample.startedInstant?.let { instant -> text.momentText(instant) } ?: sample.startedAt) + if (sample.sourceName.isEmpty()) "" else " · " + sample.sourceName
            TeaNodeRow(headline = text.readingText(sample, type), supporting = detail, onClick = if (sample.hasRoute) ({ onOpenRoute(sample) }) else null)
            if (sample.id == samples.lastOrNull()?.id) LaunchedEffect(sample.id) { loadMore() }
        }
        loadFailed?.let { failure ->
            Text(failure, Modifier.padding(horizontal = 16.dp, vertical = 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
            TextButton(onClick = { scope.launch { loadFirstPage() } }, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.common_try_again)) }
        }
    }
}

private const val PAGE_SIZE = 50
