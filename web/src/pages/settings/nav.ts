import type { Key } from '../../i18n/i18n'

// Every settings surface, in one list. The rail links into it and the
// breadcrumb reads it — so a new surface is added here and appears in both,
// rather than in one of them and not the other.
//
// No React in this module on purpose: it is data about the navigation, and
// keeping it free of components means anything can read it.
//
// Two categories, and they are reached from two different places. What
// configures the server sits in the rail beside Domains, because it is
// configuration of the thing the rail is about. What configures the person
// signed in hangs off their own name at the foot of the rail, which is where
// people look for it.
//
// Which is why the server's is under /manage, beside Mail and Domains, and
// not under /settings: /settings is the signed-in person's own, and /manage
// is the operator's. It is reached the way the other operator pages are, and
// a breadcrumb reading "Settings > Server" named a page that does not exist.
// The account's surfaces live under /settings, because that is a place you go
// into from your own name.
//
// It was three rows — Setup, Integrations, Server — and they are one subject:
// what this server is, what it talks to, and which version it is running.
// Three rows made somebody choose between them before knowing which one held
// the thing they wanted. They are tabs of /manage/server now.
export type SettingsCategory = 'server' | 'account'

export type SettingsSurface = {
  segment: string
  path: string
  label: Key
  description: Key
  category: SettingsCategory
  // Shown in the rail only when the person has what it is about: 'finance'
  // is there when their agent is on and a provider is offered or something
  // is linked already, 'mailbox' when they have a mailbox.
  shownWhen?: 'finance' | 'mailbox'
  // 'mailbox' puts the row in the mailbox's rail rather than the account's:
  // a place somebody reads every day, beside the calendar, rather than a
  // setting. The page keeps its path and its name in the breadcrumb.
  rail?: 'mailbox'
}

export const SETTINGS_CATEGORIES: { id: SettingsCategory; label: Key }[] = [
  { id: 'server', label: 'settings.category.server' },
  { id: 'account', label: 'settings.category.account' },
]

export const SETTINGS_SURFACES: SettingsSurface[] = [
  {
    segment: 'server',
    path: '/manage/server',
    label: 'server.title',
    description: 'server.description',
    category: 'server',
  },
  // First among the account surfaces, and where /settings lands. A page of
  // cards pointing at six pages was a page whose only content was a menu, and
  // the rail is already that menu.
  {
    segment: 'preference',
    path: '/settings/preference',
    label: 'preferences.title',
    description: 'settings.preferences.description',
    category: 'account',
  },
  // The person's agent: what it may reach, what it remembers, what it does
  // on its own. Under the account because it is theirs, not the server's.
  {
    segment: 'agent',
    path: '/settings/agent',
    label: 'agent.title',
    description: 'settings.agent.description',
    category: 'account',
  },
  // The mailbox's own settings: its name and signature, folders, rules, the
  // auto reply and the mail programs that read it. The person's, so under
  // /settings with the rest of what is theirs.
  {
    segment: 'mailbox',
    path: '/settings/mailbox',
    label: 'nav.mailboxSettings',
    description: 'settings.mailbox.description',
    category: 'account',
    shownWhen: 'mailbox',
  },
  // What the agent knows, as pages the person can read and correct. Its
  // own surface rather than a card on the agent page: it is a place
  // somebody browses, and a browser inside a settings form is neither.
  {
    segment: 'knowledge',
    path: '/knowledge',
    label: 'knowledge.title',
    description: 'settings.knowledge.description',
    category: 'account',
    rail: 'mailbox',
  },
  // What the institutions a person linked report: spending, budgets, net
  // worth. Beside Knowledge and for the same reason, a place somebody reads
  // rather than a setting, and not under /settings for it. Linking them and
  // the reporting currency are setup, and stay on the agent page.
  {
    segment: 'finance',
    path: '/finance',
    label: 'finance.title',
    description: 'settings.finance.description',
    category: 'account',
    shownWhen: 'finance',
    rail: 'mailbox',
  },
  {
    segment: 'password',
    path: '/settings/password',
    label: 'nav.changePassword',
    description: 'settings.password.description',
    category: 'account',
  },
  {
    segment: 'passkeys',
    path: '/settings/passkeys',
    label: 'passkeys.title',
    description: 'settings.passkeys.description',
    category: 'account',
  },
  {
    segment: 'tokens',
    path: '/settings/tokens',
    label: 'tokens.title',
    description: 'settings.tokens.description',
    category: 'account',
  },
  {
    segment: 'apps',
    path: '/settings/apps',
    label: 'apps.title',
    description: 'settings.apps.description',
    category: 'account',
  },
  {
    segment: 'sessions',
    path: '/settings/sessions',
    label: 'sessions.title',
    description: 'settings.sessions.description',
    category: 'account',
  },
]

// Where /settings on its own goes. A path somebody typed is not a page.
export const SETTINGS_LANDING = '/settings/preference'

export function surfacesByCategory(category: SettingsCategory): SettingsSurface[] {
  return SETTINGS_SURFACES.filter((surface) => surface.category === category)
}

// matchSettingsSurface finds the surface a path is on, so that
// /manage/server/storage resolves to the server surface and names itself in the
// breadcrumb from the same list the rail renders.
export function matchSettingsSurface(pathname: string): SettingsSurface | undefined {
  return SETTINGS_SURFACES.find(
    (surface) => pathname === surface.path || pathname.startsWith(`${surface.path}/`),
  )
}
