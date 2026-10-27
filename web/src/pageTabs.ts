import type { ServiceCapabilities } from './api'
import type { Key } from './i18n/i18n'

// The tabs of every page that has them, and how a tab is addressed. The pages
// draw their tab rows from these lists and the search box offers a tab by the
// same list, so the two cannot disagree about which tabs a page has, in which
// order, or at which address one is. No React in here: the rail imports it.

// A tab. The permission keys and the services decide whether the person has
// it at all, the same rule for the page's row of tabs and for the search box.
export type PageTab = {
  id: string
  label: Key
  // The tab is the person's when they hold any of these permissions.
  anyOf?: string[]
  // The tab is there when this is true of the server's services.
  isShown?: (services: ServiceCapabilities | undefined) => boolean
}

// How a tab is put in the address: as the last part of the page's path
// (/manage/access/users), or as a query parameter (/location?tab=travel).
// The first tab, or the one a page opens on, is the page's own address.
type TabAddress = { kind: 'path'; defaultTabId?: string } | { kind: 'query'; parameter: string; defaultTabId?: string }

export type PageTabs = { path: string; address: TabAddress; tabs: PageTab[] }

const hasMailRouting = (services: ServiceCapabilities | undefined) => services?.isMailRoutingEnabled !== false

export const ACCESS_TABS: PageTab[] = [
  { id: 'users', label: 'access.users.tab', anyOf: ['user:manage', 'group:manage'] },
  { id: 'groups', label: 'access.groups.tab', anyOf: ['user:manage', 'group:manage'] },
  { id: 'roles', label: 'server.tabRoles', anyOf: ['role:manage', 'group:manage'] },
  { id: 'audit', label: 'server.tabAudit', anyOf: ['audit:read'] },
]

// The tabs of the Server page after About and Setup: the optional services,
// in the order mail moves through them.
export const INTEGRATION_TABS: PageTab[] = [
  { id: 'identity', label: 'serverSettings.tabIdentity' },
  { id: 'mail', label: 'serverSettings.tabMail', isShown: hasMailRouting },
  { id: 'sending', label: 'integrations.tabSending', isShown: hasMailRouting },
  { id: 'listeners', label: 'serverSettings.tabListeners' },
  { id: 'certificates', label: 'serverSettings.tabCertificates' },
  { id: 'storage', label: 'integrations.tabStorage' },
  { id: 'resolver', label: 'serverSettings.tabResolver', isShown: hasMailRouting },
  { id: 'scanning', label: 'integrations.tabScanning', isShown: hasMailRouting },
  // Its own tab rather than a card under Scanning: the spam filter has an
  // engine to choose and four parts to switch, and beside the antivirus's
  // host and port it was the one card on the page that needed reading.
  { id: 'spam', label: 'integrations.tabSpam', isShown: hasMailRouting },
  { id: 'sessions', label: 'serverSettings.tabSessions' },
  { id: 'sso', label: 'integrations.tabSso' },
]

export const SERVER_TABS: PageTab[] = [
  { id: 'about', label: 'server.tabAbout' },
  { id: 'setup', label: 'server.tabSetup' },
  ...INTEGRATION_TABS,
]

// Runs are content, and so are behind agent:act rather than the audit
// permission the rest of the page needs. The settings are the operator's,
// behind server:manage as the rest of the server's are.
export const AGENT_ADMIN_TABS: PageTab[] = [
  { id: 'general', label: 'agentAdmin.tabGeneral', anyOf: ['server:manage'] },
  { id: 'usage', label: 'agentAdmin.tabUsage' },
  { id: 'runs', label: 'agentAdmin.tabRuns', anyOf: ['agent:act'] },
  { id: 'models', label: 'agentAdmin.tabModels', anyOf: ['server:manage'] },
  { id: 'tools', label: 'agentAdmin.tabTools', anyOf: ['server:manage'] },
  { id: 'skills', label: 'agentAdmin.tabSkills', anyOf: ['server:manage'] },
  { id: 'sources', label: 'agentAdmin.tabSources', anyOf: ['server:manage'] },
]

// The tabs of /settings/agent. Finance joins them beside Sources when there is
// something to link through or linked already, which only the page knows.
export const AGENT_TABS: PageTab[] = [
  { id: 'overview', label: 'agent.tabOverview' },
  { id: 'ideas', label: 'agent.tabIdeas' },
  { id: 'goals', label: 'agent.tabGoals' },
  { id: 'alerts', label: 'agent.tabAlerts' },
  { id: 'mail', label: 'agent.tabMail', isShown: (services) => services?.isMailboxEnabled !== false },
  { id: 'sources', label: 'agent.tabSources' },
  { id: 'memory', label: 'agent.tabMemory' },
  { id: 'dreams', label: 'agent.tabDreams' },
  { id: 'schedules', label: 'agent.tabSchedules' },
  { id: 'connections', label: 'agent.tabConnections' },
  { id: 'activity', label: 'agent.tabActivity' },
]
export const AGENT_FINANCE_TAB: PageTab = { id: 'finance', label: 'agent.tabFinance' }

export const MAILBOX_SETTINGS_TABS: PageTab[] = [
  { id: 'general', label: 'mailboxSettings.tabGeneral' },
  { id: 'folders', label: 'mailboxSettings.tabFolders' },
  { id: 'rules', label: 'mailboxSettings.tabRules' },
  { id: 'autoreply', label: 'mailboxSettings.tabAutoReply' },
  { id: 'devices', label: 'mailboxSettings.tabDevices' },
]

export const FINANCE_TABS: PageTab[] = [
  { id: 'spending', label: 'finance.tabSpending' },
  { id: 'transactions', label: 'finance.tabTransactions' },
  { id: 'receipts', label: 'finance.tabReceipts' },
  { id: 'taxes', label: 'finance.tabTaxes' },
  { id: 'accounts', label: 'finance.tabAccounts' },
  { id: 'budgets', label: 'finance.tabBudgets' },
  { id: 'net-worth', label: 'finance.tabNetWorth' },
  { id: 'savings-targets', label: 'finance.tabSavingsTargets' },
]

// The views of the calendar are its tabs, widest span to narrowest, then the
// list, then the reminders list beside the calendar, which a phone keeps in
// its Reminders app and which is kept here too. Each is there only while its
// service is on.
const hasCalendar = (services: ServiceCapabilities | undefined) => services?.isCalendarEnabled !== false
export const CALENDAR_TABS: PageTab[] = [
  { id: 'month', label: 'calendar.viewMonth', isShown: hasCalendar },
  { id: 'week', label: 'calendar.viewWeek', isShown: hasCalendar },
  { id: 'workweek', label: 'calendar.viewWorkweek', isShown: hasCalendar },
  { id: 'day', label: 'calendar.viewDay', isShown: hasCalendar },
  { id: 'agenda', label: 'calendar.viewAgenda', isShown: hasCalendar },
  { id: 'reminders', label: 'calendar.viewReminders', isShown: (services) => services?.isRemindersEnabled !== false },
]

// Sharing is the person's own, Visits is the page's first tab. While
// somebody else is looked at the page leaves Sharing out of the row.
export const LOCATION_TABS: PageTab[] = [
  { id: 'visits', label: 'location.tabVisits' },
  { id: 'places', label: 'location.tabPlaces' },
  { id: 'travel', label: 'location.tabTravel' },
  { id: 'sharing', label: 'location.tabSharing' },
]

export const HEALTH_TABS: PageTab[] = [
  { id: 'data', label: 'health.tabData' },
  { id: 'settings', label: 'health.tabSettings' },
]

export const JOURNAL_TABS: PageTab[] = [
  { id: 'timeline', label: 'journal.tabTimeline' },
  { id: 'settings', label: 'journal.tabSettings' },
]

export const PASSWORDS_TABS: PageTab[] = [
  { id: 'items', label: 'passwords.tabItems' },
  { id: 'settings', label: 'passwords.tabSettings' },
]

export const SHARING_TABS: PageTab[] = [
  { id: 'by-you', label: 'sharing.byYou' },
  { id: 'with-you', label: 'sharing.withYou' },
]

// Every page with tabs the search box can jump into, by the page's path. The
// tabs of a page whose address holds something else the box cannot know (a
// domain's, which is in the domain's id) are not here.
export const PAGE_TABS: PageTabs[] = [
  { path: '/calendar', address: { kind: 'query', parameter: 'view' }, tabs: CALENDAR_TABS },
  { path: '/location', address: { kind: 'query', parameter: 'tab', defaultTabId: 'visits' }, tabs: LOCATION_TABS },
  { path: '/health', address: { kind: 'query', parameter: 'tab', defaultTabId: 'data' }, tabs: HEALTH_TABS },
  { path: '/journal', address: { kind: 'query', parameter: 'tab', defaultTabId: 'timeline' }, tabs: JOURNAL_TABS },
  { path: '/passwords', address: { kind: 'query', parameter: 'tab', defaultTabId: 'items' }, tabs: PASSWORDS_TABS },
  { path: '/finance', address: { kind: 'path' }, tabs: FINANCE_TABS },
  { path: '/sharing', address: { kind: 'path', defaultTabId: 'by-you' }, tabs: SHARING_TABS },
  { path: '/settings/agent', address: { kind: 'path' }, tabs: AGENT_TABS },
  { path: '/settings/mailbox', address: { kind: 'path' }, tabs: MAILBOX_SETTINGS_TABS },
  { path: '/manage/server', address: { kind: 'path' }, tabs: SERVER_TABS },
  { path: '/manage/access', address: { kind: 'path' }, tabs: ACCESS_TABS },
  { path: '/manage/agent', address: { kind: 'path' }, tabs: AGENT_ADMIN_TABS },
]

// What decides whether the person has a tab.
export type TabContext = {
  // Absent for a list with no tab behind a permission.
  hasPermission?: (key: string) => boolean
  services?: ServiceCapabilities
}

// shownTabs is the tabs of a list the person has.
export function shownTabs<Tab extends PageTab>(tabs: Tab[], context: TabContext): Tab[] {
  return tabs.filter(
    (tab) =>
      (!tab.anyOf || tab.anyOf.some((key) => context.hasPermission?.(key) === true)) &&
      (!tab.isShown || tab.isShown(context.services)),
  )
}

// tabAddress is where a tab of a page is, as the page puts it in the address.
export function tabAddress(page: PageTabs, tabId: string): string {
  if (tabId === page.address.defaultTabId) return page.path
  return page.address.kind === 'path' ? `${page.path}/${tabId}` : `${page.path}?${page.address.parameter}=${tabId}`
}
