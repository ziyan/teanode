package com.teanode.app.contacts

import android.content.ClipData
import androidx.activity.compose.BackHandler
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowForward
import androidx.compose.material.icons.automirrored.outlined.Message
import androidx.compose.material.icons.outlined.Call
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.DirectionsCar
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Email
import androidx.compose.material.icons.outlined.Groups
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material.icons.outlined.PersonOff
import androidx.compose.material.icons.outlined.PersonRemove
import androidx.compose.material.icons.outlined.Place
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.mail.MailRoutes
import com.teanode.app.mail.MailTimes
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberServerPicture
import com.teanode.kit.AppSection
import com.teanode.kit.asReasonSentence
import com.teanode.kit.contacts.ContactDetail
import com.teanode.kit.contacts.ContactWords
import com.teanode.kit.contacts.PersonCard
import com.teanode.kit.contacts.PersonCardHealthSection
import com.teanode.kit.contacts.PersonCardLocation
import com.teanode.kit.contacts.PersonCardMember
import com.teanode.kit.contacts.PersonCardSectionState
import com.teanode.kit.contacts.PersonCardTarget
import com.teanode.kit.contacts.readPersonCard
import com.teanode.kit.mail.ServerTime
import kotlinx.coroutines.launch
import java.time.Instant
import java.time.LocalDate

/** PersonCardModel is one card, read when it opens and again after anything that changes it. */
class PersonCardModel(private val read: suspend () -> PersonCard) {
    var card by mutableStateOf<PersonCard?>(null)
        private set
    var loadFailure by mutableStateOf<Throwable?>(null)
        private set

    suspend fun load(onRefreshFailure: (Throwable) -> Unit) {
        try {
            card = read()
            loadFailure = null
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            if (card == null) loadFailure = failed else onRefreshFailure(failed)
        }
    }
}

/**
 * PersonCardScreen is a person's card: their picture and name, what they are to the person, buttons to message, call, write
 * and get directions, and a few items from each part of the app that concerns them. Each section ends in a row that
 * opens its part. One that failed says so, and the rest still show.
 */
@Composable
fun PersonCardScreen(model: ContactsModel, target: PersonCardTarget, onBack: () -> Unit, previewCard: PersonCard? = null) {
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    val navigator = LocalAppNavigator.current
    val cardModel = remember(target) { PersonCardModel { model.connection.readPersonCard(target) } }
    var isEditingAccount by remember { mutableStateOf(false) }
    var isLinkingContact by remember { mutableStateOf(false) }
    var isConfirmingDelete by remember { mutableStateOf(false) }
    var isConfirmingRemovePicture by remember { mutableStateOf(false) }
    val shownCard = previewCard ?: cardModel.card

    suspend fun load() {
        if (previewCard == null) cardModel.load { failed -> model.snackbar?.showError(failed) }
    }
    LaunchedEffect(target) {
        load()
        if (cardModel.card?.hasContact == true) model.loadAddressBook()
    }

    val contact = shownCard?.contact
    val isContactInLoadedBook = run {
        val bookId = model.addressBook?.id ?: return@run previewCard != null
        val contactBookId = contact?.addressBookId ?: return@run true
        contactBookId == bookId
    }

    if (isEditingAccount && contact != null) {
        BackHandler { isEditingAccount = false }
        ContactAccountPickerScreen(model, contact, onBack = { isEditingAccount = false }, onLinked = { isEditingAccount = false; scope.launch { load() } })
        return
    }
    val linkingPerson = shownCard?.person
    if (isLinkingContact && linkingPerson != null) {
        BackHandler { isLinkingContact = false }
        PersonContactPickerScreen(model, linkingPerson, onBack = { isLinkingContact = false }, onLinked = { isLinkingContact = false; scope.launch { load() } })
        return
    }

    TeaNodeScreen(
        title = shownCard?.displayName?.ifEmpty { null } ?: stringResource(R.string.contacts_title),
        onBack = onBack,
        actions = {
            if (contact != null) {
                IconButton(onClick = { navigator.navigate(ContactsRoutes.editor(contact.id)) }, enabled = isContactInLoadedBook) { Icon(Icons.Outlined.Edit, stringResource(R.string.contacts_edit)) }
            }
        },
    ) { contentPadding ->
        when {
            shownCard != null -> Column(Modifier.padding(contentPadding).fillMaxSize().verticalScroll(rememberScrollState())) {
                PersonCardHeader(shownCard, isMine = contact != null && contact.id == model.myContactId, model = model)
                if (shownCard.person != null && shownCard.needsContact) PersonCardLinkOffer(shownCard) { isLinkingContact = true }
                PersonCardLocationRows(shownCard)
                PersonCardHealthRows(shownCard)
                PersonCardFinanceRows(shownCard)
                PersonCardPhotosRows(shownCard)
                PersonCardMailRows(shownCard)
                PersonCardRecordingsRows(shownCard)
                PersonCardMemoryRows(shownCard)
                PersonCardDetailsRows(shownCard, model)
                PersonCardCirclesRows(shownCard)
                PersonCardRelationshipRows(shownCard)
                if (isContactInLoadedBook && contact != null) {
                    PersonCardManageRows(
                        contact, model, onChooseAccount = { isEditingAccount = true }, onChanged = { scope.launch { load() } },
                        onDelete = { isConfirmingDelete = true }, onRemovePicture = { isConfirmingRemovePicture = true },
                    )
                }
                Spacer(Modifier.height(24.dp))
            }
            cardModel.loadFailure != null -> ErrorState(cardModel.loadFailure?.message.orEmpty().asReasonSentence(), onRetry = { scope.launch { load() } }, modifier = Modifier.padding(contentPadding))
            else -> LoadingState(Modifier.padding(contentPadding))
        }
    }

    if (isConfirmingDelete && contact != null) {
        ConfirmationDialog(
            title = stringResource(R.string.contacts_delete_named_question, contact.name.orEmpty()), text = stringResource(R.string.contacts_delete_message),
            confirmLabel = stringResource(R.string.contacts_delete_contact), isDestructive = true,
            onConfirm = { isConfirmingDelete = false; scope.launch { if (model.delete(contact.id)) onBack() } }, onDismiss = { isConfirmingDelete = false },
        )
    }
    if (isConfirmingRemovePicture && contact != null) {
        ConfirmationDialog(
            title = stringResource(R.string.contacts_remove_picture_of_question, contact.name.orEmpty()), text = stringResource(R.string.contacts_remove_picture_message),
            confirmLabel = stringResource(R.string.contacts_remove_picture), isDestructive = true,
            onConfirm = { isConfirmingRemovePicture = false; scope.launch { if (model.removePicture(contact.id)) load() } }, onDismiss = { isConfirmingRemovePicture = false },
        )
    }
}

/** Who they are: a large picture, their name, what they are to the person, and the buttons for what can be done with them at once. */
@Composable
fun PersonCardHeader(card: PersonCard, isMine: Boolean, model: ContactsModel) {
    val context = LocalContext.current
    val navigator = LocalAppNavigator.current
    val scope = rememberCoroutineScope()
    var isOpeningChat by remember { mutableStateOf(false) }
    val person = card.person
    val contact = card.contact
    Column(Modifier.fillMaxWidth().padding(16.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(10.dp)) {
        if (person != null) {
            AvatarView(person.displayName, person.avatarUrl, person.avatarColorIndex, size = 72.dp)
        } else if (contact != null) {
            ContactPicture(contact.id, contact.name.orEmpty(), contact.hasPhoto == true, 72)
        }
        Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text(card.displayName.ifEmpty { stringResource(R.string.contacts_no_name) }, style = MaterialTheme.typography.headlineSmall, textAlign = TextAlign.Center)
            val relationship = if (person != null) RelationshipWords.word(context, person)?.let { word -> stringResource(R.string.contacts_your_relationship, word) } else contact?.organization?.ifEmpty { null }
            if (relationship != null) Text(relationship, color = MaterialTheme.colorScheme.onSurfaceVariant)
            if (person != null && person.displayName != person.username) Text("@${person.username}", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            if (isMine) Text(stringResource(R.string.contacts_this_is_you), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        val messageUserId = if (card.canMessage) person?.userId else null
        val callAddress = card.callAddress
        val mailAddress = card.mailAddress
        val directionsAddress = card.directionsAddress
        if (messageUserId != null || callAddress != null || mailAddress != null || directionsAddress != null) {
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                if (messageUserId != null) {
                    PersonCardActionButton(Icons.AutoMirrored.Outlined.Message, stringResource(R.string.contacts_message), Modifier.weight(1f)) {
                        if (!isOpeningChat) {
                            isOpeningChat = true
                            scope.launch {
                                try {
                                    openDirectChat(model, navigator, messageUserId)
                                } catch (failed: kotlinx.coroutines.CancellationException) {
                                    throw failed
                                } catch (failed: Throwable) {
                                    model.snackbar?.showError(failed)
                                } finally {
                                    isOpeningChat = false
                                }
                            }
                        }
                    }
                }
                if (callAddress != null) PersonCardActionButton(Icons.Outlined.Call, stringResource(R.string.contacts_call), Modifier.weight(1f)) { startView(context, Intent.ACTION_DIAL, callAddress) }
                if (mailAddress != null) PersonCardActionButton(Icons.Outlined.Email, stringResource(R.string.contacts_mail), Modifier.weight(1f)) { navigator.navigate(MailRoutes.compose(com.teanode.app.mail.ComposeKind.New, to = mailAddress)) }
                if (directionsAddress != null) PersonCardActionButton(Icons.Outlined.DirectionsCar, stringResource(R.string.contacts_directions), Modifier.weight(1f)) { startView(context, Intent.ACTION_VIEW, directionsAddress) }
            }
        }
    }
}

private fun startView(context: Context, action: String, address: String) {
    runCatching { context.startActivity(Intent(action, Uri.parse(address)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) }
}

/** One of the buttons under the name: a symbol over a word on a rounded rectangle. */
@Composable
fun PersonCardActionButton(icon: ImageVector, title: String, modifier: Modifier = Modifier, onClick: () -> Unit) {
    Surface(onClick = onClick, modifier = modifier, shape = RoundedCornerShape(12.dp), color = MaterialTheme.colorScheme.surfaceVariant) {
        Column(Modifier.padding(vertical = 10.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Icon(icon, null, Modifier.size(24.dp), tint = MaterialTheme.colorScheme.primary)
            Text(title, style = MaterialTheme.typography.labelMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

/** The one offer to link a contact, for the sections that wait for one. */
@Composable
private fun PersonCardLinkOffer(card: PersonCard, onLink: () -> Unit) {
    Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
        OutlinedButton(onClick = onLink) {
            Icon(Icons.Outlined.Link, null, Modifier.size(18.dp))
            Spacer(Modifier.width(8.dp))
            Text(stringResource(R.string.contacts_link_a_contact))
        }
        Text(stringResource(R.string.contacts_link_a_contact_footer, card.displayName), Modifier.padding(top = 4.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@Composable
private fun StateRow(state: PersonCardSectionState, notShared: String) {
    when (state) {
        PersonCardSectionState.NotShared -> Text(notShared, Modifier.padding(horizontal = 16.dp, vertical = 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
        PersonCardSectionState.Failed -> Text(stringResource(R.string.contacts_could_not_be_loaded), Modifier.padding(horizontal = 16.dp, vertical = 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
        else -> Unit
    }
}

@Composable
private fun ShowAllRow(title: String, onClick: () -> Unit) {
    TeaNodeRow(headline = stringResource(R.string.contacts_show_all_in, title), leadingIcon = Icons.AutoMirrored.Outlined.ArrowForward, onClick = onClick)
}

@Composable
private fun PlainText(text: String) {
    Text(text, Modifier.padding(horizontal = 16.dp, vertical = 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
}

@Composable
private fun PersonCardLocationRows(card: PersonCard) {
    val section = card.location ?: return
    if (!section.sectionState.isListed) return
    val navigator = LocalAppNavigator.current
    TeaNodeSectionHeader(stringResource(R.string.section_location))
    if (section.sectionState != PersonCardSectionState.Shown) {
        StateRow(section.sectionState, stringResource(R.string.contacts_location_not_shared, card.displayName))
        return
    }
    val location = section.location
    if (location == null) {
        PlainText(stringResource(R.string.contacts_no_position_yet))
        return
    }
    WhereRow(location)
    if (location.shareLevel == "current") Text(stringResource(R.string.contacts_shares_where_now, card.displayName), Modifier.padding(horizontal = 16.dp, vertical = 4.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    val openVisitId = location.openVisit?.id
    for (visit in (section.todayVisits ?: emptyList()).filter { visit -> visit.id != openVisitId }) {
        val arrived = ServerTime.instant(visit.arrivedAt)
        TeaNodeRow(
            headline = visit.place.placeName.ifEmpty { stringResource(R.string.contacts_a_place) }, leadingIcon = Icons.Outlined.Place,
            supporting = visitTimes(visit.arrivedAt, visit.departedAt),
        )
    }
    if (location.shareLevel == "history") {
        TeaNodeRow(headline = stringResource(R.string.contacts_visits_and_places), leadingIcon = Icons.Outlined.Schedule, onClick = { navigator.navigate(ContactsRoutes.visits(location.userId)) })
    }
    ShowAllRow(stringResource(R.string.section_location)) { navigator.openSection(AppSection.Location) }
}

@Composable
private fun visitTimes(arrivedAt: String?, departedAt: String?): String {
    val arrived = ServerTime.instant(arrivedAt)
    val departed = ServerTime.instant(departedAt)
    return when {
        arrived != null && departed != null -> MailTimes.timeOfDay(arrived) + " - " + MailTimes.timeOfDay(departed)
        arrived != null -> stringResource(R.string.contacts_since_time, MailTimes.timeOfDay(arrived))
        else -> ""
    }
}

@Composable
private fun WhereRow(location: PersonCardLocation) {
    val pause = location.pause
    val pauseInEffect = pause != null && (ServerTime.instant(pause.endedAt)?.isAfter(Instant.now()) ?: true) && pause.startedAt != null
    val measured = ServerTime.instant(location.latestPoint?.measuredAt)
    val fresh = if (measured == null) stringResource(R.string.contacts_no_position_yet) else android.text.format.DateUtils.getRelativeTimeSpanString(minOf(measured.toEpochMilli(), System.currentTimeMillis() - 1000), System.currentTimeMillis(), android.text.format.DateUtils.MINUTE_IN_MILLIS).toString()
    val visit = location.openVisit
    val title = when {
        pauseInEffect -> stringResource(R.string.contacts_location_paused)
        else -> visit?.place?.placeName?.ifEmpty { null } ?: fresh
    }
    val detail = if (!pauseInEffect && visit != null) visitTimes(visit.arrivedAt, null).ifEmpty { fresh }.let { since -> if (since == fresh) fresh else "$since · $fresh" } else null
    TeaNodeRow(headline = title, supporting = detail, leadingIcon = if (pauseInEffect) Icons.Outlined.Schedule else Icons.Outlined.Place)
}

@Composable
private fun PersonCardHealthRows(card: PersonCard) {
    val section = card.health ?: return
    if (!section.sectionState.isListed) return
    val navigator = LocalAppNavigator.current
    TeaNodeSectionHeader(stringResource(R.string.section_health))
    if (section.sectionState != PersonCardSectionState.Shown) {
        StateRow(section.sectionState, stringResource(R.string.contacts_health_not_shared, card.displayName))
        return
    }
    val types = section.healthTypes ?: emptyList()
    if (types.isEmpty()) {
        PlainText(stringResource(R.string.contacts_no_health_data_yet))
        return
    }
    for (type in types) {
        val day = type.latestDaySummary
        val amount = day?.sumValue ?: day?.averageValue
        TeaNodeRow(
            headline = type.typeName.ifEmpty { type.healthType },
            supporting = day?.summaryDay?.let { summaryDay -> if (PersonCardHealthSection.isToday(type, LocalDate.now())) stringResource(R.string.contacts_today) else summaryDay },
            trailing = { Text(listOfNotNull(amount?.let { value -> if (value == value.toLong().toDouble()) value.toLong().toString() else String.format("%.1f", value) }, (day?.quantityUnit ?: type.quantityUnit)?.takeIf { unit -> unit != "count" }).joinToString(" ")) },
            onClick = { navigator.openSection(AppSection.Health) },
        )
    }
    if (section.hasMore) ShowAllRow(stringResource(R.string.section_health)) { navigator.openSection(AppSection.Health) }
}

@Composable
private fun PersonCardFinanceRows(card: PersonCard) {
    val section = card.finance ?: return
    if (!section.sectionState.isListed) return
    val navigator = LocalAppNavigator.current
    TeaNodeSectionHeader(stringResource(R.string.section_finance))
    if (section.sectionState != PersonCardSectionState.Shown) {
        StateRow(section.sectionState, stringResource(R.string.contacts_finance_not_shared, card.displayName))
        return
    }
    val allLevel = section.allFinanceShareLevel?.takeIf { level -> level.isNotEmpty() }
    if (allLevel != null) {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
            Text(stringResource(if (allLevel == "transactions") R.string.contacts_finance_all_transactions else R.string.contacts_finance_all_spending))
            Text(stringResource(R.string.contacts_across_all_accounts), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
    for (account in section.financeAccounts ?: emptyList()) {
        TeaNodeRow(
            headline = account.accountName, supporting = account.accountMask?.takeIf { mask -> mask.isNotEmpty() }?.let { mask -> "•••• $mask" },
            trailing = { account.currentBalance?.let { balance -> Text(java.text.NumberFormat.getCurrencyInstance().apply { runCatching { currency = java.util.Currency.getInstance(account.currencyCode) } }.format(balance)) } },
        )
    }
    if (section.hasMore || allLevel != null) ShowAllRow(stringResource(R.string.section_finance)) { navigator.openSection(AppSection.Finance) }
}

@Composable
private fun PersonCardPhotosRows(card: PersonCard) {
    val section = card.photos ?: return
    if (!section.photosState.isListed && !section.albumsState.isListed) return
    val navigator = LocalAppNavigator.current
    TeaNodeSectionHeader(stringResource(R.string.section_photos))
    if (section.photosState == PersonCardSectionState.Shown) {
        val photos = section.photos ?: emptyList()
        if (photos.isNotEmpty()) {
            Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                for (photo in photos) {
                    val picture = rememberServerPicture(photo.smallPreviewUrl)
                    Box(Modifier.size(76.dp).clip(RoundedCornerShape(8.dp)).background(MaterialTheme.colorScheme.surfaceVariant).clickable { navigator.openSection(AppSection.Photos) }) {
                        if (picture != null) androidx.compose.foundation.Image(picture, photo.placeName.ifEmpty { stringResource(R.string.contacts_photo) }, Modifier.fillMaxSize(), contentScale = androidx.compose.ui.layout.ContentScale.Crop)
                    }
                }
            }
        } else if ((section.albums ?: emptyList()).isEmpty()) {
            PlainText(stringResource(R.string.contacts_nothing_yet))
        }
    } else {
        StateRow(section.photosState, stringResource(R.string.contacts_nothing_yet))
    }
    if (section.albumsState == PersonCardSectionState.Shown) {
        for (album in section.albums ?: emptyList()) {
            TeaNodeRow(headline = album.albumTitle, supporting = pluralStringResource(R.plurals.contacts_photos_count, album.photoCount, album.photoCount), onClick = { navigator.openSection(AppSection.Photos) })
        }
    } else if (section.albumsState == PersonCardSectionState.Failed) {
        StateRow(PersonCardSectionState.Failed, stringResource(R.string.contacts_nothing_yet))
    }
    if (section.hasMorePhotos) ShowAllRow(stringResource(R.string.section_photos)) { navigator.openSection(AppSection.Photos) }
    if (section.hasMoreAlbums) ShowAllRow(stringResource(R.string.contacts_albums)) { navigator.openSection(AppSection.Photos) }
}

@Composable
private fun PersonCardMailRows(card: PersonCard) {
    val section = card.mail ?: return
    if (!section.sectionState.isListed) return
    val navigator = LocalAppNavigator.current
    TeaNodeSectionHeader(stringResource(R.string.section_mail))
    if (section.sectionState != PersonCardSectionState.Shown) {
        StateRow(section.sectionState, stringResource(R.string.contacts_nothing_yet))
        return
    }
    val mails = section.mails ?: emptyList()
    if (mails.isEmpty()) PlainText(stringResource(R.string.contacts_nothing_yet))
    for (mail in mails) {
        TeaNodeRow(
            headline = mail.subject.ifEmpty { stringResource(R.string.mail_no_subject) },
            supporting = if (mail.snippet.isEmpty()) mail.senderText else "${mail.senderText} · ${mail.snippet}",
            overline = ServerTime.instant(mail.receivedAt)?.let { moment -> MailTimes.dateAndTime(moment) },
            onClick = { navigator.navigate(MailRoutes.thread(mail.mailboxItemId, null)) },
        )
    }
    if (section.hasMore) ShowAllRow(stringResource(R.string.section_mail)) { navigator.navigate(MailRoutes.search(card.mailAddress ?: card.displayName)) }
}

@Composable
private fun PersonCardRecordingsRows(card: PersonCard) {
    val section = card.recordings ?: return
    if (!section.sectionState.isListed) return
    val navigator = LocalAppNavigator.current
    TeaNodeSectionHeader(stringResource(R.string.section_recordings))
    if (section.sectionState != PersonCardSectionState.Shown) {
        StateRow(section.sectionState, stringResource(R.string.contacts_nothing_yet))
        return
    }
    val recordings = section.recordings ?: emptyList()
    if (recordings.isEmpty()) PlainText(stringResource(R.string.contacts_nothing_yet))
    for (recording in recordings) {
        TeaNodeRow(
            headline = recording.title, supporting = pluralStringResource(R.plurals.contacts_speaks_in_passages, recording.spokenPassageCount, recording.spokenPassageCount),
            overline = ServerTime.instant(recording.recordedAt)?.let { moment -> MailTimes.dateAndTime(moment) }, onClick = { navigator.openSection(AppSection.Recordings) },
        )
    }
    if (section.hasMore) ShowAllRow(stringResource(R.string.section_recordings)) { navigator.openSection(AppSection.Recordings) }
}

@Composable
private fun PersonCardMemoryRows(card: PersonCard) {
    val section = card.memory ?: return
    if (!section.sectionState.isListed) return
    val navigator = LocalAppNavigator.current
    TeaNodeSectionHeader(stringResource(R.string.contacts_in_memory))
    if (section.sectionState != PersonCardSectionState.Shown) {
        StateRow(section.sectionState, stringResource(R.string.contacts_nothing_yet))
        return
    }
    val pages = section.pages ?: emptyList()
    if (pages.isEmpty()) PlainText(stringResource(R.string.contacts_nothing_yet))
    for (page in pages) {
        val matched = stringResource(R.string.contacts_found_by_name)
        TeaNodeRow(
            headline = page.nodeName,
            supporting = if (page.isMatchedByName == true) (if (page.summary.isEmpty()) matched else "${page.summary} · $matched") else page.summary,
            onClick = { navigator.openSection(AppSection.Knowledge) },
        )
    }
    if (section.hasMore) ShowAllRow(stringResource(R.string.section_knowledge)) { navigator.openSection(AppSection.Knowledge) }
}

/** The contact's addresses, numbers, postal addresses and notes, each opening what reaches it. */
@Composable
private fun PersonCardDetailsRows(card: PersonCard, model: ContactsModel) {
    val contact = card.contact ?: return
    val context = LocalContext.current
    val navigator = LocalAppNavigator.current
    val emails = contact.emails ?: emptyList()
    if (emails.isNotEmpty()) {
        TeaNodeSectionHeader(stringResource(R.string.contacts_email))
        for (email in emails) ReachRow(email, Icons.Outlined.Email, onClick = { navigator.navigate(MailRoutes.compose(com.teanode.app.mail.ComposeKind.New, to = email)) }, onCopy = { copy(context, email); model.snackbar?.show(context.getString(R.string.contacts_copied)) })
    }
    val phones = contact.phones ?: emptyList()
    if (phones.isNotEmpty()) {
        TeaNodeSectionHeader(stringResource(R.string.contacts_phone))
        for (phone in phones) ReachRow(phone, Icons.Outlined.Call, onClick = { startView(context, Intent.ACTION_DIAL, "tel:" + phone.filter { character -> character.isDigit() || character == '+' }) }, onCopy = { copy(context, phone); model.snackbar?.show(context.getString(R.string.contacts_copied)) })
    }
    val addresses = contact.addresses ?: emptyList()
    if (addresses.isNotEmpty()) {
        TeaNodeSectionHeader(stringResource(R.string.contacts_address))
        for (address in addresses) {
            val written = ContactWords.written(address)
            ReachRow(written, Icons.Outlined.Place, onClick = { startView(context, Intent.ACTION_VIEW, "geo:0,0?q=" + Uri.encode(written)) }, onCopy = { copy(context, written); model.snackbar?.show(context.getString(R.string.contacts_copied)) })
        }
    }
    if (!contact.note.isNullOrEmpty()) {
        TeaNodeSectionHeader(stringResource(R.string.contacts_notes))
        SelectionContainer { Text(contact.note.orEmpty(), Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) }
    }
}

private fun copy(context: Context, text: String) {
    val clipboard = context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
    clipboard.setPrimaryClip(ClipData.newPlainText("contact", text))
}

/** A way to reach them: tap to use it, and a button to copy it. */
@Composable
private fun ReachRow(text: String, icon: ImageVector, onClick: () -> Unit, onCopy: () -> Unit) {
    TeaNodeRow(
        headline = text, leadingIcon = icon, onClick = onClick,
        trailing = { IconButton(onClick = onCopy) { Icon(Icons.Outlined.ContentCopy, stringResource(R.string.contacts_copy)) } },
    )
}

@Composable
private fun PersonCardCirclesRows(card: PersonCard) {
    val circles = card.sharedCircles ?: return
    if (circles.isEmpty()) return
    TeaNodeSectionHeader(stringResource(R.string.contacts_circles))
    for (circle in circles) {
        val kindTitle = when (circle.circleKind) {
            "family" -> stringResource(R.string.contacts_circle_family)
            "friends" -> stringResource(R.string.contacts_circle_friends)
            "colleagues" -> stringResource(R.string.contacts_circle_colleagues)
            else -> null
        }
        TeaNodeRow(
            headline = circle.circleName, leadingIcon = Icons.Outlined.Groups,
            trailing = if (kindTitle != null && kindTitle.lowercase() != circle.circleName.lowercase()) ({ Text(kindTitle, color = MaterialTheme.colorScheme.onSurfaceVariant) }) else null,
        )
    }
}

/** What they are to the person, as the Circles screen words it. */
@Composable
private fun PersonCardRelationshipRows(card: PersonCard) {
    val person = card.person ?: return
    val context = LocalContext.current
    val word = RelationshipWords.word(context, person)
    TeaNodeSectionHeader(stringResource(R.string.contacts_relationship))
    TeaNodeRow(headline = stringResource(R.string.contacts_relationship), trailing = { Text(word?.let { shown -> RelationshipWords.capitalized(shown) } ?: stringResource(R.string.contacts_none), color = MaterialTheme.colorScheme.onSurfaceVariant) })
}

/** What the person can do about their contact for them: say which account they are, mark them as the person, take their picture off, delete them. */
@Composable
private fun PersonCardManageRows(contact: ContactDetail, model: ContactsModel, onChooseAccount: () -> Unit, onChanged: () -> Unit, onDelete: () -> Unit, onRemovePicture: () -> Unit) {
    val scope = rememberCoroutineScope()
    val account = contact.account
    if (account != null && contact.isLinkedToAccount) {
        Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            AvatarView(account.displayName, account.avatarUrl, account.avatarColorIndex, size = 36.dp)
            Text(stringResource(R.string.contacts_in_circles_as, account.displayName), Modifier.padding(start = 12.dp))
        }
        TeaNodeRow(headline = stringResource(R.string.contacts_change), leadingIcon = Icons.Outlined.Groups, onClick = onChooseAccount)
        TeaNodeRow(headline = stringResource(R.string.contacts_remove), leadingIcon = Icons.Outlined.PersonRemove, onClick = { scope.launch { if (model.unlink(contact.id)) onChanged() } })
    } else {
        TeaNodeRow(headline = stringResource(R.string.contacts_is_in_your_circles_as), leadingIcon = Icons.Outlined.Groups, onClick = onChooseAccount)
    }
    Text(stringResource(R.string.contacts_only_you_see_this), Modifier.padding(horizontal = 16.dp, vertical = 4.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    HorizontalDivider(Modifier.padding(vertical = 8.dp))
    if (contact.id == model.myContactId) {
        TeaNodeRow(headline = stringResource(R.string.contacts_this_is_not_me), onClick = { scope.launch { model.setMine(null) } })
    } else {
        TeaNodeRow(headline = stringResource(R.string.contacts_this_is_me), onClick = { scope.launch { model.setMine(contact.id) } })
    }
    if (contact.hasPhoto == true) TeaNodeRow(headline = stringResource(R.string.contacts_remove_picture), leadingIcon = Icons.Outlined.PersonOff, onClick = onRemovePicture)
    TeaNodeRow(headline = stringResource(R.string.contacts_delete_contact), onClick = onDelete)
    Text(stringResource(R.string.contacts_agent_knows_you), Modifier.padding(horizontal = 16.dp, vertical = 4.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}
