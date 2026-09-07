import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'

import { MailboxFolder, MailboxView, graphql } from './api'
import { Key } from './i18n/i18n'
import { useSession } from './session'

// The mailboxes the signed-in person owns, with their folder trees and
// unread counts. Loaded once for the whole page rather than by the rail and
// the mailbox page separately: the rail draws the tree, the page reads a
// folder, and both want the same counts to move when a message is read.
//
// Refreshed after every action that changes a count, and on a slow clock
// otherwise, so that mail arriving while the page is open shows up without a
// reload. A push channel can replace the clock when the IMAP server's
// LISTEN/NOTIFY is exposed to the browser.

const MAILBOXES = `{
  ListMailboxes {
    mailbox {
      id userId name signatureText signatureHtml
      addresses { aliasId domainId domain localPart address }
      rules { name enabled stop conditions { field header operator value } actions { kind folderId address } }
      autoReply { enabled from until subject text html }
    }
    folders { id mailboxId parentId name kind unread total }
    unread
  }
}`

const CURRENT_KEY = 'teanode.mailbox.current'
const POLL_INTERVAL = 30_000

type Mailboxes = {
  views: MailboxView[]
  loaded: boolean
  error: Error | null
  refresh: () => Promise<void>

  // The mailbox the rail shows the tree of. One of the views, or null before
  // they have loaded or when there are none.
  current: MailboxView | null
  setCurrentId: (mailboxId: string) => void
}

const Context = createContext<Mailboxes>({
  views: [],
  loaded: false,
  error: null,
  refresh: async () => {},
  current: null,
  setCurrentId: () => {},
})

export function MailboxesProvider({ children }: { children: React.ReactNode }) {
  const session = useSession()
  const [views, setViews] = useState<MailboxView[]>([])
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState<Error | null>(null)
  const [currentId, setCurrent] = useState<string | null>(() => {
    try {
      return window.localStorage.getItem(CURRENT_KEY)
    } catch {
      return null
    }
  })

  // The console and a person with no mail:read anywhere own no mailboxes,
  // and asking would only be refused.
  const eligible = Boolean(session.userId)

  const refresh = useCallback(async () => {
    if (!eligible) {
      setViews([])
      setLoaded(true)
      return
    }
    try {
      const response = await graphql<{ ListMailboxes: MailboxView[] }>(MAILBOXES)
      setViews(response.ListMailboxes ?? [])
      setError(null)
    } catch (failure) {
      setError(failure instanceof Error ? failure : new Error(String(failure)))
    } finally {
      setLoaded(true)
    }
  }, [eligible])

  useEffect(() => {
    void refresh()
    if (!eligible) {
      return
    }
    // Only while the tab is visible: a background tab polling for a day is a
    // day of requests nobody reads.
    const timer = window.setInterval(() => {
      if (document.visibilityState === 'visible') {
        void refresh()
      }
    }, POLL_INTERVAL)
    return () => window.clearInterval(timer)
  }, [refresh, eligible])

  const setCurrentId = useCallback((mailboxId: string) => {
    setCurrent(mailboxId)
    try {
      window.localStorage.setItem(CURRENT_KEY, mailboxId)
    } catch {
      // Remembered for this visit only.
    }
  }, [])

  const current = useMemo(
    () => views.find((view) => view.mailbox.id === currentId) ?? views[0] ?? null,
    [views, currentId],
  )

  const value = useMemo(
    () => ({ views, loaded, error, refresh, current, setCurrentId }),
    [views, loaded, error, refresh, current, setCurrentId],
  )
  return <Context.Provider value={value}>{children}</Context.Provider>
}

export function useMailboxes(): Mailboxes {
  return useContext(Context)
}

// The name a folder is shown under. The system folders are translated; a
// folder the owner made is called what they called it.
const KIND_LABELS: Record<string, Key> = {
  inbox: 'mailbox.folder.inbox',
  sent: 'mailbox.folder.sent',
  drafts: 'mailbox.folder.drafts',
  archive: 'mailbox.folder.archive',
  junk: 'mailbox.folder.junk',
  trash: 'mailbox.folder.trash',
}

export function folderLabel(t: (key: Key) => string, folder: MailboxFolder): string {
  const key = folder.kind ? KIND_LABELS[folder.kind] : undefined
  return key ? t(key) : folder.name
}

// The folder tree in the order it is drawn: system folders in their fixed
// order, then the owner's own folders alphabetically, each followed by its
// children at one more level of depth.
export type FolderRow = { folder: MailboxFolder; depth: number }

const KIND_ORDER = ['inbox', 'drafts', 'sent', 'archive', 'junk', 'trash']

export function folderRows(folders: MailboxFolder[]): FolderRow[] {
  const byParent = new Map<string, MailboxFolder[]>()
  for (const folder of folders) {
    const parent = folder.parentId ?? ''
    byParent.set(parent, [...(byParent.get(parent) ?? []), folder])
  }
  const order = (folder: MailboxFolder) => {
    const index = folder.kind ? KIND_ORDER.indexOf(folder.kind) : -1
    return index === -1 ? KIND_ORDER.length : index
  }
  const rows: FolderRow[] = []
  const walk = (parent: string, depth: number) => {
    const children = [...(byParent.get(parent) ?? [])].sort(
      (left, right) => order(left) - order(right) || left.name.localeCompare(right.name),
    )
    for (const folder of children) {
      rows.push({ folder, depth })
      walk(folder.id, depth + 1)
    }
  }
  walk('', 0)
  return rows
}

export function folderOfKind(view: MailboxView | null, kind: string): MailboxFolder | undefined {
  return view?.folders.find((folder) => folder.kind === kind)
}
