import SwiftUI
import TeaNodeKit

/// SettingsPage is a page of Settings by name, so the Search tab can open
/// one, and one inside another, without going through the rows: the path of
/// the Settings stack holds them. Settings' own rows still push their pages
/// themselves.
enum SettingsPage: Hashable {
    case profile, circles, sharing, password, passkeys, apiTokens, devices, apps, sessions
    case photoBackup, photoFaces, health, journal
    /// Agent settings, with the page of it to open once it is read, if any.
    case agentSettings(AgentSettingsPart?)
    case knowledgeSources, memory, dreams, schedules, alerts, connections, activity
    case manage, manageMail, manageQueue, manageReports, manageDomains
    case managePeople, manageGroups, manageRoles, manageAuditLog, manageServices, manageServer, manageAgents
    case mailbox, mailboxNameAndSignature, mailboxAutoReply, mailboxFolders, mailboxRules, mailboxPrograms
}

/// AgentSettingsPart is a page of Agent settings by name. Its pages share the
/// settings Agent settings reads, so it opens them itself once they are read.
enum AgentSettingsPart: Hashable {
    case about, instructions, language, writingVoice, speakingVoice, reach, categories, model, speakingFirst, askFirst, whenAway
}

/// SettingsPageScreen is the page a SettingsPage names, made as Settings'
/// row for it makes it.
struct SettingsPageScreen: View {
    let page: SettingsPage
    @Environment(AppModel.self) private var app
    @Environment(MailModel.self) private var mail: MailModel?

    var body: some View {
        if let connection = app.connection {
            screen(connection: connection)
        }
    }

    @ViewBuilder
    private func screen(connection: Connection) -> some View {
        switch page {
        case .profile: PreferencesView(connection: connection) { saved in app.profileSaved(saved) }
        case .circles: CirclesView(connection: connection)
        case .sharing: SharingView(connection: connection)
        case .password: ChangePasswordView(connection: connection)
        case .passkeys: PasskeysView(connection: connection)
        case .apiTokens: TokensView(connection: connection)
        case .devices: DevicesView(connection: connection)
        case .apps: ConnectedAppsView(connection: connection)
        case .sessions: SessionsView(connection: connection)
        case .photoBackup: PhotoBackupView(connection: connection)
        case .photoFaces: PhotoFacesSettingsView(connection: connection)
        case .health: HealthSettingsScreen(connection: connection)
        case .journal: JournalSettingsScreen(connection: connection)
        case .agentSettings(let part): AgentSettingsView(connection: connection, openedPart: part)
        case .knowledgeSources: KnowledgeSourcesView(connection: connection)
        case .memory: AgentMemorySettingsView(connection: connection)
        case .dreams: DreamsView(connection: connection)
        case .schedules: SchedulesView(connection: connection)
        case .alerts: MutedAlertsView(connection: connection)
        case .connections: ConnectionsView(connection: connection)
        case .activity: AgentActivityView(connection: connection)
        case .manage: ManageOverviewView(connection: connection)
        case .manageMail: AllMailView(connection: connection)
        case .manageQueue: DeliveryQueueView(connection: connection)
        case .manageReports: ReportsView(connection: connection)
        case .manageDomains: DomainsView(connection: connection)
        case .managePeople: PeopleView(connection: connection)
        case .manageGroups: GroupsView(connection: connection)
        case .manageRoles: RolesView(connection: connection)
        case .manageAuditLog: AuditLogView(connection: connection)
        case .manageServices: ServerServicesView(connection: connection)
        case .manageServer: ServerSettingsView(connection: connection)
        case .manageAgents: AgentAdminView(connection: connection)
        case .mailbox, .mailboxNameAndSignature, .mailboxAutoReply, .mailboxFolders, .mailboxRules, .mailboxPrograms:
            if let mail { mailboxScreen(mail: mail) }
        }
    }

    @ViewBuilder
    private func mailboxScreen(mail: MailModel) -> some View {
        if page == .mailbox {
            MailboxSettingsView(mail: mail)
        } else if let mailbox = mail.mailbox {
            switch page {
            case .mailboxNameAndSignature: MailboxGeneralView(mail: mail, mailbox: mailbox)
            case .mailboxAutoReply: AutoReplyView(mail: mail, mailbox: mailbox)
            case .mailboxFolders: MailFoldersView(mail: mail)
            case .mailboxRules: MailRulesView(mail: mail, mailboxId: mailbox.id)
            default: MailAppPasswordsView(mail: mail, mailboxId: mailbox.id)
            }
        }
    }
}
