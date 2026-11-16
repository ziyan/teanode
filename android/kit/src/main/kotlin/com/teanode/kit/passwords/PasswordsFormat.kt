package com.teanode.kit.passwords

// What the Passwords screens say about opened items that is not a screen: which items a list shows, the line under a name,
// what a search matches, and how a card number, an expiry and a one-time code are written. The iPhone app's PasswordsWords.

/** Which items the list shows. */
sealed class PasswordsFilter {
    data object Everything : PasswordsFilter()

    data class Kind(val itemKind: PasswordsItemKind) : PasswordsFilter()

    data object Shared : PasswordsFilter()

    data object UsableByAgent : PasswordsFilter()

    data class Folder(val name: String) : PasswordsFilter()

    fun includes(item: PasswordItem): Boolean = when (this) {
        Everything -> true
        is Kind -> item.itemKind == itemKind
        Shared -> item.isSharedWithMe || item.recordedShares.isNotEmpty()
        UsableByAgent -> item.isUsableByAgent
        is Folder -> item.folder == name
    }
}

/** The folder the item is in; empty for none. */
val PasswordItem.folder: String
    get() = when (val opened = fields) {
        is PasswordsItemFields.Login -> opened.fields.folder
        is PasswordsItemFields.Card -> opened.fields.folder
        is PasswordsItemFields.Note -> opened.fields.folder
    }

/** The line under the name in a list: the user name of a login, the end of a card number, the first line of a note. */
val PasswordItem.subtitle: String
    get() = when (val opened = fields) {
        is PasswordsItemFields.Login -> opened.fields.userName
        is PasswordsItemFields.Card -> PasswordsFormat.maskedCardNumber(opened.fields.number)
        is PasswordsItemFields.Note -> opened.fields.text.lineSequence().firstOrNull { line -> line.isNotBlank() } ?: ""
    }

/** The addresses of a login. */
val PasswordItem.sites: List<String>
    get() = (fields as? PasswordsItemFields.Login)?.fields?.sites ?: emptyList()

/** The folders the items are in, by name. */
fun List<PasswordItem>.folders(): List<String> =
    map { item -> item.folder }.filter { folder -> folder.isNotEmpty() }.toSet().sortedWith(String.CASE_INSENSITIVE_ORDER)

/**
 * Whether the item matches words typed in the search field. A password, card number or security code is never searched.
 */
fun PasswordItem.matches(words: String): Boolean {
    val trimmed = words.trim()
    if (trimmed.isEmpty()) return true
    val haystacks = mutableListOf(name, folder, ownerName)
    when (val opened = fields) {
        is PasswordsItemFields.Login -> {
            haystacks.add(opened.fields.userName)
            haystacks.addAll(opened.fields.sites)
            haystacks.addAll(opened.fields.passkeys.map { passkey -> passkey.relyingPartyId })
        }
        is PasswordsItemFields.Card -> haystacks.addAll(listOf(opened.fields.cardholderName, opened.fields.brand))
        is PasswordsItemFields.Note -> Unit
    }
    return haystacks.any { haystack -> haystack.contains(trimmed, ignoreCase = true) }
}

/** PasswordsFormat writes values for the screen. */
object PasswordsFormat {
    /** "•••• 4242" for a card number: only the last four digits show. */
    fun maskedCardNumber(number: String): String {
        val digits = number.filter { character -> character.isDigit() }
        if (digits.length < 4) return if (digits.isEmpty()) "" else "••••"
        return "•••• " + digits.takeLast(4)
    }

    /** A card number in groups of four, as it is printed. */
    fun groupedCardNumber(number: String): String = number.filter { character -> character.isDigit() }.chunked(4).joinToString(" ")

    /** "09/29" for a card's month and year; empty when neither is known. */
    fun expiryText(month: String, year: String): String {
        val trimmedMonth = month.trim()
        val trimmedYear = year.trim()
        if (trimmedMonth.isEmpty() && trimmedYear.isEmpty()) return ""
        val paddedMonth = if (trimmedMonth.length == 1) "0$trimmedMonth" else trimmedMonth
        val shortYear = if (trimmedYear.length == 4) trimmedYear.takeLast(2) else trimmedYear
        return "$paddedMonth/$shortYear"
    }

    /** A one-time code in two halves: "123 456". */
    fun groupedOneTimeCode(code: String): String {
        val half = code.length / 2
        if (half == 0) return code
        return code.take(half) + " " + code.drop(half)
    }

    /** The recovery key in rows of four groups: the 13 groups it is written in do not fit a phone's width on one line. */
    fun recoveryKeyLines(recoveryKeyText: String): List<String> =
        recoveryKeyText.split(" ").filter { group -> group.isNotEmpty() }.chunked(4).map { row -> row.joinToString(" ") }

    /** The address to open for a login's site; a site written without a scheme is taken as https. Null when it is not a web address. */
    fun urlOfSite(site: String): String? {
        val trimmed = site.trim()
        if (trimmed.isEmpty()) return null
        val written = if (trimmed.contains("://")) trimmed else "https://$trimmed"
        val scheme = written.substringBefore("://").lowercase()
        if (scheme != "https" && scheme != "http") return null
        if (written.substringAfter("://").isEmpty()) return null
        return written
    }

    /** A site shown without its scheme: "mail.example.com". */
    fun shownSite(site: String): String {
        var text = site.trim()
        for (prefix in listOf("https://", "http://")) if (text.lowercase().startsWith(prefix)) text = text.drop(prefix.length)
        if (text.endsWith("/")) text = text.dropLast(1)
        return text
    }

    /** The lock times the settings offer. */
    val lockAfterChoices: List<Int> = listOf(1, 5, 15, 30, 60, 240, 1440, 10080)

    /** How long a copied secret stays on the clipboard. */
    const val CLIPBOARD_SECONDS_KEPT = 90L
}
