package com.teanode.app.settings

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.finance.FinancePalette
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.manage.StorageBreakdown
import com.teanode.kit.manage.StorageDisk
import com.teanode.kit.manage.StorageUsage
import com.teanode.kit.manage.StorageUsageRow
import com.teanode.kit.manage.storageUsage
import com.teanode.kit.manage.usageBounds
import java.util.Locale

/**
 * What the database and the files hold, for the operator: the disks the server measured and the day each fills, the sizes by area,
 * kind of file or person, the days sampled as stacked columns, and what the recent growth comes to. It needs server:manage. The
 * iPhone app's StorageUsageView is the model.
 */
@Composable
fun StorageUsageScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    var breakdown by remember { mutableStateOf(StorageBreakdown.Area) }
    var days by remember { mutableStateOf(30) }
    val loaded = rememberLoad(breakdown, days) { val bounds = usageBounds(days); connection.storageUsage(breakdown, bounds.first, bounds.second) }
    LoadedSettingsPage(stringResource(R.string.settings_storage_title), onBack, loaded) { usage ->
        usage.fillingDisks.forEach { disk ->
            item {
                TeaNodeRow(
                    headline = stringResource(R.string.settings_storage_banner, diskName(disk.diskLabel), disk.projectedFullOn.orEmpty(), Math.round(disk.daysUntilFull ?: 0.0).toInt()),
                    leadingIcon = Icons.Outlined.WarningAmber, supportingMaxLines = 3,
                )
            }
        }
        item { TeaNodeSectionHeader(stringResource(R.string.settings_storage_disks)) }
        usage.disks.forEach { disk -> item { DiskRow(disk, usage) } }
        item {
            ChoiceRow(stringResource(R.string.settings_storage_group_by), StorageBreakdown.entries, breakdown, label = { choice -> breakdownName(choice) }, onSelect = { breakdown = it })
            ChoiceRow(stringResource(R.string.settings_usage_range), listOf(7, 30, 90), days, label = { choice -> stringResource(R.string.settings_usage_last_days, choice) }, onSelect = { days = it })
        }
        item {
            TeaNodeRow(headline = stringResource(R.string.settings_storage_database), supporting = formatStorageBytes(usage.databaseSizeBytes))
            TeaNodeRow(headline = stringResource(R.string.settings_storage_files), supporting = formatStorageBytes(usage.fileSizeBytes))
            TeaNodeRow(headline = stringResource(R.string.settings_storage_total), supporting = formatStorageBytes(usage.totalSizeBytes), isHeadlineBold = true)
        }
        item { StorageChart(usage, breakdown) }
        if (usage.growth.isGrowthProjectable) {
            item { TeaNodeSectionHeader(stringResource(R.string.settings_storage_at_this_rate)) }
            item {
                TeaNodeRow(headline = stringResource(R.string.settings_storage_per_day), supporting = formatStorageBytes(usage.growth.growthBytesPerDay.toLong()))
                TeaNodeRow(headline = stringResource(R.string.settings_storage_now), supporting = formatStorageBytes(usage.growth.currentSizeBytes))
                TeaNodeRow(headline = stringResource(R.string.settings_storage_in_days, 30), supporting = formatStorageBytes(usage.growth.projectedSize30DaysBytes))
                TeaNodeRow(headline = stringResource(R.string.settings_storage_in_days, 90), supporting = formatStorageBytes(usage.growth.projectedSize90DaysBytes))
                TeaNodeRow(headline = stringResource(R.string.settings_storage_in_days, 365), supporting = formatStorageBytes(usage.growth.projectedSize365DaysBytes))
                if (!usage.growth.areFilesCounted) FooterText(stringResource(R.string.settings_storage_files_not_counted))
            }
        }
        item { TeaNodeSectionHeader(breakdownName(breakdown)) }
        if (usage.usageRows.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_storage_nothing)) }
        items(usage.usageRows.size) { index -> UsageRowView(usage.usageRows[index], breakdown) }
    }
}

@Composable
private fun DiskRow(disk: StorageDisk, usage: StorageUsage) {
    val fullOn = when {
        disk.projectedFullOn != null -> stringResource(R.string.settings_storage_full_on, disk.projectedFullOn.orEmpty())
        !disk.isDiskMeasured -> null
        usage.growth.isGrowthProjectable -> stringResource(R.string.settings_storage_not_growing)
        else -> stringResource(R.string.settings_storage_not_enough_history)
    }
    val usedFraction = disk.usedFraction
    TeaNodeRow(
        headline = diskName(disk.diskLabel), overline = fullOn,
        supporting = if (usedFraction != null) stringResource(R.string.settings_storage_free_of, formatStorageBytes(disk.diskFreeBytes ?: 0), formatStorageBytes(disk.diskTotalBytes ?: 0)) else stringResource(R.string.settings_storage_not_measured),
        supportingMaxLines = 3,
    )
    if (usedFraction != null) {
        LinearProgressIndicator(
            progress = { usedFraction.toFloat() }, modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
            color = if (disk.isProjectedFull) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.primary,
        )
    }
}

/** The days sampled, a stacked column a day: the largest areas or kinds apart and the rest together, or the database and the files for people. */
@Composable
private fun StorageChart(usage: StorageUsage, breakdown: StorageBreakdown) {
    val points = usage.chartPoints
    if (points.isEmpty()) {
        TeaNodeRow(headline = stringResource(R.string.settings_storage_no_history))
        return
    }
    val seriesKeys = points.map { point -> point.seriesKey }.distinct().let { keys -> keys.filter { key -> key != StorageUsage.REST_SERIES_KEY } + keys.filter { key -> key == StorageUsage.REST_SERIES_KEY } }
    val colors = seriesKeys.mapIndexed { index, key -> if (key == StorageUsage.REST_SERIES_KEY) MaterialTheme.colorScheme.outline else FinancePalette.color(index) }
    val columnDays = points.map { point -> point.sampledOn }.distinct()
    val tallest = columnDays.maxOf { day -> points.filter { point -> point.sampledOn == day }.sumOf { point -> point.sizeBytes } }.coerceAtLeast(1)
    Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(stringResource(R.string.settings_storage_over_time), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Canvas(Modifier.fillMaxWidth().height(160.dp)) {
            val slot = size.width / columnDays.size
            val columnWidth = (slot * 0.7f).coerceAtLeast(2f)
            columnDays.forEachIndexed { column, day ->
                var base = size.height
                seriesKeys.forEachIndexed { index, key ->
                    val bytes = points.filter { point -> point.sampledOn == day && point.seriesKey == key }.sumOf { point -> point.sizeBytes }
                    val height = size.height * bytes / tallest
                    base -= height.toFloat()
                    drawRect(colors[index], Offset(column * slot + (slot - columnWidth) / 2, base), Size(columnWidth, height.toFloat()))
                }
            }
        }
        Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(16.dp)) {
            seriesKeys.forEachIndexed { index, key -> Legend(seriesName(key, breakdown), colors[index]) }
        }
    }
}

@Composable
private fun Legend(name: String, color: Color) {
    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        Canvas(Modifier.padding(top = 6.dp).width(10.dp).height(10.dp)) { drawCircle(color) }
        Text(name, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@Composable
private fun UsageRowView(row: StorageUsageRow, breakdown: StorageBreakdown) {
    val name = when (breakdown) {
        StorageBreakdown.Area -> areaName(row.rowKey)
        StorageBreakdown.Kind -> kindName(row.rowKey)
        StorageBreakdown.Person -> if (row.rowKey.isEmpty()) stringResource(R.string.settings_storage_unowned) else row.rowLabel
    }
    val detail = when (breakdown) {
        StorageBreakdown.Area -> listOf(formatStorageBytes(row.rowSizeBytes), stringResource(R.string.settings_storage_tables, row.rowTableCount))
        StorageBreakdown.Kind -> listOf(formatStorageBytes(row.rowSizeBytes))
        StorageBreakdown.Person -> listOf(formatStorageBytes(row.rowSizeBytes)) + row.rowFileSizes.filter { size -> size.sizeBytes > 0 }.map { size -> "${kindName(size.sizeKey)} ${formatStorageBytes(size.sizeBytes)}" }
    }
    TeaNodeRow(headline = name, supporting = detail.joinToString("\n"), supportingMaxLines = 8)
}

/** A size in 1024-based units, with a decimal from a megabyte up. */
fun formatStorageBytes(sizeBytes: Long): String {
    val units = listOf("B", "KB", "MB", "GB", "TB", "PB")
    var size = sizeBytes.toDouble()
    var index = 0
    while (size >= 1024 && index < units.size - 1) { size /= 1024; index++ }
    return if (index < 2) String.format(Locale.getDefault(), "%.0f %s", size, units[index]) else String.format(Locale.getDefault(), "%.1f %s", size, units[index])
}

@Composable
private fun breakdownName(breakdown: StorageBreakdown): String = stringResource(
    when (breakdown) { StorageBreakdown.Area -> R.string.settings_storage_by_area; StorageBreakdown.Kind -> R.string.settings_storage_by_kind; StorageBreakdown.Person -> R.string.settings_storage_by_person },
)

@Composable
private fun diskName(diskLabel: String): String = when (diskLabel) {
    "dataDirectory" -> stringResource(R.string.settings_storage_disk_data_directory)
    "fileDirectory" -> stringResource(R.string.settings_storage_disk_file_directory)
    "objectStore" -> stringResource(R.string.settings_storage_disk_object_store)
    else -> diskLabel
}

@Composable
private fun areaName(areaKey: String): String = when (areaKey) {
    "mail" -> stringResource(R.string.settings_storage_area_mail)
    "memory" -> stringResource(R.string.settings_storage_area_memory)
    "conversations" -> stringResource(R.string.settings_storage_area_conversations)
    "photos" -> stringResource(R.string.settings_storage_area_photos)
    "recordings" -> stringResource(R.string.settings_storage_area_recordings)
    "finance" -> stringResource(R.string.settings_storage_area_finance)
    "location" -> stringResource(R.string.settings_storage_area_location)
    "health" -> stringResource(R.string.settings_storage_area_health)
    "journal" -> stringResource(R.string.settings_storage_area_journal)
    "search" -> stringResource(R.string.settings_storage_area_search)
    "other" -> stringResource(R.string.settings_storage_area_other)
    else -> areaKey
}

@Composable
private fun kindName(kindKey: String): String = when (kindKey) {
    "photoOriginal" -> stringResource(R.string.settings_storage_kind_photo_original)
    "photoPreview" -> stringResource(R.string.settings_storage_kind_photo_preview)
    "recordingOriginal" -> stringResource(R.string.settings_storage_kind_recording_original)
    "recordingDerived" -> stringResource(R.string.settings_storage_kind_recording_derived)
    "mail" -> stringResource(R.string.settings_storage_kind_mail)
    "agentAttachment" -> stringResource(R.string.settings_storage_kind_agent_attachment)
    else -> kindKey
}

@Composable
private fun seriesName(seriesKey: String, breakdown: StorageBreakdown): String = when (seriesKey) {
    StorageUsage.REST_SERIES_KEY -> stringResource(R.string.settings_storage_rest)
    StorageUsage.DATABASE_SERIES_KEY -> stringResource(R.string.settings_storage_database)
    StorageUsage.FILES_SERIES_KEY -> stringResource(R.string.settings_storage_files)
    else -> if (breakdown == StorageBreakdown.Kind) kindName(seriesKey) else areaName(seriesKey)
}
