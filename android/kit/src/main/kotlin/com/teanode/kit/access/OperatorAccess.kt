package com.teanode.kit.access

import com.teanode.kit.Connection
import kotlinx.serialization.Serializable

/**
 * What the signed-in person may open on the management side, as the dashboard reads it from the session: whether they
 * hold any permission that opens it at all, and which of its pages they may open.
 */
data class OperatorAccess(
    /** Whether any permission that opens the management side is held: the session's `manages`. */
    val isOperator: Boolean,
    /** Server permissions, and domain permissions held over every domain. */
    val everywherePermissions: List<String> = emptyList(),
    val domainPermissions: List<DomainPermissions> = emptyList(),
) {
    /** The permissions held over one domain. */
    @Serializable
    data class DomainPermissions(val domainId: String, val permissions: List<String> = emptyList())

    /** Whether a domain permission is held over at least one domain, or everywhere through the all-domains permission that widens it. */
    fun hasAnywhere(permission: String): Boolean {
        val isHeldEverywhere = everywherePermissions.any { held -> held == permission || (held.endsWith("-all") && held.removeSuffix("-all") == permission) }
        return isHeldEverywhere || domainPermissions.any { domain -> domain.permissions.contains(permission) }
    }

    val canAuditMail: Boolean get() = hasAnywhere("mail:audit")
    val canManageQueue: Boolean get() = hasAnywhere("queue:manage")
    val canReadReports: Boolean get() = hasAnywhere("report:read")
    val canManageDomains: Boolean get() = hasAnywhere("domain:manage")

    /** Whether any of the mail delivery pages may be opened. */
    val canOpenMailDelivery: Boolean get() = canAuditMail || canManageQueue || canReadReports || canManageDomains

    /** Whether a server permission is held, the way the dashboard's hasPermission reads it: everywhere, never through a domain. */
    fun holdsEverywhere(permission: String): Boolean = everywherePermissions.contains(permission)

    /** The server's pages, its settings and the operator's agent settings need server:manage. */
    val canManageServer: Boolean get() = holdsEverywhere("server:manage")

    /** Everybody's agents, their limits and what they cost need agent:audit. */
    val canAuditAgents: Boolean get() = holdsEverywhere("agent:audit")

    /** The runs are content, and are behind agent:act. */
    val canActAsAgents: Boolean get() = holdsEverywhere("agent:act")

    /** Whether the agents page is offered at all. */
    val canOpenAgentAdmin: Boolean get() = canAuditAgents || canActAsAgents

    /** Whether the Manage page lists anything the person may open. */
    val canOpenManage: Boolean get() = isOperator || canOpenMailDelivery || canManageServer || canOpenAgentAdmin
}

private const val MANAGE_OPERATOR_ACCESS = """query { GetSession { manages permissions { everywhere byDomain { domainId permissions } } } }"""

@Serializable private class OperatorPermissions(val everywhere: List<String>? = null, val byDomain: List<OperatorAccess.DomainPermissions>? = null)

@Serializable private class OperatorSession(val manages: Boolean? = null, val permissions: OperatorPermissions? = null)

@Serializable private class OperatorAccessAnswer(val GetSession: OperatorSession)

/** What the signed-in person may open on the management side: the session's `manages`, and the permissions behind it. */
suspend fun Connection.operatorAccess(): OperatorAccess {
    val session = execute<OperatorAccessAnswer>(MANAGE_OPERATOR_ACCESS).GetSession
    return OperatorAccess(session.manages ?: false, session.permissions?.everywhere ?: emptyList(), session.permissions?.byDomain ?: emptyList())
}
