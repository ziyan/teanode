package com.teanode.app.settings.manage

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Group
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material.icons.outlined.PersonOff
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
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
import com.teanode.app.settings.FooterText
import com.teanode.app.settings.FormDialog
import com.teanode.app.settings.FormField
import com.teanode.app.settings.SettingsPage
import com.teanode.app.settings.SwitchRow
import com.teanode.app.settings.rememberAccessDirectory
import com.teanode.app.settings.shortDateTime
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.access.AccessUser
import com.teanode.kit.access.AccessUserDraft
import com.teanode.kit.access.canManageUsers
import com.teanode.kit.access.deleteAccessUser
import com.teanode.kit.access.setAccessUserDisabled
import com.teanode.kit.access.setAccessUserPassword
import com.teanode.kit.access.updateAccessUser
import com.teanode.kit.circles.CircleMemberRole
import com.teanode.kit.circles.PeopleCircle
import com.teanode.kit.circles.addCircleMember
import com.teanode.kit.circles.circles
import com.teanode.kit.photos.ServerTime

/** The groups' names said as a list: "A, B and C" in the reader's language. */
private fun namesText(names: List<String>): String = android.icu.text.ListFormatter.getInstance(java.util.Locale.getDefault()).format(names)

/**
 * One person: their account, whether they may sign in, the groups they are in, and the roles and domains those groups give them. Their
 * password is set and the person removed from here.
 */
@Composable
fun PersonDetailScreen(userId: String, onBack: () -> Unit) {
    val directory = rememberAccessDirectory()
    val connection = LocalConnection.current
    val resources = LocalContext.current.resources
    var circles by remember { mutableStateOf<List<PeopleCircle>>(emptyList()) }
    LaunchedEffect(connection) { circles = runCatching { connection.circles() }.getOrDefault(emptyList()) }
    var isEditing by remember { mutableStateOf(false) }
    var isSettingPassword by remember { mutableStateOf(false) }
    var isAddingToCircle by remember { mutableStateOf(false) }
    var isRemoving by remember { mutableStateOf(false) }
    val user = directory.users.firstOrNull { candidate -> candidate.id == userId }
    val canManage = directory.session?.canManageUsers == true
    val isCurrentUser = userId == directory.session?.userId
    TeaNodeScreen(
        title = user?.displayName.orEmpty(), onBack = onBack,
        actions = { if (canManage && user != null) IconButton(onClick = { isEditing = true }) { Icon(Icons.Outlined.Edit, stringResource(R.string.manage_edit)) } },
    ) { contentPadding ->
        when {
            directory.loadError != null -> ErrorState(LocalAppSession.current.describeLoadProblem(directory.loadError!!), onRetry = { directory.reload() }, modifier = Modifier.padding(contentPadding))
            !directory.isLoaded -> LoadingState(Modifier.padding(contentPadding))
            user == null -> EmptyState(Icons.Outlined.PersonOff, stringResource(R.string.manage_person_gone), Modifier.padding(contentPadding))
            else -> {
                val memberships = directory.groups.filter { group -> user.groupIds.contains(group.id) }
                val givenRoles = directory.roles.filter { role -> memberships.any { group -> group.roleIds.contains(role.id) } }
                val givenDomains = directory.domains.filter { domain -> memberships.any { group -> group.domainIds.contains(domain.id) } }
                Column(Modifier.padding(contentPadding).verticalScroll(rememberScrollState())) {
                    TeaNodeRow(
                        headline = user.displayName, supporting = if (isCurrentUser) stringResource(R.string.manage_person_this_is_you) else null,
                        leading = { AvatarView(user.displayName, user.avatarUrl, user.avatarColorIndex, size = 48.dp) },
                    )
                    TeaNodeRow(headline = stringResource(R.string.settings_profile_username), supporting = user.username)
                    TeaNodeRow(headline = stringResource(R.string.settings_profile_name), supporting = user.name?.ifEmpty { null } ?: stringResource(R.string.manage_not_set))
                    TeaNodeRow(headline = stringResource(R.string.settings_people_email), supporting = user.email?.ifEmpty { null } ?: stringResource(R.string.manage_not_set))
                    TeaNodeRow(headline = stringResource(R.string.settings_circles_password), supporting = stringResource(if (user.hasPassword) R.string.manage_set else R.string.settings_people_no_password))
                    ServerTime.instant(user.createdAt)?.let { instant -> TeaNodeRow(headline = stringResource(R.string.manage_created), supporting = shortDateTime(instant)) }
                    if (canManage && !isCurrentUser) {
                        SwitchRow(
                            stringResource(R.string.settings_people_disabled), user.isDisabled,
                            { isDisabled -> directory.act(resources.getString(if (isDisabled) R.string.settings_access_disabled else R.string.settings_access_enabled)) { connection.setAccessUserDisabled(user.id, isDisabled) } },
                        )
                        FooterText(stringResource(R.string.settings_people_disabled_footer))
                    }
                    TeaNodeSectionHeader(stringResource(R.string.manage_person_groups))
                    if (memberships.isEmpty()) TeaNodeRow(headline = stringResource(R.string.manage_person_no_group))
                    memberships.forEach { group -> TeaNodeRow(headline = group.name, leadingIcon = Icons.Outlined.Group) }
                    FooterText(stringResource(R.string.manage_person_groups_footer))
                    if (givenRoles.isNotEmpty()) {
                        TeaNodeSectionHeader(stringResource(R.string.manage_person_roles))
                        givenRoles.forEach { role ->
                            TeaNodeRow(headline = role.name, supporting = stringResource(R.string.manage_person_through, namesText(memberships.filter { group -> group.roleIds.contains(role.id) }.map { group -> group.name })))
                        }
                        FooterText(stringResource(R.string.manage_person_roles_footer))
                    }
                    if (givenDomains.isNotEmpty()) {
                        TeaNodeSectionHeader(stringResource(R.string.manage_person_domains))
                        givenDomains.forEach { domain ->
                            TeaNodeRow(headline = domain.domain, supporting = stringResource(R.string.manage_person_through, namesText(memberships.filter { group -> group.domainIds.contains(domain.id) }.map { group -> group.name })))
                        }
                    }
                    if (canManage) {
                        TeaNodeSectionHeader(stringResource(R.string.manage_person_actions))
                        TeaNodeRow(headline = stringResource(R.string.settings_people_set_password), leadingIcon = Icons.Outlined.Key, onClick = { isSettingPassword = true })
                        TeaNodeRow(headline = stringResource(R.string.manage_add_to_circle), leadingIcon = Icons.Outlined.Group, onClick = { isAddingToCircle = true })
                        if (!isCurrentUser) TeaNodeRow(headline = stringResource(R.string.settings_people_remove), leadingIcon = Icons.Outlined.Delete, onClick = { isRemoving = true })
                    }
                }
            }
        }
    }
    if (user != null) {
        if (isEditing) PersonEditDialog(user, directory.isBusy, onDismiss = { isEditing = false }) { draft ->
            directory.act(resources.getString(R.string.settings_saved), onDone = { isDone -> if (isDone) isEditing = false }) { connection.updateAccessUser(user.id, draft) }
        }
        if (isSettingPassword) {
            var password by remember { mutableStateOf("") }
            FormDialog(
                title = stringResource(R.string.settings_people_set_password_for, user.username), confirmLabel = stringResource(R.string.settings_save), isConfirmEnabled = password.isNotEmpty(), isWorking = directory.isBusy,
                onConfirm = { directory.act(resources.getString(R.string.settings_people_password_set), onDone = { isDone -> if (isDone) isSettingPassword = false }) { connection.setAccessUserPassword(user.id, password) } },
                onDismiss = { isSettingPassword = false },
            ) {
                FormField(password, { typed -> password = typed }, stringResource(R.string.settings_password_new), isPassword = true)
                Text(stringResource(R.string.settings_people_password_signed_out), style = MaterialTheme.typography.bodySmall)
            }
        }
        if (isAddingToCircle) AddToCircleDialog(user, circles, onDismiss = { isAddingToCircle = false }) { circle ->
            isAddingToCircle = false
            directory.act(resources.getString(R.string.manage_now_in_circle, user.username, circle.name)) { connection.addCircleMember(circle.id, user.username, CircleMemberRole.Member) }
        }
        if (isRemoving) ConfirmationDialog(
            title = stringResource(R.string.settings_people_remove_named, user.username), confirmLabel = stringResource(R.string.settings_remove), isDestructive = true, text = stringResource(R.string.settings_people_remove_detail),
            onConfirm = { isRemoving = false; directory.act(resources.getString(R.string.settings_access_removed), onDone = { isDone -> if (isDone) onBack() }) { connection.deleteAccessUser(user.id) } },
            onDismiss = { isRemoving = false },
        )
    }
}

/** Puts an existing account in one of the circles the reader is in, with no invitation to accept: for the operator who knows the account is there already. */
@Composable
fun AddToCircleDialog(user: AccessUser, circles: List<PeopleCircle>, onDismiss: () -> Unit, onChoose: (PeopleCircle) -> Unit) {
    FormDialog(
        title = stringResource(R.string.manage_add_to_circle), confirmLabel = stringResource(R.string.common_done), isConfirmEnabled = true, onConfirm = onDismiss, onDismiss = onDismiss,
    ) {
        Column(Modifier.verticalScroll(rememberScrollState())) {
            Text(user.displayName, style = MaterialTheme.typography.titleSmall)
            if (circles.isEmpty()) Text(stringResource(R.string.manage_no_circle_yet), style = MaterialTheme.typography.bodyMedium)
            circles.forEach { circle -> TeaNodeRow(headline = circle.name, leadingIcon = Icons.Outlined.Group, onClick = { onChoose(circle) }) }
        }
    }
}

@Composable
private fun PersonEditDialog(user: AccessUser, isBusy: Boolean, onDismiss: () -> Unit, onSave: (AccessUserDraft) -> Unit) {
    var draft by remember(user.id) { mutableStateOf(AccessUserDraft.of(user)) }
    FormDialog(
        title = stringResource(R.string.settings_people_edit, user.username), confirmLabel = stringResource(R.string.settings_save), isConfirmEnabled = draft.canSubmit, isWorking = isBusy,
        onConfirm = { onSave(draft) }, onDismiss = onDismiss,
    ) {
        Column(Modifier.verticalScroll(rememberScrollState())) {
            FormField(draft.username, { typed -> draft = draft.copy(username = typed) }, stringResource(R.string.settings_profile_username))
            FormField(draft.name, { typed -> draft = draft.copy(name = typed) }, stringResource(R.string.settings_profile_name))
            FormField(draft.email, { typed -> draft = draft.copy(email = typed) }, stringResource(R.string.settings_people_email))
            Text(stringResource(R.string.manage_person_groups_footer), style = MaterialTheme.typography.bodySmall)
        }
    }
}
