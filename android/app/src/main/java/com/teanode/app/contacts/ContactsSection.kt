package com.teanode.app.contacts

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.AccountCircle
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.PersonAddAlt
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Contacts
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.AutoStories
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.contacts.ContactLetterSection
import com.teanode.kit.contacts.ContactSummary
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/**
 * The first screen of Contacts: the address book, a letter at a time, searched by name, address or phone, with the
 * contacts the server thinks are people in the person's circles offered first, and the circles' books to switch to.
 */
@Composable
fun ContactsSection() {
    val navigator = LocalAppNavigator.current
    val model = rememberContactsModel()
    ContactsScreen(
        model = model,
        onOpenContact = { contactId -> navigator.navigate(ContactsRoutes.contact(contactId)) },
        onNewContact = { navigator.navigate(ContactsRoutes.editor(null)) },
        onEditCircleContact = { contactId -> navigator.navigate(ContactsRoutes.editor(contactId)) },
        onImport = { navigator.navigate(ContactsRoutes.IMPORT) },
    )
}

@Composable
fun ContactsScreen(
    model: ContactsModel,
    onOpenContact: (String) -> Unit,
    onNewContact: () -> Unit,
    onEditCircleContact: (String) -> Unit,
    onImport: () -> Unit,
) {
    val scope = rememberCoroutineScope()
    var isBookMenuOpen by remember { mutableStateOf(false) }
    var isMoreMenuOpen by remember { mutableStateOf(false) }
    var removingCircleContact by remember { mutableStateOf<ContactSummary?>(null) }
    var isRenamingBook by remember { mutableStateOf(false) }
    var bookNameDraft by remember { mutableStateOf("") }
    var isConfirmingBookDeletion by remember { mutableStateOf(false) }
    var typedText by remember { mutableStateOf(model.searchText) }

    LaunchedEffect(model.searchText) {
        if (model.searchText.isNotEmpty()) delay(250)
        model.load()
    }
    LaunchedEffect(model) { model.loadSuggestions() }
    LaunchedEffect(model.books.size) { model.loadCirclesWithoutBook() }

    TeaNodeScreen(
        title = model.title,
        actions = {
            if (model.books.size > 1) {
                Box {
                    IconButton(onClick = { isBookMenuOpen = true }) { Icon(Icons.Outlined.AutoStories, stringResource(R.string.contacts_address_book)) }
                    DropdownMenu(expanded = isBookMenuOpen, onDismissRequest = { isBookMenuOpen = false }) {
                        for (book in model.books) {
                            DropdownMenuItem(
                                text = { Text(if (book.isOfACircle) book.shownName else stringResource(R.string.contacts_your_contacts)) },
                                leadingIcon = { RadioButton(selected = book.id == model.addressBook?.id, onClick = null) },
                                onClick = { isBookMenuOpen = false; scope.launch { model.choose(book) } },
                            )
                        }
                    }
                }
            }
            IconButton(onClick = onNewContact, enabled = model.addressBook != null) { Icon(Icons.Outlined.PersonAddAlt, stringResource(R.string.contacts_new_contact)) }
            Box {
                IconButton(onClick = { isMoreMenuOpen = true }, enabled = model.addressBook != null) { Icon(Icons.Outlined.MoreVert, stringResource(R.string.contacts_more)) }
                DropdownMenu(expanded = isMoreMenuOpen, onDismissRequest = { isMoreMenuOpen = false }) {
                    if (!model.isShowingCircleBook) {
                        DropdownMenuItem(text = { Text(stringResource(R.string.contacts_import_from_phone)) }, leadingIcon = { Icon(Icons.Outlined.Download, null) }, onClick = { isMoreMenuOpen = false; onImport() })
                    }
                    for (circle in model.circlesWithoutBook) {
                        DropdownMenuItem(
                            text = { Text(stringResource(if (circle.circleKind == "family") R.string.contacts_make_family_book else R.string.contacts_make_shared_book, circle.name)) },
                            leadingIcon = { Icon(Icons.Outlined.Contacts, null) },
                            onClick = { isMoreMenuOpen = false; scope.launch { model.makeBook(circle) } },
                        )
                    }
                    if (model.isShowingCircleBook && model.addressBook?.canManage == true) {
                        DropdownMenuItem(text = { Text(stringResource(R.string.contacts_rename_book)) }, leadingIcon = { Icon(Icons.Outlined.Edit, null) }, onClick = { isMoreMenuOpen = false; bookNameDraft = model.addressBook?.name.orEmpty(); isRenamingBook = true })
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.contacts_delete_book), color = MaterialTheme.colorScheme.error) }, leadingIcon = { Icon(Icons.Outlined.Delete, null, tint = MaterialTheme.colorScheme.error) },
                            onClick = { isMoreMenuOpen = false; isConfirmingBookDeletion = true },
                        )
                    }
                }
            }
        },
    ) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize()) {
            OutlinedTextField(
                typedText, { typed -> typedText = typed; if (typed.isEmpty()) model.searchText = "" },
                Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), singleLine = true, leadingIcon = { Icon(Icons.Outlined.Search, null) },
                placeholder = { Text(stringResource(R.string.contacts_search_prompt)) }, shape = RoundedCornerShape(28.dp),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search), keyboardActions = KeyboardActions(onSearch = { model.searchText = typedText }),
            )
            when {
                model.loadFailure != null && !model.isLoaded -> ErrorState(model.loadFailure?.message.orEmpty(), onRetry = { scope.launch { model.load() } })
                !model.isLoaded -> LoadingState()
                model.contacts.isEmpty() -> if (model.searchText.isEmpty()) {
                    EmptyState(
                        Icons.Outlined.AccountCircle, stringResource(R.string.contacts_no_contacts_yet),
                        description = stringResource(if (model.isShowingCircleBook) R.string.contacts_no_contacts_circle_description else R.string.contacts_no_contacts_description),
                    )
                } else {
                    EmptyState(Icons.Outlined.Search, stringResource(R.string.contacts_nothing_matches))
                }
                else -> LazyColumn(Modifier.fillMaxSize()) {
                    val circleName = model.addressBook?.circleName
                    if (model.isShowingCircleBook && circleName != null) {
                        item { Text(stringResource(R.string.contacts_everyone_can_change, circleName), Modifier.padding(horizontal = 16.dp, vertical = 8.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                    }
                    if (model.searchText.isEmpty() && !model.isShowingCircleBook && model.shownSuggestions.isNotEmpty()) {
                        item { LetterHeader(stringResource(R.string.contacts_may_be_people_in_circles)) }
                        items(model.shownSuggestions, key = { suggestion -> "suggestion-" + suggestion.id }) { suggestion -> ContactSuggestionRows(model, suggestion) }
                    }
                    for (section in ContactLetterSection.sections(model.contacts)) {
                        item(key = "letter-" + section.letter) { LetterHeader(section.letter) }
                        items(section.contacts, key = { contact -> contact.id }) { contact ->
                            if (model.isShowingCircleBook) {
                                CircleContactRow(model, contact, onEdit = { onEditCircleContact(contact.id) }, onRemove = { removingCircleContact = contact })
                            } else {
                                ContactRow(contact, isMine = contact.id == model.myContactId, modifier = Modifier.clickable { onOpenContact(contact.id) })
                            }
                        }
                    }
                }
            }
        }
    }

    removingCircleContact?.let { pending ->
        ConfirmationDialog(
            title = stringResource(R.string.contacts_remove_from_book_question), text = stringResource(R.string.contacts_removed_for_everyone, pending.shownName, model.addressBook?.circleName.orEmpty()),
            confirmLabel = stringResource(R.string.contacts_remove_named, pending.shownName), isDestructive = true,
            onConfirm = { removingCircleContact = null; scope.launch { model.delete(pending.id) } }, onDismiss = { removingCircleContact = null },
        )
    }
    if (isRenamingBook) {
        com.teanode.app.mail.NameDialog(stringResource(R.string.contacts_rename_the_book), stringResource(R.string.contacts_name), bookNameDraft, { typed -> bookNameDraft = typed }, stringResource(R.string.contacts_save), onDismiss = { isRenamingBook = false }) {
            isRenamingBook = false
            scope.launch { model.renameBook(bookNameDraft) }
        }
    }
    if (isConfirmingBookDeletion) {
        ConfirmationDialog(
            title = stringResource(R.string.contacts_delete_this_book_question), text = stringResource(R.string.contacts_delete_book_message, model.addressBook?.circleName.orEmpty()),
            confirmLabel = stringResource(R.string.contacts_delete_book), isDestructive = true,
            onConfirm = { isConfirmingBookDeletion = false; scope.launch { model.deleteBook() } }, onDismiss = { isConfirmingBookDeletion = false },
        )
    }
}

@Composable
private fun LetterHeader(title: String) {
    Text(title, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 2.dp))
}

/** A contact in a circle's book: opened to edit, or removed for everyone in the circle. */
@Composable
private fun CircleContactRow(model: ContactsModel, contact: ContactSummary, onEdit: () -> Unit, onRemove: () -> Unit) {
    var isMenuOpen by remember { mutableStateOf(false) }
    Row(Modifier.fillMaxWidth().clickable(onClick = onEdit), verticalAlignment = Alignment.CenterVertically) {
        ContactRow(contact, isMine = false, modifier = Modifier.weight(1f), addressBookId = model.addressBook?.id)
        Box {
            IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, stringResource(R.string.contacts_more)) }
            DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                DropdownMenuItem(text = { Text(stringResource(R.string.contacts_remove), color = MaterialTheme.colorScheme.error) }, leadingIcon = { Icon(Icons.Outlined.Delete, null, tint = MaterialTheme.colorScheme.error) }, onClick = { isMenuOpen = false; onRemove() })
            }
        }
    }
}

/** A contact the server thinks is somebody in the person's circles, offered to link or to leave for later. */
@Composable
fun ContactSuggestionRows(model: ContactsModel, suggestion: com.teanode.kit.contacts.ContactAccountSuggestion) {
    val scope = rememberCoroutineScope()
    val name = suggestion.contact.shownName.ifEmpty { stringResource(R.string.contacts_no_name) }
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            AvatarView(suggestion.account.displayName, suggestion.account.avatarUrl, suggestion.account.avatarColorIndex)
            Column(Modifier.padding(start = 12.dp)) {
                Text(name, style = MaterialTheme.typography.titleMedium, maxLines = 2, overflow = TextOverflow.Ellipsis)
                Text(stringResource(R.string.contacts_may_be, name, suggestion.account.displayName), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            TextButton(onClick = { scope.launch { model.link(suggestion.contact.id, suggestion.account, suggestion.contact.shownName, suggestion.addressBookId) } }) {
                Icon(Icons.Outlined.Link, null, Modifier.padding(end = 8.dp))
                Text(stringResource(R.string.contacts_link))
            }
            TextButton(onClick = { model.dismissSuggestion(suggestion.contact.id) }) {
                Icon(Icons.Outlined.Schedule, null, Modifier.padding(end = 8.dp))
                Text(stringResource(R.string.contacts_not_now))
            }
        }
    }
}

/** A contact in the list: their picture, their name, and what else to tell them apart by. */
@Composable
fun ContactRow(contact: ContactSummary, isMine: Boolean, modifier: Modifier = Modifier, addressBookId: String? = null) {
    Row(modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        ContactPicture(contact.id, contact.shownName, contact.hasPhoto == true, 40, addressBookId)
        Column(Modifier.padding(start = 12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    contact.shownName.ifEmpty { stringResource(R.string.contacts_no_name) }, style = MaterialTheme.typography.titleMedium.copy(fontWeight = FontWeight.SemiBold),
                    maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false),
                )
                if (isMine) {
                    Text(
                        stringResource(R.string.contacts_you), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(start = 6.dp).background(MaterialTheme.colorScheme.surfaceVariant, androidx.compose.foundation.shape.CircleShape).padding(horizontal = 8.dp, vertical = 1.dp),
                    )
                }
            }
            val detail = listOf(contact.organization, contact.emails?.firstOrNull(), contact.phones?.firstOrNull()).filterNotNull().firstOrNull { text -> text.isNotEmpty() && text != contact.shownName }
            if (detail != null) Text(detail, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

/** A contact's picture when the address book has one, otherwise the first letter of their name. */
@Composable
fun ContactPicture(contactId: String, name: String, hasPhoto: Boolean, sizeDp: Int, addressBookId: String? = null) {
    AvatarView(name, if (hasPhoto) ContactSummary.pictureAddress(contactId, addressBookId) else null, null, size = sizeDp.dp)
}
