package com.teanode.app.settings

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Group
import androidx.compose.material.icons.outlined.Groups
import androidx.compose.material.icons.outlined.History
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.Shield
import androidx.compose.material.icons.outlined.ToggleOn
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.Checkbox
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.SnackbarController
import com.teanode.app.ui.components.TeaNodeNavigationRow
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.AccessSession
import com.teanode.kit.Connection
import com.teanode.kit.access.AccessDomain
import com.teanode.kit.access.AccessGroup
import com.teanode.kit.access.AccessGroupDraft
import com.teanode.kit.access.AccessPermission
import com.teanode.kit.access.AccessPermissionKind
import com.teanode.kit.access.AccessRole
import com.teanode.kit.access.AccessRoleDraft
import com.teanode.kit.access.AccessUser
import com.teanode.kit.access.AccessUserDraft
import com.teanode.kit.access.AuditAction
import com.teanode.kit.access.AuditEvent
import com.teanode.kit.access.OperatorAccess
import com.teanode.kit.access.accessDomains
import com.teanode.kit.access.accessGroups
import com.teanode.kit.access.accessPermissions
import com.teanode.kit.access.accessRoles
import com.teanode.kit.access.accessUsers
import com.teanode.kit.access.auditEvents
import com.teanode.kit.access.canManageGroups
import com.teanode.kit.access.canManageRoles
import com.teanode.kit.access.canManageUsers
import com.teanode.kit.access.createAccessGroup
import com.teanode.kit.access.createAccessRole
import com.teanode.kit.access.createAccessUser
import com.teanode.kit.access.deleteAccessGroup
import com.teanode.kit.access.deleteAccessRole
import com.teanode.kit.access.deleteAccessUser
import com.teanode.kit.access.operatorAccess
import com.teanode.kit.access.setAccessGroupDomains
import com.teanode.kit.access.setAccessGroupMembers
import com.teanode.kit.access.setAccessGroupRoles
import com.teanode.kit.access.setAccessRolePermissions
import com.teanode.kit.access.setAccessUserDisabled
import com.teanode.kit.access.setAccessUserPassword
import com.teanode.kit.access.updateAccessGroup
import com.teanode.kit.access.updateAccessRole
import com.teanode.kit.access.updateAccessUser
import com.teanode.kit.accessSession
import com.teanode.kit.photos.ServerTime
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch

/** What the people, groups and roles pages share: everything read together, and every change said and read back. */
@Stable
class AccessDirectory(private val connection: Connection, private val scope: CoroutineScope, private val snackbar: SnackbarController) {
    var session: AccessSession? by mutableStateOf(null)
    val users = mutableStateListOf<AccessUser>()
    val groups = mutableStateListOf<AccessGroup>()
    val roles = mutableStateListOf<AccessRole>()
    val permissions = mutableStateListOf<AccessPermission>()
    val domains = mutableStateListOf<AccessDomain>()
    var isLoaded: Boolean by mutableStateOf(false)
    var loadError: Throwable? by mutableStateOf(null)
    var isBusy: Boolean by mutableStateOf(false)

    fun reload() {
        scope.launch { load() }
    }

    /** Reads what the person may read: a person who may only manage groups reads no accounts, so each read stands alone. */
    suspend fun load() {
        coroutineScope {
            val sessionRead = async { runCatching { connection.accessSession() } }
            val usersRead = async { runCatching { connection.accessUsers() } }
            val groupsRead = async { runCatching { connection.accessGroups() } }
            val rolesRead = async { runCatching { connection.accessRoles() } }
            val permissionsRead = async { runCatching { connection.accessPermissions() } }
            val domainsRead = async { runCatching { connection.accessDomains() } }
            sessionRead.await().getOrNull()?.let { read -> session = read }
            usersRead.await().getOrNull()?.let { read -> users.clear(); users.addAll(read) }
            val groupsResult = groupsRead.await()
            groupsResult.getOrNull()?.let { read -> groups.clear(); groups.addAll(read) }
            rolesRead.await().getOrNull()?.let { read -> roles.clear(); roles.addAll(read) }
            permissionsRead.await().getOrNull()?.let { read -> permissions.clear(); permissions.addAll(read) }
            domainsRead.await().getOrNull()?.let { read -> domains.clear(); domains.addAll(read) }
            val firstFailure = listOf(groupsResult).firstNotNullOfOrNull { result -> result.exceptionOrNull() }
            loadError = if (users.isEmpty() && groups.isEmpty() && roles.isEmpty() && firstFailure != null) firstFailure else null
            isLoaded = true
        }
    }

    fun act(saying: String, onDone: (Boolean) -> Unit = {}, work: suspend () -> Unit) {
        if (isBusy) return
        isBusy = true
        scope.launch {
            var isDone = false
            try {
                work()
                snackbar.show(saying)
                isDone = true
            } catch (failure: kotlinx.coroutines.CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                snackbar.showError(failure)
            }
            try { load() } finally { isBusy = false }
            onDone(isDone)
        }
    }
}

@Composable
fun rememberAccessDirectory(): AccessDirectory {
    val connection = LocalConnection.current
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    val directory = remember(connection) { AccessDirectory(connection, scope, snackbar) }
    LaunchedEffect(directory) { directory.load() }
    return directory
}

@Composable
private fun AccessPage(
    title: String,
    onBack: () -> Unit,
    directory: AccessDirectory,
    floatingActionButton: @Composable () -> Unit = {},
    content: androidx.compose.foundation.lazy.LazyListScope.() -> Unit,
) {
    TeaNodeScreen(title = title, onBack = onBack, floatingActionButton = floatingActionButton) { contentPadding ->
        val error = directory.loadError
        when {
            error != null -> ErrorState(LocalAppSession.current.describeLoadProblem(error), onRetry = { directory.reload() }, modifier = Modifier.padding(contentPadding))
            !directory.isLoaded -> LoadingState(Modifier.padding(contentPadding))
            else -> LazyColumn(Modifier.padding(contentPadding), contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 88.dp), content = content)
        }
    }
}

@Composable
private fun memberCountText(count: Int): String = if (count == 1) stringResource(R.string.settings_access_one_member) else stringResource(R.string.settings_access_members, count)

@Composable
private fun permissionCountText(count: Int): String = if (count == 1) stringResource(R.string.settings_access_one_permission) else stringResource(R.string.settings_access_permissions, count)

/** What a permission lets somebody do; its key when the app has no words for it yet. */
@Composable
fun permissionLabel(permissionKey: String): String {
    val resource = when (permissionKey) {
        "mail:read" -> R.string.settings_permission_mail_read
        "mail:write" -> R.string.settings_permission_mail_write
        "mail:send" -> R.string.settings_permission_mail_send
        "mailbox:manage" -> R.string.settings_permission_mailbox_manage
        "mail:audit" -> R.string.settings_permission_mail_audit
        "mail:audit-all" -> R.string.settings_permission_mail_audit_all
        "domain:manage" -> R.string.settings_permission_domain_manage
        "domain:manage-all" -> R.string.settings_permission_domain_manage_all
        "queue:manage" -> R.string.settings_permission_queue_manage
        "report:read" -> R.string.settings_permission_report_read
        "user:manage" -> R.string.settings_permission_user_manage
        "group:manage" -> R.string.settings_permission_group_manage
        "role:manage" -> R.string.settings_permission_role_manage
        "server:manage" -> R.string.settings_permission_server_manage
        "audit:read" -> R.string.settings_permission_audit_read
        "agent:use" -> R.string.settings_permission_agent_use
        "contacts:use" -> R.string.settings_permission_contacts_use
        "calendar:use" -> R.string.settings_permission_calendar_use
        "circle:use" -> R.string.settings_permission_circle_use
        "location:use" -> R.string.settings_permission_location_use
        "photos:use" -> R.string.settings_permission_photos_use
        "health:use" -> R.string.settings_permission_health_use
        "passwords:use" -> R.string.settings_permission_passwords_use
        "shopping:use" -> R.string.settings_permission_shopping_use
        "agent:audit" -> R.string.settings_permission_agent_audit
        "agent:act" -> R.string.settings_permission_agent_act
        else -> null
    }
    return if (resource == null) permissionKey else stringResource(resource)
}

@Composable
private fun permissionKindTitle(kind: AccessPermissionKind): String = stringResource(
    when (kind) {
        AccessPermissionKind.Server -> R.string.settings_access_kind_server
        AccessPermissionKind.Domain -> R.string.settings_access_kind_domain
        AccessPermissionKind.AllDomains -> R.string.settings_access_kind_all_domains
    },
)

/** Everybody with an account on the server: search, make, edit, disable, set a password, remove. */
@Composable
fun PeopleScreen(onBack: () -> Unit) {
    val directory = rememberAccessDirectory()
    val connection = LocalConnection.current
    var searchText by remember { mutableStateOf("") }
    var editing by remember { mutableStateOf<AccessUser?>(null) }
    var isAdding by remember { mutableStateOf(false) }
    var settingPassword by remember { mutableStateOf<AccessUser?>(null) }
    var removing by remember { mutableStateOf<AccessUser?>(null) }
    val canManage = directory.session?.canManageUsers == true
    val removedText = stringResource(R.string.settings_access_removed)
    val disabledText = stringResource(R.string.settings_access_disabled)
    val enabledText = stringResource(R.string.settings_access_enabled)
    AccessPage(
        stringResource(R.string.settings_people_title), onBack, directory,
        floatingActionButton = { if (canManage) FloatingActionButton(onClick = { isAdding = true }) { Icon(Icons.Outlined.Add, stringResource(R.string.settings_people_new)) } },
    ) {
        item { FormField(searchText, { searchText = it }, stringResource(R.string.common_search)) }
        val shown = directory.users.filter { user -> user.matches(searchText) }
        if (directory.users.isEmpty()) item { FooterText(stringResource(if (canManage) R.string.settings_people_none else R.string.settings_people_groups_only)) }
        items(shown.size) { index ->
            val user = shown[index]
            val isYou = user.id == directory.session?.userId
            val tags = buildList {
                if (user.isDisabled) add(stringResource(R.string.settings_access_disabled_tag))
                if (!user.hasPassword) add(stringResource(R.string.settings_people_no_password))
                if (isYou) add(stringResource(R.string.settings_circles_you))
            }
            TeaNodeRow(
                headline = user.displayName, supporting = (listOf(user.username).filter { name -> name != user.displayName } + listOfNotNull(user.email?.ifEmpty { null }) + tags).joinToString(" · ").ifEmpty { null },
                leading = { AvatarView(user.displayName, user.avatarUrl, user.avatarColorIndex, size = 40.dp) }, supportingMaxLines = 5,
                onClick = if (canManage) ({ editing = user }) else null,
            )
        }
        item { FooterText(stringResource(R.string.settings_people_footer)) }
    }
    if (isAdding || editing != null) {
        val edited = editing
        var draft by remember(edited?.id) { mutableStateOf(edited?.let { user -> AccessUserDraft.of(user) } ?: AccessUserDraft()) }
        val isYou = edited != null && edited.id == directory.session?.userId
        val savedText = stringResource(R.string.settings_saved)
        val createdText = stringResource(R.string.settings_access_created, draft.username.trim())
        FormDialog(
            title = if (edited == null) stringResource(R.string.settings_people_new) else stringResource(R.string.settings_people_edit, edited.username),
            confirmLabel = stringResource(R.string.settings_save), isConfirmEnabled = draft.canSubmit, isWorking = directory.isBusy,
            onConfirm = {
                directory.act(if (edited == null) createdText else savedText, onDone = { isDone -> if (isDone) { isAdding = false; editing = null } }) {
                    if (edited == null) connection.createAccessUser(draft) else connection.updateAccessUser(edited.id, draft)
                }
            },
            onDismiss = { isAdding = false; editing = null },
        ) {
            Column(Modifier.verticalScroll(rememberScrollState())) {
                FormField(draft.username, { draft = draft.copy(username = it) }, stringResource(R.string.settings_profile_username))
                if (edited == null) {
                    FormField(draft.password, { draft = draft.copy(password = it) }, stringResource(R.string.settings_circles_password), isPassword = true)
                    Text(stringResource(R.string.settings_people_password_footer), style = MaterialTheme.typography.bodySmall)
                }
                FormField(draft.name, { draft = draft.copy(name = it) }, stringResource(R.string.settings_profile_name))
                FormField(draft.email, { draft = draft.copy(email = it) }, stringResource(R.string.settings_people_email))
                if (edited != null) {
                    SwitchRow(stringResource(R.string.settings_people_disabled), edited.isDisabled, { isDisabled -> directory.act(if (isDisabled) disabledText else enabledText, onDone = { editing = null }) { connection.setAccessUserDisabled(edited.id, isDisabled) } }, isEnabled = !isYou)
                    Text(stringResource(R.string.settings_people_disabled_footer), style = MaterialTheme.typography.bodySmall)
                    TeaNodeRow(headline = stringResource(R.string.settings_people_set_password), leadingIcon = Icons.Outlined.Key, onClick = { settingPassword = edited; editing = null })
                    if (!isYou) TeaNodeRow(headline = stringResource(R.string.settings_people_remove), leadingIcon = Icons.Outlined.Delete, onClick = { removing = edited; editing = null })
                }
            }
        }
    }
    settingPassword?.let { user ->
        var password by remember(user.id) { mutableStateOf("") }
        val savedText = stringResource(R.string.settings_people_password_set)
        FormDialog(
            title = stringResource(R.string.settings_people_set_password_for, user.username), confirmLabel = stringResource(R.string.settings_save), isConfirmEnabled = password.isNotEmpty(), isWorking = directory.isBusy,
            onConfirm = { directory.act(savedText, onDone = { isDone -> if (isDone) settingPassword = null }) { connection.setAccessUserPassword(user.id, password) } }, onDismiss = { settingPassword = null },
        ) {
            FormField(password, { password = it }, stringResource(R.string.settings_password_new), isPassword = true)
            Text(stringResource(R.string.settings_people_password_signed_out), style = MaterialTheme.typography.bodySmall)
        }
    }
    removing?.let { user ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_people_remove_named, user.username), confirmLabel = stringResource(R.string.settings_remove), isDestructive = true, text = stringResource(R.string.settings_people_remove_detail),
            onConfirm = { removing = null; directory.act(removedText) { connection.deleteAccessUser(user.id) } }, onDismiss = { removing = null },
        )
    }
}

/** Groups: who is in them, the roles they carry and the domains those roles apply to. */
@Composable
fun GroupsScreen(onBack: () -> Unit) {
    val directory = rememberAccessDirectory()
    val connection = LocalConnection.current
    var editing by remember { mutableStateOf<AccessGroup?>(null) }
    var isAdding by remember { mutableStateOf(false) }
    var removing by remember { mutableStateOf<AccessGroup?>(null) }
    var choosing by remember { mutableStateOf<Pair<AccessGroup, String>?>(null) }
    val canManage = directory.session?.canManageGroups == true
    val removedText = stringResource(R.string.settings_access_removed)
    AccessPage(
        stringResource(R.string.settings_groups_title), onBack, directory,
        floatingActionButton = { if (canManage) FloatingActionButton(onClick = { isAdding = true }) { Icon(Icons.Outlined.Add, stringResource(R.string.settings_groups_new)) } },
    ) {
        if (directory.groups.isEmpty()) item { FooterText(stringResource(R.string.settings_groups_none)) }
        items(directory.groups.size) { index ->
            val group = directory.groups[index]
            TeaNodeRow(
                headline = group.name, leadingIcon = Icons.Outlined.Groups,
                supporting = listOfNotNull(group.description?.ifEmpty { null }, memberCountText(group.userIds.size)).joinToString(" · "), onClick = { editing = group },
            )
        }
        item { FooterText(stringResource(R.string.settings_groups_footer)) }
    }
    if (isAdding || editing != null) {
        val edited = editing
        var draft by remember(edited?.id) { mutableStateOf(edited?.let { group -> AccessGroupDraft.of(group) } ?: AccessGroupDraft()) }
        val savedText = stringResource(R.string.settings_saved)
        val createdText = stringResource(R.string.settings_access_created, draft.name.trim())
        FormDialog(
            title = if (edited == null) stringResource(R.string.settings_groups_new) else stringResource(R.string.settings_groups_edit, edited.name), confirmLabel = stringResource(R.string.settings_save),
            isConfirmEnabled = draft.canSubmit && canManage, isWorking = directory.isBusy,
            onConfirm = { directory.act(if (edited == null) createdText else savedText, onDone = { isDone -> if (isDone) { isAdding = false; editing = null } }) { if (edited == null) connection.createAccessGroup(draft) else connection.updateAccessGroup(edited.id, draft) } },
            onDismiss = { isAdding = false; editing = null },
        ) {
            Column(Modifier.verticalScroll(rememberScrollState())) {
                FormField(draft.name, { draft = draft.copy(name = it) }, stringResource(R.string.settings_profile_name))
                FormField(draft.description, { draft = draft.copy(description = it) }, stringResource(R.string.settings_groups_description), isSingleLine = false, minLines = 2)
                FormField(draft.idpGroup, { draft = draft.copy(idpGroup = it) }, stringResource(R.string.settings_groups_idp))
                Text(stringResource(R.string.settings_groups_idp_footer), style = MaterialTheme.typography.bodySmall)
                if (edited != null) {
                    TeaNodeRow(headline = stringResource(R.string.settings_groups_members), supporting = memberCountText(edited.userIds.size), onClick = { choosing = edited to "members"; editing = null })
                    TeaNodeRow(headline = stringResource(R.string.settings_groups_roles), supporting = stringResource(R.string.settings_access_chosen, edited.roleIds.size), onClick = { choosing = edited to "roles"; editing = null })
                    TeaNodeRow(headline = stringResource(R.string.settings_groups_domains), supporting = stringResource(R.string.settings_access_chosen, edited.domainIds.size), onClick = { choosing = edited to "domains"; editing = null })
                    if (canManage) TeaNodeRow(headline = stringResource(R.string.settings_groups_remove), leadingIcon = Icons.Outlined.Delete, onClick = { removing = edited; editing = null })
                }
            }
        }
    }
    choosing?.let { (group, kind) ->
        val savedText = stringResource(R.string.settings_saved)
        val options: List<Triple<String, String, String?>> = when (kind) {
            "members" -> directory.users.map { user -> Triple(user.id, user.displayName, user.username.takeIf { name -> name != user.displayName }) }
            "roles" -> directory.roles.map { role -> Triple(role.id, role.name, role.description?.ifEmpty { null }) }
            else -> directory.domains.map { domain -> Triple(domain.id, domain.domain, null) }
        }
        val initial = when (kind) { "members" -> group.userIds; "roles" -> group.roleIds; else -> group.domainIds }
        var chosen by remember(group.id, kind) { mutableStateOf(initial.toSet()) }
        FormDialog(
            title = stringResource(when (kind) { "members" -> R.string.settings_groups_who_is_in; "roles" -> R.string.settings_groups_roles; else -> R.string.settings_groups_domains }, group.name),
            confirmLabel = stringResource(R.string.settings_save), isConfirmEnabled = chosen != initial.toSet(), isWorking = directory.isBusy,
            onConfirm = {
                directory.act(savedText, onDone = { isDone -> if (isDone) choosing = null }) {
                    val ids = options.map { option -> option.first }.filter { id -> chosen.contains(id) }
                    when (kind) { "members" -> connection.setAccessGroupMembers(group.id, ids); "roles" -> connection.setAccessGroupRoles(group.id, ids); else -> connection.setAccessGroupDomains(group.id, ids) }
                }
            },
            onDismiss = { choosing = null },
        ) {
            Column(Modifier.verticalScroll(rememberScrollState())) {
                if (options.isEmpty()) Text(stringResource(R.string.settings_groups_nothing_to_choose), style = MaterialTheme.typography.bodyMedium)
                options.forEach { (id, title, subtitle) ->
                    TeaNodeRow(
                        headline = title, supporting = subtitle, modifier = Modifier.clickable { chosen = if (chosen.contains(id)) chosen - id else chosen + id },
                        trailing = { Checkbox(checked = chosen.contains(id), onCheckedChange = null) },
                    )
                }
            }
        }
    }
    removing?.let { group ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_groups_remove_named, group.name), confirmLabel = stringResource(R.string.settings_remove), isDestructive = true, text = stringResource(R.string.settings_groups_remove_detail),
            onConfirm = { removing = null; directory.act(removedText) { connection.deleteAccessGroup(group.id) } }, onDismiss = { removing = null },
        )
    }
}

/** Roles: named sets of permissions, carried by groups. */
@Composable
fun RolesScreen(onBack: () -> Unit) {
    val directory = rememberAccessDirectory()
    val connection = LocalConnection.current
    var editing by remember { mutableStateOf<AccessRole?>(null) }
    var isAdding by remember { mutableStateOf(false) }
    var removing by remember { mutableStateOf<AccessRole?>(null) }
    var ticking by remember { mutableStateOf<AccessRole?>(null) }
    val canManage = directory.session?.canManageRoles == true
    val removedText = stringResource(R.string.settings_access_removed)
    AccessPage(
        stringResource(R.string.settings_roles_title), onBack, directory,
        floatingActionButton = { if (canManage) FloatingActionButton(onClick = { isAdding = true }) { Icon(Icons.Outlined.Add, stringResource(R.string.settings_roles_new)) } },
    ) {
        if (directory.roles.isEmpty()) item { FooterText(stringResource(R.string.settings_roles_none)) }
        items(directory.roles.size) { index ->
            val role = directory.roles[index]
            TeaNodeRow(
                headline = role.name, leadingIcon = Icons.Outlined.Shield,
                supporting = listOfNotNull(role.description?.ifEmpty { null }, permissionCountText(role.permissions.size)).joinToString(" · "), onClick = { ticking = role },
                trailing = if (canManage) ({ IconButton(onClick = { editing = role }) { Icon(Icons.Outlined.Edit, stringResource(R.string.settings_edit)) } }) else null,
            )
        }
        item { FooterText(stringResource(R.string.settings_roles_footer)) }
    }
    if (isAdding || editing != null) {
        val edited = editing
        var draft by remember(edited?.id) { mutableStateOf(edited?.let { role -> AccessRoleDraft.of(role) } ?: AccessRoleDraft()) }
        val savedText = stringResource(R.string.settings_saved)
        val createdText = stringResource(R.string.settings_access_created, draft.name.trim())
        FormDialog(
            title = if (edited == null) stringResource(R.string.settings_roles_new) else stringResource(R.string.settings_roles_edit, edited.name), confirmLabel = stringResource(R.string.settings_save),
            isConfirmEnabled = draft.canSubmit, isWorking = directory.isBusy,
            onConfirm = { directory.act(if (edited == null) createdText else savedText, onDone = { isDone -> if (isDone) { isAdding = false; editing = null } }) { if (edited == null) connection.createAccessRole(draft) else connection.updateAccessRole(edited.id, draft) } },
            onDismiss = { isAdding = false; editing = null },
        ) {
            FormField(draft.name, { draft = draft.copy(name = it) }, stringResource(R.string.settings_profile_name))
            FormField(draft.description, { draft = draft.copy(description = it) }, stringResource(R.string.settings_groups_description), isSingleLine = false, minLines = 2)
            if (edited != null) TeaNodeRow(headline = stringResource(R.string.settings_roles_remove), leadingIcon = Icons.Outlined.Delete, onClick = { removing = edited; editing = null })
        }
    }
    ticking?.let { role ->
        val current = directory.roles.firstOrNull { candidate -> candidate.id == role.id } ?: role
        val savedText = stringResource(R.string.settings_saved)
        FormDialog(title = current.name, confirmLabel = stringResource(R.string.common_done), isConfirmEnabled = true, onConfirm = { ticking = null }, onDismiss = { ticking = null }) {
            Column(Modifier.verticalScroll(rememberScrollState())) {
                AccessPermissionKind.entries.forEach { kind ->
                    val inKind = directory.permissions.filter { permission -> permission.permissionKind == kind }
                    if (inKind.isNotEmpty()) {
                        TeaNodeSectionHeader(permissionKindTitle(kind))
                        inKind.forEach { permission ->
                            TeaNodeRow(
                                headline = permissionLabel(permission.key), supporting = permission.key,
                                modifier = if (canManage) Modifier.clickable {
                                    val isHeld = !current.permissions.contains(permission.key)
                                    directory.act(savedText) { connection.setAccessRolePermissions(current.id, current.permissionsSetting(permission.key, isHeld, directory.permissions)) }
                                } else Modifier,
                                trailing = { Checkbox(checked = current.permissions.contains(permission.key), onCheckedChange = null, enabled = canManage) },
                            )
                        }
                    }
                }
            }
        }
    }
    removing?.let { role ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_roles_remove_named, role.name), confirmLabel = stringResource(R.string.settings_remove), isDestructive = true, text = stringResource(R.string.settings_roles_remove_detail),
            onConfirm = { removing = null; directory.act(removedText) { connection.deleteAccessRole(role.id) } }, onDismiss = { removing = null },
        )
    }
}

@Composable
private fun auditSummary(event: AuditEvent): String {
    val name = event.resourceLabel?.ifEmpty { null } ?: event.resourceId
    return when (event.auditAction) {
        AuditAction.Create -> stringResource(R.string.settings_audit_created, event.resourceType, name)
        AuditAction.Update -> stringResource(R.string.settings_audit_changed, event.resourceType, name)
        AuditAction.Delete -> stringResource(R.string.settings_audit_removed, event.resourceType, name)
        null -> "${event.action} ${event.resourceType} $name"
    }
}

/** Every administrative change: who made it, from where, and the row before and after. */
@Composable
fun AuditLogScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    var resourceType by remember { mutableStateOf("") }
    var viewing by remember { mutableStateOf<AuditEvent?>(null) }
    val loaded = rememberLoad(resourceType) { connection.auditEvents(resourceType.ifEmpty { null }) }
    val everything = stringResource(R.string.settings_audit_everything)
    LoadedSettingsPage(
        stringResource(R.string.settings_audit_title), onBack, loaded, isEmpty = { page -> page.events.isEmpty() },
        empty = { EmptyState(Icons.Outlined.History, stringResource(R.string.settings_audit_none)) },
    ) { page ->
        item {
            ChoiceRow(stringResource(R.string.settings_audit_show_changes_to), listOf("") + AuditEvent.resourceTypes, resourceType, label = { type -> type.ifEmpty { everything } }, onSelect = { resourceType = it })
        }
        items(page.events.size) { index ->
            val event = page.events[index]
            val details = buildList {
                ServerTime.instant(event.createdAt)?.let { instant -> add(shortDateTime(instant)) }
                event.sourceIp?.ifEmpty { null }?.let { address -> add(address) }
                event.impersonatorLabel?.ifEmpty { null }?.let { label -> add(stringResource(R.string.settings_audit_impersonated, label)) }
            }
            TeaNodeRow(
                headline = auditSummary(event), supporting = (listOf(event.actorName + if (event.isByAgent) " (${stringResource(R.string.settings_audit_via_agent)})" else "") + details).joinToString(" · "),
                supportingMaxLines = 3, onClick = { viewing = event },
            )
        }
        item { FooterText(stringResource(R.string.settings_audit_footer, page.events.size, page.total)) }
    }
    viewing?.let { event ->
        FormDialog(title = auditSummary(event), confirmLabel = stringResource(R.string.common_done), isConfirmEnabled = true, onConfirm = { viewing = null }, onDismiss = { viewing = null }) {
            Column(Modifier.verticalScroll(rememberScrollState())) {
                if (event.isByAgent) Text(stringResource(R.string.settings_audit_agent_made), style = MaterialTheme.typography.bodySmall)
                TeaNodeRow(headline = event.actorName, supporting = event.resourceId)
                if (event.changedFields.isEmpty()) Text(stringResource(R.string.settings_audit_nothing_differs), style = MaterialTheme.typography.bodyMedium)
                event.changedFields.forEach { field ->
                    val unset = stringResource(R.string.settings_audit_unset)
                    TeaNodeRow(headline = field, supporting = "${AuditEvent.describe(event.beforeValue(field)) ?: unset}  →  ${AuditEvent.describe(event.afterValue(field)) ?: unset}", supportingMaxLines = 6)
                }
            }
        }
    }
}

/** The server's pages the person may open: people, groups, roles, the audit log, services, and the mail pages of the mail part. */
@Composable
fun ManageScreen(onBack: () -> Unit, onOpen: (String) -> Unit) {
    val connection = LocalConnection.current
    val access = rememberLoad { connection.operatorAccess() }
    val openPlace = rememberOpenPlace()
    LoadedSettingsPage(stringResource(R.string.settings_manage), onBack, access) { operator: OperatorAccess ->
        item { FooterText(stringResource(R.string.settings_manage_footer)) }
        if (operator.canOpenMailDelivery) {
            if (operator.canAuditMail) item { TeaNodeNavigationRow(stringResource(R.string.settings_manage_mail), { openPlace("mail/manage/all-mail") }, leadingIcon = Icons.Outlined.Group) }
            if (operator.canManageQueue) item { TeaNodeNavigationRow(stringResource(R.string.settings_manage_queue), { openPlace("mail/manage/queue") }, leadingIcon = Icons.Outlined.History) }
            if (operator.canReadReports) item { TeaNodeNavigationRow(stringResource(R.string.settings_manage_reports), { openPlace("mail/manage/reports") }, leadingIcon = Icons.Outlined.Shield) }
            if (operator.canManageDomains) item { TeaNodeNavigationRow(stringResource(R.string.settings_manage_domains), { openPlace("mail/manage/domains") }, leadingIcon = Icons.Outlined.Group) }
        }
        if (operator.isOperator) {
            item { TeaNodeNavigationRow(stringResource(R.string.settings_people_title), { onOpen(SettingsRoutes.MANAGE_PEOPLE) }, leadingIcon = Icons.Outlined.Person) }
            item { TeaNodeNavigationRow(stringResource(R.string.settings_groups_title), { onOpen(SettingsRoutes.MANAGE_GROUPS) }, leadingIcon = Icons.Outlined.Groups) }
            item { TeaNodeNavigationRow(stringResource(R.string.settings_roles_title), { onOpen(SettingsRoutes.MANAGE_ROLES) }, leadingIcon = Icons.Outlined.Shield) }
            item { TeaNodeNavigationRow(stringResource(R.string.settings_audit_title), { onOpen(SettingsRoutes.MANAGE_AUDIT) }, leadingIcon = Icons.Outlined.History) }
        }
        if (operator.canOpenAgentAdmin) item { TeaNodeNavigationRow(stringResource(R.string.settings_agents_title), { onOpen(SettingsRoutes.MANAGE_AGENTS) }, leadingIcon = Icons.Outlined.Person) }
        if (operator.canManageServer) item { TeaNodeNavigationRow(stringResource(R.string.settings_server_title), { onOpen(SettingsRoutes.MANAGE_SERVER) }, leadingIcon = Icons.Outlined.Dns) }
        if (operator.canOpenAgentAdmin) item { TeaNodeNavigationRow(stringResource(R.string.settings_usage_title), { onOpen(SettingsRoutes.MANAGE_USAGE) }, leadingIcon = Icons.Outlined.History) }
        if (operator.canManageServer) item { TeaNodeNavigationRow(stringResource(R.string.settings_services_title), { onOpen(SettingsRoutes.MANAGE_SERVICES) }, leadingIcon = Icons.Outlined.ToggleOn) }
    }
}
