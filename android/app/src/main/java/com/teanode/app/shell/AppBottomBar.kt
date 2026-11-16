package com.teanode.app.shell

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Menu
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import com.teanode.app.R
import com.teanode.kit.AppSection

/**
 * The bottom bar of the iPhone app's tab bar: the parts the person keeps there (three at most), then More, which opens
 * the drawer holding every part, and Search. A part waiting for the person shows its count on its icon.
 */
/** The test tag of a bar item, by the part's route name, or `more` and `search`. */
fun barTag(name: String): String = "bar-$name"

@Composable
fun AppBottomBar(
    barSections: List<AppSection>,
    currentRoute: String?,
    badgeCounts: Map<AppSection, Int>,
    onOpenSection: (AppSection) -> Unit,
    onOpenSearch: () -> Unit,
    onOpenMore: () -> Unit,
) {
    val currentSection = Routes.sectionOf(currentRoute)
    NavigationBar {
        for (section in barSections) {
            NavigationBarItem(
                selected = currentSection == section,
                onClick = { onOpenSection(section) },
                modifier = Modifier.testTag(barTag(section.routeName)),
                icon = { BadgedIcon(section.icon, badgeCounts[section] ?: 0) },
                label = { Text(stringResource(section.titleResource), maxLines = 1, overflow = TextOverflow.Ellipsis) },
            )
        }
        // What waits in the parts that are only in the drawer is counted on More.
        val hiddenCount = badgeCounts.filterKeys { section -> section !in barSections }.values.sum()
        NavigationBarItem(
            selected = false,
            onClick = onOpenMore,
            modifier = Modifier.testTag(barTag("more")),
            icon = { BadgedIcon(Icons.Outlined.Menu, hiddenCount) },
            label = { Text(stringResource(R.string.common_more), maxLines = 1) },
        )
        NavigationBarItem(
            selected = currentRoute == Routes.SEARCH,
            onClick = onOpenSearch,
            modifier = Modifier.testTag(barTag("search")),
            icon = { Icon(Icons.Outlined.Search, contentDescription = null) },
            label = { Text(stringResource(R.string.common_search), maxLines = 1) },
        )
    }
}

@Composable
private fun BadgedIcon(icon: androidx.compose.ui.graphics.vector.ImageVector, count: Int) {
    if (count > 0) {
        BadgedBox(badge = { Badge { Text(if (count > 99) "99+" else count.toString()) } }) { Icon(icon, contentDescription = null) }
    } else {
        Icon(icon, contentDescription = null)
    }
}
