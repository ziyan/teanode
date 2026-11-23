package com.teanode.app.passwords

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.passwords.PasswordsAutofillIndex
import com.teanode.kit.passwords.PasswordsAutofillLogin
import com.teanode.kit.passwords.PasswordsAutofillSectionKind
import com.teanode.kit.passwords.PasswordsSiteMatchLevel

/**
 * What the person sees after the fingerprint, as on the iPhone: the logins for the page asking, those for a related site, and
 * all the rest, with a search field over every login. Choosing a login that is not saved for the exact page asks first
 * ("Fill X into Y?"); the answer yes calls [onChoose]. An app names no page, so nothing is suggested and nothing is asked.
 * This list is only ever drawn after the account key was opened, so the logins are never read before the fingerprint.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PasswordsAutofillChooser(
    logins: List<PasswordsAutofillLogin>,
    pageAddress: String?,
    isOneTimeCode: Boolean,
    onChoose: (PasswordsAutofillLogin) -> Unit,
    onCancel: () -> Unit,
    initialSearchText: String = "",
    initialQuestionItemId: String? = null,
) {
    var searchText by rememberSaveable { mutableStateOf(initialSearchText) }
    var questionItemId by rememberSaveable { mutableStateOf(initialQuestionItemId) }
    val levels = remember(logins, pageAddress) { PasswordsAutofillIndex.levels(logins, pageAddress) }
    val sections = remember(logins, levels, searchText, isOneTimeCode) { PasswordsAutofillIndex.sections(logins, levels, searchText, isOneTimeCode) }

    fun choose(login: PasswordsAutofillLogin) {
        val level = levels[login.itemId] ?: PasswordsSiteMatchLevel.None
        if (PasswordsAutofillIndex.isSiteMismatch(level, pageAddress)) questionItemId = login.itemId else onChoose(login)
    }

    Surface(modifier = Modifier.fillMaxSize()) {
        Scaffold(
            topBar = {
                TopAppBar(
                    title = { Text(stringResource(R.string.passwords_title)) },
                    navigationIcon = { IconButton(onClick = onCancel) { Icon(Icons.Filled.Close, contentDescription = stringResource(R.string.common_cancel)) } },
                )
            },
        ) { padding ->
            Column(modifier = Modifier.padding(padding).fillMaxSize()) {
                OutlinedTextField(
                    value = searchText,
                    onValueChange = { text -> searchText = text },
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
                    placeholder = { Text(stringResource(R.string.passwords_autofill_search)) },
                    leadingIcon = { Icon(Icons.Outlined.Search, contentDescription = null) },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                )
                if (sections.isEmpty()) {
                    when {
                        searchText.isNotBlank() -> EmptyState(Icons.Outlined.Search, stringResource(R.string.passwords_autofill_no_results))
                        isOneTimeCode -> EmptyState(
                            Icons.Outlined.Key, stringResource(R.string.passwords_autofill_codes_empty_title),
                            description = stringResource(R.string.passwords_autofill_codes_empty_description),
                        )
                        else -> EmptyState(
                            Icons.Outlined.Key, stringResource(R.string.passwords_autofill_empty_title),
                            description = stringResource(R.string.passwords_autofill_empty_description),
                        )
                    }
                } else {
                    LazyColumn(contentPadding = PaddingValues(bottom = 16.dp)) {
                        for (section in sections) {
                            if (section.hasHeading) {
                                item(key = "heading-" + section.kind.name) {
                                    TeaNodeSectionHeader(
                                        stringResource(
                                            when (section.kind) {
                                                PasswordsAutofillSectionKind.Suggested -> R.string.passwords_autofill_suggested
                                                PasswordsAutofillSectionKind.Related -> R.string.passwords_autofill_related
                                                PasswordsAutofillSectionKind.All -> R.string.passwords_autofill_all
                                            },
                                        ),
                                    )
                                }
                            }
                            items(section.logins, key = { login -> section.kind.name + ":" + login.itemId }) { login ->
                                TeaNodeRow(
                                    headline = login.displayName,
                                    supporting = login.userName.ifEmpty { null },
                                    overline = login.sharedByName?.let { name -> stringResource(R.string.passwords_shared_by, name) },
                                    onClick = { choose(login) },
                                )
                            }
                        }
                    }
                }
            }
        }
    }

    val question = logins.firstOrNull { login -> login.itemId == questionItemId }
    val requestedSite = PasswordsAutofillIndex.requestedSiteText(pageAddress)
    if (question != null && requestedSite != null) {
        val savedSites = PasswordsAutofillIndex.savedSitesText(question)
        ConfirmationDialog(
            title = stringResource(R.string.passwords_autofill_question_title, question.displayName, requestedSite),
            text = if (savedSites.isEmpty()) stringResource(R.string.passwords_autofill_saved_nowhere) else stringResource(R.string.passwords_autofill_saved_for, savedSites),
            confirmLabel = stringResource(R.string.passwords_autofill_fill),
            onConfirm = { questionItemId = null; onChoose(question) },
            onDismiss = { questionItemId = null },
        )
    }
}
