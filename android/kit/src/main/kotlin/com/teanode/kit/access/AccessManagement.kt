package com.teanode.kit.access

import com.teanode.kit.AccessSession
import com.teanode.kit.Connection
import com.teanode.kit.sortedByKeys
import com.teanode.kit.settings.trimmedText
import com.teanode.kit.settings.variablesOf
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull

val AccessSession.canManageUsers: Boolean get() = holds("user:manage")
val AccessSession.canManageGroups: Boolean get() = holds("group:manage")
val AccessSession.canManageRoles: Boolean get() = holds("role:manage")
val AccessSession.canReadAudit: Boolean get() = holds("audit:read")

/** Somebody with an account on the server, as the people page lists them. */
@Serializable
data class AccessUser(
    val id: String,
    /** What they sign in with. */
    val username: String,
    /** What to call them, when they have said. */
    val name: String? = null,
    /** The address that receives notifications. */
    val email: String? = null,
    /** Where their picture is, a path to read with the app's token; null when they have none. */
    val avatarUrl: String? = null,
    /** Which of the twelve colors their initial is drawn on without a picture. */
    val avatarColorIndex: Int? = null,
    /** When they were disabled, or null while they may sign in. */
    val disabledAt: String? = null,
    /** Whether they have a password at all; one without signs in with a passkey or through an identity provider. */
    val hasPassword: Boolean = true,
    val groupIds: List<String> = emptyList(),
    val createdAt: String = "",
) {
    val isDisabled: Boolean get() = !disabledAt.isNullOrEmpty()

    /** What a list of people calls them: their name, or the username when they have none. */
    val displayName: String get() = name?.takeIf { text -> text.isNotEmpty() } ?: username

    /** Whether a search for these words finds them, by username, name or address, ignoring case. */
    fun matches(searchText: String): Boolean {
        val wanted = trimmedText(searchText).lowercase()
        if (wanted.isEmpty()) return true
        return listOf(username, name ?: "", email ?: "").any { text -> text.lowercase().contains(wanted) }
    }
}

/** A person as the new person and edit forms hold them. */
data class AccessUserDraft(
    val username: String = "",
    /** Only for a new person; empty leaves them without one. */
    val password: String = "",
    val name: String = "",
    val email: String = "",
) {
    /** Whether the form can be sent: a username is the one thing needed. */
    val canSubmit: Boolean get() = trimmedText(username).isNotEmpty()

    /** As CreateUser takes it. No groups are named, so the server puts the person in its default group when it still has one. */
    val createVariables: Map<String, JsonElement>
        get() = variablesOf("username" to trimmedText(username), "password" to password.ifEmpty { null }, "name" to name, "email" to email)

    /** As UpdateUser takes it; an empty name or address clears it. */
    fun updateVariables(userId: String): Map<String, JsonElement> =
        variablesOf("userId" to userId, "username" to trimmedText(username), "name" to name, "email" to email)

    companion object {
        fun of(user: AccessUser): AccessUserDraft = AccessUserDraft(username = user.username, name = user.name ?: "", email = user.email ?: "")
    }
}

/** A group of people: who is in it, the roles it carries, and the domains those roles' domain permissions apply to. */
@Serializable
data class AccessGroup(
    val id: String,
    val name: String,
    val description: String? = null,
    /** The group's name at the identity provider, which signing in through it keeps the members in step with. */
    val idpGroup: String? = null,
    val userIds: List<String> = emptyList(),
    val roleIds: List<String> = emptyList(),
    val domainIds: List<String> = emptyList(),
)

/** A group's name, description and identity provider group as the group form holds them. */
data class AccessGroupDraft(val name: String = "", val description: String = "", val idpGroup: String = "") {
    val canSubmit: Boolean get() = trimmedText(name).isNotEmpty()

    val variables: Map<String, JsonElement>
        get() = variablesOf("name" to trimmedText(name), "description" to description, "idpGroup" to trimmedText(idpGroup))

    companion object {
        fun of(group: AccessGroup): AccessGroupDraft = AccessGroupDraft(group.name, group.description ?: "", group.idpGroup ?: "")
    }
}

/** Where a permission applies. */
enum class AccessPermissionKind(val serverName: String) {
    /** On the server, wherever the group applies. */
    Server("server"),

    /** Over the domains of the group carrying the role. */
    Domain("domain"),

    /** Over every domain, present and future. */
    AllDomains("all-domains"),
}

/** One permission the server knows and how far it reaches. */
@Serializable
data class AccessPermission(
    /** Such as mail:read or user:manage. */
    val key: String,
    /** One of server, domain or all-domains. */
    val kind: String,
    /** For an all-domains permission, the domain permission it stands in for on every domain. */
    val widens: String? = null,
) {
    val permissionKind: AccessPermissionKind? get() = AccessPermissionKind.entries.firstOrNull { kindOption -> kindOption.serverName == kind }
}

/** A named set of permissions, carried by groups. */
@Serializable
data class AccessRole(val id: String, val name: String, val description: String? = null, val permissions: List<String> = emptyList()) {
    /** The role's permissions with one ticked or unticked, in the order the server lists them. What the server does not list is dropped, as the dashboard does. */
    fun permissionsSetting(permissionKey: String, isHeld: Boolean, catalog: List<AccessPermission>): List<String> =
        catalog.map { permission -> permission.key }.filter { key -> if (key == permissionKey) isHeld else permissions.contains(key) }
}

/** A role's name and description as the role form holds them; its permissions are ticked on the role's own page. */
data class AccessRoleDraft(val name: String = "", val description: String = "") {
    val canSubmit: Boolean get() = trimmedText(name).isNotEmpty()

    val variables: Map<String, JsonElement> get() = variablesOf("name" to trimmedText(name), "description" to description)

    companion object {
        fun of(role: AccessRole): AccessRoleDraft = AccessRoleDraft(role.name, role.description ?: "")
    }
}

/** A domain a group can be given, by its name. */
@Serializable
data class AccessDomain(val id: String, val domain: String)

/** What an audit event did to the thing it is about. */
enum class AuditAction(val serverName: String) { Create("create"), Update("update"), Delete("delete") }

/** One administrative change: who made it, from where, what it changed, and the row before and after. */
@Serializable
data class AuditEvent(
    val id: String,
    val createdAt: String,
    /** user, agent, console and the like. */
    val actorKind: String,
    val actorLabel: String? = null,
    /** The operator who was signed in as the actor, when one was. */
    val impersonatorLabel: String? = null,
    val sourceIp: String? = null,
    /** The server instance that made the change. */
    val instance: String? = null,
    /** user, group, role, domain and the like. */
    val resourceType: String,
    val resourceId: String,
    /** What the thing is called now, resolved by the server. */
    val resourceLabel: String? = null,
    /** Where the thing is on the dashboard. */
    val resourceLink: String? = null,
    /** create, update or delete. */
    val action: String,
    val before: JsonElement? = null,
    val after: JsonElement? = null,
) {
    val auditAction: AuditAction? get() = AuditAction.entries.firstOrNull { option -> option.serverName == action }

    /** Who made the change: their name, or what kind of actor it was. */
    val actorName: String get() = actorLabel?.takeIf { text -> text.isNotEmpty() } ?: actorKind

    /** Whether the person's agent made the change for them. */
    val isByAgent: Boolean get() = actorKind == "agent"

    /** The fields that differ between the two sides, by name: nearly always two or three of thirty. */
    val changedFields: List<String>
        get() {
            val beforeFields = fieldsOf(before)
            val afterFields = fieldsOf(after)
            return (beforeFields.keys + afterFields.keys).filter { field -> beforeFields[field] != afterFields[field] }.toSortedSet().toList()
        }

    /** What one side held in a field. */
    fun beforeValue(field: String): JsonElement? = fieldsOf(before)[field]

    fun afterValue(field: String): JsonElement? = fieldsOf(after)[field]

    private fun fieldsOf(side: JsonElement?): Map<String, JsonElement> = (side as? JsonObject) ?: emptyMap()

    companion object {
        /** A value as a line of text: a string as it is, anything else as JSON, and null for a value that is missing, null or empty. */
        fun describe(fieldValue: JsonElement?): String? = when {
            fieldValue == null || fieldValue is JsonNull -> null
            fieldValue is JsonPrimitive && fieldValue.isString -> fieldValue.contentOrNull?.takeIf { text -> text.isNotEmpty() }
            else -> fieldValue.sortedByKeys().toString()
        }

        /** The kinds of thing the log can be narrowed to, as the dashboard offers them. */
        val resourceTypes: List<String> = listOf(
            "user", "group", "role", "domain", "alias", "credential", "mailbox", "mailbox_address", "mailbox_app_password", "token",
            "passkey", "configuration", "agent", "addressbook",
        )
    }
}

/** One page of the audit log, with how many events there are in all. */
@Serializable
data class AuditEventPage(val events: List<AuditEvent>, val total: Int)

@Serializable private class ListUsersAnswer(val ListUsers: List<AccessUser>)

@Serializable private class CreateUserAnswer(val CreateUser: AccessUser)

@Serializable private class UpdateUserAnswer(val UpdateUser: AccessUser)

@Serializable private class ListGroupsAnswer(val ListGroups: List<AccessGroup>)

@Serializable private class CreateGroupAnswer(val CreateGroup: AccessGroup)

@Serializable private class UpdateGroupAnswer(val UpdateGroup: AccessGroup)

@Serializable private class ListRolesAnswer(val ListRoles: List<AccessRole>)

@Serializable private class ListPermissionsAnswer(val ListPermissions: List<AccessPermission>)

@Serializable private class CreateRoleAnswer(val CreateRole: AccessRole)

@Serializable private class UpdateRoleAnswer(val UpdateRole: AccessRole)

@Serializable private class ListDomainsAnswer(val ListDomains: List<AccessDomain>)

@Serializable private class ListAuditEventsAnswer(val ListAuditEvents: AuditEventPage)

/** Everybody with an account. Needs user:manage. */
suspend fun Connection.accessUsers(): List<AccessUser> = execute<ListUsersAnswer>(AccessManagementDocuments.ACCESS_USERS).ListUsers

/** Makes an account, in the server's default group. */
suspend fun Connection.createAccessUser(draft: AccessUserDraft): AccessUser =
    execute<CreateUserAnswer>(AccessManagementDocuments.CREATE_ACCESS_USER, draft.createVariables).CreateUser

/** Changes a person's username, name and notification address. */
suspend fun Connection.updateAccessUser(userId: String, draft: AccessUserDraft): AccessUser =
    execute<UpdateUserAnswer>(AccessManagementDocuments.UPDATE_ACCESS_USER, draft.updateVariables(userId)).UpdateUser

/** Disables a person, who then cannot sign in, or lets them sign in again. Everything of theirs is kept either way. */
suspend fun Connection.setAccessUserDisabled(userId: String, isDisabled: Boolean): AccessUser =
    execute<UpdateUserAnswer>(AccessManagementDocuments.UPDATE_ACCESS_USER, variablesOf("userId" to userId, "disabled" to isDisabled)).UpdateUser

/** Sets a person's password. The server signs them out everywhere and revokes their API tokens. */
suspend fun Connection.setAccessUserPassword(userId: String, password: String) {
    execute<JsonObject>(AccessManagementDocuments.SET_ACCESS_USER_PASSWORD, variablesOf("userId" to userId, "password" to password))
}

/** Removes an account with its sessions, tokens, passkeys and memberships. */
suspend fun Connection.deleteAccessUser(userId: String) {
    execute<JsonObject>(AccessManagementDocuments.DELETE_ACCESS_USER, variablesOf("userId" to userId))
}

suspend fun Connection.accessGroups(): List<AccessGroup> = execute<ListGroupsAnswer>(AccessManagementDocuments.ACCESS_GROUPS).ListGroups

suspend fun Connection.createAccessGroup(draft: AccessGroupDraft): AccessGroup =
    execute<CreateGroupAnswer>(AccessManagementDocuments.CREATE_ACCESS_GROUP, draft.variables).CreateGroup

/** Changes a group's name, description and identity provider group. */
suspend fun Connection.updateAccessGroup(groupId: String, draft: AccessGroupDraft): AccessGroup =
    updateAccessGroup(draft.variables + variablesOf("groupId" to groupId))

/** Replaces who is in a group. Somebody who may only manage people may change this and nothing else about a group, so nothing else is sent. */
suspend fun Connection.setAccessGroupMembers(groupId: String, userIds: List<String>): AccessGroup =
    updateAccessGroup(variablesOf("groupId" to groupId, "userIds" to userIds))

/** Replaces the roles a group carries. */
suspend fun Connection.setAccessGroupRoles(groupId: String, roleIds: List<String>): AccessGroup =
    updateAccessGroup(variablesOf("groupId" to groupId, "roleIds" to roleIds))

/** Replaces the domains a group's domain permissions apply to. */
suspend fun Connection.setAccessGroupDomains(groupId: String, domainIds: List<String>): AccessGroup =
    updateAccessGroup(variablesOf("groupId" to groupId, "domainIds" to domainIds))

private suspend fun Connection.updateAccessGroup(variables: Map<String, JsonElement>): AccessGroup =
    execute<UpdateGroupAnswer>(AccessManagementDocuments.UPDATE_ACCESS_GROUP, variables).UpdateGroup

/** Removes a group; its members keep their accounts. */
suspend fun Connection.deleteAccessGroup(groupId: String) {
    execute<JsonObject>(AccessManagementDocuments.DELETE_ACCESS_GROUP, variablesOf("groupId" to groupId))
}

/** The roles. Needs role:manage or group:manage. */
suspend fun Connection.accessRoles(): List<AccessRole> = execute<ListRolesAnswer>(AccessManagementDocuments.ACCESS_ROLES).ListRoles

/** Every permission the server knows, in its order. */
suspend fun Connection.accessPermissions(): List<AccessPermission> = execute<ListPermissionsAnswer>(AccessManagementDocuments.ACCESS_PERMISSIONS).ListPermissions

/** Makes a role holding no permissions; they are ticked once it exists. */
suspend fun Connection.createAccessRole(draft: AccessRoleDraft): AccessRole =
    execute<CreateRoleAnswer>(AccessManagementDocuments.CREATE_ACCESS_ROLE, draft.variables + variablesOf("permissions" to emptyList<String>())).CreateRole

/** Changes a role's name and description. */
suspend fun Connection.updateAccessRole(roleId: String, draft: AccessRoleDraft): AccessRole =
    execute<UpdateRoleAnswer>(AccessManagementDocuments.UPDATE_ACCESS_ROLE, draft.variables + variablesOf("roleId" to roleId)).UpdateRole

/** Replaces the permissions a role holds. */
suspend fun Connection.setAccessRolePermissions(roleId: String, permissions: List<String>): AccessRole =
    execute<UpdateRoleAnswer>(AccessManagementDocuments.UPDATE_ACCESS_ROLE, variablesOf("roleId" to roleId, "permissions" to permissions)).UpdateRole

/** Removes a role; every group that carried it loses it. */
suspend fun Connection.deleteAccessRole(roleId: String) {
    execute<JsonObject>(AccessManagementDocuments.DELETE_ACCESS_ROLE, variablesOf("roleId" to roleId))
}

/** The domains a group can be given. */
suspend fun Connection.accessDomains(): List<AccessDomain> = execute<ListDomainsAnswer>(AccessManagementDocuments.ACCESS_DOMAINS).ListDomains

/** A page of the audit log, newest first, narrowed to one kind of thing when one is given. Needs audit:read. */
suspend fun Connection.auditEvents(resourceType: String? = null, first: Int = 50, offset: Int = 0): AuditEventPage =
    execute<ListAuditEventsAnswer>(
        AccessManagementDocuments.AUDIT_EVENTS,
        variablesOf("first" to first, "offset" to offset, "resourceType" to resourceType?.takeIf { text -> text.isNotEmpty() }),
    ).ListAuditEvents
