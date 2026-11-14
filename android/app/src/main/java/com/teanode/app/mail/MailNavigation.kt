package com.teanode.app.mail

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.FilterList
import androidx.compose.material.icons.outlined.Inbox
import androidx.compose.material.icons.outlined.Newspaper
import androidx.compose.material.icons.outlined.Settings
import androidx.navigation.NamedNavArgument
import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.AppPlace
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

private fun stringArgument(name: String, isOptional: Boolean = false): NamedNavArgument = navArgument(name) {
    type = NavType.StringType
    if (isOptional) {
        nullable = true
        defaultValue = null
    }
}

/** The routes of Mail: the list of folders, a folder or view, a conversation, a message being written, and the pages for the mailbox. */
fun NavGraphBuilder.mailRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Mail), deepLinks = Routes.deepLinks(Routes.home(AppSection.Mail))) { MailSection() }
    composable(MailRoutes.PLACE, arguments = listOf(stringArgument("place")), deepLinks = Routes.deepLinks(MailRoutes.PLACE)) { entry ->
        val mail = rememberMailModel()
        val place = MailPlace.fromRouteText(entry.arguments?.getString("place"))
        val nav = LocalAppNavigator.current
        if (place == MailPlace.Subscriptions) {
            SubscriptionsScreen(mail, onBack = { nav.back() }, onOpen = { subscription -> nav.navigate(MailRoutes.subscription(subscription.key)) })
        } else {
            ThreadListScreen(mail, place, onBack = { nav.back() }, onOpenThread = { itemId, folderId -> nav.navigate(MailRoutes.thread(itemId, folderId)) }, onCompose = { nav.navigate(MailRoutes.compose(ComposeKind.New)) })
        }
    }
    composable(MailRoutes.SEARCH, arguments = listOf(stringArgument("words", isOptional = true)), deepLinks = Routes.deepLinks(MailRoutes.SEARCH)) { entry ->
        val mail = rememberMailModel()
        val nav = LocalAppNavigator.current
        val inbox = mail.folderOfKind("inbox")
        if (inbox != null) {
            ThreadListScreen(
                mail, MailPlace.Folder(inbox.id), onBack = { nav.back() }, searchedWords = entry.arguments?.getString("words").orEmpty(),
                onOpenThread = { itemId, folderId -> nav.navigate(MailRoutes.thread(itemId, folderId)) }, onCompose = { nav.navigate(MailRoutes.compose(ComposeKind.New)) },
            )
        } else {
            androidx.compose.runtime.LaunchedEffect(mail) { mail.load() }
            com.teanode.app.ui.components.LoadingState()
        }
    }
    composable(MailRoutes.THREAD, arguments = listOf(stringArgument("itemId"), stringArgument("folderId", isOptional = true)), deepLinks = Routes.deepLinks(MailRoutes.THREAD)) { entry ->
        val mail = rememberMailModel()
        val nav = LocalAppNavigator.current
        androidx.compose.runtime.LaunchedEffect(mail) { if (!mail.isLoaded) mail.load() }
        MailThreadScreen(
            mail, entry.arguments?.getString("itemId").orEmpty(), entry.arguments?.getString("folderId"), onBack = { nav.back() },
            onCompose = { kind, itemId -> nav.navigate(MailRoutes.compose(kind, itemId)) },
        )
    }
    composable(
        MailRoutes.COMPOSE, arguments = listOf(stringArgument("kind"), stringArgument("itemId", isOptional = true), stringArgument("to", isOptional = true)),
        deepLinks = Routes.deepLinks(MailRoutes.COMPOSE),
    ) { entry ->
        val mail = rememberMailModel()
        val nav = LocalAppNavigator.current
        val start = ComposeStart(ComposeKind.fromRouteName(entry.arguments?.getString("kind")), entry.arguments?.getString("itemId"), entry.arguments?.getString("to"))
        ComposeScreen(mail, start, onClose = { nav.back() })
    }
    composable(MailRoutes.SUBSCRIPTION, arguments = listOf(stringArgument("key")), deepLinks = Routes.deepLinks(MailRoutes.SUBSCRIPTION)) { entry ->
        val mail = rememberMailModel()
        val nav = LocalAppNavigator.current
        SubscriptionReaderScreen(mail, entry.arguments?.getString("key").orEmpty(), onBack = { nav.back() }, onChanged = { mail.reload() })
    }
    composable(MailRoutes.SETTINGS, deepLinks = Routes.deepLinks(MailRoutes.SETTINGS)) {
        val mail = rememberMailModel()
        val nav = LocalAppNavigator.current
        MailboxSettingsScreen(
            mail, onBack = { nav.back() }, onOpenGeneral = { nav.navigate(MailRoutes.SETTINGS_GENERAL) }, onOpenAutoReply = { nav.navigate(MailRoutes.SETTINGS_AUTO_REPLY) },
            onOpenFolders = { nav.navigate(MailRoutes.SETTINGS_FOLDERS) }, onOpenRules = { nav.navigate(MailRoutes.RULES) }, onOpenPrograms = { nav.navigate(MailRoutes.SETTINGS_PROGRAMS) },
        )
    }
    composable(MailRoutes.SETTINGS_GENERAL, deepLinks = Routes.deepLinks(MailRoutes.SETTINGS_GENERAL)) { MailboxGeneralScreen(rememberMailModel(), onBack = { navigator.back() }) }
    composable(MailRoutes.SETTINGS_AUTO_REPLY, deepLinks = Routes.deepLinks(MailRoutes.SETTINGS_AUTO_REPLY)) { AutoReplyScreen(rememberMailModel(), onBack = { navigator.back() }) }
    composable(MailRoutes.SETTINGS_FOLDERS, deepLinks = Routes.deepLinks(MailRoutes.SETTINGS_FOLDERS)) { MailFoldersScreen(rememberMailModel(), onBack = { navigator.back() }) }
    composable(MailRoutes.SETTINGS_PROGRAMS, deepLinks = Routes.deepLinks(MailRoutes.SETTINGS_PROGRAMS)) { MailAppPasswordsScreen(rememberMailModel(), onBack = { navigator.back() }) }
    composable(MailRoutes.RULES, deepLinks = Routes.deepLinks(MailRoutes.RULES)) { MailRulesScreen(rememberMailModel(), onBack = { navigator.back() }) }
    mailManageRoutes(navigator)
}

/** What Mail plugs into the shell; listed in `Areas`. */
val mailArea = Area(
    section = AppSection.Mail,
    routes = NavGraphBuilder::mailRoutes,
    places = {
        listOf(
            AppPlace("mail-folders", com.teanode.app.R.string.mail_title, Routes.home(AppSection.Mail), Icons.Outlined.Inbox),
            AppPlace("mail-subscriptions", com.teanode.app.R.string.mail_subscriptions, MailRoutes.place(MailPlace.Subscriptions), Icons.Outlined.Newspaper, listOf(com.teanode.app.R.string.mail_title)),
            AppPlace("mail-settings", com.teanode.app.R.string.mail_mailbox_settings_title, MailRoutes.SETTINGS, Icons.Outlined.Settings, listOf(com.teanode.app.R.string.mail_title)),
            AppPlace("mail-rules", com.teanode.app.R.string.mail_rules_title, MailRoutes.RULES, Icons.Outlined.FilterList, listOf(com.teanode.app.R.string.mail_title)),
        )
    },
    demoAnswerers = mailDemoAnswerers,
)
