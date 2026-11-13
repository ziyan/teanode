package com.teanode.app.contacts

import android.Manifest
import android.content.Intent
import android.net.Uri
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AccountCircle
import androidx.compose.material.icons.outlined.PersonOff
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.Checkbox
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.asReasonSentence
import com.teanode.kit.contacts.ContactCard
import com.teanode.kit.contacts.ContactMatcher
import com.teanode.kit.contacts.contacts
import com.teanode.kit.contacts.identity
import com.teanode.kit.contacts.importContact
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Semaphore
import kotlinx.coroutines.sync.withPermit

/** What the import screen is showing. */
sealed class ImportPhase {
    data object Loading : ImportPhase()
    data object Denied : ImportPhase()
    data class Failed(val reasonText: String) : ImportPhase()
    data object Ready : ImportPhase()
}

/** ContactImportModel is the phone's contacts, which of them the address book already has, and what has been chosen. */
class ContactImportModel(private val contactsModel: ContactsModel, private val addressBookId: String, private val phoneContactsOf: suspend () -> List<PhoneContact>, private val hasAccess: () -> Boolean) {
    var phase by mutableStateOf<ImportPhase>(ImportPhase.Loading)
        private set
    var phoneContacts by mutableStateOf(emptyList<PhoneContact>())
        private set
    var alreadyPresentIds by mutableStateOf(emptySet<String>())
        private set
    var chosenIds by mutableStateOf(emptySet<String>())
        private set
    var isImporting by mutableStateOf(false)
        private set
    var finishedCount by mutableStateOf(0)
        private set
    var totalCount by mutableStateOf(0)
        private set
    var searchText by mutableStateOf("")

    val shownContacts: List<PhoneContact> get() = searchText.trim().let { words -> if (words.isEmpty()) phoneContacts else phoneContacts.filter { contact -> contact.name.contains(words, ignoreCase = true) } }
    private val shownNewIds: List<String> get() = shownContacts.map { contact -> contact.id }.filter { id -> !alreadyPresentIds.contains(id) }
    val areAllShownNewChosen: Boolean get() = shownNewIds.isNotEmpty() && shownNewIds.all { id -> chosenIds.contains(id) }

    suspend fun load() {
        phase = ImportPhase.Loading
        if (!hasAccess()) {
            phase = ImportPhase.Denied
            return
        }
        try {
            val found = phoneContactsOf()
            val existing = contactsModel.connection.contacts(addressBookId)
            val matcher = ContactMatcher(existing.map { contact -> contact.identity })
            val presentIds = found.filter { contact -> matcher.isAlreadyPresent(contact.identity) }.map { contact -> contact.id }
            phoneContacts = found
            alreadyPresentIds = presentIds.toSet()
            chosenIds = found.map { contact -> contact.id }.toSet() - alreadyPresentIds
            phase = ImportPhase.Ready
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            phase = ImportPhase.Failed((failed.message ?: "").asReasonSentence())
        }
    }

    fun toggle(id: String) {
        chosenIds = if (chosenIds.contains(id)) chosenIds - id else chosenIds + id
    }

    fun toggleAllShown() {
        chosenIds = if (areAllShownNewChosen) chosenIds - shownContacts.map { contact -> contact.id }.toSet() else chosenIds + shownNewIds
    }

    /** Sends the cards chosen, a few at a time, and says how many went in and how many could not. */
    suspend fun importChosen(): Pair<Int, Int> {
        val cards = phoneContacts.filter { contact -> chosenIds.contains(contact.id) }.map { contact -> contact.card }
        isImporting = true
        finishedCount = 0
        totalCount = cards.size
        var importedCount = 0
        val permits = Semaphore(CONCURRENT_IMPORT_COUNT)
        coroutineScope {
            cards.map { card ->
                async(Dispatchers.IO) {
                    val isImported = permits.withPermit { send(card) }
                    synchronized(this@ContactImportModel) {
                        if (isImported) importedCount += 1
                    }
                    finishedCount += 1
                }
            }.awaitAll()
        }
        isImporting = false
        return importedCount to (cards.size - importedCount)
    }

    private suspend fun send(card: String?): Boolean {
        if (card == null || !ContactCard.fits(card)) return false
        return try {
            contactsModel.connection.importContact(addressBookId, card)
            true
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (_: Throwable) {
            false
        }
    }

    companion object {
        const val CONCURRENT_IMPORT_COUNT = 4
    }
}

/** Brings the contacts on the phone into the address book: which of them it already has, and the cards sent for the rest. */
@Composable
fun ContactImportScreen(model: ContactsModel, onBack: () -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var addressBookId by remember { mutableStateOf<String?>(model.addressBook?.takeIf { book -> !book.isOfACircle }?.id) }
    LaunchedEffect(model) { if (addressBookId == null) addressBookId = model.readAddressBookId() }
    val bookId = addressBookId
    if (bookId == null) {
        TeaNodeScreen(title = stringResource(R.string.contacts_import_from_phone), onBack = onBack) { contentPadding -> LoadingState(Modifier.padding(contentPadding)) }
        return
    }
    val importModel = remember(bookId) { ContactImportModel(model, bookId, { PhoneContacts.readAll(context) }, { PhoneContacts.hasAccess(context) }) }
    val askPermission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { scope.launch { importModel.load() } }
    LaunchedEffect(importModel) {
        if (!PhoneContacts.hasAccess(context)) askPermission.launch(Manifest.permission.READ_CONTACTS) else importModel.load()
    }
    ContactImportContent(
        importModel, onBack, onOpenSettings = { runCatching { context.startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:" + context.packageName)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) } },
        onImport = {
            scope.launch {
                val (importedCount, failedCount) = importModel.importChosen()
                model.imported(importedCount, failedCount)
                onBack()
            }
        },
    )
}

@Composable
fun ContactImportContent(importModel: ContactImportModel, onBack: () -> Unit, onOpenSettings: () -> Unit, onImport: () -> Unit) {
    val isReady = importModel.phase == ImportPhase.Ready
    TeaNodeScreen(
        title = stringResource(R.string.contacts_import_from_phone), onBack = if (importModel.isImporting) null else onBack,
        actions = { TextButton(enabled = isReady && importModel.chosenIds.isNotEmpty() && !importModel.isImporting, onClick = onImport) { Text(stringResource(R.string.contacts_import)) } },
    ) { contentPadding ->
        Box(Modifier.padding(contentPadding).fillMaxSize()) {
            when (val phase = importModel.phase) {
                ImportPhase.Loading -> Column(Modifier.fillMaxSize(), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
                    androidx.compose.material3.CircularProgressIndicator()
                    Text(stringResource(R.string.contacts_reading_contacts), Modifier.padding(top = 16.dp))
                }
                ImportPhase.Denied -> EmptyState(
                    Icons.Outlined.PersonOff, stringResource(R.string.contacts_access_is_off), description = stringResource(R.string.contacts_access_description),
                    actionLabel = stringResource(R.string.contacts_open_settings), onAction = onOpenSettings,
                )
                is ImportPhase.Failed -> EmptyState(Icons.Outlined.PersonOff, stringResource(R.string.contacts_could_not_be_read), description = phase.reasonText)
                ImportPhase.Ready -> if (importModel.phoneContacts.isEmpty()) {
                    EmptyState(Icons.Outlined.AccountCircle, stringResource(R.string.contacts_none_on_phone))
                } else {
                    Column(Modifier.fillMaxSize()) {
                        OutlinedTextField(
                            importModel.searchText, { typed -> importModel.searchText = typed }, Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), singleLine = true,
                            leadingIcon = { Icon(Icons.Outlined.Search, null) }, placeholder = { Text(stringResource(R.string.contacts_name)) }, shape = RoundedCornerShape(28.dp),
                        )
                        Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                            Text(stringResource(R.string.contacts_chosen_count, importModel.chosenIds.size), Modifier.weight(1f), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                            TextButton(onClick = { importModel.toggleAllShown() }, enabled = importModel.shownContacts.isNotEmpty()) {
                                Text(stringResource(if (importModel.areAllShownNewChosen) R.string.contacts_select_none else R.string.contacts_select_all))
                            }
                        }
                        if (importModel.shownContacts.isEmpty()) {
                            EmptyState(Icons.Outlined.Search, stringResource(R.string.contacts_nothing_matches))
                        } else {
                            LazyColumn(Modifier.fillMaxSize()) {
                                items(importModel.shownContacts, key = { contact -> contact.id }) { contact ->
                                    val isAlreadyPresent = importModel.alreadyPresentIds.contains(contact.id)
                                    Row(Modifier.fillMaxWidth().clickable { importModel.toggle(contact.id) }.padding(horizontal = 16.dp, vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
                                        Checkbox(importModel.chosenIds.contains(contact.id), onCheckedChange = null)
                                        Column(Modifier.padding(start = 12.dp)) {
                                            Text(contact.name.ifEmpty { stringResource(R.string.contacts_no_name) })
                                            val detail = if (isAlreadyPresent) stringResource(R.string.contacts_already_in_teanode) else contact.firstAddressText
                                            if (detail.isNotEmpty()) Text(detail, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
            if (importModel.isImporting) {
                Surface(Modifier.fillMaxSize()) {
                    Column(Modifier.padding(32.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
                        LinearProgressIndicator(progress = { importModel.finishedCount.toFloat() / maxOf(importModel.totalCount, 1) }, modifier = Modifier.fillMaxWidth())
                        Text(stringResource(R.string.contacts_importing_progress, importModel.finishedCount, importModel.totalCount), Modifier.padding(top = 16.dp), style = MaterialTheme.typography.titleMedium, textAlign = TextAlign.Center)
                    }
                }
            }
        }
    }
}
