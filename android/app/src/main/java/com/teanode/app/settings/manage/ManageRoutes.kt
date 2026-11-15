package com.teanode.app.settings.manage

import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/** The places of the operator's administration that are not in SettingsRoutes: the agents page and what it opens. */
object ManageRoutes {
    val AGENT_LIST = Routes.child(AppSection.Settings, "manage/agents/list")
    val AGENT_PERSONAL = Routes.child(AppSection.Settings, "manage/agents/personal")
    val AGENT_RUNS = Routes.child(AppSection.Settings, "manage/agents/runs")
    val AGENT_PROVIDERS = Routes.child(AppSection.Settings, "manage/agents/providers")
    val AGENT_PROVIDER = Routes.child(AppSection.Settings, "manage/agents/providers/{providerIndex}")
    val AGENT_MODELS = Routes.child(AppSection.Settings, "manage/agents/models")
    val AGENT_VOICE = Routes.child(AppSection.Settings, "manage/agents/voice")
    val AGENT_RECORDING = Routes.child(AppSection.Settings, "manage/agents/recording")
    val AGENT_FEATURES = Routes.child(AppSection.Settings, "manage/agents/features")
    val AGENT_LIMITS = Routes.child(AppSection.Settings, "manage/agents/limits")
    val AGENT_TOOLS = Routes.child(AppSection.Settings, "manage/agents/tools")
    val AGENT_WEB_SEARCH = Routes.child(AppSection.Settings, "manage/agents/web-search")
    val AGENT_BROWSER = Routes.child(AppSection.Settings, "manage/agents/browser")
    val AGENT_SERVERS = Routes.child(AppSection.Settings, "manage/agents/servers")
    val AGENT_SERVER = Routes.child(AppSection.Settings, "manage/agents/servers/{serverIndex}")
    val AGENT_SKILLS = Routes.child(AppSection.Settings, "manage/agents/skills")
    val AGENT_SKILL = Routes.child(AppSection.Settings, "manage/agents/skills/{skillName}")
    val AGENT_SOURCE_TYPES = Routes.child(AppSection.Settings, "manage/agents/source-types")
    val AGENT_FINANCE = Routes.child(AppSection.Settings, "manage/agents/finance")
    val PERSON = Routes.child(AppSection.Settings, "manage/people/{userId}")
    val RELEASE_NOTES = Routes.child(AppSection.Settings, "manage/release-notes")

    /** The index a new provider or server is saved under: past the end of the list. */
    const val NEW_ITEM = "new"

    fun provider(providerIndex: Int?): String = Routes.child(AppSection.Settings, "manage/agents/providers/${providerIndex ?: NEW_ITEM}")

    fun server(serverIndex: Int?): String = Routes.child(AppSection.Settings, "manage/agents/servers/${serverIndex ?: NEW_ITEM}")

    fun skill(skillName: String): String = Routes.child(AppSection.Settings, "manage/agents/skills/${android.net.Uri.encode(skillName)}")

    fun person(userId: String): String = Routes.child(AppSection.Settings, "manage/people/${android.net.Uri.encode(userId)}")
}
