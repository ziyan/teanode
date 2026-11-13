package com.teanode.app.settings

import android.content.Intent
import android.os.Build
import android.provider.Settings
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ExitToApp
import androidx.compose.material.icons.outlined.AccountCircle
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Apps
import androidx.compose.material.icons.outlined.CloudUpload
import androidx.compose.material.icons.outlined.Devices
import androidx.compose.material.icons.outlined.Group
import androidx.compose.material.icons.outlined.IosShare
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.PhoneAndroid
import androidx.compose.material.icons.outlined.Tune
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Inbox
import androidx.compose.material.icons.outlined.Terminal
import androidx.compose.material.icons.outlined.VerifiedUser
import androidx.compose.material3.Icon
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shell.Routes
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.CountBadge
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeNavigationRow
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.theme.AppearanceChoice
import com.teanode.kit.AppSection
import com.teanode.kit.access.OperatorAccess
import com.teanode.kit.access.operatorAccess
import com.teanode.kit.circles.circleInvitations

/** Opens a place another part owns, saying so when this build has no such place. */
@Composable
fun rememberOpenPlace(): (String) -> Unit {
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val missingText = stringResource(R.string.settings_place_missing)
    return { route ->
        try {
            navigator.navigate(route)
        } catch (missing: IllegalArgumentException) {
            snackbar.showProblem(missingText)
        }
    }
}

@Composable
fun appearanceTitle(choice: AppearanceChoice): String = when (choice) {
    AppearanceChoice.System -> stringResource(R.string.settings_appearance_system)
    AppearanceChoice.Light -> stringResource(R.string.settings_appearance_light)
    AppearanceChoice.Dark -> stringResource(R.string.settings_appearance_dark)
}

/** The first screen of Settings: the accounts, the person's own pages, the agent's, the server's, the look of the app, and signing out. */
@Composable
fun SettingsSection() {
    val session = LocalAppSession.current
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val context = LocalContext.current
    val openPlace = rememberOpenPlace()
    val facts by session.facts.collectAsState()
    val accounts by session.accounts.collectAsState()
    val appearance by session.appearance.collectAsState()
    var operatorAccess by remember { mutableStateOf<OperatorAccess?>(null) }
    var invitationCount by remember { mutableStateOf(0) }
    var isConfirmingSignOut by remember { mutableStateOf(false) }
    val canUseCircles = facts.session?.holds("circle:use") == true
    LaunchedEffect(connection) { operatorAccess = runCatching { connection.operatorAccess() }.getOrNull() }
    LaunchedEffect(connection, canUseCircles) { invitationCount = if (canUseCircles) runCatching { connection.circleInvitations().size }.getOrDefault(0) else 0 }
    val version = remember { versionText(context) }
    TeaNodeScreen(title = stringResource(R.string.section_settings)) { contentPadding ->
        LazyColumn(Modifier.padding(contentPadding), contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 24.dp)) {
            item { TeaNodeSectionHeader(stringResource(R.string.settings_accounts)) }
            items(accounts.accounts.size) { index ->
                val account = accounts.accounts[index]
                val isActive = account.accountKey == accounts.active?.accountKey
                TeaNodeRow(
                    headline = account.username, supporting = account.server.host,
                    leading = { AvatarView(account.username, null, null, size = 36.dp) },
                    trailing = if (isActive) ({ RadioButton(selected = true, onClick = null) }) else null,
                    onClick = if (isActive) null else ({ session.switchTo(account) }),
                )
            }
            item { TeaNodeRow(headline = stringResource(R.string.settings_add_account), leadingIcon = Icons.Outlined.Add, onClick = { session.startAddingAccount() }) }

            item { TeaNodeSectionHeader(stringResource(R.string.settings_your_account)) }
            item { TeaNodeNavigationRow(stringResource(R.string.settings_profile_title), { navigator.navigate(SettingsRoutes.PROFILE) }, leadingIcon = Icons.Outlined.AccountCircle) }
            if (canUseCircles) {
                item {
                    TeaNodeNavigationRow(
                        stringResource(R.string.settings_circles_title), { navigator.navigate(SettingsRoutes.CIRCLES) }, leadingIcon = Icons.Outlined.Group,
                        trailing = { CountBadge(invitationCount) },
                    )
                }
                item { TeaNodeNavigationRow(stringResource(R.string.settings_sharing_title), { navigator.navigate(SettingsRoutes.SHARING) }, leadingIcon = Icons.Outlined.IosShare) }
            }
            item { TeaNodeNavigationRow(stringResource(R.string.settings_password_title), { navigator.navigate(SettingsRoutes.PASSWORD) }, leadingIcon = Icons.Outlined.Key) }
            item { TeaNodeNavigationRow(stringResource(R.string.settings_passkeys_title), { navigator.navigate(SettingsRoutes.PASSKEYS) }, leadingIcon = Icons.Outlined.VerifiedUser) }
            item { TeaNodeNavigationRow(stringResource(R.string.settings_tokens_title), { navigator.navigate(SettingsRoutes.API_TOKENS) }, leadingIcon = Icons.Outlined.Terminal) }
            item { TeaNodeNavigationRow(stringResource(R.string.settings_devices_title), { navigator.navigate(SettingsRoutes.DEVICES) }, leadingIcon = Icons.Outlined.PhoneAndroid) }
            item { TeaNodeNavigationRow(stringResource(R.string.settings_apps_title), { navigator.navigate(SettingsRoutes.APPS) }, leadingIcon = Icons.Outlined.Apps) }
            item { TeaNodeNavigationRow(stringResource(R.string.settings_sessions_title), { navigator.navigate(SettingsRoutes.SESSIONS) }, leadingIcon = Icons.Outlined.Devices) }

            if (facts.isAvailable(AppSection.Photos)) {
                item { TeaNodeSectionHeader(stringResource(R.string.section_photos)) }
                item { TeaNodeNavigationRow(stringResource(R.string.settings_photo_backup), { openPlace(Routes.child(AppSection.Photos, "backup")) }, leadingIcon = Icons.Outlined.CloudUpload) }
            }

            item { TeaNodeSectionHeader(stringResource(R.string.settings_your_agent)) }
            item { TeaNodeNavigationRow(stringResource(R.string.settings_agent_title), { navigator.navigate(SettingsRoutes.AGENT) }, leadingIcon = Icons.Outlined.Tune) }

            val access = operatorAccess
            if (access != null && access.canOpenManage) {
                item { TeaNodeSectionHeader(stringResource(R.string.settings_manage)) }
                item { TeaNodeNavigationRow(stringResource(R.string.settings_manage), { navigator.navigate(SettingsRoutes.MANAGE) }, leadingIcon = Icons.Outlined.Dns) }
                item { FooterText(stringResource(R.string.settings_manage_footer)) }
            }

            item { TeaNodeSectionHeader(stringResource(R.string.settings_display)) }
            item {
                ChoiceRow(
                    stringResource(R.string.settings_appearance), AppearanceChoice.entries, appearance, label = { choice -> appearanceTitle(choice) },
                    onSelect = { choice -> session.setAppearance(choice) },
                )
            }
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                item {
                    TeaNodeRow(
                        headline = stringResource(R.string.settings_language), supporting = stringResource(R.string.settings_language_detail),
                        onClick = {
                            context.startActivity(Intent(Settings.ACTION_APP_LOCALE_SETTINGS, android.net.Uri.fromParts("package", context.packageName, null)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                        },
                    )
                }
            }
            if (facts.services?.isMailboxEnabled != false) {
                item { TeaNodeSectionHeader(stringResource(R.string.settings_mailbox)) }
                item { TeaNodeNavigationRow(stringResource(R.string.settings_mailbox), { openPlace("mail/settings") }, leadingIcon = Icons.Outlined.Inbox) }
            }
            item { TeaNodeRow(headline = if (accounts.accounts.size > 1) stringResource(R.string.settings_sign_out_of, accounts.active?.server?.host.orEmpty()) else stringResource(R.string.settings_sign_out), leadingIcon = Icons.AutoMirrored.Outlined.ExitToApp, onClick = { isConfirmingSignOut = true }) }
            item { FooterText(stringResource(R.string.settings_sign_out_footer)) }
            item { TeaNodeSectionHeader(stringResource(R.string.settings_about)) }
            item { TeaNodeRow(headline = stringResource(R.string.settings_version), trailing = { Text(version) }) }
        }
    }
    if (isConfirmingSignOut) {
        com.teanode.app.session.SignOutConfirmation(accounts.active, onConfirm = { isConfirmingSignOut = false; session.signOut() }, onDismiss = { isConfirmingSignOut = false })
    }
}

private fun versionText(context: android.content.Context): String = try {
    val info = context.packageManager.getPackageInfo(context.packageName, 0)
    "${info.versionName} (${androidx.core.content.pm.PackageInfoCompat.getLongVersionCode(info)})"
} catch (missing: Exception) {
    ""
}
