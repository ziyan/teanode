package com.teanode.app.contacts

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.contacts.ContactAddress
import com.teanode.kit.contacts.ContactDetail
import com.teanode.kit.contacts.ContactWords
import com.teanode.kit.contacts.contact
import com.teanode.kit.contacts.saveContact
import kotlinx.coroutines.launch

/**
 * The editor of a contact: a new one when [contactId] is null, else the one read from the address book. Every field is
 * sent back, since an empty one clears what is there.
 */
@Composable
fun ContactEditorScreen(model: ContactsModel, contactId: String?, onBack: () -> Unit) {
    var contact by remember(contactId) { mutableStateOf<ContactDetail?>(null) }
    var failure by remember(contactId) { mutableStateOf<Throwable?>(null) }
    val scope = rememberCoroutineScope()
    suspend fun read() {
        if (contactId == null) return
        try {
            model.loadAddressBook()
            contact = model.connection.contact(contactId, model.addressBook?.id)
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            failure = failed
        }
    }
    LaunchedEffect(contactId) { if (model.addressBook == null) model.loadAddressBook(); read() }
    when {
        contactId == null || contact != null -> ContactEditorForm(model, contact, onBack)
        failure != null -> TeaNodeScreen(title = stringResource(R.string.contacts_edit_contact), onBack = onBack) { contentPadding ->
            ErrorState(stringResource(R.string.contacts_could_not_read_contact), onRetry = { failure = null; scope.launch { read() } }, modifier = Modifier.padding(contentPadding))
        }
        else -> TeaNodeScreen(title = stringResource(R.string.contacts_edit_contact), onBack = onBack) { contentPadding -> LoadingState(Modifier.padding(contentPadding)) }
    }
}

@Composable
private fun ContactEditorForm(model: ContactsModel, contact: ContactDetail?, onBack: () -> Unit) {
    val scope = rememberCoroutineScope()
    var name by remember(contact) { mutableStateOf(contact?.name.orEmpty()) }
    var organization by remember(contact) { mutableStateOf(contact?.organization.orEmpty()) }
    val emails = remember(contact) { mutableStateListOf<String>().also { list -> list.addAll(contact?.emails ?: emptyList()) } }
    val phones = remember(contact) { mutableStateListOf<String>().also { list -> list.addAll(contact?.phones ?: emptyList()) } }
    var address by remember(contact) { mutableStateOf(contact?.addresses?.firstOrNull() ?: ContactAddress()) }
    var note by remember(contact) { mutableStateOf(contact?.note.orEmpty()) }
    var isSaving by remember { mutableStateOf(false) }

    TeaNodeScreen(
        title = stringResource(if (contact == null) R.string.contacts_new_contact else R.string.contacts_edit_contact),
        onBack = onBack,
        actions = {
            TextButton(enabled = !isSaving && !(name.isBlank() && emails.all { email -> email.isBlank() }) && model.addressBook != null, onClick = {
                val book = model.addressBook ?: return@TextButton
                val kept = (contact?.addresses ?: emptyList()).drop(1)
                val first = if (ContactWords.written(address).isEmpty()) emptyList() else listOf(address)
                isSaving = true
                scope.launch {
                    try {
                        model.connection.saveContact(
                            book.id, contact?.id, name.trim(), organization.trim(), emails.map { email -> email.trim() }.filter { email -> email.isNotEmpty() },
                            phones.map { phone -> phone.trim() }.filter { phone -> phone.isNotEmpty() }, note.trim(), first + kept,
                        )
                        model.saved()
                        onBack()
                    } catch (failed: kotlinx.coroutines.CancellationException) {
                        throw failed
                    } catch (failed: Throwable) {
                        model.snackbar?.showError(failed)
                    } finally {
                        isSaving = false
                    }
                }
            }) { Text(stringResource(R.string.contacts_save)) }
        },
    ) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            OutlinedTextField(name, { typed -> name = typed }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.contacts_name)) }, singleLine = true)
            OutlinedTextField(organization, { typed -> organization = typed }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.contacts_organization)) }, singleLine = true)
            Text(stringResource(R.string.contacts_email), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
            emails.forEachIndexed { index, email ->
                Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                    OutlinedTextField(
                        email, { typed -> emails[index] = typed }, Modifier.weight(1f), label = { Text(stringResource(R.string.contacts_email)) }, singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email, capitalization = KeyboardCapitalization.None, autoCorrectEnabled = false),
                    )
                    IconButton(onClick = { emails.removeAt(index) }) { Icon(Icons.Outlined.Close, stringResource(R.string.contacts_remove)) }
                }
            }
            OutlinedButton(onClick = { emails.add("") }) { Icon(Icons.Outlined.Add, null, Modifier.size(18.dp)); Spacer(Modifier.width(8.dp)); Text(stringResource(R.string.contacts_add_email)) }
            Text(stringResource(R.string.contacts_phone), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
            phones.forEachIndexed { index, phone ->
                Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                    OutlinedTextField(phone, { typed -> phones[index] = typed }, Modifier.weight(1f), label = { Text(stringResource(R.string.contacts_phone)) }, singleLine = true, keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Phone))
                    IconButton(onClick = { phones.removeAt(index) }) { Icon(Icons.Outlined.Close, stringResource(R.string.contacts_remove)) }
                }
            }
            OutlinedButton(onClick = { phones.add("") }) { Icon(Icons.Outlined.Add, null, Modifier.size(18.dp)); Spacer(Modifier.width(8.dp)); Text(stringResource(R.string.contacts_add_phone)) }
            Text(stringResource(R.string.contacts_address), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
            OutlinedTextField(address.street.orEmpty(), { typed -> address = address.copy(street = typed.ifEmpty { null }) }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.contacts_street)) })
            OutlinedTextField(address.locality.orEmpty(), { typed -> address = address.copy(locality = typed.ifEmpty { null }) }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.contacts_city)) }, singleLine = true)
            OutlinedTextField(address.region.orEmpty(), { typed -> address = address.copy(region = typed.ifEmpty { null }) }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.contacts_region)) }, singleLine = true)
            OutlinedTextField(address.postalCode.orEmpty(), { typed -> address = address.copy(postalCode = typed.ifEmpty { null }) }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.contacts_postal_code)) }, singleLine = true)
            OutlinedTextField(address.country.orEmpty(), { typed -> address = address.copy(country = typed.ifEmpty { null }) }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.contacts_country)) }, singleLine = true)
            Text(stringResource(R.string.contacts_notes), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
            OutlinedTextField(note, { typed -> note = typed }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.contacts_notes)) }, minLines = 3)
        }
    }
}
