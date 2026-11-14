package com.teanode.kit.agent

/** The GraphQL documents of the agent's billing. Copied from the iPhone app's Documents (ios/TeaNodeKit), text unchanged. */
object AgentBillingDocuments {
    val AGENT_BILLING_CHECK_FIELDS = """
providerName checkedAt errorMessage
compared { model tokenKind configuredPerMillion billedPerMillion dayCount isPattern }
changed { model tokenKind beforePerMillion afterPerMillion }
""".trimEnd()

    val AGENT_BILLING_CHECKS = """
query { AgentBillingChecks { ${AGENT_BILLING_CHECK_FIELDS} } }
""".trimEnd()

    val CHECK_AGENT_PROVIDER_PRICES = """
mutation { CheckAgentProviderPrices { ${AGENT_BILLING_CHECK_FIELDS} } }
""".trimEnd()

}
