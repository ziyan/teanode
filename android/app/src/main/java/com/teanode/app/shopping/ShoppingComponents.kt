package com.teanode.app.shopping

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.RadioButtonUnchecked
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.teanode.app.ui.components.rememberServerPicture
import com.teanode.kit.shopping.ShoppingItem
import com.teanode.kit.shopping.ShoppingMoney

/** An item's picture, or its first letter on a soft ground where it has none. [size] null fills the width as a square. */
@Composable
internal fun ShoppingPictureView(item: ShoppingItem, modifier: Modifier = Modifier, size: Dp? = 44.dp) {
    val picture = rememberServerPicture(if (item.hasPicture) item.picturePath else null)
    val shape = RoundedCornerShape(if (size == null) 12.dp else 8.dp)
    val sizing = if (size == null) modifier.fillMaxWidth().aspectRatio(1f) else modifier.size(size)
    Box(
        sizing.clip(shape).background(MaterialTheme.colorScheme.surfaceVariant),
        contentAlignment = Alignment.Center,
    ) {
        if (picture != null) {
            Image(picture, contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.matchParentSizeCompat())
        } else {
            Text(
                item.initial, color = MaterialTheme.colorScheme.onSurfaceVariant,
                style = if (size == null) MaterialTheme.typography.headlineLarge else MaterialTheme.typography.titleMedium,
            )
        }
    }
}

private fun Modifier.matchParentSizeCompat(): Modifier = this.fillMaxWidth().aspectRatio(1f)

/** An item on a list as a row: its picture, its words, and what its count costs. */
@Composable
internal fun ShoppingListItemRow(item: ShoppingItem, words: ShoppingWords, onOpen: () -> Unit, modifier: Modifier = Modifier) {
    val price = ShoppingMoney.shownLineTotal(item)
    ListItem(
        headlineContent = {
            Text(
                item.itemDescription, maxLines = 2, overflow = TextOverflow.Ellipsis,
                color = if (item.isNotNeeded) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
            )
        },
        supportingContent = { Text(words.listItemDetail(item), maxLines = 3, overflow = TextOverflow.Ellipsis) },
        leadingContent = { ShoppingPictureView(item) },
        trailingContent = if (price.isEmpty()) null else ({ Text(price, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }),
        colors = ListItemDefaults.colors(containerColor = Color.Transparent),
        modifier = modifier.clickable(onClick = onOpen),
    )
}

/** An item in the pictures layout of a list: the picture across the tile, and under it what it is, what it needs and about what it costs. */
@Composable
internal fun ShoppingListItemTile(item: ShoppingItem, groupName: String, words: ShoppingWords, onOpen: () -> Unit, modifier: Modifier = Modifier) {
    val price = ShoppingMoney.shownLineTotal(item)
    Card(
        modifier = modifier.clickable(onClick = onOpen),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerHigh),
        shape = RoundedCornerShape(16.dp),
    ) {
        Column(Modifier.padding(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            ShoppingPictureView(item, size = null)
            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(
                    groupName, style = MaterialTheme.typography.labelMedium, fontWeight = FontWeight.SemiBold,
                    color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis,
                )
                Text(
                    item.itemDescription, style = MaterialTheme.typography.titleSmall, maxLines = 2, overflow = TextOverflow.Ellipsis,
                    color = if (item.isNotNeeded) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
                )
                Text(words.listItemDetail(item), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 3, overflow = TextOverflow.Ellipsis)
                if (price.isNotEmpty()) Text(price, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

/** An item at a store as a row: a check that puts it in the cart, its picture and its words. */
@OptIn(ExperimentalFoundationApi::class)
@Composable
internal fun ShoppingStoreItemRow(
    item: ShoppingItem,
    isChecked: Boolean,
    detail: String,
    onToggle: () -> Unit,
    onMenu: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val price = ShoppingMoney.shownLineTotal(item)
    ListItem(
        headlineContent = {
            Text(
                item.shownTitle, maxLines = 2, overflow = TextOverflow.Ellipsis,
                color = if (isChecked || item.isOutOfStock) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
                textDecoration = if (isChecked) androidx.compose.ui.text.style.TextDecoration.LineThrough else null,
            )
        },
        supportingContent = if (detail.isEmpty()) null else ({ Text(detail, maxLines = 2, overflow = TextOverflow.Ellipsis) }),
        leadingContent = {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                Checkbox(checked = isChecked, onCheckedChange = { onToggle() })
                ShoppingPictureView(item, size = 40.dp)
            }
        },
        trailingContent = if (price.isEmpty()) null else ({ Text(price, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }),
        colors = ListItemDefaults.colors(containerColor = Color.Transparent),
        modifier = modifier.combinedClickable(onClick = onToggle, onLongClick = onMenu),
    )
}

/** An item at a store in the pictures layout: a tile whose check puts it in the cart. */
@Composable
internal fun ShoppingStoreItemTile(
    item: ShoppingItem,
    isChecked: Boolean,
    groupName: String,
    detail: String,
    onToggle: () -> Unit,
    onOpen: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Card(
        modifier = modifier.clickable(onClick = onToggle),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerHigh),
        shape = RoundedCornerShape(16.dp),
    ) {
        Column(Modifier.padding(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Box {
                ShoppingPictureView(item, size = null)
                Icon(
                    if (isChecked) Icons.Outlined.CheckCircle else Icons.Outlined.RadioButtonUnchecked, contentDescription = null,
                    tint = if (isChecked) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.align(Alignment.TopEnd).padding(6.dp).background(MaterialTheme.colorScheme.surface.copy(alpha = 0.85f), CircleShape).size(28.dp),
                )
            }
            Column(Modifier.clickable(role = Role.Button, onClick = onOpen), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(groupName, style = MaterialTheme.typography.labelMedium, fontWeight = FontWeight.SemiBold, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(item.shownTitle, style = MaterialTheme.typography.titleSmall, maxLines = 2, overflow = TextOverflow.Ellipsis)
                if (detail.isNotEmpty()) Text(detail, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 3, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

@Composable
internal fun rememberShoppingWords(): ShoppingWords {
    val context = LocalContext.current
    return androidx.compose.runtime.remember(context) { ShoppingWords(context) }
}

