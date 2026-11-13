package com.teanode.app.contacts

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Contacts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.foundation.layout.padding
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.navigation.NamedNavArgument
import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.AppPlace
import com.teanode.app.shell.Area
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shell.Routes
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.AppSection
import com.teanode.kit.contacts.PersonCardTarget

private fun stringArgument(name: String, isOptional: Boolean = false): NamedNavArgument = navArgument(name) {
    type = NavType.StringType
    if (isOptional) {
        nullable = true
        defaultValue = null
    }
}

/**
 * The routes of Contacts: the list, a contact (a person's card), its editor, the import from the phone, and the card and
 * visits of a person of the circles. Another part opens somebody's card with [ContactsRoutes.person].
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.contactsRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Contacts), deepLinks = Routes.deepLinks(Routes.home(AppSection.Contacts))) { ContactsSection() }
    composable(ContactsRoutes.CONTACT, arguments = listOf(stringArgument("contactId")), deepLinks = Routes.deepLinks(ContactsRoutes.CONTACT)) { entry ->
        val model = rememberContactsModel()
        val nav = LocalAppNavigator.current
        ContactCardDestination(model, entry.arguments?.getString("contactId").orEmpty(), onBack = { nav.back() })
    }
    composable(ContactsRoutes.EDITOR, arguments = listOf(stringArgument("contactId", isOptional = true)), deepLinks = Routes.deepLinks(ContactsRoutes.EDITOR)) { entry ->
        val model = rememberContactsModel()
        val nav = LocalAppNavigator.current
        ContactEditorScreen(model, entry.arguments?.getString("contactId"), onBack = { nav.back() })
    }
    composable(ContactsRoutes.IMPORT, deepLinks = Routes.deepLinks(ContactsRoutes.IMPORT)) {
        val model = rememberContactsModel()
        val nav = LocalAppNavigator.current
        ContactImportScreen(model, onBack = { nav.back() })
    }
    composable(ContactsRoutes.PERSON_ACCOUNT, arguments = listOf(stringArgument("userId")), deepLinks = Routes.deepLinks(ContactsRoutes.PERSON_ACCOUNT)) { entry ->
        val model = rememberContactsModel()
        val nav = LocalAppNavigator.current
        PersonCardScreen(model, PersonCardTarget.Account(entry.arguments?.getString("userId").orEmpty()), onBack = { nav.back() })
    }
    composable(ContactsRoutes.PERSON_CONTACT, arguments = listOf(stringArgument("addressBookId"), stringArgument("contactId")), deepLinks = Routes.deepLinks(ContactsRoutes.PERSON_CONTACT)) { entry ->
        val model = rememberContactsModel()
        val nav = LocalAppNavigator.current
        PersonCardScreen(model, PersonCardTarget.Contact(entry.arguments?.getString("addressBookId").orEmpty(), entry.arguments?.getString("contactId").orEmpty()), onBack = { nav.back() })
    }
    composable(ContactsRoutes.VISITS, arguments = listOf(stringArgument("userId")), deepLinks = Routes.deepLinks(ContactsRoutes.VISITS)) { entry ->
        val nav = LocalAppNavigator.current
        PersonVisitsScreen(LocalConnection.current, entry.arguments?.getString("userId").orEmpty(), onBack = { nav.back() })
    }
}

/** One contact whole, as the list opens it: the same card as anybody else's. It needs the address book the contact is in, which a link may open before the list has read it. */
@Composable
fun ContactCardDestination(model: ContactsModel, contactId: String, onBack: () -> Unit) {
    var addressBookId by remember { mutableStateOf<String?>(null) }
    var hasFailed by remember { mutableStateOf(false) }
    LaunchedEffect(contactId) {
        addressBookId = model.readAddressBookId()
        hasFailed = addressBookId == null
        if (model.addressBook == null) model.loadAddressBook()
    }
    val bookId = addressBookId
    when {
        bookId != null -> PersonCardScreen(model, PersonCardTarget.Contact(bookId, contactId), onBack)
        hasFailed -> TeaNodeScreen(title = stringResource(R.string.contacts_title), onBack = onBack) { contentPadding ->
            ErrorState(stringResource(R.string.contacts_could_not_read_contact), onRetry = { hasFailed = false; }, modifier = Modifier.padding(contentPadding))
        }
        else -> TeaNodeScreen(title = stringResource(R.string.contacts_title), onBack = onBack) { contentPadding -> LoadingState(Modifier.padding(contentPadding)) }
    }
}

/** What Contacts plugs into the shell; listed in `Areas`. */
val contactsArea = Area(
    section = AppSection.Contacts,
    routes = NavGraphBuilder::contactsRoutes,
    places = { listOf(AppPlace("contacts-list", R.string.contacts_title, Routes.home(AppSection.Contacts), Icons.Outlined.Contacts)) },
    demoAnswerers = contactsDemoAnswerers,
)
