import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { AddressBookPage } from './addressBook'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))

beforeEach(() => {
  vi.mocked(graphql).mockReset()
})
afterEach(cleanup)

let shownSearch = ''
function SearchProbe() {
  shownSearch = useLocation().search
  return null
}

function answer(hasPhoto = false) {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('ListAddressBooks'))
      return { ListAddressBooks: [{ id: 'book-1', name: 'Mine', contacts: 1 }] } as never
    if (document.includes('ListContacts'))
      return {
        ListContacts: [{ id: 'card-1', uid: 'u1', name: 'Fixture Guest', emails: ['guest@example.com'], phones: [] }],
      } as never
    if (document.includes('GetContact'))
      return {
        GetContact: {
          id: 'card-2',
          name: 'Linked Guest',
          organization: '',
          emails: ['linked@example.com'],
          phones: [],
          note: '',
          addresses: [],
          hasPhoto,
        },
      } as never
    if (document.includes('RemoveContactPicture'))
      return { RemoveContactPicture: { id: 'card-2', hasPhoto: false } } as never
    if (document.includes('ReadRecordMemoryLinks'))
      return {
        ReadRecordMemoryLinks: [
          {
            id: 'link-1',
            nodePath: 'people/linked-guest',
            nodeName: 'Linked Guest Page',
            nodeKind: 'person',
            recordKind: 'contact',
            recordId: 'card-2',
            recordRelation: 'is_contact',
            linkStatus: 'stated',
            passageStartMS: 0,
            passageEndMS: 0,
            transcriptRunId: '',
            note: '',
            createdAt: '2026-01-01T00:00:00Z',
            recordTitle: 'Linked Guest',
            dashboardPath: '/contacts?contact=card-2',
            isUnavailable: false,
          },
        ],
      } as never
    return { AgentGraphPage: { contact: null } } as never
  })
}

function show(address: string) {
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter initialEntries={[address]}>
          <AddressBookPage />
          <SearchProbe />
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
}

it('opens the card a link from memory names, with the pages it is in, and forgets the address when it closes', async () => {
  answer()
  show('/contacts?contact=card-2')
  const dialog = await screen.findByRole('dialog')
  expect((within(dialog).getAllByRole('textbox')[0] as HTMLInputElement).value).toBe('Linked Guest')
  expect(await within(dialog).findByRole('link', { name: 'Linked Guest Page' })).toBeTruthy()
  const askedAbout = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('ReadRecordMemoryLinks'))
  expect(askedAbout?.[1]).toMatchObject({ recordKind: 'contact', recordId: 'card-2' })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
  await waitFor(() => expect(shownSearch).not.toContain('contact='))
})

it('opens no card unless a link names one', async () => {
  answer()
  show('/contacts')
  await screen.findByText('Fixture Guest')
  expect(screen.queryByRole('dialog')).toBeNull()
  expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('GetContact'))).toBe(false)
})

it('offers to remove the picture of a contact that has one, and only then', async () => {
  answer(false)
  show('/contacts?contact=card-2')
  const dialog = await screen.findByRole('dialog')
  expect(within(dialog).queryByRole('button', { name: 'Remove picture' })).toBeNull()
  cleanup()

  answer(true)
  show('/contacts?contact=card-2')
  const withPicture = await screen.findByRole('dialog')
  const removedPicture = () =>
    vi
      .mocked(graphql)
      .mock.calls.some(([document, variables]) => document.includes('RemoveContactPicture') && variables?.id === 'card-2')
  // It asks first, and a no leaves the picture where it was.
  fireEvent.click(await within(withPicture).findByRole('button', { name: 'Remove picture' }))
  const question = await screen.findByRole('alertdialog')
  expect(within(question).getByText(/Devices synchronizing your address book will lose it too/)).toBeTruthy()
  expect(removedPicture()).toBe(false)
  fireEvent.click(within(question).getByRole('button', { name: 'Cancel' }))
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
  expect(removedPicture()).toBe(false)
  expect(screen.getByRole('dialog')).toBeTruthy()

  fireEvent.click(await within(withPicture).findByRole('button', { name: 'Remove picture' }))
  fireEvent.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Remove picture' }))
  await waitFor(() => expect(removedPicture()).toBe(true))
  expect(await screen.findByText('Removed the picture of Linked Guest')).toBeTruthy()
  await waitFor(() => expect(within(withPicture).queryByRole('button', { name: 'Remove picture' })).toBeNull())
})
