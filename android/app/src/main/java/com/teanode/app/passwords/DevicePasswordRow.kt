package com.teanode.app.passwords

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.kit.passwords.DevicePassword
import com.teanode.kit.passwords.isMaybeSafeToDelete
import java.text.DateFormat
import java.time.Instant
import java.util.Date

/**
 * A device's password in a list: its name, when it was made, when it was last used, and a muted hint when it looks
 * abandoned. Sync and mail passwords share it so the person reads both lists the same way and can tell several
 * passwords with one name apart. The Settings part draws its lists with it.
 */
@Composable
fun DevicePasswordRow(name: String, password: DevicePassword, modifier: Modifier = Modifier, scopes: String = "", nowEpochMS: Long = System.currentTimeMillis()) {
    val created = password.passwordCreatedAt?.let { text -> runCatching { Instant.parse(text).toEpochMilli() }.getOrNull() }
    val lastUsed = password.passwordLastUsedAt?.let { text -> runCatching { Instant.parse(text).toEpochMilli() }.getOrNull() }
    val usage = buildList {
        if (created != null) add(stringResource(R.string.passwords_device_password_created, DateFormat.getDateTimeInstance(DateFormat.MEDIUM, DateFormat.SHORT).format(Date(created))))
        if (lastUsed != null) add(stringResource(R.string.passwords_device_password_last_used, relativeTimeText(lastUsed, nowEpochMS)))
        else add(stringResource(R.string.passwords_device_password_never_used))
    }.joinToString(" · ")
    Column(modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
        Text(name, style = MaterialTheme.typography.bodyLarge)
        if (scopes.isNotEmpty()) Text(scopes, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(usage, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        if (password.isMaybeSafeToDelete(Instant.ofEpochMilli(nowEpochMS))) {
            Text(stringResource(R.string.passwords_device_password_may_delete), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.outline)
        }
    }
}
