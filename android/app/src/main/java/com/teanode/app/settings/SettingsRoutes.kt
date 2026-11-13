package com.teanode.app.settings

import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/** Every place of Settings by route; each also opens from `teanode://app/<route>`. */
object SettingsRoutes {
    val HOME = Routes.home(AppSection.Settings)
    val PROFILE = Routes.child(AppSection.Settings, "profile")
    val CIRCLES = Routes.child(AppSection.Settings, "circles")
    val CIRCLE = Routes.child(AppSection.Settings, "circles/{circleId}")
    val SHARING = Routes.child(AppSection.Settings, "sharing")
    val PASSWORD = Routes.child(AppSection.Settings, "password")
    val PASSKEYS = Routes.child(AppSection.Settings, "passkeys")
    val API_TOKENS = Routes.child(AppSection.Settings, "api-tokens")
    val DEVICES = Routes.child(AppSection.Settings, "devices")
    val PHONES = Routes.child(AppSection.Settings, "devices/phones")
    val PHONE = Routes.child(AppSection.Settings, "devices/phones/{phoneId}")
    val AGENT = Routes.child(AppSection.Settings, "agent")
    val AGENT_PART = Routes.child(AppSection.Settings, "agent/{part}")
    val MANAGE_PEOPLE = Routes.child(AppSection.Settings, "manage/people")
    val MANAGE_GROUPS = Routes.child(AppSection.Settings, "manage/groups")
    val MANAGE_ROLES = Routes.child(AppSection.Settings, "manage/roles")
    val MANAGE_AUDIT = Routes.child(AppSection.Settings, "manage/audit-log")
    val MANAGE_SERVICES = Routes.child(AppSection.Settings, "manage/services")
    val SYNC = Routes.child(AppSection.Settings, "devices/sync")
    val MANAGE = Routes.child(AppSection.Settings, "manage")
    val APPS = Routes.child(AppSection.Settings, "apps")
    val SESSIONS = Routes.child(AppSection.Settings, "sessions")

    fun circle(circleId: String): String = Routes.child(AppSection.Settings, "circles/$circleId")

    fun agentPart(part: String): String = Routes.child(AppSection.Settings, "agent/$part")

    fun phone(phoneId: String): String = Routes.child(AppSection.Settings, "devices/phones/$phoneId")
}
