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
// Which is why the server's is at the top level — /server — and not under
// /settings. It is not a section of anything: it sits in the rail next to Mail
// and Domains and is reached the same way, and a breadcrumb reading
// "Settings > Server" named a page that does not exist. The account's surfaces
// do live under /settings, because that is a place you go into from your own
// name.
//
// It was three rows — Setup, Integrations, Server — and they are one subject:
// what this server is, what it talks to, and which version it is running.
// Three rows made somebody choose between them before knowing which one held
// the thing they wanted. They are tabs of /server now.
export type SettingsCategory = 'server' | 'account'

export type SettingsSurface = {
  segment: string
  path: string
  label: Key
  description: Key
  category: SettingsCategory
}

export const SETTINGS_CATEGORIES: { id: SettingsCategory; label: Key }[] = [
  { id: 'server', label: 'settings.category.server' },
  { id: 'account', label: 'settings.category.account' },
]

export const SETTINGS_SURFACES: SettingsSurface[] = [
  {
    segment: 'server',
    path: '/server',
    label: 'server.title',
    description: 'server.description',
    category: 'server',
  },
  // First among the account surfaces, and where /settings lands. A page of
  // cards pointing at six pages was a page whose only content was a menu, and
  // the rail is already that menu.
  {
    segment: 'profile',
    path: '/settings/profile',
    label: 'profile.title',
    description: 'settings.profile.description',
    category: 'account',
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
    segment: 'sessions',
    path: '/settings/sessions',
    label: 'sessions.title',
    description: 'settings.sessions.description',
    category: 'account',
  },
]

// Where /settings on its own goes. A path somebody typed is not a page.
export const SETTINGS_LANDING = '/settings/profile'

export function surfacesByCategory(category: SettingsCategory): SettingsSurface[] {
  return SETTINGS_SURFACES.filter((surface) => surface.category === category)
}

// matchSettingsSurface finds the surface a path is on, so that
// /server/storage resolves to the server surface and names itself in the
// breadcrumb from the same list the rail renders.
export function matchSettingsSurface(pathname: string): SettingsSurface | undefined {
  return SETTINGS_SURFACES.find(
    (surface) => pathname === surface.path || pathname.startsWith(`${surface.path}/`),
  )
}
