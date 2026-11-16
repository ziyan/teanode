package com.teanode.kit.sharing

/** The GraphQL documents of Sharing. Copied from the iPhone app's Documents (ios/TeaNodeKit), text unchanged. */
object SharingDocuments {
    val SHARING = """
query {
  ListSharing {
    sharesMade { shareKind shareId subjectId subjectName subjectMask isSubjectDeleted shareLevel shareScopes passwordItemCount granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName createdAt }
    sharesReceived { shareKind shareId subjectId subjectName subjectMask shareLevel shareScopes passwordItemCount ownerUserId ownerUsername ownerName granteeCircleId granteeCircleName isDirect canDecline createdAt }
  }
}
""".trimEnd()

    val DECLINE_SHARE = """
mutation (${'$'}shareKind: String!, ${'$'}shareId: String!) { DeclineShare(shareKind: ${'$'}shareKind, shareId: ${'$'}shareId) }
""".trimEnd()

}
