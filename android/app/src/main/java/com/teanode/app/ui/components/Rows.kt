package com.teanode.app.ui.components

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.background
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

/**
 * TeaNodeRow is a row of a list: a headline, optionally a line above it, a line or two below, something at the start
 * and something at the end. Tapping it does [onClick]. Use it for every list row so rows look and behave alike; do not
 * lay a row out by hand.
 */
@Composable
fun TeaNodeRow(
    headline: String,
    modifier: Modifier = Modifier,
    supporting: String? = null,
    overline: String? = null,
    supportingMaxLines: Int = 2,
    leadingIcon: ImageVector? = null,
    leading: (@Composable () -> Unit)? = null,
    trailing: (@Composable () -> Unit)? = null,
    onClick: (() -> Unit)? = null,
    isHeadlineBold: Boolean = false,
    containerColor: Color = Color.Transparent,
) {
    ListItem(
        headlineContent = {
            Text(
                headline, maxLines = 2, overflow = TextOverflow.Ellipsis,
                style = if (isHeadlineBold) MaterialTheme.typography.bodyLarge.copy(fontWeight = androidx.compose.ui.text.font.FontWeight.SemiBold) else MaterialTheme.typography.bodyLarge,
            )
        },
        modifier = if (onClick != null) modifier.clickable(onClick = onClick) else modifier,
        overlineContent = overline?.let { text -> { Text(text, maxLines = 1, overflow = TextOverflow.Ellipsis) } },
        supportingContent = supporting?.let { text -> { Text(text, maxLines = supportingMaxLines, overflow = TextOverflow.Ellipsis) } },
        leadingContent = leading ?: leadingIcon?.let { icon -> { Icon(icon, contentDescription = null) } },
        trailingContent = trailing,
        colors = ListItemDefaults.colors(containerColor = containerColor),
    )
}

/** A row that opens another screen: [TeaNodeRow] with a chevron at the end. */
@Composable
fun TeaNodeNavigationRow(
    headline: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    supporting: String? = null,
    leadingIcon: ImageVector? = null,
    leading: (@Composable () -> Unit)? = null,
    trailing: (@Composable () -> Unit)? = null,
) {
    TeaNodeRow(
        headline = headline, modifier = modifier, supporting = supporting, leadingIcon = leadingIcon, leading = leading, onClick = onClick,
        trailing = {
            Row(verticalAlignment = Alignment.CenterVertically) {
                trailing?.invoke()
                Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        },
    )
}

/** The heading above a group of rows. */
@Composable
fun TeaNodeSectionHeader(text: String, modifier: Modifier = Modifier) {
    Text(
        text = text,
        modifier = modifier.padding(PaddingValues(start = 16.dp, end = 16.dp, top = 16.dp, bottom = 8.dp)),
        style = MaterialTheme.typography.titleSmall,
        color = MaterialTheme.colorScheme.primary,
    )
}

/** The line between two groups of rows. */
@Composable
fun TeaNodeDivider(modifier: Modifier = Modifier) {
    HorizontalDivider(modifier, color = MaterialTheme.colorScheme.outlineVariant)
}

/** CountBadge is the red number on a part that has things waiting for the person, as the iPhone's badges are. */
@Composable
fun CountBadge(count: Int, modifier: Modifier = Modifier) {
    if (count <= 0) return
    Box(
        modifier = modifier
            .defaultMinSize(minWidth = 22.dp, minHeight = 22.dp)
            .background(MaterialTheme.colorScheme.error, CircleShape)
            .padding(horizontal = 6.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = if (count > 99) "99+" else count.toString(),
            color = MaterialTheme.colorScheme.onError,
            style = MaterialTheme.typography.labelSmall,
        )
    }
}
