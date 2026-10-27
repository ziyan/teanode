import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'

import type { MailboxFolder, ServiceCapabilities } from '../api'
import { loadCatalog, TranslationProvider } from '../i18n/i18n'
import { navigablePages } from './sidebar'
import { SidebarSearch } from './sidebarSearch'

afterEach(() => {
  cleanup()
  window.localStorage.removeItem('teanode.language')
})

const SERVICES = {
  isCalendarEnabled: true,
  isRemindersEnabled: false,
  isContactsEnabled: true,
  isPhotosEnabled: false,
  isAssistantEnabled: true,
} as ServiceCapabilities

function Where() {
  const location = useLocation()
  return <p data-testid="where">{location.pathname + location.search}</p>
}

// The box of a person who holds the given permissions and has a mailbox.
function renderBox(
  permissions: string[] = ['journal:use', 'shopping:use'],
  onSubmitted = vi.fn(),
  folders: MailboxFolder[] = [],
) {
  const pages = navigablePages({
    permissions: { everywhere: permissions, byDomain: [] },
    services: SERVICES,
    hasMailbox: true,
    hasMailboxRail: true,
    isAgentAvailable: false,
    isFinanceShown: false,
    hasCoding: false,
  })
  render(
    <TranslationProvider>
      <MemoryRouter initialEntries={['/page']}>
        <SidebarSearch pages={pages} folders={folders} isHidden={false} onReveal={() => {}} onSubmitted={onSubmitted} />
        <Routes>
          <Route path="*" element={<Where />} />
        </Routes>
      </MemoryRouter>
    </TranslationProvider>,
  )
  const box = screen.getByRole('combobox') as HTMLInputElement
  box.focus()
  return { box, onSubmitted }
}

function optionNames(): string[] {
  return screen.queryAllByRole('option').map((option) => option.textContent ?? '')
}

function press(box: HTMLInputElement) {
  fireEvent.submit(box.closest('form')!)
}

it('offers the page a few letters of its name are in, and Enter goes there', () => {
  const { box, onSubmitted } = renderBox()
  fireEvent.change(box, { target: { value: 'jo' } })
  expect(optionNames()).toEqual(['Journal', 'Search everything for “jo”'])
  expect(screen.getAllByRole('option')[0].getAttribute('aria-selected')).toBe('true')
  press(box)
  expect(screen.getByTestId('where').textContent).toBe('/journal')
  expect(box.value).toBe('')
  expect(onSubmitted).toHaveBeenCalledTimes(1)
})

it('puts a name that starts with the words before one that only has a word starting with them', () => {
  const { box } = renderBox()
  fireEvent.change(box, { target: { value: 'set' } })
  expect(optionNames()[0]).toBe('Settings')
  fireEvent.change(box, { target: { value: 'new' } })
  expect(optionNames()).toEqual(['New message', 'Search everything for “new”'])
})

it('does not match letters inside a word, so a name typed to be found is searched for', () => {
  const { box } = renderBox()
  // "ourn" is inside Journal, and Enter still searches for it.
  fireEvent.change(box, { target: { value: 'ourn' } })
  expect(optionNames()).toEqual(['Search everything for “ourn”'])
  press(box)
  expect(screen.getByTestId('where').textContent).toBe('/search?q=ourn')
})

it('does not offer a page the person may not open', () => {
  const first = renderBox(['journal:use'])
  fireEvent.change(first.box, { target: { value: 'shop' } })
  expect(optionNames()).toEqual(['Search everything for “shop”'])
  cleanup()
  const second = renderBox(['journal:use', 'shopping:use'])
  fireEvent.change(second.box, { target: { value: 'shop' } })
  expect(optionNames()).toEqual(['Shopping', 'Search everything for “shop”'])
})

it('offers nothing for an empty box', () => {
  const { box } = renderBox()
  fireEvent.change(box, { target: { value: '   ' } })
  expect(screen.queryByRole('listbox')).toBeNull()
  expect(box.getAttribute('aria-expanded')).toBe('false')
})

it('searches everything on Enter from the last row, and when no page matches', () => {
  const { box } = renderBox()
  fireEvent.change(box, { target: { value: 'jo' } })
  fireEvent.keyDown(box, { key: 'ArrowDown' })
  expect(screen.getAllByRole('option')[1].getAttribute('aria-selected')).toBe('true')
  expect(box.getAttribute('aria-activedescendant')).toBe(screen.getAllByRole('option')[1].id)
  press(box)
  expect(screen.getByTestId('where').textContent).toBe('/search?q=jo')
  cleanup()

  const none = renderBox()
  fireEvent.change(none.box, { target: { value: 'zzzz' } })
  expect(optionNames()).toEqual(['Search everything for “zzzz”'])
  press(none.box)
  expect(screen.getByTestId('where').textContent).toBe('/search?q=zzzz')
})

it('moves the highlight with the arrows and runs a clicked row', () => {
  const { box } = renderBox()
  fireEvent.change(box, { target: { value: 'sh' } })
  fireEvent.keyDown(box, { key: 'ArrowDown' })
  fireEvent.keyDown(box, { key: 'ArrowUp' })
  expect(screen.getAllByRole('option')[0].getAttribute('aria-selected')).toBe('true')
  fireEvent.click(within(screen.getByRole('listbox')).getByRole('option', { name: 'Shopping' }))
  expect(screen.getByTestId('where').textContent).toBe('/shopping')
})

it('finds a page by its English name while the language is Chinese', async () => {
  window.localStorage.setItem('teanode.language', 'zh')
  await loadCatalog('zh')
  const { box } = renderBox()
  fireEvent.change(box, { target: { value: 'journal' } })
  expect(optionNames()[0]).toBe('日记')
  // Nothing to continue: the name shown does not start with the letters.
  expect(box.value).toBe('journal')
  press(box)
  expect(screen.getByTestId('where').textContent).toBe('/journal')
})

it('shows the rest of the name selected after the words, and Tab takes it', () => {
  const { box } = renderBox()
  fireEvent.change(box, { target: { value: 'jo' } })
  expect(box.value).toBe('journal')
  expect(box.selectionStart).toBe(2)
  expect(box.selectionEnd).toBe('journal'.length)
  const wasNotPrevented = fireEvent.keyDown(box, { key: 'Tab' })
  expect(wasNotPrevented).toBe(false)
  expect(box.value).toBe('journal')
  expect(box.selectionStart).toBe('journal'.length)
  expect(box.selectionEnd).toBe('journal'.length)
  // The words are now the whole name, so there is nothing left to offer.
  fireEvent.change(box, { target: { value: 'journal!' } })
  expect(box.value).toBe('journal!')
})

it('removes the completion on Backspace without deleting a letter that was typed', () => {
  const { box } = renderBox()
  fireEvent.change(box, { target: { value: 'jo' } })
  expect(box.value).toBe('journal')
  fireEvent.input(box, { target: { value: 'jo' }, inputType: 'deleteContentBackward' })
  expect(box.value).toBe('jo')
  expect(optionNames()[0]).toBe('Journal')
})

it('closes the list on the first Escape and lets go of the box on the next', () => {
  const { box } = renderBox()
  fireEvent.change(box, { target: { value: 'jo' } })
  expect(screen.getByRole('listbox')).toBeTruthy()
  fireEvent.keyDown(box, { key: 'Escape' })
  expect(screen.queryByRole('listbox')).toBeNull()
  expect(box.getAttribute('aria-expanded')).toBe('false')
  expect(document.activeElement).toBe(box)
  // Closing it took the offered letters away with it.
  expect(box.value).toBe('jo')
  fireEvent.keyDown(box, { key: 'Escape' })
  expect(document.activeElement).not.toBe(box)
})

it('closes the list when the box loses focus', () => {
  const { box } = renderBox()
  fireEvent.change(box, { target: { value: 'jo' } })
  act(() => box.blur())
  expect(screen.queryByRole('listbox')).toBeNull()
  expect(box.value).toBe('jo')
})

it('offers a tab of a page by its name, and Enter goes to the tab', () => {
  const { box, onSubmitted } = renderBox(['location:use'])
  fireEvent.change(box, { target: { value: 'travel' } })
  expect(optionNames()).toEqual(['Location › Travel', 'Search everything for “travel”'])
  // Nothing is completed inside the box: the name shown starts with the page's.
  expect(box.value).toBe('travel')
  press(box)
  expect(screen.getByTestId('where').textContent).toBe('/location?tab=travel')
  expect(box.value).toBe('')
  expect(onSubmitted).toHaveBeenCalledTimes(1)
})

it('offers a tab by the start of its page and the start of the tab', () => {
  const first = renderBox(['location:use', 'journal:use'])
  fireEvent.change(first.box, { target: { value: 'loc tr' } })
  expect(optionNames()).toEqual(['Location › Travel', 'Search everything for “loc tr”'])
  fireEvent.change(first.box, { target: { value: 'journal set' } })
  expect(optionNames()).toEqual(['Journal › Settings', 'Search everything for “journal set”'])
  fireEvent.click(within(screen.getByRole('listbox')).getByRole('option', { name: 'Journal › Settings' }))
  expect(screen.getByTestId('where').textContent).toBe('/journal?tab=settings')
  cleanup()
  // A tab that is its page's first is the page's own address.
  const second = renderBox(['journal:use'])
  fireEvent.change(second.box, { target: { value: 'journal time' } })
  expect(optionNames()[0]).toBe('Journal › Timeline')
  press(second.box)
  expect(screen.getByTestId('where').textContent).toBe('/journal')
})

it('does not offer the tab of a page the person may not open', () => {
  const first = renderBox(['journal:use'])
  fireEvent.change(first.box, { target: { value: 'travel' } })
  expect(optionNames()).toEqual(['Search everything for “travel”'])
  cleanup()
  // A tab behind a permission of its own is left out with it.
  const second = renderBox(['journal:use'])
  fireEvent.change(second.box, { target: { value: 'audit' } })
  expect(optionNames()).toEqual(['Search everything for “audit”'])
})

it('puts the page before the tabs of the same words', () => {
  const { box } = renderBox(['journal:use', 'health:use', 'passwords:use'])
  fireEvent.change(box, { target: { value: 'settings' } })
  expect(optionNames()).toEqual([
    'Settings',
    'Mailbox settings',
    'Journal › Settings',
    'Health › Settings',
    'Passwords › Settings',
    'Search everything for “settings”',
  ])
  // The page is the highlighted row and is completed; a tab is not.
  expect(screen.getAllByRole('option')[0].getAttribute('aria-selected')).toBe('true')
  press(box)
  expect(screen.getByTestId('where').textContent).toBe('/settings/preference')
})

it('finds a tab by its English name while the language is Chinese', async () => {
  window.localStorage.setItem('teanode.language', 'zh')
  await loadCatalog('zh')
  const { box } = renderBox(['location:use'])
  fireEvent.change(box, { target: { value: 'location travel' } })
  expect(optionNames()).toHaveLength(2)
  press(box)
  expect(screen.getByTestId('where').textContent).toBe('/location?tab=travel')
})

const FOLDERS: MailboxFolder[] = [
  { id: 'folder-inbox', mailboxId: 'box', name: 'INBOX', kind: 'inbox', unread: 0, total: 0 },
  { id: 'folder-archive', mailboxId: 'box', name: 'Archive', kind: 'archive', unread: 0, total: 0 },
  { id: 'folder-notes', mailboxId: 'box', name: 'Notes', unread: 0, total: 0 },
  { id: 'folder-trash', mailboxId: 'box', name: 'Trash', kind: 'trash', unread: 0, total: 0 },
]

it('offers a folder of the mailbox by its name, and Enter opens it', () => {
  const { box } = renderBox(undefined, undefined, FOLDERS)
  fireEvent.change(box, { target: { value: 'arch' } })
  expect(optionNames()).toEqual(['Mailbox › Archive', 'Search everything for “arch”'])
  press(box)
  expect(screen.getByTestId('where').textContent).toBe('/mailbox/folder-archive')
})

it('finds a folder the person made by its name, and by the mailbox before it', () => {
  const { box } = renderBox(undefined, undefined, FOLDERS)
  fireEvent.change(box, { target: { value: 'notes' } })
  expect(optionNames()).toEqual(['Mailbox › Notes', 'Search everything for “notes”'])
  fireEvent.change(box, { target: { value: 'mail tr' } })
  expect(optionNames()).toEqual(['Mailbox › Trash', 'Search everything for “mail tr”'])
  fireEvent.click(within(screen.getByRole('listbox')).getByRole('option', { name: 'Mailbox › Trash' }))
  expect(screen.getByTestId('where').textContent).toBe('/mailbox/folder-trash')
})

it('finds a system folder by its English name while the language is Chinese', async () => {
  window.localStorage.setItem('teanode.language', 'zh')
  await loadCatalog('zh')
  const { box } = renderBox(undefined, undefined, FOLDERS)
  fireEvent.change(box, { target: { value: 'inbox' } })
  expect(optionNames()).toHaveLength(2)
  press(box)
  expect(screen.getByTestId('where').textContent).toBe('/mailbox/folder-inbox')
})

it('offers eight rows at most before the last', () => {
  const folders: MailboxFolder[] = Array.from({ length: 12 }, (_, index) => ({
    id: `folder-${index}`,
    mailboxId: 'box',
    name: `Receipts ${index}`,
    unread: 0,
    total: 0,
  }))
  const { box } = renderBox(undefined, undefined, folders)
  fireEvent.change(box, { target: { value: 'receipts' } })
  expect(optionNames()).toHaveLength(9)
})
