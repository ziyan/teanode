package com.teanode.kit.passwords

import java.net.URLEncoder

/** The one-time code field of an editor does not hold an `otpauth://` address or a secret key. */
class PasswordsDraftException : Exception("That is not a one-time code.")

/**
 * PasswordsDraft is what an editor holds while the person types: one text for every field of any kind of item, turned into
 * fields when they save. Keeping every kind in one draft lets the editor rebase the person's changes onto a fresher copy of
 * the item without caring which kind it is.
 */
data class PasswordsDraft(
    val name: String = "",
    val folder: String = "",
    val notes: String = "",
    val userName: String = "",
    val password: String = "",
    val sitesText: String = "",
    val oneTimeCodeText: String = "",
    val passkeys: List<PasswordsPasskeyFields> = emptyList(),
    val cardholderName: String = "",
    val number: String = "",
    val expiryMonth: String = "",
    val expiryYear: String = "",
    val securityCode: String = "",
    val brand: String = "",
    val text: String = "",
) {
    /** The fields of the given kind the draft says, trimmed as the iPhone's editor trims them. */
    fun fields(itemKind: PasswordsItemKind): PasswordsItemFields {
        val trimmedName = name.trim()
        val trimmedFolder = folder.trim()
        return when (itemKind) {
            PasswordsItemKind.Login -> PasswordsItemFields.Login(
                PasswordsLoginFields(
                    name = trimmedName, userName = userName.trim(), password = password,
                    sites = sitesText.lines().map { line -> line.trim() }.filter { line -> line.isNotEmpty() },
                    oneTimeCodeUri = oneTimeCodeUri(oneTimeCodeText, trimmedName), notes = notes, passkeys = passkeys, folder = trimmedFolder,
                ),
            )
            PasswordsItemKind.Card -> PasswordsItemFields.Card(
                PasswordsCardFields(
                    name = trimmedName, cardholderName = cardholderName.trim(), number = number.filter { character -> character.isDigit() || character == ' ' }.trim(),
                    expiryMonth = expiryMonth.trim(), expiryYear = expiryYear.trim(), securityCode = securityCode.trim(), brand = brand.trim(),
                    notes = notes, folder = trimmedFolder,
                ),
            )
            PasswordsItemKind.Note -> PasswordsItemFields.Note(PasswordsNoteFields(name = trimmedName, text = text, folder = trimmedFolder))
        }
    }

    /**
     * This draft's edits put onto a fresher copy of the item: the fields the person changed from what they loaded
     * (`original`) keep their edit, every other field comes from `fresh`, so another device's change to a field the person
     * did not touch is not lost.
     */
    fun rebased(original: PasswordsDraft, fresh: PasswordsDraft): PasswordsDraft {
        var result = fresh
        if (name != original.name) result = result.copy(name = name)
        if (folder != original.folder) result = result.copy(folder = folder)
        if (notes != original.notes) result = result.copy(notes = notes)
        if (userName != original.userName) result = result.copy(userName = userName)
        if (password != original.password) result = result.copy(password = password)
        if (sitesText != original.sitesText) result = result.copy(sitesText = sitesText)
        if (oneTimeCodeText != original.oneTimeCodeText) result = result.copy(oneTimeCodeText = oneTimeCodeText)
        if (cardholderName != original.cardholderName) result = result.copy(cardholderName = cardholderName)
        if (number != original.number) result = result.copy(number = number)
        if (expiryMonth != original.expiryMonth) result = result.copy(expiryMonth = expiryMonth)
        if (expiryYear != original.expiryYear) result = result.copy(expiryYear = expiryYear)
        if (securityCode != original.securityCode) result = result.copy(securityCode = securityCode)
        if (brand != original.brand) result = result.copy(brand = brand)
        if (text != original.text) result = result.copy(text = text)
        return result
    }

    companion object {
        /** The draft of an item, or an empty one for a new item. */
        fun of(item: PasswordItem?): PasswordsDraft {
            if (item == null) return PasswordsDraft()
            return when (val opened = item.fields) {
                is PasswordsItemFields.Login -> PasswordsDraft(
                    name = opened.fields.name, folder = opened.fields.folder, notes = opened.fields.notes, userName = opened.fields.userName,
                    password = opened.fields.password, sitesText = opened.fields.sites.joinToString("\n"), oneTimeCodeText = opened.fields.oneTimeCodeUri,
                    passkeys = opened.fields.passkeys,
                )
                is PasswordsItemFields.Card -> PasswordsDraft(
                    name = opened.fields.name, folder = opened.fields.folder, notes = opened.fields.notes, cardholderName = opened.fields.cardholderName,
                    number = opened.fields.number, expiryMonth = opened.fields.expiryMonth, expiryYear = opened.fields.expiryYear,
                    securityCode = opened.fields.securityCode, brand = opened.fields.brand,
                )
                is PasswordsItemFields.Note -> PasswordsDraft(name = opened.fields.name, folder = opened.fields.folder, text = opened.fields.text)
            }
        }

        /**
         * The `otpauth://` address a typed one-time code field means: an address as it is, or a bare secret key made into
         * one. Empty for no code; [PasswordsDraftException] for anything that does not read as a code.
         */
        fun oneTimeCodeUri(typedText: String, name: String): String {
            val trimmed = typedText.trim()
            if (trimmed.isEmpty()) return ""
            if (trimmed.lowercase().startsWith("otpauth://")) {
                if (runCatching { PasswordsOneTimeCode.parse(trimmed) }.isFailure) throw PasswordsDraftException()
                return trimmed
            }
            val secret = trimmed.filter { character -> !character.isWhitespace() && character != '-' }.uppercase()
            val label = URLEncoder.encode(name, "UTF-8").replace("+", "%20")
            val uri = "otpauth://totp/$label?secret=$secret"
            if (runCatching { PasswordsOneTimeCode.parse(uri) }.isFailure) throw PasswordsDraftException()
            return uri
        }
    }
}
