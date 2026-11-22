package com.teanode.app.shopping

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.automirrored.outlined.Undo
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
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.width
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import com.teanode.app.R
import com.teanode.kit.shopping.ShoppingTrip
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle as FilledCheckCircle
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.RadioButtonUnchecked
import androidx.compose.material.icons.outlined.ShoppingCart
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.setValue
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
internal fun ShoppingPictureView(item: ShoppingItem, modifier: Modifier = Modifier, size: Dp? = 44.dp, isZoomable: Boolean = false) {
    var isOpen by androidx.compose.runtime.remember { androidx.compose.runtime.mutableStateOf(false) }
    val picture = rememberServerPicture(if (item.hasPicture) item.picturePath else null)
    val shape = RoundedCornerShape(if (size == null) 12.dp else 8.dp)
    val sizing = if (size == null) modifier.fillMaxWidth().aspectRatio(1f) else modifier.size(size)
    Box(
        (if (isZoomable && picture != null) sizing.clip(shape).clickable(role = Role.Button) { isOpen = true } else sizing.clip(shape)).background(MaterialTheme.colorScheme.surfaceVariant),
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
    if (isOpen) PictureLightbox(item) { isOpen = false }
}

@Composable
private fun PictureLightbox(item: ShoppingItem, onDismiss: () -> Unit) {
    val picture = rememberServerPicture(item.picturePath)
    androidx.compose.ui.window.Dialog(onDismissRequest = onDismiss, properties = androidx.compose.ui.window.DialogProperties(usePlatformDefaultWidth = false)) {
        Box(Modifier.fillMaxWidth().background(Color.Black).clickable(onClick = onDismiss), contentAlignment = Alignment.Center) {
            if (picture != null) Image(picture, contentDescription = item.itemDescription, contentScale = ContentScale.Fit, modifier = Modifier.fillMaxWidth().aspectRatio(1f))
        }
    }
}

private fun Modifier.matchParentSizeCompat(): Modifier = this.fillMaxWidth().aspectRatio(1f)

/** An item on a list as a row: its picture, its words, and what its count costs. */
@Composable
@OptIn(ExperimentalFoundationApi::class)
internal fun ShoppingListItemRow(
    item: ShoppingItem,
    words: ShoppingWords,
    onOpen: () -> Unit,
    modifier: Modifier = Modifier,
    actions: ShoppingItemActions? = null,
    check: ShoppingListCheck? = null,
) {
    val price = ShoppingMoney.shownLineTotal(item)
    var isMenuOpen by androidx.compose.runtime.remember { androidx.compose.runtime.mutableStateOf(false) }
    Box(modifier) {
    ListItem(
        headlineContent = {
            Text(
                item.itemDescription, maxLines = 2, overflow = TextOverflow.Ellipsis,
                color = if (item.isNotNeeded) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
                textDecoration = if (item.isNotNeeded) androidx.compose.ui.text.style.TextDecoration.LineThrough else null,
            )
        },
        supportingContent = { Text(words.listItemDetail(item), maxLines = 3, overflow = TextOverflow.Ellipsis) },
        leadingContent = {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                if (check != null) ShoppingCheckButton(check, Modifier.padding(start = 0.dp))
                ShoppingPictureView(item, isZoomable = true)
            }
        },
        trailingContent = if (price.isEmpty()) null else ({ Text(price, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }),
        colors = ListItemDefaults.colors(containerColor = Color.Transparent),
        modifier = Modifier.combinedClickable(role = Role.Button, onClick = onOpen, onLongClick = if (actions != null) ({ isMenuOpen = true }) else null),
    )
    if (actions != null) ShoppingItemMenu(item, isMenuOpen, { isMenuOpen = false }, actions)
    }
}

/** What touch and hold on an item of a list offers, as the iPhone's swipe and menu do: Need again for one not needed now, and Remove. */
internal class ShoppingItemActions(val onNeedAgain: () -> Unit, val onRemove: () -> Unit)

@Composable
internal fun ShoppingItemMenu(item: ShoppingItem, isOpen: Boolean, onDismiss: () -> Unit, actions: ShoppingItemActions) {
    androidx.compose.material3.DropdownMenu(isOpen, onDismiss) {
        if (item.isNotNeeded) {
            androidx.compose.material3.DropdownMenuItem(
                text = { Text(androidx.compose.ui.res.stringResource(com.teanode.app.R.string.shopping_need_again)) },
                leadingIcon = { Icon(androidx.compose.material.icons.Icons.AutoMirrored.Outlined.Undo, null) },
                onClick = { onDismiss(); actions.onNeedAgain() },
            )
        }
        androidx.compose.material3.DropdownMenuItem(
            text = { Text(androidx.compose.ui.res.stringResource(com.teanode.app.R.string.shopping_remove), color = MaterialTheme.colorScheme.error) },
            leadingIcon = { Icon(androidx.compose.material.icons.Icons.Outlined.Delete, null, tint = MaterialTheme.colorScheme.error) },
            onClick = { onDismiss(); actions.onRemove() },
        )
    }
}

/** An item in the pictures layout of a list: the picture across the tile, and under it what it is, what it needs and about what it costs. */
@Composable
@OptIn(ExperimentalFoundationApi::class)
internal fun ShoppingListItemTile(
    item: ShoppingItem,
    groupName: String,
    words: ShoppingWords,
    onOpen: () -> Unit,
    modifier: Modifier = Modifier,
    actions: ShoppingItemActions? = null,
    check: ShoppingListCheck? = null,
) {
    val price = ShoppingMoney.shownLineTotal(item)
    var isMenuOpen by androidx.compose.runtime.remember { androidx.compose.runtime.mutableStateOf(false) }
    Box(modifier) {
    Card(
        modifier = Modifier.fillMaxWidth().combinedClickable(role = Role.Button, onClick = onOpen, onLongClick = if (actions != null) ({ isMenuOpen = true }) else null),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerHigh),
        shape = RoundedCornerShape(16.dp),
    ) {
        Column(Modifier.padding(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Box {
                ShoppingPictureView(item, size = null)
                if (check != null) {
                    ShoppingCheckButton(
                        check, Modifier.align(Alignment.TopEnd).padding(2.dp).background(MaterialTheme.colorScheme.surface.copy(alpha = 0.85f), CircleShape),
                    )
                }
            }
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
    if (actions != null) ShoppingItemMenu(item, isMenuOpen, { isMenuOpen = false }, actions)
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
                ShoppingCheckButton(
                    ShoppingListCheck(
                        isChecked, true,
                        if (isChecked) stringResource(R.string.shopping_take_out_of_cart, item.itemDescription) else stringResource(R.string.shopping_check_off_item, item.itemDescription),
                        onToggle,
                    ),
                )
                ShoppingPictureView(item)
            }
        },
        trailingContent = if (price.isEmpty()) null else ({ Text(price, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }),
        colors = ListItemDefaults.colors(containerColor = Color.Transparent),
        modifier = modifier.combinedClickable(role = Role.Button, onClick = onToggle, onLongClick = onMenu),
    )
}

/** An item at a store in the pictures layout: a tile whose check puts it in the cart. */
@OptIn(ExperimentalFoundationApi::class)
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
        modifier = modifier.combinedClickable(role = Role.Button, onClick = onToggle, onLongClick = onOpen),
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

/** The round check at the start of a row or tile of a list: what it shows, whether it can be tapped, what it says to a screen reader and what a tap does. */
internal class ShoppingListCheck(val isChecked: Boolean, val isEnabled: Boolean, val description: String, val onToggle: () -> Unit)

/** The check an item has on a list: filled when it is checked off or in the person's own cart, and dead when it is in somebody else's cart. */
@Composable
internal fun shoppingListCheck(item: ShoppingItem, ownUserId: String?, onToggle: () -> Unit): ShoppingListCheck {
    val isInOtherCart = item.isInCart && item.cartedByUserId != ownUserId
    val isChecked = item.isInCart || item.isNotNeeded
    val description = when {
        isInOtherCart -> stringResource(R.string.shopping_in_someones_cart, item.cartedByName.ifEmpty { stringResource(R.string.shopping_in_the_cart) })
        item.isInCart -> stringResource(R.string.shopping_take_out_of_cart, item.itemDescription)
        item.isNotNeeded -> stringResource(R.string.shopping_uncheck_item, item.itemDescription)
        else -> stringResource(R.string.shopping_check_off_item, item.itemDescription)
    }
    return ShoppingListCheck(isChecked, !isInOtherCart, description, onToggle)
}

/** A round check with a touch target of 48dp. */
@Composable
internal fun ShoppingCheckButton(check: ShoppingListCheck, modifier: Modifier = Modifier) {
    IconButton(onClick = check.onToggle, enabled = check.isEnabled, modifier = modifier.size(48.dp)) {
        Icon(
            if (check.isChecked) Icons.Filled.FilledCheckCircle else Icons.Outlined.RadioButtonUnchecked,
            contentDescription = check.description,
            tint = when {
                !check.isEnabled -> MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.6f)
                check.isChecked -> MaterialTheme.colorScheme.primary
                else -> MaterialTheme.colorScheme.onSurfaceVariant
            },
            modifier = Modifier.size(28.dp),
        )
    }
}

/** What time of day a finished trip ended, as the person's phone writes it ("3:41 PM"). */
private fun shownFinishTime(trip: ShoppingTrip): String {
    val finished = try {
        java.time.Instant.parse(trip.finishedAt.orEmpty())
    } catch (_: Exception) {
        return ""
    }
    return java.text.DateFormat.getTimeInstance(java.text.DateFormat.SHORT).format(java.util.Date.from(finished))
}

/** The row at the top of a screen that offers to open the trip the person finished a moment ago again. */
@Composable
internal fun ShoppingResumeRow(trip: ShoppingTrip, isResuming: Boolean, onResume: () -> Unit, modifier: Modifier = Modifier) {
    Card(
        modifier = modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.secondaryContainer),
        shape = RoundedCornerShape(16.dp),
    ) {
        Row(Modifier.padding(start = 16.dp, top = 12.dp, bottom = 12.dp, end = 12.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                val time = shownFinishTime(trip)
                Text(
                    if (trip.storeName.isEmpty()) stringResource(R.string.shopping_resume_finished_at, time)
                    else stringResource(R.string.shopping_resume_finished_at_store, trip.storeName, time),
                    style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSecondaryContainer,
                )
                if (trip.boughtItemCount > 0) {
                    Text(
                        pluralStringResource(R.plurals.shopping_resume_items_back, trip.boughtItemCount, trip.boughtItemCount),
                        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSecondaryContainer.copy(alpha = 0.8f),
                    )
                }
            }
            Button(onClick = onResume, enabled = !isResuming, modifier = Modifier.heightIn(min = 48.dp)) { Text(stringResource(R.string.shopping_resume)) }
        }
    }
}

/** The row at the top of the Shopping home while the person shops: where, and how many they have in their cart. It opens the store. */
@Composable
internal fun ShoppingShoppingRow(trip: ShoppingTrip, cartCount: Int, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Card(
        onClick = onClick,
        modifier = modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp).heightIn(min = 56.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.primaryContainer),
        shape = RoundedCornerShape(16.dp),
    ) {
        Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 14.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Icon(Icons.Outlined.ShoppingCart, contentDescription = null, tint = MaterialTheme.colorScheme.onPrimaryContainer)
            Text(
                if (trip.storeName.isEmpty()) stringResource(R.string.shopping_shopping_anywhere, cartCount)
                else stringResource(R.string.shopping_shopping_at, trip.storeName, cartCount),
                style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onPrimaryContainer, modifier = Modifier.weight(1f),
            )
        }
    }
}

@Composable
internal fun rememberShoppingWords(): ShoppingWords {
    val context = LocalContext.current
    return androidx.compose.runtime.remember(context) { ShoppingWords(context) }
}

