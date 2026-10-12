import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { Domain, graphql } from '../api'
import { DomainAliasesTab } from './domainAliases'
import { DomainCredentialsTab } from './domainCredentials'

const notices = vi.hoisted(() => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }))
vi.mock('../api', () => ({ graphql: vi.fn() }))
vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({ t: (key: string) => key, plural: (count: number) => String(count), language: 'en' }),
}))
vi.mock('../components/toast', () => ({ useToast: () => notices }))
const execute = vi.mocked(graphql)

const domain: Domain = {
  id: 'domain-1',
  domain: 'example.com',
  subdomain: '',
  spamFilterScoreThreshold: 5,
  hasDkimKey: false,
  aliases: [
    {
      id: 'alias-email',
      pattern: '^hello$',
      comment: 'front desk',
      kind: 'email',
      email: 'desk@example.net',
      disabled: false,
    },
    { id: 'alias-webhook', pattern: '^hook$', kind: 'webhook', webhook: 'https://example.net/in', disabled: true },
    {
      id: 'alias-relay',
      pattern: '^relay$',
      kind: 'mailServer',
      mailServer: { host: 'relay.example.net', port: 587 },
      disabled: false,
    },
    { id: 'alias-catchall', pattern: '', kind: 'null', disabled: false },
  ],
  credentials: [
    { id: 'credential-1', comment: 'laptop', alias: 'noreply', disabled: false },
    { id: 'credential-2', disabled: true },
  ],
}

const run = vi.fn(async (work: () => Promise<unknown>) => {
  await work()
})

beforeEach(() => {
  execute.mockReset()
  execute.mockResolvedValue({ ListAllMailboxes: [] })
  run.mockClear()
  notices.done.mockReset()
})
afterEach(cleanup)

function drawAliases() {
  return render(
    <MemoryRouter initialEntries={['/manage/domains/domain-1/aliases']}>
      <Routes>
        <Route path="/manage/domains/:domainId/:tab" element={<DomainAliasesTab domain={domain} run={run} />} />
      </Routes>
    </MemoryRouter>,
  )
}

function drawCredentials() {
  return render(
    <MemoryRouter initialEntries={['/manage/domains/domain-1/credentials']}>
      <Routes>
        <Route path="/manage/domains/:domainId/:tab" element={<DomainCredentialsTab domain={domain} run={run} />} />
      </Routes>
    </MemoryRouter>,
  )
}

function lastMutation() {
  const calls = execute.mock.calls.filter(([document]) => document.includes('mutation'))
  return calls[calls.length - 1]
}

function submit() {
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'common.save' }))
}

it('opens an alias with its settings filled in and saves a changed note and disabled flag', async () => {
  drawAliases()
  fireEvent.click(screen.getByRole('button', { name: '^hello$: common.edit' }))
  const dialog = screen.getByRole('dialog')
  expect(within(dialog).getByDisplayValue('^hello$')).toBeTruthy()
  expect(within(dialog).getByDisplayValue('desk@example.net')).toBeTruthy()

  fireEvent.change(within(dialog).getByDisplayValue('front desk'), { target: { value: 'reception' } })
  fireEvent.click(within(dialog).getByRole('checkbox'))
  submit()

  await waitFor(() => expect(notices.done).toHaveBeenCalledWith('common.saved'))
  const [document, variables] = lastMutation()
  expect(document).toContain('UpdateAlias')
  expect(variables).toEqual({
    aliasId: 'alias-email',
    pattern: '^hello$',
    comment: 'reception',
    kind: 'email',
    email: 'desk@example.net',
    webhook: null,
    mailboxId: null,
    disabled: true,
  })
})

it('sends only the destination of the kind it was switched to', async () => {
  drawAliases()
  fireEvent.click(screen.getByRole('button', { name: '^hello$: common.edit' }))
  fireEvent.click(screen.getByRole('combobox', { name: 'domain.kind' }))
  fireEvent.click(screen.getByRole('option', { name: 'domain.kindWebhook' }))
  const dialog = screen.getByRole('dialog')

  // The email address is not carried over into the webhook box, and Save
  // waits for a URL.
  expect(within(dialog).getByRole('button', { name: 'common.save' }).hasAttribute('disabled')).toBe(true)
  fireEvent.change(within(dialog).getByLabelText('domain.postTo'), { target: { value: ' https://example.net/new ' } })
  submit()

  await waitFor(() => expect(notices.done).toHaveBeenCalled())
  expect(lastMutation()[1]).toMatchObject({
    kind: 'webhook',
    email: null,
    webhook: 'https://example.net/new',
    mailboxId: null,
  })
})

it('keeps a relay as it is and shows it read only', async () => {
  drawAliases()
  fireEvent.click(screen.getByRole('button', { name: '^relay$: common.edit' }))
  const dialog = screen.getByRole('dialog')
  const relay = within(dialog).getByDisplayValue('relay.example.net:587')
  expect(relay.hasAttribute('readonly')).toBe(true)
  submit()

  await waitFor(() => expect(notices.done).toHaveBeenCalled())
  const [, variables] = lastMutation()
  expect(variables).toMatchObject({ kind: 'mailServer', email: null, webhook: null, mailboxId: null })
  expect(variables).not.toHaveProperty('mailServer')
})

it('does not let an address with a pattern be blanked into a catch-all', () => {
  drawAliases()
  fireEvent.click(screen.getByRole('button', { name: '^hello$: common.edit' }))
  const dialog = screen.getByRole('dialog')
  fireEvent.change(within(dialog).getByDisplayValue('^hello$'), { target: { value: '  ' } })

  expect(within(dialog).getByRole('button', { name: 'common.save' }).hasAttribute('disabled')).toBe(true)
  expect(within(dialog).getByText('domain.patternRequired')).toBeTruthy()
})

it('lets a catch-all stay a catch-all', async () => {
  drawAliases()
  fireEvent.click(screen.getByRole('button', { name: 'domain.catchAll: common.edit' }))
  submit()

  await waitFor(() => expect(notices.done).toHaveBeenCalled())
  expect(lastMutation()[1]).toMatchObject({ aliasId: 'alias-catchall', pattern: '', kind: 'null' })
})

it('does not offer a relay when making an alias', () => {
  drawAliases()
  fireEvent.click(screen.getByRole('button', { name: 'domain.addAlias' }))
  fireEvent.click(screen.getByRole('combobox', { name: 'domain.kind' }))
  expect(screen.queryByRole('option', { name: 'domain.kindRelay' })).toBeNull()
})

it('saves a credential note, the address it may send as, and disabled', async () => {
  drawCredentials()
  fireEvent.click(screen.getByRole('button', { name: 'laptop: common.edit' }))
  const dialog = screen.getByRole('dialog')

  // The username is shown and cannot be changed.
  expect(within(dialog).getByDisplayValue('credential-1').hasAttribute('readonly')).toBe(true)
  fireEvent.change(within(dialog).getByDisplayValue('noreply'), { target: { value: '' } })
  fireEvent.click(within(dialog).getByRole('checkbox'))
  submit()

  await waitFor(() => expect(notices.done).toHaveBeenCalledWith('common.saved'))
  const [document, variables] = lastMutation()
  expect(document).toContain('UpdateCredential')
  expect(variables).toEqual({ credentialId: 'credential-1', comment: 'laptop', alias: '', disabled: true })
})
