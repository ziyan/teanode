package com.teanode.app.shell

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AddCircle
import androidx.compose.material.icons.outlined.ArrowUpward
import androidx.compose.material.icons.outlined.RemoveCircleOutline
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.kit.AppSection
import com.teanode.kit.TabBarChoice

/**
 * Where the person chooses the parts in the bottom bar, as the iPhone's Music app chooses its tabs: the ones in the
 * bar, which can be moved up or taken out, and under them the others, any of which can be put in while there is room.
 */
@Composable
fun TabBarEditor(chosen: List<AppSection>, others: List<AppSection>, onChange: (List<AppSection>) -> Unit, onDismiss: () -> Unit) {
    val isFull = chosen.size >= TabBarChoice.MOST_COUNT
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.shell_tab_bar_title)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState())) {
                Text(stringResource(R.string.shell_in_tab_bar), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                for ((position, section) in chosen.withIndex()) {
                    EditorRow(section) {
                        IconButton(onClick = { onChange(chosen.toMutableList().also { list -> list.add(position - 1, list.removeAt(position)) }) }, enabled = position > 0) {
                            Icon(Icons.Outlined.ArrowUpward, contentDescription = stringResource(R.string.shell_move_up))
                        }
                        IconButton(onClick = { onChange(chosen - section) }) {
                            Icon(Icons.Outlined.RemoveCircleOutline, contentDescription = stringResource(R.string.shell_remove_from_tab_bar), tint = MaterialTheme.colorScheme.error)
                        }
                    }
                }
                Text(
                    stringResource(R.string.shell_tab_bar_limit),
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(top = 4.dp, bottom = 12.dp),
                )
                Text(stringResource(R.string.shell_in_more), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                for (section in others) {
                    EditorRow(section) {
                        IconButton(onClick = { onChange(chosen + section) }, enabled = !isFull) {
                            Icon(Icons.Outlined.AddCircle, contentDescription = stringResource(R.string.shell_put_in_tab_bar), tint = if (isFull) MaterialTheme.colorScheme.outline else MaterialTheme.colorScheme.primary)
                        }
                    }
                }
            }
        },
        confirmButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_done)) } },
    )
}

@Composable
private fun EditorRow(section: AppSection, buttons: @Composable () -> Unit) {
    androidx.compose.foundation.layout.Row(
        verticalAlignment = androidx.compose.ui.Alignment.CenterVertically,
        modifier = Modifier.padding(vertical = 2.dp),
    ) {
        Icon(section.icon, contentDescription = null, modifier = Modifier.padding(end = 12.dp))
        Text(stringResource(section.titleResource), Modifier.weight(1f))
        buttons()
    }
}
