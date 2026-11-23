package com.teanode.app.ui.components

import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import com.teanode.app.R

/**
 * ConfirmationDialog asks before something that cannot be undone, or that changes what other people see. Show it while a
 * state holds the thing being confirmed, and clear that state in both callbacks:
 *
 *     var deleting by remember { mutableStateOf<Item?>(null) }
 *     deleting?.let { item ->
 *         ConfirmationDialog(title = "Delete ${item.name}?", confirmLabel = "Delete", isDestructive = true,
 *             onConfirm = { deleting = null; delete(item) }, onDismiss = { deleting = null })
 *     }
 */
@Composable
fun ConfirmationDialog(
    title: String,
    confirmLabel: String,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
    text: String? = null,
    isDestructive: Boolean = false,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = text?.let { body -> { Text(body) } },
        confirmButton = {
            TextButton(onClick = onConfirm) {
                Text(confirmLabel, color = if (isDestructive) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.primary)
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
