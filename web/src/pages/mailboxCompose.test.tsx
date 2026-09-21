import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { SessionProvider } from '../session'
import { MailboxComposer } from './mailboxCompose'

const fixture = vi.hoisted(() => ({
  mailbox: { id: 'owned-mailbox', addresses: [{ address: 'sender@example.com' }] },
  folders: [{ id: 'sent-folder', kind: 'sent' }],
}))
vi.mock('../api', () => ({ graphql: vi.fn() }))
vi.mock('../mailboxes', () => ({
  useMailboxes: () => ({ views: [fixture], current: fixture, loaded: true, refresh: vi.fn() }),
  folderOfKind: () => fixture.folders[0],
}))
vi.mock('../i18n/i18n', () => ({ useTranslation: () => ({ t: (key: string) => key }) }))
vi.mock('../components/toast', () => ({ useToast: () => ({ done: vi.fn(), failed: vi.fn() }) }))
vi.mock('../components/richText', () => ({
  RichTextEditor: ({ value, onChange }: { value: string; onChange: (value: string) => void }) => (
    <textarea aria-label="Body" value={value} onChange={(event) => onChange(event.target.value)} />
  ),
  htmlToText: (html: string) => html,
  textToHtml: (text: string) => text,
  quotableHtml: (html: string) => html,
}))

const execute = vi.mocked(graphql)
function compose(onSent = vi.fn(), draftOf?: string) {
  return render(
    <MemoryRouter>
      <SessionProvider
        value={{
          authenticated: true,
          authenticationRequired: true,
          passkeysEnabled: false,
          username: 'fixture',
          userId: 'owner',
        }}
      >
        <MailboxComposer onSent={onSent} draftOf={draftOf} />
      </SessionProvider>
    </MemoryRouter>,
  )
}
function fill() {
  fireEvent.change(screen.getByLabelText('compose.mailbox.to'), { target: { value: 'recipient@example.net' } })
  fireEvent.change(screen.getByLabelText('compose.mailbox.subject'), { target: { value: 'Fixture subject' } })
  fireEvent.change(screen.getByLabelText('Body'), { target: { value: 'Fixture body' } })
}
async function submit(container: HTMLElement) {
  await act(async () => {
    fireEvent.submit(container.querySelector('form')!)
  })
}
const sends = () => execute.mock.calls.filter(([document]) => document.includes('SendMailboxMessage'))

beforeEach(() => {
  sessionStorage.clear()
  execute.mockReset()
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe('composer uncertain sends', () => {
  it('freezes editing and autosave after a lost response, then recovers without resending', async () => {
    vi.useFakeTimers()
    execute.mockImplementation(async (document) => {
      if (document.includes('ListAddressBooks')) return { ListAddressBooks: [] }
      if (document.includes('GetMailboxSubmission')) return { GetMailboxSubmission: { mailId: 'accepted-mail' } }
      if (document.includes('SendMailboxMessage')) throw new Error('response lost')
      throw new Error(`unexpected operation ${document}`)
    })
    const onSent = vi.fn()
    const page = compose(onSent)
    await act(async () => undefined)
    fill()
    await submit(page.container)
    expect(page.container.querySelector('.compose-fields')?.hasAttribute('disabled')).toBe(true)
    expect(screen.queryByRole('button', { name: 'compose.mailbox.saveDraft' })).toBeNull()
    await act(async () => vi.advanceTimersByTime(31_000))
    expect(execute.mock.calls.some(([document]) => document.includes('SaveMailboxDraft'))).toBe(false)
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'compose.mailbox.retrySend' }))
    })
    expect(onSent).toHaveBeenCalledOnce()
    expect(sends()).toHaveLength(1)
  })

  it('restores the pending body after remount without rereading a reconciled draft', async () => {
    execute.mockImplementation(async (document) => {
      if (document.includes('ListAddressBooks')) return { ListAddressBooks: [] }
      if (document.includes('SendMailboxMessage')) throw new Error('response lost')
      throw new Error('draft no longer exists')
    })
    const first = compose()
    await act(async () => undefined)
    fill()
    await submit(first.container)
    first.unmount()
    const restored = compose(vi.fn(), 'removed-draft')
    await act(async () => undefined)
    expect((screen.getByLabelText('Body') as HTMLTextAreaElement).value).toBe('Fixture body')
    expect(restored.container.querySelector('.compose-fields')?.hasAttribute('disabled')).toBe(true)
    expect(execute.mock.calls.some(([document]) => document.includes('GetMailboxDraft'))).toBe(false)
  })

  it('returns to editing only after cancellation succeeds', async () => {
    let hasCancelled = false
    execute.mockImplementation(async (document) => {
      if (document.includes('ListAddressBooks')) return { ListAddressBooks: [] }
      if (document.includes('CancelMailboxSubmission') && hasCancelled) return { CancelMailboxSubmission: null }
      throw new Error('connection lost')
    })
    const page = compose()
    await act(async () => undefined)
    fill()
    await submit(page.container)
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'compose.mailbox.resumeEditing' }))
    })
    expect(page.container.querySelector('.compose-fields')?.hasAttribute('disabled')).toBe(true)
    hasCancelled = true
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'compose.mailbox.resumeEditing' }))
    })
    expect(page.container.querySelector('.compose-fields')?.hasAttribute('disabled')).toBe(false)
    expect(screen.getByRole('button', { name: 'compose.mailbox.saveDraft' })).toBeTruthy()
    expect((screen.getByLabelText('Body') as HTMLTextAreaElement).value).toBe('Fixture body')
  })

  it('uses the draft and attachment indexes returned by a save already in flight', async () => {
    let finishSave!: (response: unknown) => void
    const saving = new Promise((resolve) => {
      finishSave = resolve
    })
    execute.mockImplementation(async (document) => {
      if (document.includes('ListAddressBooks')) return { ListAddressBooks: [] }
      if (document.includes('SaveMailboxDraft')) return saving
      if (document.includes('GetMailboxDraft'))
        return { GetMailboxDraft: { attachments: [{ index: 4, filename: 'fixture.txt' }] } }
      if (document.includes('SendMailboxMessage'))
        return { SendMailboxMessage: { mail: { id: 'accepted' }, item: null } }
      throw new Error('unexpected operation')
    })
    const page = compose()
    await act(async () => undefined)
    fill()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'compose.mailbox.saveDraft' }))
      fireEvent.submit(page.container.querySelector('form')!)
    })
    expect(sends()).toHaveLength(0)
    await act(async () => {
      finishSave({ SaveMailboxDraft: { id: 'saved-draft' } })
    })
    expect(sends()[0][1]).toMatchObject({ message: { draftItemId: 'saved-draft', keepAttachments: [4] } })
  })
})
