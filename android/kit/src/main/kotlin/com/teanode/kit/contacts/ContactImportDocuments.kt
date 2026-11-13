package com.teanode.kit.contacts


/** The GraphQL documents, the same text as the iPhone app's (ios/TeaNodeKit): the server's tests check that text against its schema. */
object ContactImportDocuments {
    private const val D = "$"

    const val IMPORT_CONTACT = """mutation (${D}addressBookId: String!, ${D}card: String!) {
  SaveContact(addressBookId: ${D}addressBookId, card: ${D}card) { id name }
}"""

}