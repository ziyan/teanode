package com.teanode.kit.skillsharing

/** The GraphQL documents of lending a skill. Copied from the iPhone app's Documents (ios/TeaNodeKit), text unchanged. */
object SkillSharingDocuments {
    val SKILL_SHARE_FIELDS = """
id skillName toolNames ownerUserId ownerUsername ownerName ownerAvatarUrl ownerAvatarColorIndex
granteeUserId granteeUsername granteeName granteeAvatarUrl granteeAvatarColorIndex
paymentLimitMinor monthlyLimitMinor currencyCode isApprovalRequired isOwnedByCaller waitingCallCount monthTotalMinor
""".trimEnd()

    val SKILL_SHARE_CALL_FIELDS = """
id skillShareId skillName granteeUserId granteeUsername granteeName granteeAvatarUrl granteeAvatarColorIndex ownerName
toolName toolArguments requestReason amountMinor currencyCode amountArgumentName callState toolOutput answeredAt answerNote expiresAt createdAt
""".trimEnd()

    val SKILL_SHARES = """
query { ListSkillShares { ${SKILL_SHARE_FIELDS} } }
""".trimEnd()

    val LENDABLE_SKILLS = """
query { ListAgentSkills { name description enabled readable tools { name description needsComputer isAmountDeclared isScoped } } }
""".trimEnd()

    val SHARE_SKILL = """
mutation (${'$'}skillName: String!, ${'$'}toolNames: [String!]!, ${'$'}granteeUserId: String!, ${'$'}paymentLimitMinor: Int64, ${'$'}monthlyLimitMinor: Int64, ${'$'}currencyCode: String, ${'$'}isApprovalRequired: Boolean) {
  ShareSkill(skillName: ${'$'}skillName, toolNames: ${'$'}toolNames, granteeUserId: ${'$'}granteeUserId, paymentLimitMinor: ${'$'}paymentLimitMinor, monthlyLimitMinor: ${'$'}monthlyLimitMinor, currencyCode: ${'$'}currencyCode, isApprovalRequired: ${'$'}isApprovalRequired) { id }
}
""".trimEnd()

    val UNSHARE_SKILL = """
mutation (${'$'}shareId: String!) { UnshareSkill(shareId: ${'$'}shareId) }
""".trimEnd()

    val SKILL_SHARE_CALLS = """
query (${'$'}shareId: String!, ${'$'}callState: String) {
  ListSkillShareCalls(shareId: ${'$'}shareId, callState: ${'$'}callState) { ${SKILL_SHARE_CALL_FIELDS} }
}
""".trimEnd()

    val ANSWER_SKILL_SHARE_CALL = """
mutation (${'$'}callId: String!, ${'$'}isApproved: Boolean!, ${'$'}answerNote: String) {
  AnswerSkillShareCall(callId: ${'$'}callId, isApproved: ${'$'}isApproved, answerNote: ${'$'}answerNote) { ${SKILL_SHARE_CALL_FIELDS} }
}
""".trimEnd()

}
