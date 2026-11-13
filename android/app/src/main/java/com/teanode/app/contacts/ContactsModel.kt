package com.teanode.app.contacts

import android.content.Context
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.SnackbarController
import com.teanode.kit.Connection
import com.teanode.kit.contacts.AddressBook
import com.teanode.kit.contacts.ContactAccount
import com.teanode.kit.contacts.ContactAccountSuggestion
import com.teanode.kit.contacts.ContactCircle
import com.teanode.kit.contacts.ContactSummary
import com.teanode.kit.contacts.addressBooks
import com.teanode.kit.contacts.circles
import com.teanode.kit.contacts.contactAccountSuggestions
import com.teanode.kit.contacts.contacts
import com.teanode.kit.contacts.createCircleAddressBook
import com.teanode.kit.contacts.deleteCircleAddressBook
import com.teanode.kit.contacts.deleteContact
import com.teanode.kit.contacts.myContactId
import com.teanode.kit.contacts.reachableAddressBooks
import com.teanode.kit.contacts.removeContactPicture
import com.teanode.kit.contacts.renameAddressBook
import com.teanode.kit.contacts.setContactAccount
import com.teanode.kit.contacts.setMyContact
import com.teanode.kit.refusalReason
import java.util.WeakHashMap

/**
 * ContactsModel is the person's address book as the Contacts list keeps it: the book being read, its contacts, which is
 * the person themselves, the circles' books, and the contacts the server thinks are people in their circles. One lives
 * for each account, so the list and the person's card share what changes.
 */
class ContactsModel(val connection: Connection, private val context: Context) {
    private val preferences = runCatching { context.getSharedPreferences("contacts", Context.MODE_PRIVATE) }.getOrNull()
    private val dismissedKey = "contact-account-suggestions-dismissed.${connection.accountKey}"

    var addressBook by mutableStateOf<AddressBook?>(null)
        private set
    var books by mutableStateOf(emptyList<AddressBook>())
        private set
    var circlesWithoutBook by mutableStateOf(emptyList<ContactCircle>())
        private set
    var contacts by mutableStateOf(emptyList<ContactSummary>())
        private set
    var myContactId by mutableStateOf<String?>(null)
        private set
    var isLoaded by mutableStateOf(false)
        private set
    var loadFailure by mutableStateOf<Throwable?>(null)
        private set
    var searchText by mutableStateOf("")
    var suggestions by mutableStateOf(emptyList<ContactAccountSuggestion>())
        private set
    private var dismissedSuggestionContactIds by mutableStateOf(runCatching { preferences?.getStringSet(dismissedKey, emptySet()) ?: emptySet() }.getOrDefault(emptySet()))

    /** Where the snackbar of the screen on show is: set by [rememberContactsModel] on every composition. */
    var snackbar: SnackbarController? = null

    val shownSuggestions: List<ContactAccountSuggestion> get() = suggestions.filter { suggestion -> !dismissedSuggestionContactIds.contains(suggestion.contact.id) }

    val isShowingCircleBook: Boolean get() = addressBook?.isOfACircle == true

    val title: String
        get() {
            val book = addressBook
            return if (isShowingCircleBook && book != null) book.shownName else context.getString(R.string.contacts_title)
        }

    private fun say(text: String) {
        snackbar?.show(text)
    }

    private fun fail(failure: Throwable) {
        snackbar?.showError(failure)
    }

    suspend fun loadSuggestions() {
        suggestions = runCatching { connection.contactAccountSuggestions() }.getOrDefault(emptyList())
    }

    fun dismissSuggestion(contactId: String) {
        dismissedSuggestionContactIds = dismissedSuggestionContactIds + contactId
        runCatching { preferences?.edit()?.putStringSet(dismissedKey, dismissedSuggestionContactIds)?.apply() }
    }

    /** Says a contact is somebody in the person's circles; private to the person. */
    suspend fun link(contactId: String, account: ContactAccount, contactName: String, addressBookId: String? = null, isQuiet: Boolean = false): Boolean {
        val bookId = addressBookId ?: addressBook?.id ?: return false
        return try {
            connection.setContactAccount(bookId, contactId, account.userId)
            suggestions = suggestions.filter { suggestion -> suggestion.contact.id != contactId }
            say(context.getString(R.string.contacts_is_in_circles_as, contactName, account.displayName))
            true
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            if (!isQuiet) fail(failed)
            false
        }
    }

    suspend fun unlink(contactId: String): Boolean {
        val book = addressBook ?: return false
        return try {
            connection.setContactAccount(book.id, contactId, null)
            say(context.getString(R.string.contacts_removed_link))
            loadSuggestions()
            true
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            fail(failed)
            false
        }
    }

    /** The address book the contacts are in: the person's own, which a circle's is not. */
    suspend fun readAddressBookId(): String? {
        val book = addressBook
        if (book != null && !book.isOfACircle) return book.id
        return runCatching { connection.addressBooks().firstOrNull()?.id }.getOrNull()
    }

    private suspend fun readBooks(): List<AddressBook> {
        val reachable = runCatching { connection.reachableAddressBooks() }.getOrNull()
        if (!reachable.isNullOrEmpty()) return reachable
        return connection.addressBooks()
    }

    suspend fun choose(book: AddressBook) {
        if (book.id == addressBook?.id) return
        addressBook = book
        contacts = emptyList()
        isLoaded = false
        load()
    }

    suspend fun chooseOwnBook() {
        val own = books.firstOrNull { book -> !book.isOfACircle } ?: return
        choose(own)
    }

    suspend fun loadCirclesWithoutBook() {
        val circles = runCatching { connection.circles() }.getOrNull() ?: return
        val withBook = books.mapNotNull { book -> book.circleId }.toSet()
        circlesWithoutBook = circles.filter { circle -> !withBook.contains(circle.id) }
    }

    suspend fun makeBook(circle: ContactCircle) {
        try {
            val made = connection.createCircleAddressBook(circle.id)
            say(context.getString(R.string.contacts_book_made))
            books = readBooks()
            loadCirclesWithoutBook()
            choose(books.firstOrNull { book -> book.id == made.id } ?: made)
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            snackbar?.showProblem(failed.refusalReason(context.getString(R.string.contacts_did_not_work)))
        }
    }

    suspend fun renameBook(name: String) {
        val book = addressBook ?: return
        if (!book.isOfACircle) return
        val trimmed = name.trim()
        if (trimmed.isEmpty() || trimmed == book.name) return
        try {
            val renamed = connection.renameAddressBook(book, trimmed)
            say(context.getString(R.string.contacts_book_renamed))
            books = books.map { existing -> if (existing.id == renamed.id) renamed else existing }
            addressBook = renamed
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            snackbar?.showProblem(failed.refusalReason(context.getString(R.string.contacts_did_not_work)))
        }
    }

    suspend fun deleteBook() {
        val book = addressBook ?: return
        if (!book.isOfACircle) return
        try {
            connection.deleteCircleAddressBook(book.id)
            say(context.getString(R.string.contacts_book_deleted))
            books = books.filter { existing -> existing.id != book.id }
            loadCirclesWithoutBook()
            chooseOwnBook()
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            snackbar?.showProblem(failed.refusalReason(context.getString(R.string.contacts_did_not_work)))
        }
    }

    /** Reads only the address book, for a screen that needs where a contact lives and nothing else. */
    suspend fun loadAddressBook() {
        if (addressBook != null) return
        addressBook = runCatching { connection.addressBooks().firstOrNull() }.getOrNull()
        myContactId = runCatching { connection.myContactId() }.getOrNull()
    }

    suspend fun load() {
        try {
            if (addressBook == null) {
                books = readBooks()
                addressBook = books.firstOrNull()
                myContactId = runCatching { connection.myContactId() }.getOrNull()
            }
            val book = addressBook
            if (book == null) {
                isLoaded = true
                return
            }
            contacts = connection.contacts(book.id, searchText.trim(), 500)
            isLoaded = true
            loadFailure = null
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            if (isLoaded) fail(failed) else loadFailure = failed
        }
    }

    suspend fun setMine(contactId: String?) {
        try {
            connection.setMyContact(contactId)
            myContactId = contactId
            say(context.getString(if (contactId == null) R.string.contacts_no_contact_is_you else R.string.contacts_marked_as_you))
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            fail(failed)
        }
    }

    suspend fun removePicture(contactId: String): Boolean = try {
        connection.removeContactPicture(contactId, addressBook?.id)
        say(context.getString(R.string.contacts_removed_picture))
        load()
        true
    } catch (failed: kotlinx.coroutines.CancellationException) {
        throw failed
    } catch (failed: Throwable) {
        fail(failed)
        false
    }

    suspend fun delete(contactId: String): Boolean = try {
        connection.deleteContact(contactId, addressBook?.id)
        say(context.getString(R.string.contacts_contact_removed))
        load()
        true
    } catch (failed: kotlinx.coroutines.CancellationException) {
        throw failed
    } catch (failed: Throwable) {
        fail(failed)
        false
    }

    /** Says a contact was saved and reads the list again. */
    suspend fun saved() {
        say(context.getString(R.string.contacts_contact_saved))
        load()
    }

    /** Says what an import did and reads the list again. */
    suspend fun imported(importedCount: Int, failedCount: Int) {
        load()
        if (failedCount > 0) {
            snackbar?.showProblem(context.getString(R.string.contacts_import_failed_count, failedCount, importedCount + failedCount))
        } else {
            say(context.resources.getQuantityString(R.plurals.contacts_imported_count, importedCount, importedCount))
        }
    }

    companion object {
        private val models = WeakHashMap<Connection, ContactsModel>()

        /** The model of this account's address book, made once. */
        @Synchronized
        fun of(connection: Connection, context: Context): ContactsModel = models.getOrPut(connection) { ContactsModel(connection, context.applicationContext) }
    }
}

/** The address book of the account in use. */
@Composable
fun rememberContactsModel(): ContactsModel {
    val connection = LocalConnection.current
    val context = LocalContext.current
    val snackbar = LocalSnackbar.current
    val model = remember(connection) { ContactsModel.of(connection, context) }
    SideEffect { model.snackbar = snackbar }
    return model
}
