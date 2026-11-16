package com.teanode.kit.contacts


/** The GraphQL documents, the same text as the iPhone app's (ios/TeaNodeKit): the server's tests check that text against its schema. */
object ContactDocuments {
    private const val D = "$"

    const val ADDRESS_BOOKS = """query { ListAddressBooks { id name contacts } }"""

    const val REACHABLE_ADDRESS_BOOKS = """query { ListReachableAddressBooks { id name description contacts circleId circleName isCircleBook canManage } }"""

    const val CREATE_CIRCLE_ADDRESS_BOOK = """mutation (${D}circleId: String!, ${D}name: String) {
  CreateCircleAddressBook(circleId: ${D}circleId, name: ${D}name) { id name description contacts circleId circleName isCircleBook canManage }
}"""

    const val SAVE_ADDRESS_BOOK = """mutation (${D}id: String!, ${D}name: String, ${D}description: String) {
  SaveAddressBook(id: ${D}id, name: ${D}name, description: ${D}description) { id name description contacts circleId circleName isCircleBook canManage }
}"""

    const val DELETE_CIRCLE_ADDRESS_BOOK = """mutation (${D}addressBookId: String!) { DeleteCircleAddressBook(addressBookId: ${D}addressBookId) }"""

    const val CONTACTS = """query (${D}addressBookId: String!, ${D}query: String, ${D}first: Int) {
  ListContacts(addressBookId: ${D}addressBookId, query: ${D}query, first: ${D}first) {
    id name organization emails phones hasPhoto addresses { written } accountUserId
  }
}"""

    const val CONTACT = """query (${D}id: String!, ${D}addressBookId: String) {
  GetContact(id: ${D}id, addressBookId: ${D}addressBookId) {
    id name organization emails phones note hasPhoto
    addresses { street locality region postalCode country }
    accountUserId account { userId username name avatarUrl avatarColorIndex }
  }
}"""

    const val SAVE_CONTACT = """mutation (${D}addressBookId: String!, ${D}id: String, ${D}name: String, ${D}organization: String,
          ${D}emails: [String!], ${D}phones: [String!], ${D}note: String, ${D}addresses: [AddressInput!]) {
  SaveContact(addressBookId: ${D}addressBookId, id: ${D}id, name: ${D}name, organization: ${D}organization,
              emails: ${D}emails, phones: ${D}phones, note: ${D}note, addresses: ${D}addresses) { id name }
}"""

    const val DELETE_CONTACT = """mutation (${D}id: String!, ${D}addressBookId: String) { DeleteContact(id: ${D}id, addressBookId: ${D}addressBookId) }"""

    const val REMOVE_CONTACT_PICTURE = """mutation (${D}id: String!, ${D}addressBookId: String) {
  RemoveContactPicture(id: ${D}id, addressBookId: ${D}addressBookId) { id hasPhoto }
}"""

    const val MY_CONTACT = """query { AgentGraphPage(path: "self") { contact { id } } }"""

    const val SET_MY_CONTACT = """mutation (${D}contactId: String) { SetMyContact(contactId: ${D}contactId) }"""

    const val SET_CONTACT_ACCOUNT = """mutation (${D}addressBookId: String!, ${D}contactId: String!, ${D}accountUserId: String) {
  SetContactAccount(addressBookId: ${D}addressBookId, contactId: ${D}contactId, accountUserId: ${D}accountUserId) {
    id accountUserId account { userId username name avatarUrl avatarColorIndex }
  }
}"""

    const val CONTACT_ACCOUNT_SUGGESTIONS = """query {
  ListContactAccountSuggestions {
    addressBookId
    contact { id name organization emails phones hasPhoto }
    account { userId username name avatarUrl avatarColorIndex }
  }
}"""

}