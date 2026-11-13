package com.teanode.app.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ExitToApp
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Group
import androidx.compose.material.icons.outlined.IosShare
import androidx.compose.material.icons.outlined.PersonAdd
import androidx.compose.material.icons.outlined.PersonRemove
import androidx.compose.material.icons.outlined.Send
import androidx.compose.material.icons.outlined.AdminPanelSettings
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.TeaNodeNavigationRow
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.kit.Connection
import com.teanode.kit.access.canManageUsers
import com.teanode.kit.access.createAccessUser
import com.teanode.kit.account.accountProfile
import com.teanode.kit.accessSession
import com.teanode.kit.circles.CircleAccountDraft
import com.teanode.kit.circles.CircleDraft
import com.teanode.kit.circles.CircleInvitation
import com.teanode.kit.circles.CircleInvitationDraft
import com.teanode.kit.circles.CircleKind
import com.teanode.kit.circles.CircleMember
import com.teanode.kit.circles.CircleMemberRole
import com.teanode.kit.circles.PeopleCircle
import com.teanode.kit.circles.PersonPronoun
import com.teanode.kit.circles.PersonRelationshipDraft
import com.teanode.kit.circles.RelationshipKind
import com.teanode.kit.circles.RelationshipTerm
import com.teanode.kit.circles.RelationshipTermGender
import com.teanode.kit.circles.addCircleMember
import com.teanode.kit.circles.answerCircleInvitation
import com.teanode.kit.circles.circleInvitations
import com.teanode.kit.circles.circles
import com.teanode.kit.circles.createCircle
import com.teanode.kit.circles.deleteCircle
import com.teanode.kit.circles.inviteToCircle
import com.teanode.kit.circles.leaveCircle
import com.teanode.kit.circles.relationshipTerms
import com.teanode.kit.circles.removeCircleMember
import com.teanode.kit.circles.renameCircle
import com.teanode.kit.circles.saveRelationship
import com.teanode.kit.circles.setCircleMemberRole
import com.teanode.kit.circles.withdrawCircleInvitation
import com.teanode.kit.photos.ServerTime
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch

/** What the circles screens share: the circles, the invitations, and who is reading. Everything is read again after a change. */
@Stable
class CircleDirectory(private val connection: Connection, private val scope: CoroutineScope, private val snackbar: com.teanode.app.ui.components.SnackbarController) {
    val circles = mutableStateListOf<PeopleCircle>()
    val invitations = mutableStateListOf<CircleInvitation>()
    var currentUserId: String? by mutableStateOf(null)
    var canManageUsers: Boolean by mutableStateOf(false)
    var ownPronoun: PersonPronoun by mutableStateOf(PersonPronoun.NotSet)
    var isLoaded: Boolean by mutableStateOf(false)
    var loadError: Throwable? by mutableStateOf(null)
    var isBusy: Boolean by mutableStateOf(false)

    fun circle(circleId: String): PeopleCircle? = circles.firstOrNull { circle -> circle.id == circleId }

    /** Reads the circles and the invitations. Only the circles' own refusal is said; who is reading and the invitations stay as they were when they cannot be read. */
    suspend fun load() {
        coroutineScope {
            val sessionRead = async { runCatching { connection.accessSession() }.getOrNull() }
            val invitationsRead = async { runCatching { connection.circleInvitations() }.getOrNull() }
            val profileRead = async { runCatching { connection.accountProfile() }.getOrNull() }
            try {
                val read = connection.circles()
                circles.clear()
                circles.addAll(read)
                loadError = null
                isLoaded = true
            } catch (failure: kotlinx.coroutines.CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                if (isLoaded) snackbar.showError(failure) else loadError = failure
            }
            invitationsRead.await()?.let { read -> invitations.clear(); invitations.addAll(read) }
            sessionRead.await()?.let { session -> currentUserId = session.userId; canManageUsers = session.canManageUsers }
            profileRead.await()?.let { profile -> ownPronoun = profile.chosenPronoun; if (currentUserId == null) currentUserId = profile.id }
        }
    }

    fun reload() {
        scope.launch { load() }
    }

    /** Runs a change, says it was done or why it was refused, and reads everything again either way. */
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
fun rememberCircleDirectory(): CircleDirectory {
    val connection = LocalConnection.current
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    val directory = remember(connection) { CircleDirectory(connection, scope, snackbar) }
    androidx.compose.runtime.LaunchedEffect(directory) { directory.load() }
    return directory
}

@Composable
fun circleKindTitle(kind: CircleKind): String = when (kind) {
    CircleKind.Family -> stringResource(R.string.settings_circles_family)
    CircleKind.Friends -> stringResource(R.string.settings_circles_friends)
    CircleKind.Colleagues -> stringResource(R.string.settings_circles_colleagues)
}

/** The word for what one person is to another, in the gender asked: "mother" for a parent in a feminine word. */
@Composable
fun relationshipWord(kind: RelationshipKind, gender: RelationshipTermGender): String {
    val words = when (kind) {
        RelationshipKind.Spouse -> listOf(R.string.settings_relationship_spouse, R.string.settings_relationship_husband, R.string.settings_relationship_wife)
        RelationshipKind.Partner -> listOf(R.string.settings_relationship_partner, R.string.settings_relationship_boyfriend, R.string.settings_relationship_girlfriend)
        RelationshipKind.Parent -> listOf(R.string.settings_relationship_parent, R.string.settings_relationship_father, R.string.settings_relationship_mother)
        RelationshipKind.Child -> listOf(R.string.settings_relationship_child, R.string.settings_relationship_son, R.string.settings_relationship_daughter)
        RelationshipKind.Sibling -> listOf(R.string.settings_relationship_sibling, R.string.settings_relationship_brother, R.string.settings_relationship_sister)
        RelationshipKind.Grandparent -> listOf(R.string.settings_relationship_grandparent, R.string.settings_relationship_grandfather, R.string.settings_relationship_grandmother)
        RelationshipKind.Grandchild -> listOf(R.string.settings_relationship_grandchild, R.string.settings_relationship_grandson, R.string.settings_relationship_granddaughter)
        RelationshipKind.AuntOrUncle -> listOf(R.string.settings_relationship_aunt_or_uncle, R.string.settings_relationship_uncle, R.string.settings_relationship_aunt)
        RelationshipKind.NieceOrNephew -> listOf(R.string.settings_relationship_niece_or_nephew, R.string.settings_relationship_nephew, R.string.settings_relationship_niece)
        RelationshipKind.Cousin -> listOf(R.string.settings_relationship_cousin, R.string.settings_relationship_cousin, R.string.settings_relationship_cousin)
        RelationshipKind.ParentInLaw -> listOf(R.string.settings_relationship_parent_in_law, R.string.settings_relationship_father_in_law, R.string.settings_relationship_mother_in_law)
        RelationshipKind.ChildInLaw -> listOf(R.string.settings_relationship_child_in_law, R.string.settings_relationship_son_in_law, R.string.settings_relationship_daughter_in_law)
        RelationshipKind.SiblingInLaw -> listOf(R.string.settings_relationship_sibling_in_law, R.string.settings_relationship_brother_in_law, R.string.settings_relationship_sister_in_law)
        RelationshipKind.StepParent -> listOf(R.string.settings_relationship_step_parent, R.string.settings_relationship_stepfather, R.string.settings_relationship_stepmother)
        RelationshipKind.StepChild -> listOf(R.string.settings_relationship_stepchild, R.string.settings_relationship_stepson, R.string.settings_relationship_stepdaughter)
        RelationshipKind.Friend -> listOf(R.string.settings_relationship_friend, R.string.settings_relationship_friend, R.string.settings_relationship_friend)
        RelationshipKind.Colleague -> listOf(R.string.settings_relationship_colleague, R.string.settings_relationship_colleague, R.string.settings_relationship_colleague)
    }
    return stringResource(
        when (gender) {
            RelationshipTermGender.Neutral -> words[0]
            RelationshipTermGender.Masculine -> words[1]
            RelationshipTermGender.Feminine -> words[2]
        },
    )
}

/** What a member is to the person, worded; the server's English word for a kind the app does not know; null when there is none. */
@Composable
fun relationshipWordOf(member: CircleMember): String? {
    if (!member.hasRelationship) return null
    val kind = member.knownRelationshipKind ?: return member.relationshipWord
    return relationshipWord(kind, member.knownRelationshipTermGender)
}

@Composable
private fun relationshipWordOf(term: RelationshipTerm): String {
    val kind = term.knownRelationshipKind ?: return term.word
    return relationshipWord(kind, term.knownTermGender)
}

private fun capitalized(word: String): String = word.take(1).uppercase() + word.drop(1)

@Composable
private fun memberCountText(count: Int): String = if (count == 1) stringResource(R.string.settings_circles_one_person) else stringResource(R.string.settings_circles_people, count)

/** The circles the person is in, with the invitations waiting above them. */
@Composable
fun CirclesScreen(onBack: () -> Unit, onOpenCircle: (String) -> Unit, onOpenSharing: () -> Unit) {
    val directory = rememberCircleDirectory()
    var isAdding by remember { mutableStateOf(false) }
    TeaNodeScreen(
        title = stringResource(R.string.settings_circles_title), onBack = onBack,
        floatingActionButton = { FloatingActionButton(onClick = { isAdding = true }) { Icon(Icons.Outlined.Add, stringResource(R.string.settings_circles_new)) } },
    ) { contentPadding ->
        val loadError = directory.loadError
        when {
            loadError != null && !directory.isLoaded -> ErrorState(LocalAppSession.current.describeLoadProblem(loadError), onRetry = { directory.reload() }, modifier = Modifier.padding(contentPadding))
            !directory.isLoaded -> LoadingState(Modifier.padding(contentPadding))
            directory.circles.isEmpty() && directory.invitations.isEmpty() -> EmptyState(
                Icons.Outlined.Group, stringResource(R.string.settings_circles_none),
                description = stringResource(if (directory.canManageUsers) R.string.settings_circles_none_operator else R.string.settings_circles_none_detail),
                modifier = Modifier.padding(contentPadding),
            )
            else -> androidx.compose.foundation.lazy.LazyColumn(Modifier.padding(contentPadding), contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 88.dp)) {
                invitationRows(directory)
                if (directory.circles.isNotEmpty()) {
                    items(directory.circles.size) { index ->
                        val circle = directory.circles[index]
                        CircleRow(circle, onClick = { onOpenCircle(circle.id) })
                    }
                    item { FooterText(stringResource(R.string.settings_circles_footer)) }
                    item { TeaNodeNavigationRow(stringResource(R.string.settings_sharing_title), onClick = onOpenSharing, leadingIcon = Icons.Outlined.IosShare) }
                }
            }
        }
    }
    if (isAdding) {
        CircleFormDialog(directory, editedCircle = null, onDismiss = { isAdding = false })
    }
}

private fun LazyListScope.invitationRows(directory: CircleDirectory) {
    items(directory.invitations.size) { index ->
        val invitation = directory.invitations[index]
        val joinedText = stringResource(R.string.settings_circles_joined, invitation.circleName)
        val declinedText = stringResource(R.string.settings_circles_declined)
        val connection = LocalConnection.current
        if (index == 0) TeaNodeSectionHeader(stringResource(R.string.settings_circles_invitations))
        TeaNodeRow(
            headline = invitation.circleName,
            supporting = stringResource(R.string.settings_circles_invited_by, invitation.invitedByName) + (invitation.knownCircleKind?.let { kind -> " · " + circleKindTitle(kind) } ?: ""),
            leadingIcon = Icons.Outlined.Group,
            trailing = {
                Row {
                    IconButton(onClick = { directory.act(declinedText) { connection.answerCircleInvitation(invitation.id, false) } }) { Icon(Icons.Outlined.Close, stringResource(R.string.settings_circles_decline)) }
                    IconButton(onClick = { directory.act(joinedText) { connection.answerCircleInvitation(invitation.id, true) } }) { Icon(Icons.Outlined.Check, stringResource(R.string.settings_circles_accept)) }
                }
            },
        )
    }
}

@Composable
private fun CircleRow(circle: PeopleCircle, onClick: () -> Unit) {
    val people = memberCountText(circle.members.size)
    val details = circle.knownCircleKind?.let { kind -> "${circleKindTitle(kind)} · $people" } ?: people
    TeaNodeRow(
        headline = circle.name, supporting = details, onClick = onClick,
        leading = {
            Row {
                circle.members.take(3).forEachIndexed { position, member ->
                    AvatarView(member.displayName, member.avatarUrl, member.avatarColorIndex, size = 30.dp, modifier = Modifier.offset(x = (-10 * position).dp))
                }
            }
        },
    )
}

@Composable
private fun CircleFormDialog(directory: CircleDirectory, editedCircle: PeopleCircle?, onDismiss: () -> Unit) {
    val connection = LocalConnection.current
    var draft by remember { mutableStateOf(editedCircle?.let { circle -> CircleDraft.of(circle) } ?: CircleDraft()) }
    val savedText = stringResource(if (editedCircle == null) R.string.settings_circles_made else R.string.settings_circles_renamed)
    FormDialog(
        title = stringResource(if (editedCircle == null) R.string.settings_circles_new else R.string.settings_circles_rename),
        confirmLabel = stringResource(if (editedCircle == null) R.string.settings_create else R.string.settings_save), isConfirmEnabled = draft.canSubmit, isWorking = directory.isBusy,
        onConfirm = {
            directory.act(savedText, onDone = { isDone -> if (isDone) onDismiss() }) {
                if (editedCircle == null) connection.createCircle(draft) else connection.renameCircle(editedCircle.id, draft.name)
            }
        },
        onDismiss = onDismiss,
    ) {
        FormField(draft.name, { draft = draft.copy(name = it) }, stringResource(R.string.settings_circles_name))
        if (editedCircle == null) {
            ChoiceRow(stringResource(R.string.settings_circles_kind), CircleKind.entries, draft.circleKind, label = { kind -> circleKindTitle(kind) }, onSelect = { draft = draft.copy(circleKind = it) })
        }
    }
}

/** One circle: who is in it, and for an admin the form to invite somebody, the invitations still waiting, renaming and deleting. */
@Composable
fun CircleDetailScreen(circleId: String, onBack: () -> Unit) {
    val directory = rememberCircleDirectory()
    val connection = LocalConnection.current
    val circle = directory.circle(circleId)
    var isRenaming by remember { mutableStateOf(false) }
    var isConfirmingDelete by remember { mutableStateOf(false) }
    var isConfirmingLeave by remember { mutableStateOf(false) }
    var isMakingAccount by remember { mutableStateOf(false) }
    var removing by remember { mutableStateOf<CircleMember?>(null) }
    var relationshipMember by remember { mutableStateOf<CircleMember?>(null) }
    var invitationDraft by remember { mutableStateOf(CircleInvitationDraft()) }
    var openMenuUserId by remember { mutableStateOf<String?>(null) }
    val sentText = stringResource(R.string.settings_circles_invitation_sent)
    val withdrawnText = stringResource(R.string.settings_circles_invitation_withdrawn)
    val deletedText = stringResource(R.string.settings_circles_deleted)
    val leftText = if (circle != null) stringResource(R.string.settings_circles_left, circle.name) else ""
    TeaNodeScreen(title = circle?.name.orEmpty(), onBack = onBack) { contentPadding ->
        if (circle == null) {
            if (directory.isLoaded) EmptyState(Icons.Outlined.Group, stringResource(R.string.settings_circles_gone), modifier = Modifier.padding(contentPadding)) else LoadingState(Modifier.padding(contentPadding))
            return@TeaNodeScreen
        }
        androidx.compose.foundation.lazy.LazyColumn(Modifier.padding(contentPadding), contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 24.dp)) {
            item { TeaNodeSectionHeader(stringResource(R.string.settings_circles_members)) }
            items(circle.members.size) { index ->
                val member = circle.members[index]
                val isCurrentUser = member.userId == directory.currentUserId
                val isNowMemberText = stringResource(R.string.settings_circles_is_now_member, member.displayName)
                val isNowAdminText = stringResource(R.string.settings_circles_is_now_admin, member.displayName)
                val lines = buildList {
                    if (!isCurrentUser) relationshipWordOf(member)?.let { word -> add(capitalized(word)) }
                    if (member.displayName != member.username) add(member.username)
                    if (member.isAdmin) add(stringResource(R.string.settings_circles_admin))
                }
                TeaNodeRow(
                    headline = if (isCurrentUser) "${member.displayName} (${stringResource(R.string.settings_circles_you)})" else member.displayName,
                    supporting = lines.joinToString(" · ").ifEmpty { null },
                    leading = { AvatarView(member.displayName, member.avatarUrl, member.avatarColorIndex, size = 36.dp) },
                    trailing = if (!isCurrentUser) ({
                        Row {
                            IconButton(onClick = { openMenuUserId = member.userId }) { Icon(Icons.Outlined.Edit, stringResource(R.string.common_more)) }
                            DropdownMenu(expanded = openMenuUserId == member.userId, onDismissRequest = { openMenuUserId = null }) {
                                DropdownMenuItem(text = { Text(stringResource(R.string.settings_circles_relationship)) }, onClick = { openMenuUserId = null; relationshipMember = member })
                                if (circle.isAdmin) {
                                    DropdownMenuItem(
                                        text = { Text(stringResource(if (member.isAdmin) R.string.settings_circles_make_member else R.string.settings_circles_make_admin)) },
                                        onClick = {
                                            openMenuUserId = null
                                            val role = if (member.isAdmin) CircleMemberRole.Member else CircleMemberRole.Admin
                                            directory.act(if (member.isAdmin) isNowMemberText else isNowAdminText) { connection.setCircleMemberRole(circle.id, member.userId, role) }
                                        },
                                    )
                                    DropdownMenuItem(text = { Text(stringResource(R.string.settings_circles_remove_from)) }, onClick = { openMenuUserId = null; removing = member })
                                }
                            }
                        }
                    }) else null,
                )
            }
            if (circle.isAdmin) {
                item {
                    TeaNodeSectionHeader(stringResource(R.string.settings_circles_invite))
                    FormField(invitationDraft.username, { invitationDraft = invitationDraft.copy(username = it) }, stringResource(R.string.settings_circles_username))
                    SwitchRow(stringResource(R.string.settings_circles_make_them_admin), invitationDraft.isAdmin, { invitationDraft = invitationDraft.copy(isAdmin = it) })
                    TeaNodeRow(
                        headline = stringResource(R.string.settings_circles_send_invitation), leadingIcon = Icons.Outlined.Send,
                        onClick = if (invitationDraft.canSubmit && !directory.isBusy) ({ directory.act(sentText, onDone = { isDone -> if (isDone) invitationDraft = CircleInvitationDraft() }) { connection.inviteToCircle(circle.id, invitationDraft) } }) else null,
                    )
                    FooterText(stringResource(if (directory.canManageUsers) R.string.settings_circles_invite_footer else R.string.settings_circles_invite_footer_ask))
                    if (directory.canManageUsers) {
                        TeaNodeRow(headline = stringResource(R.string.settings_circles_make_account), leadingIcon = Icons.Outlined.PersonAdd, onClick = { isMakingAccount = true })
                        FooterText(stringResource(R.string.settings_circles_make_account_footer))
                    }
                }
                if (circle.pendingInvitations.isNotEmpty()) {
                    item { TeaNodeSectionHeader(stringResource(R.string.settings_circles_waiting)) }
                    items(circle.pendingInvitations.size) { index ->
                        val pending = circle.pendingInvitations[index]
                        val expiresAt = ServerTime.instant(pending.expiresAt)
                        TeaNodeRow(
                            headline = pending.invitedUsername,
                            supporting = if (expiresAt != null) stringResource(R.string.settings_circles_invited_by_expires, pending.invitedByName, shortDateTime(expiresAt)) else stringResource(R.string.settings_circles_invited_by, pending.invitedByName),
                            trailing = { TextButton(onClick = { directory.act(withdrawnText) { connection.withdrawCircleInvitation(pending.id) } }) { Text(stringResource(R.string.settings_circles_withdraw)) } },
                        )
                    }
                }
                item {
                    TeaNodeRow(headline = stringResource(R.string.settings_circles_rename), leadingIcon = Icons.Outlined.Edit, onClick = { isRenaming = true })
                    TeaNodeRow(headline = stringResource(R.string.settings_circles_delete), leadingIcon = Icons.Outlined.Delete, onClick = { isConfirmingDelete = true })
                }
            }
            item { TeaNodeRow(headline = stringResource(R.string.settings_circles_leave), leadingIcon = Icons.AutoMirrored.Outlined.ExitToApp, onClick = { isConfirmingLeave = true }) }
        }
        if (isRenaming) CircleFormDialog(directory, circle, onDismiss = { isRenaming = false })
        if (isConfirmingDelete) {
            ConfirmationDialog(
                stringResource(R.string.settings_circles_delete), stringResource(R.string.settings_circles_delete), isDestructive = true, text = stringResource(R.string.settings_circles_delete_detail),
                onConfirm = { isConfirmingDelete = false; directory.act(deletedText, onDone = { isDone -> if (isDone) onBack() }) { connection.deleteCircle(circle.id) } }, onDismiss = { isConfirmingDelete = false },
            )
        }
        if (isConfirmingLeave) {
            ConfirmationDialog(
                stringResource(R.string.settings_circles_leave), stringResource(R.string.settings_circles_leave), isDestructive = true, text = stringResource(R.string.settings_circles_leave_detail, circle.name),
                onConfirm = { isConfirmingLeave = false; directory.act(leftText, onDone = { isDone -> if (isDone) onBack() }) { connection.leaveCircle(circle.id) } }, onDismiss = { isConfirmingLeave = false },
            )
        }
        removing?.let { member ->
            val removedText = stringResource(R.string.settings_circles_member_removed, member.displayName)
            ConfirmationDialog(
                stringResource(R.string.settings_circles_remove_title, member.displayName), stringResource(R.string.settings_circles_remove_confirm, member.displayName), isDestructive = true,
                text = stringResource(R.string.settings_circles_remove_detail, member.displayName, circle.name),
                onConfirm = { removing = null; directory.act(removedText) { connection.removeCircleMember(circle.id, member.userId) } }, onDismiss = { removing = null },
            )
        }
        if (isMakingAccount) CircleAccountDialog(directory, circle, onDismiss = { isMakingAccount = false })
        relationshipMember?.let { member -> CircleRelationshipDialog(directory, member, onDismiss = { relationshipMember = null }) }
    }
}

@Composable
private fun CircleAccountDialog(directory: CircleDirectory, circle: PeopleCircle, onDismiss: () -> Unit) {
    val connection = LocalConnection.current
    var draft by remember { mutableStateOf(CircleAccountDraft()) }
    val doneText = stringResource(R.string.settings_circles_account_made, draft.name.trim().ifEmpty { draft.trimmedUsername })
    FormDialog(
        title = stringResource(R.string.settings_circles_make_account), confirmLabel = stringResource(R.string.settings_create), isConfirmEnabled = draft.canSubmit, isWorking = directory.isBusy,
        onConfirm = {
            directory.act(doneText, onDone = { isDone -> if (isDone) onDismiss() }) {
                connection.createAccessUser(draft.accountDraft)
                connection.addCircleMember(circle.id, draft.trimmedUsername, draft.memberRole)
            }
        },
        onDismiss = onDismiss,
    ) {
        FormField(draft.username, { draft = draft.copy(username = it) }, stringResource(R.string.settings_circles_username))
        FormField(draft.name, { draft = draft.copy(name = it) }, stringResource(R.string.settings_profile_name))
        FormField(draft.password, { draft = draft.copy(password = it) }, stringResource(R.string.settings_circles_password), isPassword = true)
        SwitchRow(stringResource(R.string.settings_circles_make_them_admin), draft.isAdmin, { draft = draft.copy(isAdmin = it) })
        Text(stringResource(R.string.settings_circles_make_account_footer), style = MaterialTheme.typography.bodySmall)
    }
}

@Composable
private fun CircleRelationshipDialog(directory: CircleDirectory, member: CircleMember, onDismiss: () -> Unit) {
    val connection = LocalConnection.current
    val loaded = com.teanode.app.ui.components.rememberLoad { connection.relationshipTerms() }
    val terms = loaded.value
    val savedText = stringResource(R.string.settings_circles_relationship_saved)
    val removedText = stringResource(R.string.settings_circles_relationship_removed)
    var draft by remember(terms) { mutableStateOf(terms?.let { list -> PersonRelationshipDraft.of(member, list, directory.ownPronoun) }) }
    val current = draft
    FormDialog(
        title = member.displayName, confirmLabel = stringResource(R.string.settings_save), isConfirmEnabled = current?.canSave == true, isWorking = directory.isBusy,
        onConfirm = {
            val saving = current ?: return@FormDialog
            directory.act(if (saving.isRemoving) removedText else savedText, onDone = { isDone -> if (isDone) onDismiss() }) { connection.saveRelationship(saving, directory.currentUserId.orEmpty()) }
        },
        onDismiss = onDismiss,
    ) {
        if (current == null) {
            Text(stringResource(R.string.common_loading))
        } else {
            val none = stringResource(R.string.settings_circles_relationship_none)
            ChoiceRow(
                stringResource(R.string.settings_circles_relationship), listOf("") + current.terms.map { term -> term.term }, current.selectedTerm,
                label = { selected -> if (selected.isEmpty()) none else current.terms.firstOrNull { term -> term.term == selected }?.let { term -> capitalized(relationshipWordOf(term)) } ?: selected },
                onSelect = { draft = current.copy(selectedTerm = it) },
            )
            current.oppositeKind?.let { opposite ->
                Text(stringResource(R.string.settings_circles_will_see_you_as, member.displayName, relationshipWord(opposite, current.oppositeTermGender)), style = MaterialTheme.typography.bodySmall)
            }
            if (current.isPronounAsked) {
                ChoiceRow(
                    stringResource(R.string.settings_circles_how_describe), listOf(PersonPronoun.NotSet) + PersonPronoun.choices, current.pronoun,
                    label = { pronoun -> pronounTitle(pronoun) }, onSelect = { draft = current.copy(pronoun = it) },
                )
                Text(stringResource(R.string.settings_profile_pronoun_footer), style = MaterialTheme.typography.bodySmall)
            }
        }
    }
}
