package com.teanode.kit.circles

/** The GraphQL documents of Circles. Copied from the iPhone app's Documents (ios/TeaNodeKit), text unchanged. */
object CircleDocuments {
    val CIRCLE_INVITATION_FIELDS = """
id circleId circleName circleKind invitedByName invitedUsername memberRole createdAt expiresAt
""".trimEnd()

    val CIRCLE_FIELDS = """
id name circleKind createdAt memberRole
members { userId username name avatarUrl avatarColorIndex memberRole joinedAt relationshipKind relationshipTermGender relationshipWord linkedContactId }
pendingInvitations { id circleId circleName circleKind invitedByName invitedUsername memberRole createdAt expiresAt }
""".trimEnd()

    val CIRCLES = """
query { ListCircles { ${CIRCLE_FIELDS} } }
""".trimEnd()

    val CIRCLE = """
query (${'$'}circleId: String!) { ReadCircle(circleId: ${'$'}circleId) { ${CIRCLE_FIELDS} } }
""".trimEnd()

    val CIRCLE_INVITATIONS = """
query { ListCircleInvitations { ${CIRCLE_INVITATION_FIELDS} } }
""".trimEnd()

    val CREATE_CIRCLE = """
mutation (${'$'}name: String!, ${'$'}circleKind: String!) {
  CreateCircle(name: ${'$'}name, circleKind: ${'$'}circleKind) { id }
}
""".trimEnd()

    val RENAME_CIRCLE = """
mutation (${'$'}circleId: String!, ${'$'}name: String!) { RenameCircle(circleId: ${'$'}circleId, name: ${'$'}name) { id } }
""".trimEnd()

    val DELETE_CIRCLE = """
mutation (${'$'}circleId: String!) { DeleteCircle(circleId: ${'$'}circleId) }
""".trimEnd()

    val INVITE_TO_CIRCLE = """
mutation (${'$'}circleId: String!, ${'$'}username: String!, ${'$'}memberRole: String!) {
  InviteToCircle(circleId: ${'$'}circleId, username: ${'$'}username, memberRole: ${'$'}memberRole)
}
""".trimEnd()

    val ADD_CIRCLE_MEMBER = """
mutation (${'$'}circleId: String!, ${'$'}username: String!, ${'$'}memberRole: String!) {
  AddCircleMember(circleId: ${'$'}circleId, username: ${'$'}username, memberRole: ${'$'}memberRole) { id }
}
""".trimEnd()

    val ANSWER_CIRCLE_INVITATION = """
mutation (${'$'}invitationId: String!, ${'$'}isAccepted: Boolean!) {
  AnswerCircleInvitation(invitationId: ${'$'}invitationId, isAccepted: ${'$'}isAccepted) { id }
}
""".trimEnd()

    val WITHDRAW_CIRCLE_INVITATION = """
mutation (${'$'}invitationId: String!) { WithdrawCircleInvitation(invitationId: ${'$'}invitationId) }
""".trimEnd()

    val SET_CIRCLE_MEMBER_ROLE = """
mutation (${'$'}circleId: String!, ${'$'}userId: String!, ${'$'}memberRole: String!) {
  SetCircleMemberRole(circleId: ${'$'}circleId, userId: ${'$'}userId, memberRole: ${'$'}memberRole) { id }
}
""".trimEnd()

    val REMOVE_CIRCLE_MEMBER = """
mutation (${'$'}circleId: String!, ${'$'}userId: String!) { RemoveCircleMember(circleId: ${'$'}circleId, userId: ${'$'}userId) { id } }
""".trimEnd()

    val LEAVE_CIRCLE = """
mutation (${'$'}circleId: String!) { LeaveCircle(circleId: ${'$'}circleId) }
""".trimEnd()

    val RELATIONSHIP_TERMS = """
query { ListRelationshipTerms { term relationshipKind termGender word } }
""".trimEnd()

    val SET_PERSON_RELATIONSHIP = """
mutation (${'$'}userId: String!, ${'$'}relationshipTerm: String!) {
  SetPersonRelationship(userId: ${'$'}userId, relationshipTerm: ${'$'}relationshipTerm) {
    userId username name avatarUrl avatarColorIndex memberRole joinedAt relationshipKind relationshipTermGender relationshipWord linkedContactId
  }
}
""".trimEnd()

    val REMOVE_PERSON_RELATIONSHIP = """
mutation (${'$'}userId: String!) { RemovePersonRelationship(userId: ${'$'}userId) }
""".trimEnd()

}
