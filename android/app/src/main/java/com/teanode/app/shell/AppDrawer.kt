package com.teanode.app.shell

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.ArrowDropDown
import androidx.compose.material.icons.automirrored.outlined.Logout
import androidx.compose.material.icons.outlined.ViewWeek
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.NavigationDrawerItem
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.clickable
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.CountBadge
import com.teanode.kit.AppSection
import com.teanode.kit.AppSectionGroup
import com.teanode.kit.SectionFacts

/**
 * The drawer holds every part of TeaNode in the groups the iPhone's More tab uses, with Settings last, and above them
 * the account in use. A part the server does not offer, or the person may not use, is left out.
 */
@Composable
fun AppDrawer(
    currentSection: AppSection?,
    facts: SectionFacts,
    badgeCounts: Map<AppSection, Int>,
    onOpen: (AppSection) -> Unit,
    onEditTabBar: () -> Unit,
) {
    ModalDrawerSheet {
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState())) {
            AccountHeader()
            for (group in AppSectionGroup.entries) {
                val sections = group.sections.filter { section -> facts.isAvailable(section) }
                if (sections.isEmpty()) continue
                Text(
                    text = stringResource(group.titleResource),
                    style = MaterialTheme.typography.titleSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(start = 28.dp, end = 16.dp, top = 16.dp, bottom = 8.dp),
                )
                for (section in sections) DrawerItem(section, currentSection == section, badgeCounts[section] ?: 0, onOpen)
            }
        }
        // Settings and the tab bar are always in reach, however long the list above is.
        HorizontalDivider(Modifier.padding(horizontal = 28.dp, vertical = 8.dp))
        DrawerItem(AppSection.Settings, currentSection == AppSection.Settings, 0, onOpen)
        NavigationDrawerItem(
            label = { Text(stringResource(R.string.shell_edit_tab_bar)) },
            icon = { Icon(Icons.Outlined.ViewWeek, contentDescription = null) },
            selected = false,
            onClick = onEditTabBar,
            modifier = Modifier.padding(horizontal = 12.dp),
        )
        Spacer(Modifier.height(12.dp))
    }
}

@Composable
private fun DrawerItem(section: AppSection, isSelected: Boolean, badgeCount: Int, onOpen: (AppSection) -> Unit) {
    NavigationDrawerItem(
        label = { Text(stringResource(section.titleResource)) },
        icon = { Icon(section.icon, contentDescription = null) },
        badge = { CountBadge(badgeCount) },
        selected = isSelected,
        onClick = { onOpen(section) },
        modifier = Modifier.padding(horizontal = 12.dp),
    )
}

/** Who is signed in, where, and the menu to switch to another account, add one or sign out. */
@Composable
private fun AccountHeader() {
    val session = LocalAppSession.current
    val ring by session.accounts.collectAsState()
    val account = ring.active ?: return
    var isMenuOpen by remember { mutableStateOf(false) }
    Row(
        Modifier.fillMaxWidth().clickable { isMenuOpen = true }.padding(horizontal = 28.dp, vertical = 20.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        AvatarView(name = account.username, avatarUrl = null, colorIndex = null, size = 44.dp)
        Column(Modifier.weight(1f)) {
            Text(account.username, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                account.server.host, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1, overflow = TextOverflow.Ellipsis,
            )
        }
        Icon(Icons.Outlined.ArrowDropDown, contentDescription = stringResource(R.string.shell_account_menu))
        DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
            for (other in ring.accounts.filter { kept -> kept.accountKey != account.accountKey }) {
                DropdownMenuItem(
                    text = { Text("${other.username} (${other.server.host})") },
                    onClick = {
                        isMenuOpen = false
                        session.switchTo(other)
                    },
                )
            }
            DropdownMenuItem(
                text = { Text(stringResource(R.string.signin_add_account)) },
                leadingIcon = { Icon(Icons.Outlined.Add, contentDescription = null) },
                onClick = {
                    isMenuOpen = false
                    session.startAddingAccount()
                },
            )
            DropdownMenuItem(
                text = { Text(stringResource(R.string.shell_sign_out)) },
                leadingIcon = { Icon(Icons.AutoMirrored.Outlined.Logout, contentDescription = null) },
                onClick = {
                    isMenuOpen = false
                    session.signOut()
                },
            )
        }
    }
}

