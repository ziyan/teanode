package com.teanode.app.settings

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AccountCircle
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material.icons.outlined.NotificationsActive
import androidx.compose.material.icons.outlined.NotificationsOff
import androidx.compose.material.icons.outlined.PhoneAndroid
import androidx.compose.material.icons.outlined.Photo
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.TeaNodeDivider
import com.teanode.app.ui.components.TeaNodeNavigationRow
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.app.ui.components.rememberLoad
import com.teanode.app.ui.components.LoadedContent
import com.teanode.kit.account.AccountProfile
import com.teanode.kit.account.MeasurementSystem
import com.teanode.kit.account.PairedPhone
import com.teanode.kit.account.PairedPhoneDraft
import com.teanode.kit.account.PairedPhoneList
import com.teanode.kit.account.PasswordChangeDraft
import com.teanode.kit.account.Passkey
import com.teanode.kit.account.PhoneNotificationState
import com.teanode.kit.account.ProfileDraft
import com.teanode.kit.account.TimezoneMode
import com.teanode.kit.account.accountProfile
import com.teanode.kit.account.changePassword
import com.teanode.kit.account.deletePasskey
import com.teanode.kit.account.pairedPhones
import com.teanode.kit.account.passkeys
import com.teanode.kit.account.removeAccountAvatar
import com.teanode.kit.account.removePhone
import com.teanode.kit.account.renamePhone
import com.teanode.kit.account.setAccountAvatar
import com.teanode.kit.account.testPhone
import com.teanode.kit.account.updateAccountProfile
import com.teanode.kit.account.updatePhone
import com.teanode.kit.circles.PersonPronoun
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.ByteArrayOutputStream
import java.util.TimeZone

/** The profile page: picture, name, pronoun, units, username, notification address and time zone. */
@Composable
fun ProfileScreen(onBack: () -> Unit, onSaved: (AccountProfile) -> Unit = {}) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { connection.accountProfile() }
    val context = LocalContext.current
    var draft by remember { mutableStateOf<ProfileDraft?>(null) }
    var pictureProfile by remember { mutableStateOf<AccountProfile?>(null) }
    var isConfirmingRename by remember { mutableStateOf(false) }
    val savedText = stringResource(R.string.settings_saved)
    val pictureSavedText = stringResource(R.string.settings_profile_picture_saved)
    val pictureRemovedText = stringResource(R.string.settings_profile_picture_removed)
    val unreadablePictureText = stringResource(R.string.settings_profile_picture_unreadable)
    val snackbarProblem = com.teanode.app.ui.components.LocalSnackbar.current
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri: Uri? ->
        if (uri == null) return@rememberLauncherForActivityResult
        actions.run(pictureSavedText) {
            val jpeg = withContext(Dispatchers.IO) { AvatarPicture.jpeg(context, uri) }
            if (jpeg == null) {
                snackbarProblem.showProblem(unreadablePictureText)
                return@run
            }
            val saved = connection.setAccountAvatar(jpeg)
            pictureProfile = saved
            onSaved(saved)
        }
    }
    val value = loaded.value
    if (value != null && draft == null) {
        draft = ProfileDraft(value)
        pictureProfile = value
    }
    val current = draft
    TeaNodeScreen(
        title = stringResource(R.string.settings_profile_title), onBack = onBack,
        actions = {
            if (current != null) {
                BarTextAction(stringResource(R.string.settings_save), onClick = { if (current.isRenaming) isConfirmingRename = true else save(actions, connection, current, savedText, onSaved) { draft = it } }, isEnabled = current.canSave, isWorking = actions.isWorking)
            }
        },
    ) { contentPadding ->
        LoadedContent(loaded, Modifier.padding(contentPadding)) { profile ->
            if (current == null) return@LoadedContent
            if (profile == null) {
                EmptyState(Icons.Outlined.AccountCircle, stringResource(R.string.settings_profile_none), description = stringResource(R.string.settings_profile_none_detail))
                return@LoadedContent
            }
            LazyColumn {
                pictureProfile?.let { shown ->
                    item {
                        TeaNodeSectionHeader(stringResource(R.string.settings_profile_picture))
                        Row(Modifier.fillMaxWidth().padding(16.dp), horizontalArrangement = Arrangement.Center) {
                            AvatarView(shown.displayName, shown.avatarUrl, shown.avatarColorIndex, size = 96.dp)
                        }
                        TeaNodeRow(headline = stringResource(R.string.settings_profile_choose_picture), leadingIcon = Icons.Outlined.Photo, onClick = if (actions.isWorking) null else ({ picker.launch("image/*") }))
                        if (!shown.avatarUrl.isNullOrEmpty()) {
                            TeaNodeRow(
                                headline = stringResource(R.string.settings_profile_remove_picture), leadingIcon = Icons.Outlined.Delete,
                                onClick = if (actions.isWorking) null else ({ actions.run(pictureRemovedText) { val saved = connection.removeAccountAvatar(); pictureProfile = saved; onSaved(saved) } }),
                            )
                        }
                        FooterText(stringResource(R.string.settings_profile_picture_footer))
                    }
                }
                item {
                    TeaNodeSectionHeader(stringResource(R.string.settings_profile_name))
                    FormField(current.name, { draft = current.copy(name = it) }, stringResource(R.string.settings_profile_name), placeholder = stringResource(R.string.settings_profile_name_prompt))
                    FooterText(stringResource(R.string.settings_profile_name_footer))
                    ChoiceRow(
                        stringResource(R.string.settings_profile_pronoun), listOf(PersonPronoun.NotSet) + PersonPronoun.choices, current.pronoun,
                        label = { pronoun -> pronounTitle(pronoun) }, onSelect = { draft = current.copy(pronoun = it) },
                    )
                    FooterText(stringResource(R.string.settings_profile_pronoun_footer))
                    ChoiceRow(
                        stringResource(R.string.settings_profile_units), MeasurementSystem.entries, current.measurementSystem,
                        label = { system -> measurementSystemTitle(system) }, onSelect = { draft = current.copy(measurementSystem = it) },
                    )
                    FooterText(stringResource(R.string.settings_profile_units_footer))
                    TeaNodeSectionHeader(stringResource(R.string.settings_profile_username))
                    FormField(current.username, { draft = current.copy(username = it) }, stringResource(R.string.settings_profile_username))
                    FooterText(stringResource(R.string.settings_profile_username_footer))
                    TeaNodeSectionHeader(stringResource(R.string.settings_profile_email))
                    FormField(current.email, { draft = current.copy(email = it) }, stringResource(R.string.settings_profile_email), placeholder = "you@example.com", keyboardType = androidx.compose.ui.text.input.KeyboardType.Email)
                    FooterText(stringResource(R.string.settings_profile_email_footer))
                    TeaNodeSectionHeader(stringResource(R.string.settings_profile_timezone))
                    val deviceZone = TimeZone.getDefault().id
                    val automaticZone = current.profile.timezone.orEmpty().ifEmpty { deviceZone }
                    ChoiceRow(
                        stringResource(R.string.settings_profile_timezone), TimezoneMode.entries, current.timezoneMode,
                        label = { mode -> if (mode == TimezoneMode.Auto) stringResource(R.string.settings_profile_timezone_follows, automaticZone) else stringResource(R.string.settings_profile_timezone_fixed) },
                        onSelect = { draft = current.choosing(it, deviceZone) },
                    )
                    if (current.timezoneMode == TimezoneMode.Fixed) {
                        FormField(current.timezone, { draft = current.copy(timezone = it) }, stringResource(R.string.settings_profile_timezone), placeholder = "Europe/Berlin")
                    }
                    FooterText(stringResource(R.string.settings_profile_timezone_footer))
                }
            }
        }
    }
    if (isConfirmingRename && current != null) {
        ConfirmationDialog(
            title = stringResource(R.string.settings_profile_rename_title), confirmLabel = stringResource(R.string.settings_profile_rename_confirm),
            text = stringResource(R.string.settings_profile_rename_detail, current.username.trim(), current.profile.username),
            onConfirm = { isConfirmingRename = false; save(actions, connection, current, savedText, onSaved) { draft = it } },
            onDismiss = { isConfirmingRename = false },
        )
    }
}

private fun save(actions: ActionRunner, connection: com.teanode.kit.Connection, draft: ProfileDraft, savedText: String, onSaved: (AccountProfile) -> Unit, onDraft: (ProfileDraft) -> Unit) {
    actions.run(savedText) {
        val saved = connection.updateAccountProfile(draft)
        onDraft(ProfileDraft(saved))
        onSaved(saved)
    }
}

@Composable
fun pronounTitle(pronoun: PersonPronoun): String = when (pronoun) {
    PersonPronoun.NotSet -> stringResource(R.string.settings_pronoun_not_set)
    PersonPronoun.He -> stringResource(R.string.settings_pronoun_he)
    PersonPronoun.She -> stringResource(R.string.settings_pronoun_she)
    PersonPronoun.They -> stringResource(R.string.settings_pronoun_they)
}

@Composable
private fun measurementSystemTitle(system: MeasurementSystem): String = when (system) {
    MeasurementSystem.Automatic -> stringResource(R.string.settings_profile_units_automatic)
    MeasurementSystem.Metric -> stringResource(R.string.settings_profile_units_metric)
    MeasurementSystem.Us -> stringResource(R.string.settings_profile_units_us)
}

/** What the chosen picture becomes before it is sent. */
object AvatarPicture {
    /** The longest side a picture is sent at; the server keeps 512 pixels, so this is only to keep the upload small. */
    const val LONGEST_SIDE_PIXELS = 2048

    /** A JPEG at most [LONGEST_SIDE_PIXELS] on its longest side, drawn again so no metadata, location included, goes with it; null when the bytes are not a picture. */
    fun jpeg(context: android.content.Context, uri: Uri): ByteArray? {
        val bytes = context.contentResolver.openInputStream(uri)?.use { stream -> stream.readBytes() } ?: return null
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeByteArray(bytes, 0, bytes.size, bounds)
        if (bounds.outWidth <= 0 || bounds.outHeight <= 0) return null
        var sampleSize = 1
        while (maxOf(bounds.outWidth, bounds.outHeight) / (sampleSize * 2) >= LONGEST_SIDE_PIXELS) sampleSize *= 2
        val decoded = BitmapFactory.decodeByteArray(bytes, 0, bytes.size, BitmapFactory.Options().apply { inSampleSize = sampleSize }) ?: return null
        val scale = minOf(1f, LONGEST_SIDE_PIXELS.toFloat() / maxOf(decoded.width, decoded.height))
        val scaled = if (scale < 1f) Bitmap.createScaledBitmap(decoded, (decoded.width * scale).toInt(), (decoded.height * scale).toInt(), true) else decoded
        return ByteArrayOutputStream().also { output -> scaled.compress(Bitmap.CompressFormat.JPEG, 90, output) }.toByteArray()
    }
}

/** The change-password page. The current password is asked for even though the phone is signed in, because a phone can be left unlocked. */
@Composable
fun ChangePasswordScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    var draft by remember { mutableStateOf(PasswordChangeDraft()) }
    val changedText = stringResource(R.string.settings_password_changed)
    TeaNodeScreen(
        title = stringResource(R.string.settings_password_title), onBack = onBack,
        actions = {
            BarTextAction(
                stringResource(R.string.settings_password_change), isEnabled = draft.canChange, isWorking = actions.isWorking,
                onClick = { actions.run(changedText, onSuccess = { draft = PasswordChangeDraft() }) { connection.changePassword(draft.currentPassword, draft.newPassword) } },
            )
        },
    ) { contentPadding ->
        Column(Modifier.padding(contentPadding)) {
            FormField(draft.currentPassword, { draft = draft.copy(currentPassword = it) }, stringResource(R.string.settings_password_current), isPassword = true)
            FooterText(stringResource(R.string.settings_password_signed_in_as, connection.account.username))
            FormField(draft.newPassword, { draft = draft.copy(newPassword = it) }, stringResource(R.string.settings_password_new), isPassword = true)
            FormField(
                draft.newPasswordAgain, { draft = draft.copy(newPasswordAgain = it) }, stringResource(R.string.settings_password_new_again), isPassword = true,
                isError = draft.doesConfirmationDiffer, supporting = if (draft.doesConfirmationDiffer) stringResource(R.string.settings_password_mismatch) else null,
            )
        }
    }
}

/** Devices: the phones the app is on, and the built-in apps that can be set up. */
@Composable
fun DevicesScreen(onBack: () -> Unit, onOpenPhones: () -> Unit, onOpenSync: () -> Unit = {}) {
    TeaNodeScreen(title = stringResource(R.string.settings_devices_title), onBack = onBack) { contentPadding ->
        Column(Modifier.padding(contentPadding)) {
            TeaNodeSectionHeader(stringResource(R.string.settings_devices_app))
            TeaNodeNavigationRow(stringResource(R.string.settings_devices_phones), onClick = onOpenPhones, leadingIcon = Icons.Outlined.PhoneAndroid)
            FooterText(stringResource(R.string.settings_devices_app_footer))
            TeaNodeSectionHeader(stringResource(R.string.settings_sync_title))
            TeaNodeNavigationRow(stringResource(R.string.settings_sync_title), onClick = onOpenSync, leadingIcon = Icons.Outlined.Key)
            FooterText(stringResource(R.string.settings_sync_footer))
        }
    }
}

/** The kinds of notification a phone can be told about, in words. */
@Composable
fun notificationKindLabel(notificationKind: String): String = when (notificationKind) {
    "message" -> stringResource(R.string.settings_kind_message)
    "approval" -> stringResource(R.string.settings_kind_approval)
    "question" -> stringResource(R.string.settings_kind_question)
    "answer" -> stringResource(R.string.settings_kind_answer)
    "circle" -> stringResource(R.string.settings_kind_circle)
    "chat" -> stringResource(R.string.settings_kind_chat)
    "skill_share_call" -> stringResource(R.string.settings_kind_skill_share_call)
    "location" -> stringResource(R.string.settings_kind_location)
    "photo" -> stringResource(R.string.settings_kind_photo)
    "shopping" -> stringResource(R.string.settings_kind_shopping)
    else -> notificationKind
}

@Composable
private fun phoneName(phone: PairedPhone): String = phone.phoneName.ifEmpty { stringResource(R.string.settings_phones_no_name) }

/** The phones the app is connected on. */
@Composable
fun PhonesScreen(onBack: () -> Unit, onOpenPhone: (PairedPhone, List<String>) -> Unit) {
    val connection = LocalConnection.current
    val loaded = rememberLoad { connection.pairedPhones() }
    LoadedSettingsPage(
        stringResource(R.string.settings_phones_title), onBack, loaded, isEmpty = { list -> list.phones.isEmpty() },
        empty = { EmptyState(Icons.Outlined.PhoneAndroid, stringResource(R.string.settings_phones_empty)) },
    ) { list ->
        items(list.phones.size) { index ->
            val phone = list.phones[index]
            PairedPhoneRow(phone, list.notificationKinds, onClick = { onOpenPhone(phone, list.notificationKinds) })
        }
        item {
            FooterText(stringResource(if (list.isPushEnabled) R.string.settings_phones_footer else R.string.settings_phones_push_off))
        }
    }
}

@Composable
fun PairedPhoneRow(phone: PairedPhone, allNotificationKinds: List<String>, onClick: () -> Unit) {
    val allText = stringResource(R.string.settings_phones_all_notifications)
    val noneText = stringResource(R.string.settings_phones_no_notifications)
    val notificationsText = when {
        phone.notificationKinds.isEmpty() -> noneText
        allNotificationKinds.isNotEmpty() && phone.notificationKinds.toSet().containsAll(allNotificationKinds) -> allText
        else -> phone.notificationKinds.map { kind -> notificationKindLabel(kind) }.joinToString(", ")
    }
    val detail = listOf(phone.modelDisplayName, if (phone.notificationState == PhoneNotificationState.NotRegistered) "" else notificationsText, phone.lastDeliveryError)
        .filter { text -> text.isNotEmpty() }.joinToString(" · ")
    TeaNodeRow(
        headline = phoneName(phone), supporting = detail.ifEmpty { null }, leadingIcon = Icons.Outlined.PhoneAndroid, onClick = onClick,
        trailing = {
            when (phone.notificationState) {
                PhoneNotificationState.NotRegistered -> Icon(Icons.Outlined.NotificationsOff, stringResource(R.string.settings_phones_off), tint = MaterialTheme.colorScheme.tertiary)
                PhoneNotificationState.LastFailed -> Icon(Icons.Outlined.WarningAmber, stringResource(R.string.settings_phones_failed), tint = MaterialTheme.colorScheme.error)
                PhoneNotificationState.Notified -> Unit
            }
        },
    )
}

/** Changes one phone's name and what it is told about; can send it a test notification and remove it. */
@Composable
fun PhoneSettingsScreen(phone: PairedPhone, allNotificationKinds: List<String>, onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    var draft by remember { mutableStateOf(PairedPhoneDraft(phone)) }
    var isConfirmingRemoval by remember { mutableStateOf(false) }
    val savedText = stringResource(R.string.settings_saved)
    val testText = stringResource(R.string.settings_phones_test_sent, phoneName(phone))
    val removedText = stringResource(R.string.settings_phones_removed, phoneName(phone))
    TeaNodeScreen(
        title = stringResource(R.string.settings_phone_title), onBack = onBack,
        actions = {
            BarTextAction(
                stringResource(R.string.settings_save), isEnabled = draft.canSave, isWorking = actions.isWorking,
                onClick = {
                    val saving = draft
                    actions.run(savedText, onSuccess = onBack) {
                        if (saving.hasNameChanged) connection.renamePhone(saving.phone.id, saving.phoneName.trim())
                        if (saving.hasNotificationKindsChanged) connection.updatePhone(saving.phone.id, saving.orderedNotificationKinds(allNotificationKinds))
                    }
                },
            )
        },
    ) { contentPadding ->
        LazyColumn(Modifier.padding(contentPadding)) {
            item {
                TeaNodeSectionHeader(stringResource(R.string.settings_phone_name))
                FormField(draft.phoneName, { draft = draft.copy(phoneName = it) }, stringResource(R.string.settings_phone_name))
                TeaNodeSectionHeader(stringResource(R.string.settings_phone_tell_about))
            }
            items(allNotificationKinds.size) { index ->
                val kind = allNotificationKinds[index]
                SwitchRow(
                    notificationKindLabel(kind), draft.notificationKinds.contains(kind),
                    { isOn -> draft = draft.copy(notificationKinds = if (isOn) draft.notificationKinds + kind else draft.notificationKinds - kind) },
                )
            }
            item {
                TeaNodeDivider()
                TeaNodeRow(headline = stringResource(R.string.settings_phones_test), leadingIcon = Icons.Outlined.NotificationsActive, onClick = { actions.run(testText) { connection.testPhone(phone.id) } })
                TeaNodeRow(headline = stringResource(R.string.settings_remove), leadingIcon = Icons.Outlined.Delete, onClick = { isConfirmingRemoval = true })
            }
        }
    }
    if (isConfirmingRemoval) {
        ConfirmationDialog(
            title = stringResource(R.string.settings_phones_remove_title), confirmLabel = stringResource(R.string.settings_remove), isDestructive = true,
            text = stringResource(R.string.settings_phones_remove_detail, phoneName(phone)),
            onConfirm = { isConfirmingRemoval = false; actions.run(removedText, onSuccess = onBack) { connection.removePhone(phone.id) } },
            onDismiss = { isConfirmingRemoval = false },
        )
    }
}

/** The passkeys that can sign in as the person, and removing one. New passkeys are added on the dashboard. */
@Composable
fun PasskeysScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { connection.passkeys() }
    var removing by remember { mutableStateOf<Passkey?>(null) }
    LoadedSettingsPage(stringResource(R.string.settings_passkeys_title), onBack, loaded) { list ->
        if (list.passkeys.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_passkeys_none)) }
        items(list.passkeys.size) { index ->
            val passkey = list.passkeys[index]
            PasskeyRow(passkey, onRemove = { removing = passkey })
        }
        item {
            FooterText(stringResource(R.string.settings_passkeys_footer))
            FooterText(stringResource(if (list.policy?.enabled == false) R.string.settings_passkeys_not_offered else R.string.settings_passkeys_add_on_dashboard))
        }
    }
    removing?.let { passkey ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_passkeys_remove_title), confirmLabel = stringResource(R.string.settings_remove), isDestructive = true,
            text = stringResource(R.string.settings_passkeys_remove_detail, passkey.name),
            onConfirm = { removing = null; actions.run(null, onSuccess = { loaded.refresh() }) { connection.deletePasskey(passkey.id) } },
            onDismiss = { removing = null },
        )
    }
}

@Composable
private fun PasskeyRow(passkey: Passkey, onRemove: () -> Unit) {
    val registered = passkey.createdInstant?.let { instant -> stringResource(R.string.settings_passkeys_registered, relativeTimeText(instant.toEpochMilli())) }
    val lastUsedInstant = passkey.lastUsedInstant
    val used = if (lastUsedInstant != null) {
        val lastUsed = stringResource(R.string.settings_passkeys_last_used, relativeTimeText(lastUsedInstant.toEpochMilli()))
        val address = passkey.ip.orEmpty()
        if (address.isEmpty()) lastUsed else "$lastUsed ($address)"
    } else {
        stringResource(R.string.settings_passkeys_never_used)
    }
    TeaNodeRow(
        headline = passkey.name, supporting = listOfNotNull(stringResource(if (passkey.isSynced) R.string.settings_passkeys_synced else R.string.settings_passkeys_this_device), registered, used).joinToString(" · "),
        leadingIcon = Icons.Outlined.Key, supportingMaxLines = 3,
        trailing = { IconButton(onClick = onRemove) { Icon(Icons.Outlined.Delete, stringResource(R.string.settings_remove)) } },
    )
}
