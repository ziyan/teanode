package com.teanode.kit

/**
 * AppSection is a part of TeaNode, as the dashboard's sidebar has them: a tab of the bottom bar or a row of the
 * navigation drawer. The agent's part is Chats: it holds the chats with people and groups as well as the agent's own.
 * The names are the iPhone app's (`AppSection`), and [routeName] is what a deep link and a route say.
 */
enum class AppSection(val routeName: String) {
    Agent("agent"),
    Journal("journal"),
    Goals("goals"),
    Ideas("ideas"),
    Mail("mail"),
    Calendar("calendar"),
    Contacts("contacts"),
    Photos("photos"),
    Location("location"),
    Health("health"),
    Passwords("passwords"),
    Shopping("shopping"),
    Knowledge("knowledge"),
    Finance("finance"),
    Recordings("recordings"),
    Coding("coding"),
    Settings("settings"),
    ;

    companion object {
        fun fromRouteName(routeName: String): AppSection? = entries.firstOrNull { section -> section.routeName == routeName }
    }
}

/** AppSectionGroup is a heading in the navigation drawer, with the parts of TeaNode that belong under it. */
enum class AppSectionGroup(val sections: List<AppSection>) {
    EveryDay(
        listOf(
            AppSection.Agent, AppSection.Journal, AppSection.Mail, AppSection.Photos, AppSection.Calendar, AppSection.Contacts,
            AppSection.Location, AppSection.Health, AppSection.Passwords, AppSection.Shopping,
        ),
    ),
    Work(listOf(AppSection.Coding, AppSection.Knowledge, AppSection.Recordings)),
    Plans(listOf(AppSection.Goals, AppSection.Ideas)),
    Money(listOf(AppSection.Finance)),
}

/**
 * TabBarChoice is which parts the person keeps in the bottom bar, in their order, kept on this device. Every other
 * part is in the drawer, which the bar's More button opens, and Settings is the last row of it. Three at most, as the
 * bar holds five and More and Search are two.
 */
object TabBarChoice {
    const val STORAGE_KEY = "tabBarSections"
    const val MOST_COUNT = 3
    val defaultSections = listOf(AppSection.Agent, AppSection.Mail, AppSection.Photos)
    val defaultText: String get() = text(defaultSections)

    /** The parts written in text, as [text] writes them: known ones, each once, Settings never, three at most. */
    fun sections(text: String): List<AppSection> {
        val chosen = ArrayList<AppSection>()
        for (name in text.split(',')) {
            val section = AppSection.fromRouteName(name) ?: continue
            if (section == AppSection.Settings || section in chosen) continue
            chosen.add(section)
        }
        return chosen.take(MOST_COUNT)
    }

    fun text(sections: List<AppSection>): String = sections.joinToString(",") { section -> section.routeName }
}

/**
 * SectionFacts is what the app knows about the server and the person, and so which parts it offers: a part the server
 * does not offer, or the person may not use, is left out. Until the server has said, only Settings is offered.
 */
data class SectionFacts(
    val services: ServiceCapabilities? = null,
    val session: AccessSession? = null,
    /** Whether any computer runs the coding sessions (herdr); the Coding part is not offered without one. */
    val hasCodingComputers: Boolean = false,
    /** Whether the account has recorded or been shown recordings; Recordings is not offered without it. */
    val hasRecordingOwner: Boolean = false,
) {
    fun isAvailable(section: AppSection): Boolean {
        val offered = services ?: ServiceCapabilities.unavailable
        return when (section) {
            AppSection.Mail -> offered.isMailboxEnabled
            AppSection.Calendar -> offered.isCalendarEnabled || offered.isRemindersEnabled
            AppSection.Contacts -> offered.isContactsEnabled
            // Hidden when an operator turned Photos off for the server, and from someone who may not use it. A list of
            // permissions that could not be read leaves it offered: the server refuses what is not allowed, and a
            // failed read must not hide the library.
            AppSection.Photos -> offered.isPhotosEnabled && (session?.permissions == null || session.canUsePhotos)
            AppSection.Recordings -> hasRecordingOwner
            // The agent's screen also holds the person's chats, which need no agent.
            AppSection.Agent -> offered.isAssistantEnabled || holds("circle:use")
            AppSection.Goals, AppSection.Ideas, AppSection.Knowledge, AppSection.Finance -> offered.isAssistantEnabled
            AppSection.Location -> holds("location:use")
            AppSection.Health -> holds("health:use")
            AppSection.Journal -> holds("journal:use")
            AppSection.Passwords -> holds("passwords:use")
            AppSection.Shopping -> holds("shopping:use")
            AppSection.Coding -> offered.isAssistantEnabled && hasCodingComputers
            AppSection.Settings -> true
        }
    }

    private fun holds(permissionKey: String): Boolean = session?.holds(permissionKey) ?: false
}
