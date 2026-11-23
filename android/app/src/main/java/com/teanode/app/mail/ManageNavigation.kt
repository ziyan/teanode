package com.teanode.app.mail

import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.mail.manage.AllMailScreen
import com.teanode.app.mail.manage.AuditedMailScreen
import com.teanode.app.mail.manage.DeliveryQueueScreen
import com.teanode.app.mail.manage.DomainScreen
import com.teanode.app.mail.manage.DomainsScreen
import com.teanode.app.mail.manage.ManageDemoData
import com.teanode.app.mail.manage.ManageOverviewScreen
import com.teanode.app.mail.manage.ReportDetailScreen
import com.teanode.app.mail.manage.ReportsScreen
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes

/** The routes of the operator's pages: the overview, all mail and one message, the delivery queue, the reports, the domains and one domain. */
fun NavGraphBuilder.mailManageRoutes(navigator: AppNavigator) {
    composable(MailRoutes.MANAGE, deepLinks = Routes.deepLinks(MailRoutes.MANAGE)) {
        ManageOverviewScreen(
            onBack = { navigator.back() }, onOpenAllMail = { navigator.navigate(MailRoutes.MANAGE_ALL_MAIL) }, onOpenQueue = { navigator.navigate(MailRoutes.MANAGE_QUEUE) },
            onOpenReports = { navigator.navigate(MailRoutes.MANAGE_REPORTS) }, onOpenDomains = { navigator.navigate(MailRoutes.MANAGE_DOMAINS) },
        )
    }
    composable(MailRoutes.MANAGE_ALL_MAIL, deepLinks = Routes.deepLinks(MailRoutes.MANAGE_ALL_MAIL)) {
        AllMailScreen(onBack = { navigator.back() }, onOpenMail = { mailId -> navigator.navigate(MailRoutes.manageMail(mailId)) })
    }
    composable(MailRoutes.MANAGE_MAIL, arguments = listOf(navArgument("mailId") { type = NavType.StringType }), deepLinks = Routes.deepLinks(MailRoutes.MANAGE_MAIL)) { entry ->
        AuditedMailScreen(entry.arguments?.getString("mailId").orEmpty(), onBack = { navigator.back() })
    }
    composable(MailRoutes.MANAGE_QUEUE, deepLinks = Routes.deepLinks(MailRoutes.MANAGE_QUEUE)) {
        DeliveryQueueScreen(onBack = { navigator.back() }, onOpenMail = { mailId -> navigator.navigate(MailRoutes.manageMail(mailId)) })
    }
    composable(MailRoutes.MANAGE_REPORTS, deepLinks = Routes.deepLinks(MailRoutes.MANAGE_REPORTS)) {
        ReportsScreen(onBack = { navigator.back() }, onOpenReport = { reportId -> navigator.navigate(MailRoutes.manageReport(reportId)) })
    }
    composable(MailRoutes.MANAGE_REPORT, arguments = listOf(navArgument("reportId") { type = NavType.StringType }), deepLinks = Routes.deepLinks(MailRoutes.MANAGE_REPORT)) { entry ->
        ReportDetailScreen(entry.arguments?.getString("reportId").orEmpty(), onBack = { navigator.back() })
    }
    composable(MailRoutes.MANAGE_DOMAINS, deepLinks = Routes.deepLinks(MailRoutes.MANAGE_DOMAINS)) {
        DomainsScreen(onBack = { navigator.back() }, onOpenDomain = { domainId -> navigator.navigate(MailRoutes.manageDomain(domainId)) })
    }
    composable(MailRoutes.MANAGE_DOMAIN, arguments = listOf(navArgument("domainId") { type = NavType.StringType }), deepLinks = Routes.deepLinks(MailRoutes.MANAGE_DOMAIN)) { entry ->
        DomainScreen(entry.arguments?.getString("domainId").orEmpty(), onBack = { navigator.back() })
    }
}

/** What demo mode answers for the operator's pages. */
val mailManageDemoAnswerers: List<DemoAnswerer> = ManageDemoData.answerers
