package com.teanode.app.contacts

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
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
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.asReasonSentence
import com.teanode.kit.contacts.ContactAccount
import com.teanode.kit.contacts.ContactAccountSuggestion
import com.teanode.kit.contacts.ContactDetail
import com.teanode.kit.contacts.ContactSummary
import com.teanode.kit.circles.CircleMember
import com.teanode.kit.chats.chatPeople
import com.teanode.kit.contacts.contactAccountSuggestions
import com.teanode.kit.contacts.contacts
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** Chooses which account in the person's circles a contact is, from the people they share a circle with. Only the person sees the choice. */
@Composable
fun ContactAccountPickerScreen(model: ContactsModel, contact: ContactDetail, onBack: () -> Unit, onLinked: () -> Unit) {
    val scope = rememberCoroutineScope()
    var people by remember { mutableStateOf(emptyList<ContactAccount>()) }
    var hasReadPeople by remember { mutableStateOf(false) }
    var takenAccountIds by remember { mutableStateOf(emptySet<String>()) }
    var searchText by remember { mutableStateOf("") }
    var isLinking by remember { mutableStateOf(false) }
    var failureText by remember { mutableStateOf<String?>(null) }
    val noLinkText = stringResource(R.string.contacts_could_not_link)

    LaunchedEffect(contact.id) {
        try {
            people = model.connection.chatPeople()
            val bookId = model.addressBook?.id
            if (bookId != null) {
                val bookContacts = runCatching { model.connection.contacts(bookId, first = 500) }.getOrNull()
                if (bookContacts != null) takenAccountIds = bookContacts.filter { other -> other.id != contact.id }.mapNotNull { other -> other.accountUserId }.filter { id -> id.isNotEmpty() }.toSet()
            }
            hasReadPeople = true
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            failureText = (failed.message ?: "").asReasonSentence()
        }
    }
    val words = searchText.trim()
    val shown = people.filter { person -> !takenAccountIds.contains(person.userId) }.filter { person -> words.isEmpty() || person.displayName.contains(words, ignoreCase = true) || person.username.contains(words, ignoreCase = true) }
    TeaNodeScreen(title = stringResource(R.string.contacts_is_in_your_circles_as), onBack = onBack) { contentPadding ->
        LazyColumn(Modifier.padding(contentPadding).fillMaxSize()) {
            failureText?.let { text -> item { Text(text, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(16.dp)) } }
            item {
                OutlinedTextField(searchText, { typed -> searchText = typed }, Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), singleLine = true, placeholder = { Text(stringResource(R.string.contacts_search_people)) })
            }
            items(shown, key = { person -> person.userId }) { person ->
                Row(
                    Modifier.fillMaxWidth().clickable(enabled = !isLinking) {
                        isLinking = true
                        scope.launch {
                            if (model.link(contact.id, person, contact.name.orEmpty(), isQuiet = true)) onLinked() else failureText = noLinkText
                            isLinking = false
                        }
                    }.padding(horizontal = 16.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    AvatarView(person.displayName, person.avatarUrl, person.avatarColorIndex, size = 36.dp)
                    Column(Modifier.weight(1f).padding(start = 12.dp)) {
                        Text(person.displayName)
                        if (person.displayName != person.username) Text(person.username, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    if (person.userId == contact.accountUserId) Icon(Icons.Outlined.Check, stringResource(R.string.contacts_current_choice), tint = MaterialTheme.colorScheme.primary)
                }
            }
            if (hasReadPeople && people.isEmpty()) item { Text(stringResource(R.string.contacts_no_shared_circle), Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
            item { Text(stringResource(R.string.contacts_choose_who_is, contact.name.orEmpty()), Modifier.padding(16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        }
    }
}

/**
 * Chooses which of the person's contacts is somebody in their circles, from the card of that somebody: the other way round
 * from the contact screen. The contacts the server thinks are them come first, then a search of the address book.
 */
@Composable
fun PersonContactPickerScreen(model: ContactsModel, person: CircleMember, onBack: () -> Unit, onLinked: () -> Unit) {
    val scope = rememberCoroutineScope()
    var suggestions by remember { mutableStateOf(emptyList<ContactAccountSuggestion>()) }
    var found by remember { mutableStateOf(emptyList<ContactSummary>()) }
    var hasReadContacts by remember { mutableStateOf(false) }
    var searchText by remember { mutableStateOf("") }
    var isLinking by remember { mutableStateOf(false) }
    var failureText by remember { mutableStateOf<String?>(null) }
    val noLinkText = stringResource(R.string.contacts_could_not_link)

    LaunchedEffect(person.userId) { suggestions = runCatching { model.connection.contactAccountSuggestions() }.getOrDefault(emptyList()) }
    LaunchedEffect(searchText) {
        if (searchText.isNotEmpty()) delay(250)
        val bookId = model.addressBook?.id ?: model.readAddressBookId() ?: return@LaunchedEffect
        try {
            found = model.connection.contacts(bookId, searchText.trim(), 50)
            hasReadContacts = true
            failureText = null
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            failureText = (failed.message ?: "").asReasonSentence()
        }
    }
    val offered = suggestions.filter { suggestion -> suggestion.account.userId == person.userId }.map { suggestion -> suggestion.contact }
    val offeredIds = offered.map { contact -> contact.id }.toSet()
    val others = found.filter { contact -> !offeredIds.contains(contact.id) }

    fun choose(contact: ContactSummary) {
        isLinking = true
        scope.launch {
            val suggestedBookId = suggestions.firstOrNull { suggestion -> suggestion.contact.id == contact.id }?.addressBookId
            val account = ContactAccount(person.userId, person.username, person.name, person.avatarUrl, person.avatarColorIndex)
            if (model.link(contact.id, account, contact.shownName, suggestedBookId ?: model.addressBook?.id, isQuiet = true)) onLinked() else failureText = noLinkText
            isLinking = false
        }
    }

    TeaNodeScreen(title = stringResource(R.string.contacts_link_a_contact_title), onBack = onBack) { contentPadding ->
        LazyColumn(Modifier.padding(contentPadding).fillMaxSize()) {
            failureText?.let { text -> item { Text(text, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(16.dp)) } }
            item {
                OutlinedTextField(searchText, { typed -> searchText = typed }, Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), singleLine = true, placeholder = { Text(stringResource(R.string.contacts_name_address_or_phone)) })
            }
            if (searchText.isEmpty() && offered.isNotEmpty()) {
                item { Text(stringResource(R.string.contacts_may_be_people_in_circles), Modifier.padding(horizontal = 16.dp, vertical = 8.dp), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary) }
                items(offered, key = { contact -> "offered-" + contact.id }) { contact -> ContactChoice(contact, isLinking) { choose(contact) } }
            }
            items(others, key = { contact -> contact.id }) { contact -> ContactChoice(contact, isLinking) { choose(contact) } }
            if (hasReadContacts && found.isEmpty()) item { Text(stringResource(R.string.contacts_no_contacts_yet_short), Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
            item { Text(stringResource(R.string.contacts_choose_which_contact_is, person.displayName), Modifier.padding(16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        }
    }
}

@Composable
private fun ContactChoice(contact: ContactSummary, isLinking: Boolean, onChoose: () -> Unit) {
    Column(Modifier.fillMaxWidth().clickable(enabled = !isLinking, onClick = onChoose).padding(horizontal = 16.dp, vertical = 8.dp)) {
        Text(contact.shownName.ifEmpty { stringResource(R.string.contacts_no_name) })
        val detail = listOf(contact.organization, contact.emails?.firstOrNull(), contact.phones?.firstOrNull()).filterNotNull().firstOrNull { text -> text.isNotEmpty() && text != contact.shownName }
        if (detail != null) Text(detail, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}
