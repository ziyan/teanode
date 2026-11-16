package com.teanode.kit.account

/** The GraphQL documents of the account profile, phones and passkeys. Copied from the iPhone app's Documents (ios/TeaNodeKit), text unchanged. */
object AccountProfileDocuments {
    val ACCOUNT_PROFILE_FIELDS = """
id username name email avatarUrl avatarColorIndex timezone timezoneMode pronoun measurementSystem
""".trimEnd()

    val ACCOUNT_PROFILE = """
query { GetCurrentUser { ${ACCOUNT_PROFILE_FIELDS} } }
""".trimEnd()

    val SET_ACCOUNT_AVATAR = """
mutation (${'$'}content: String!) { SetUserAvatar(content: ${'$'}content) { ${ACCOUNT_PROFILE_FIELDS} } }
""".trimEnd()

    val REMOVE_ACCOUNT_AVATAR = """
mutation { RemoveUserAvatar { ${ACCOUNT_PROFILE_FIELDS} } }
""".trimEnd()

    val UPDATE_ACCOUNT_PROFILE = """
mutation (${'$'}userId: String!, ${'$'}username: String, ${'$'}name: String, ${'$'}email: String, ${'$'}timezone: String, ${'$'}timezoneMode: String, ${'$'}pronoun: String, ${'$'}measurementSystem: String) {
  UpdateUser(
    userId: ${'$'}userId, username: ${'$'}username, name: ${'$'}name, email: ${'$'}email, timezone: ${'$'}timezone, timezoneMode: ${'$'}timezoneMode,
    pronoun: ${'$'}pronoun, measurementSystem: ${'$'}measurementSystem
  ) {
    ${ACCOUNT_PROFILE_FIELDS}
  }
}
""".trimEnd()

    val UPDATE_ACCOUNT_PRONOUN = """
mutation (${'$'}userId: String!, ${'$'}pronoun: String) {
  UpdateUser(userId: ${'$'}userId, pronoun: ${'$'}pronoun) { ${ACCOUNT_PROFILE_FIELDS} }
}
""".trimEnd()

    val CHANGE_PASSWORD = """
mutation (${'$'}currentPassword: String!, ${'$'}newPassword: String!) {
  ChangePassword(currentPassword: ${'$'}currentPassword, newPassword: ${'$'}newPassword) { authenticated username }
}
""".trimEnd()

    val PAIRED_PHONES = """
query {
  ListAgentPhones {
    phones { id phoneName phoneModel phoneModelName notificationKinds canBeNotified lastSeenAt lastDeliveredAt lastDeliveryError }
    isPushEnabled notificationKinds
  }
}
""".trimEnd()

    val RENAME_PHONE = """
mutation (${'$'}phoneId: String!, ${'$'}phoneName: String) { UpdateAgentPhone(phoneId: ${'$'}phoneId, phoneName: ${'$'}phoneName) { id } }
""".trimEnd()

    val TEST_PHONE = """
mutation (${'$'}phoneId: String!) { TestAgentPhone(phoneId: ${'$'}phoneId) { id } }
""".trimEnd()

    val REMOVE_PHONE = """
mutation (${'$'}phoneId: String!) { RemoveAgentPhone(phoneId: ${'$'}phoneId) }
""".trimEnd()

    val PASSKEYS = """
query {
  GetPasskeyPolicy { enabled maximumPerUser }
  ListPasskeys { id name createdAt usedAt ip backupEligible backupState transports }
}
""".trimEnd()

    val DELETE_PASSKEY = """
mutation (${'$'}passkeyId: String!) { DeletePasskey(passkeyId: ${'$'}passkeyId) }
""".trimEnd()

}
