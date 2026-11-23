package com.teanode.app.contacts

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.provider.ContactsContract
import androidx.core.content.ContextCompat
import com.teanode.kit.contacts.ContactCardWriter
import com.teanode.kit.contacts.ContactIdentity
import com.teanode.kit.contacts.PhoneContactFields
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/** One contact of the phone, with the card it is sent as. */
class PhoneContact(
    val id: String,
    val name: String,
    val firstAddressText: String,
    val identity: ContactIdentity,
    val card: String?,
)

/** PhoneContacts reads the contacts on the phone, with the permission the person gave, as plain values. */
object PhoneContacts {
    fun hasAccess(context: Context): Boolean = ContextCompat.checkSelfPermission(context, Manifest.permission.READ_CONTACTS) == PackageManager.PERMISSION_GRANTED

    suspend fun readAll(context: Context): List<PhoneContact> = withContext(Dispatchers.IO) { readAllNow(context) }

    private class Gathered(val id: String) {
        var displayName = ""
        var givenName = ""
        var familyName = ""
        var organization = ""
        var note = ""
        val emails = mutableListOf<String>()
        val phones = mutableListOf<String>()
    }

    private fun readAllNow(context: Context): List<PhoneContact> {
        val gathered = LinkedHashMap<String, Gathered>()
        val projection = arrayOf(
            ContactsContract.Data.CONTACT_ID, ContactsContract.Data.MIMETYPE, ContactsContract.Data.DISPLAY_NAME_PRIMARY,
            ContactsContract.Data.DATA1, ContactsContract.Data.DATA2, ContactsContract.Data.DATA3,
        )
        context.contentResolver.query(ContactsContract.Data.CONTENT_URI, projection, null, null, "${ContactsContract.Data.DISPLAY_NAME_PRIMARY} COLLATE LOCALIZED ASC")?.use { cursor ->
            while (cursor.moveToNext()) {
                val contactId = cursor.getString(0) ?: continue
                val entry = gathered.getOrPut(contactId) { Gathered(contactId) }
                entry.displayName = cursor.getString(2).orEmpty()
                val first = cursor.getString(3).orEmpty()
                when (cursor.getString(1)) {
                    ContactsContract.CommonDataKinds.StructuredName.CONTENT_ITEM_TYPE -> {
                        entry.givenName = cursor.getString(4).orEmpty()
                        entry.familyName = cursor.getString(5).orEmpty()
                    }
                    ContactsContract.CommonDataKinds.Email.CONTENT_ITEM_TYPE -> if (first.isNotEmpty()) entry.emails.add(first)
                    ContactsContract.CommonDataKinds.Phone.CONTENT_ITEM_TYPE -> if (first.isNotEmpty()) entry.phones.add(first)
                    ContactsContract.CommonDataKinds.Organization.CONTENT_ITEM_TYPE -> if (first.isNotEmpty()) entry.organization = first
                    ContactsContract.CommonDataKinds.Note.CONTENT_ITEM_TYPE -> if (first.isNotEmpty()) entry.note = first
                }
            }
        }
        return gathered.values.map { contact ->
            val name = contact.displayName.ifEmpty { contact.organization }
            PhoneContact(
                contact.id, name, contact.emails.firstOrNull() ?: contact.phones.firstOrNull() ?: "", ContactIdentity(name, contact.emails, contact.phones),
                ContactCardWriter.card(PhoneContactFields(contact.id, contact.givenName, contact.familyName, name, contact.organization, contact.emails, contact.phones, contact.note)),
            )
        }
    }
}
