package com.teanode.kit.passwords

// What the autofill list shows, as the iPhone's AutoFillIndex works it out: the logins for the page asking (the exact
// origin), those for a related host under their own heading, then the rest, or with words typed in the search field the
// logins that contain them across all sites. A login that is not saved for the exact page is filled only after the person
// says yes (`isSiteMismatch`). An app with no web page names no site, so nothing is suggested and nothing is asked.

/** A login as the list knows it: no password, notes or keys beyond whether it has a password and a one-time code. */
data class PasswordsAutofillLogin(
    val itemId: String,
    val displayName: String,
    val userName: String,
    val sites: List<String>,
    val hasPassword: Boolean,
    val hasOneTimeCode: Boolean,
    /** Who shared the login with the person; null for the person's own. */
    val sharedByName: String?,
) {
    /** Whether the words typed in the search field are in the name, the user name or one of the sites. */
    fun matches(searchText: String): Boolean =
        displayName.contains(searchText, ignoreCase = true) || userName.contains(searchText, ignoreCase = true) ||
            sites.any { site -> site.contains(searchText, ignoreCase = true) }
}

enum class PasswordsAutofillSectionKind { Suggested, Related, All }

/** A group of rows. [hasHeading] is false for a list with nothing above it, which needs none. */
data class PasswordsAutofillSection(val kind: PasswordsAutofillSectionKind, val hasHeading: Boolean, val logins: List<PasswordsAutofillLogin>)

object PasswordsAutofillIndex {
    /** The logins among opened items, by name: a deleted item, a note or a card is not one. */
    fun logins(items: List<PasswordItem>): List<PasswordsAutofillLogin> =
        items.filter { item -> item.deletedAtEpochMS == null && item.fields is PasswordsItemFields.Login }
            .sortedWith(compareBy(String.CASE_INSENSITIVE_ORDER) { item -> item.name })
            .map { item ->
                val login = (item.fields as PasswordsItemFields.Login).fields
                PasswordsAutofillLogin(
                    itemId = item.id, displayName = item.name, userName = login.userName, sites = login.sites,
                    hasPassword = login.password.isNotEmpty(), hasOneTimeCode = login.oneTimeCodeUri.isNotEmpty(),
                    sharedByName = if (item.isSharedWithMe && item.ownerName.isNotEmpty()) item.ownerName else null,
                )
            }

    /** How well each login fits the page asking, by item id. Empty when the page names no usable site (an app). */
    fun levels(logins: List<PasswordsAutofillLogin>, pageAddress: String?): Map<String, PasswordsSiteMatchLevel> {
        if (pageAddress == null || PasswordsSiteMatch.normalizedHost(pageAddress) == null) return emptyMap()
        return logins.associate { login -> login.itemId to PasswordsSiteMatch.level(login.sites, pageAddress, isPageAddress = true) }
    }

    fun sections(
        logins: List<PasswordsAutofillLogin>,
        levels: Map<String, PasswordsSiteMatchLevel>,
        searchText: String,
        isOneTimeCode: Boolean,
    ): List<PasswordsAutofillSection> {
        val words = searchText.trim()
        val isSearching = words.isNotEmpty()
        val candidates = if (isSearching) logins.filter { login -> login.matches(words) } else logins
        val fillable = candidates.filter { login -> if (isOneTimeCode) login.hasOneTimeCode else login.hasPassword }
        val suggested = mutableListOf<PasswordsAutofillLogin>()
        val related = mutableListOf<PasswordsAutofillLogin>()
        if (!isSearching) {
            for (login in fillable) {
                when (levels[login.itemId] ?: PasswordsSiteMatchLevel.None) {
                    PasswordsSiteMatchLevel.ExactHost -> suggested.add(login)
                    PasswordsSiteMatchLevel.RelatedHost -> related.add(login)
                    PasswordsSiteMatchLevel.None -> Unit
                }
            }
        }
        val sections = mutableListOf<PasswordsAutofillSection>()
        if (suggested.isNotEmpty()) sections.add(PasswordsAutofillSection(PasswordsAutofillSectionKind.Suggested, true, suggested))
        if (related.isNotEmpty()) sections.add(PasswordsAutofillSection(PasswordsAutofillSectionKind.Related, true, related))
        val shownIds = (suggested + related).map { login -> login.itemId }.toSet()
        val rest = fillable.filter { login -> login.itemId !in shownIds }
        if (rest.isNotEmpty()) sections.add(PasswordsAutofillSection(PasswordsAutofillSectionKind.All, shownIds.isNotEmpty(), rest))
        return sections
    }

    /** The host of the page asking; null when it names no site (an app is not a site). */
    fun requestedSiteText(pageAddress: String?): String? = pageAddress?.let { address -> PasswordsSiteMatch.normalizedHost(address) }

    /**
     * Whether the login is not saved for the exact page asking, so it is filled only after the person says yes. A login for
     * a related host is asked about like any other. Never when no site is named, so there is nothing to compare.
     */
    fun isSiteMismatch(level: PasswordsSiteMatchLevel, pageAddress: String?): Boolean =
        requestedSiteText(pageAddress) != null && level != PasswordsSiteMatchLevel.ExactHost

    /** The hosts a login is saved for, for the question asked before filling it elsewhere. */
    fun savedSitesText(login: PasswordsAutofillLogin): String =
        login.sites.mapNotNull { site -> PasswordsSiteMatch.normalizedHost(site) }.joinToString(", ")
}
