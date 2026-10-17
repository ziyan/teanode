import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { useAgentIdentity } from '../agentPreferences'
import { useTranslation } from '../i18n/i18n'
import { useMailboxes } from '../mailboxes'
import { isPathAvailable } from '../services'
import { hasPermission, useSession } from '../session'
import { Shortcut, shortcutKeys, ShortcutSection, useMountedShortcuts, useShortcuts } from '../shortcuts'
import { ConfirmDialog } from './dialog'
import { firstManagementPath } from './sidebar'

// Asked by the account menu, which has the list of shortcuts as a row for
// somebody who has not yet learned that ? shows it.
export const SHORTCUTS_HELP_EVENT = 'teanode:shortcuts-help'

export function showShortcuts() {
  window.dispatchEvent(new Event(SHORTCUTS_HELP_EVENT))
}

// The search box of the page being read, when it has one that can be seen.
// The page's rather than the drawer's: the drawer has its own, and / is
// pressed from the page.
function pageSearch(): HTMLInputElement | null {
  const boxes = document.querySelectorAll<HTMLInputElement>('main.content input[type="search"]')
  return Array.from(boxes).find((box) => box.offsetParent !== null && !box.disabled) ?? null
}

// NavigationShortcuts is the keys that work on every page: g and a letter to
// go to a place in the rail, c to write a message, / to search, [ to narrow
// the rail, and ? for the list of every key that works where you are.
//
// Each place is offered only to somebody the rail would offer it to, so the
// list never names a page the reader cannot open.
export function NavigationShortcuts({ onToggleSidebar }: { onToggleSidebar?: () => void }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const session = useSession()
  const mailboxes = useMailboxes()
  const agent = useAgentIdentity()
  const [isShowingList, setShowingList] = useState(false)

  useEffect(() => {
    const show = () => setShowingList(true)
    window.addEventListener(SHORTCUTS_HELP_EVENT, show)
    return () => window.removeEventListener(SHORTCUTS_HELP_EVENT, show)
  }, [])

  const current = mailboxes.current
  const hasMailbox = Boolean(session.userId) && Boolean(current)
  const hasAgentUse = hasPermission(session.permissions, 'agent:use')
  const managePath = firstManagementPath(session.permissions, session.services)

  const goShortcuts = useMemo<Shortcut[]>(() => {
    const go = (key: string, label: string, to: string): Shortcut => ({
      prefix: 'g',
      key,
      label,
      run: () => void navigate(to),
    })
    const folderOfKind = (kind: string) => current?.folders.find((folder) => folder.kind === kind)
    const list: Shortcut[] = []
    if (hasMailbox) {
      list.push(go('i', t('mailbox.folder.inbox'), '/mailbox'))
      list.push(go('s', t('mailbox.folder.starred'), '/mailbox/starred'))
      const drafts = folderOfKind('drafts')
      if (drafts) list.push(go('d', t('mailbox.folder.drafts'), `/mailbox/${drafts.id}`))
      const sent = folderOfKind('sent')
      if (sent) list.push(go('t', t('mailbox.folder.sent'), `/mailbox/${sent.id}`))
    }
    if (agent.isAvailable) {
      list.push(go('a', agent.name || t('nav.agent'), '/agent'))
    }
    const services = session.services
    if (isPathAvailable('/calendar', services) && (hasMailbox || hasPermission(session.permissions, 'calendar:use'))) {
      list.push(go('c', t('nav.calendar'), '/calendar'))
    }
    if (isPathAvailable('/contacts', services) && (hasMailbox || hasPermission(session.permissions, 'contacts:use'))) {
      list.push(go('p', t('nav.contacts'), '/contacts'))
    }
    if (isPathAvailable('/photos', services) && hasPermission(session.permissions, 'photos:use')) {
      list.push(go('h', t('nav.photos'), '/photos'))
    }
    if (services?.isAssistantEnabled && hasAgentUse) {
      list.push(go('k', t('knowledge.title'), '/knowledge'))
    }
    if (hasAgentUse) {
      list.push(go('r', t('recordings.title'), '/recordings'))
    }
    if (hasAgentUse) {
      list.push(go('o', t('nav.coding'), '/coding'))
    }
    if (agent.isAvailable) {
      list.push(go('f', t('finance.title'), '/finance'))
    }
    if (managePath) {
      list.push(go('m', t('nav.manage'), managePath))
    }
    list.push(go(',', t('nav.settings'), '/settings'))
    return list
  }, [
    navigate,
    t,
    current,
    hasMailbox,
    agent.isAvailable,
    agent.name,
    session.services,
    session.permissions,
    hasAgentUse,
    managePath,
  ])

  const anywhereShortcuts = useMemo<Shortcut[]>(() => {
    const list: Shortcut[] = []
    if (hasMailbox) {
      list.push({ key: 'c', label: t('mailbox.newMessage'), run: () => void navigate('/mailbox/compose') })
    }
    list.push({
      key: '/',
      label: t('shortcuts.search'),
      run: () => {
        const box = pageSearch()
        if (!box) return false
        box.focus()
        box.select()
      },
    })
    if (onToggleSidebar) {
      list.push({ key: '[', label: t('shortcuts.toggleRail'), run: onToggleSidebar })
    }
    list.push({ key: '?', shift: true, label: t('shortcuts.help'), run: () => setShowingList(true) })
    return list
  }, [navigate, t, hasMailbox, onToggleSidebar])

  useShortcuts(goShortcuts, true, 'go')
  useShortcuts(anywhereShortcuts, true, 'anywhere')

  if (!isShowingList) {
    return null
  }
  return (
    <ShortcutList
      hasAgentDrawer={agent.isAvailable && Boolean(session.username)}
      onClose={() => setShowingList(false)}
    />
  )
}

const SECTIONS: {
  section: ShortcutSection
  label: 'shortcuts.section.page' | 'shortcuts.section.go' | 'shortcuts.section.anywhere'
}[] = [
  { section: 'page', label: 'shortcuts.section.page' },
  { section: 'go', label: 'shortcuts.section.go' },
  { section: 'anywhere', label: 'shortcuts.section.anywhere' },
]

// ShortcutList is every key that works on the page behind it, the page's own
// first: those are what somebody pressing ? on a page is usually after.
function ShortcutList({ hasAgentDrawer, onClose }: { hasAgentDrawer: boolean; onClose: () => void }) {
  const { t } = useTranslation()
  const registrations = useMountedShortcuts()

  return (
    <ConfirmDialog
      title={t('shortcuts.title')}
      wide
      body={
        <>
          <p className="muted">{t('shortcuts.intro')}</p>
          <div>
            {SECTIONS.map(({ section, label }) => {
              // The same key from two sets is one row: the one matched first,
              // which is the one that runs.
              const seen = new Set<string>()
              const shortcuts = registrations
                .filter((registration) => registration.section === section)
                .flatMap((registration) => registration.shortcuts)
                .filter((shortcut) => {
                  const name = shortcutKeys(shortcut).join(' ')
                  if (seen.has(name)) return false
                  seen.add(name)
                  return true
                })
              const rows = shortcuts.map((shortcut) => ({ keys: shortcutKeys(shortcut), label: shortcut.label }))
              if (section === 'anywhere' && hasAgentDrawer) {
                rows.push({ keys: ['Esc'], label: t('shortcuts.agentDrawer') })
              }
              if (rows.length === 0) {
                return null
              }
              return (
                <section key={section} className={`shortcut-section shortcut-section-${section}`}>
                  <h4>{t(label)}</h4>
                  <dl className="shortcut-list">
                    {rows.map((row) => (
                      <div key={row.keys.join(' ')}>
                        <dt>
                          {row.keys.map((key, index) => (
                            <span key={index}>
                              {index > 0 && <span className="shortcut-then">{t('shortcuts.then')}</span>}
                              <kbd>{key}</kbd>
                            </span>
                          ))}
                        </dt>
                        <dd>{row.label}</dd>
                      </div>
                    ))}
                  </dl>
                </section>
              )
            })}
          </div>
        </>
      }
      onClose={onClose}
    />
  )
}
