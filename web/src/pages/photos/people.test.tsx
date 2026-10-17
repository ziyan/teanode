import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import { PhotoSections, forgetKnownPeople } from './albumControls'
import { FaceRecognitionPreferences, PhotoPeoplePage } from './people'
import { NoAddressBookError, createContact } from '../../photos/people'
import { MergePersonDialog, NamePersonDialog } from './personControls'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))
vi.mock('../../session', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../session')>()),
  useSession: () => ({
    userId: 'person-1',
    services: { isPhotosEnabled: true },
    permissions: { everywhere: ['photos:use'], byDomain: [] },
  }),
}))

beforeEach(() => {
  vi.mocked(graphql).mockReset()
  forgetKnownPeople()
})
afterEach(cleanup)

const group = (overrides: Record<string, unknown>) => ({
  id: 'group-1',
  createdAt: '2030-07-14T00:00:00Z',
  updatedAt: '2030-07-14T00:00:00Z',
  contactId: 'contact-1',
  contactAddressBookId: 'book-1',
  contactName: 'Sample Person',
  faceCount: 5,
  photoCount: 4,
  facePictureUrl: '/face-1',
  coverPhotoFaceId: 'face-1',
  isHidden: false,
  ...overrides,
})

const photoLibrary = (overrides: Record<string, unknown> = {}) => ({
  isFaceRecognitionAvailable: true,
  faceRecognitionUnavailableMessage: '',
  isPhotoFaceRecognitionEnabled: true,
  ...overrides,
})

function serve(groups: unknown[], library = photoLibrary()) {
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ReadPhotoLibrary')) return { ReadPhotoLibrary: library } as never
    if (document.includes('ListPhotoFaceGroups')) {
      const all = groups as { isHidden: boolean }[]
      return { ListPhotoFaceGroups: variables?.isHiddenIncluded ? all : all.filter((each) => !each.isHidden) } as never
    }
    if (document.includes('SetPhotoFaceRecognition')) return { SetPhotoFaceRecognition: variables?.isEnabled } as never
    throw new Error(`unexpected document ${document}`)
  })
}

const showPeoplePage = () =>
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter initialEntries={['/photos/people']}>
          <Routes>
            <Route path="/photos/people" element={<PhotoPeoplePage />} />
            <Route path="/photos/people/:photoFaceGroupId" element={<p>The person page</p>} />
            <Route path="/photos/albums" element={<p>The albums page</p>} />
          </Routes>
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )

it('shows each person with the name of their contact, or an invitation to name them', async () => {
  serve([group({}), group({ id: 'group-2', contactId: '', contactName: '', photoCount: 1 })])
  showPeoplePage()
  const named = await screen.findByRole('link', { name: /Sample Person/ })
  expect(named.getAttribute('href')).toBe('/photos/people/group-1')
  expect(named.textContent).toContain('4 photos')
  const unnamed = screen.getByRole('link', { name: /Unnamed person/ })
  expect(unnamed.getAttribute('href')).toBe('/photos/people/group-2')
  expect(unnamed.textContent).toContain('1 photo')
})

it('lists the hidden people only after Show hidden is chosen', async () => {
  serve([group({}), group({ id: 'group-2', contactName: 'Hidden Person', isHidden: true })])
  showPeoplePage()
  await screen.findByRole('link', { name: /Sample Person/ })
  expect(screen.queryByRole('link', { name: /Hidden Person/ })).toBeNull()
  fireEvent.click(screen.getByLabelText('Show hidden people'))
  expect(await screen.findByRole('link', { name: /Hidden Person/ })).toBeTruthy()
  const listed = vi.mocked(graphql).mock.calls.filter(([document]) => document.includes('ListPhotoFaceGroups'))
  expect(listed.some(([, variables]) => variables?.isHiddenIncluded === true)).toBe(true)
})

it("says that finding people is not set up in a sentence of its own, not in the server's words", async () => {
  serve(
    [],
    photoLibrary({
      isFaceRecognitionAvailable: false,
      faceRecognitionUnavailableMessage: 'No face reader is installed.',
    }),
  )
  showPeoplePage()
  expect(await screen.findByText('Finding people is not set up on this server. Ask whoever runs it.')).toBeTruthy()
  expect(screen.queryByText('No face reader is installed.')).toBeNull()
})

it('offers the switch on the page while it is off, and turns it on', async () => {
  serve([], photoLibrary({ isPhotoFaceRecognitionEnabled: false }))
  showPeoplePage()
  const box = (await screen.findByLabelText('Find people in my photos')) as HTMLInputElement
  expect(box.checked).toBe(false)
  fireEvent.click(box)
  await waitFor(() => {
    const call = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('SetPhotoFaceRecognition'))
    expect(call?.[1]).toEqual({ isEnabled: true })
  })
})

it('asks before turning faces off, because it deletes the faces found and their names', async () => {
  serve([], photoLibrary())
  render(
    <TranslationProvider>
      <ToastProvider>
        <FaceRecognitionPreferences />
      </ToastProvider>
    </TranslationProvider>,
  )
  const box = (await screen.findByLabelText('Find people in my photos')) as HTMLInputElement
  expect(box.checked).toBe(true)
  fireEvent.click(box)
  const dialog = await screen.findByRole('alertdialog')
  expect(within(dialog).getByText(/deletes every person found/)).toBeTruthy()
  expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('SetPhotoFaceRecognition'))).toBe(false)
  fireEvent.click(within(dialog).getByRole('button', { name: 'Turn off and delete' }))
  await waitFor(() => {
    const call = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('SetPhotoFaceRecognition'))
    expect(call?.[1]).toEqual({ isEnabled: false })
  })
})

it('offers People in the choice when faces can be found, and leaves it out when they cannot and nobody is listed', async () => {
  serve([], photoLibrary())
  const first = render(
    <TranslationProvider>
      <PhotoSections current="library" onSelect={vi.fn()} />
    </TranslationProvider>,
  )
  expect(await screen.findByRole('button', { name: 'People' })).toBeTruthy()
  first.unmount()

  serve([], photoLibrary({ isFaceRecognitionAvailable: false }))
  render(
    <TranslationProvider>
      <PhotoSections current="library" onSelect={vi.fn()} />
    </TranslationProvider>,
  )
  await waitFor(() =>
    expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('ListPhotoFaceGroups'))).toBe(true),
  )
  expect(screen.queryByRole('button', { name: 'People' })).toBeNull()
})

it('keeps People in the choice when faces cannot be found but people were found before', async () => {
  serve([group({})], photoLibrary({ isFaceRecognitionAvailable: false }))
  render(
    <TranslationProvider>
      <PhotoSections current="library" onSelect={vi.fn()} />
    </TranslationProvider>,
  )
  expect(await screen.findByRole('button', { name: 'People' })).toBeTruthy()
})

function serveContacts(contacts: { id: string; name: string }[]) {
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ListAddressBooks')) return { ListAddressBooks: [{ id: 'book-1' }] } as never
    if (document.includes('ListContacts')) {
      const query = String(variables?.query ?? '').toLowerCase()
      return {
        ListContacts: contacts
          .filter((contact) => contact.name.toLowerCase().includes(query))
          .map((contact) => ({ ...contact, organization: '', emails: [] })),
      } as never
    }
    throw new Error(`unexpected document ${document}`)
  })
}

it('names a person with a contact chosen from the address book', async () => {
  serveContacts([
    { id: 'contact-1', name: 'Sample Person' },
    { id: 'contact-2', name: 'Other Person' },
  ])
  const onSubmit = vi.fn()
  render(
    <TranslationProvider>
      <ToastProvider>
        <NamePersonDialog currentName="" onSubmit={onSubmit} onClose={vi.fn()} />
      </ToastProvider>
    </TranslationProvider>,
  )
  const dialog = screen.getByRole('dialog')
  expect(within(dialog).getByRole('button', { name: 'Save' }).hasAttribute('disabled')).toBe(true)
  fireEvent.click(await within(dialog).findByLabelText('Other Person'))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  expect(onSubmit).toHaveBeenCalledWith({ kind: 'contact', contactId: 'contact-2' })
})

it('offers a new contact for a name the address book does not have, and not for one it has', async () => {
  serveContacts([{ id: 'contact-1', name: 'Sample Person' }])
  const onSubmit = vi.fn()
  render(
    <TranslationProvider>
      <ToastProvider>
        <NamePersonDialog currentName="" onSubmit={onSubmit} onClose={vi.fn()} />
      </ToastProvider>
    </TranslationProvider>,
  )
  const dialog = screen.getByRole('dialog')
  const field = within(dialog).getByRole('searchbox')
  fireEvent.change(field, { target: { value: '  Someone Else ' } })
  fireEvent.click(await within(dialog).findByLabelText('New contact named “Someone Else”'))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  expect(onSubmit).toHaveBeenCalledWith({ kind: 'new', contactName: 'Someone Else' })

  fireEvent.change(field, { target: { value: 'sample person' } })
  await within(dialog).findByLabelText('Sample Person')
  await waitFor(() => expect(within(dialog).queryByLabelText(/New contact named/)).toBeNull())
})

it('does not save a contact that was chosen and is no longer in the list', async () => {
  serveContacts([
    { id: 'contact-1', name: 'Sample Person' },
    { id: 'contact-2', name: 'Other Person' },
  ])
  const onSubmit = vi.fn()
  render(
    <TranslationProvider>
      <ToastProvider>
        <NamePersonDialog currentName="" onSubmit={onSubmit} onClose={vi.fn()} />
      </ToastProvider>
    </TranslationProvider>,
  )
  const dialog = screen.getByRole('dialog')
  fireEvent.click(await within(dialog).findByLabelText('Other Person'))
  expect(within(dialog).getByRole('button', { name: 'Save' }).hasAttribute('disabled')).toBe(false)
  // A search that no longer lists the chosen contact leaves nothing chosen.
  fireEvent.change(within(dialog).getByRole('searchbox'), { target: { value: 'sample' } })
  await waitFor(() => expect(within(dialog).queryByLabelText('Other Person')).toBeNull())
  expect(within(dialog).getByRole('button', { name: 'Save' }).hasAttribute('disabled')).toBe(true)
  // Searching for it again does not choose it again by itself.
  fireEvent.change(within(dialog).getByRole('searchbox'), { target: { value: '' } })
  expect(((await within(dialog).findByLabelText('Other Person')) as HTMLInputElement).checked).toBe(false)
  expect(onSubmit).not.toHaveBeenCalled()
})

it('says that there is no address book with an error the page can put in its own words', async () => {
  vi.mocked(graphql).mockResolvedValue({ ListAddressBooks: [] } as never)
  await expect(createContact('Someone')).rejects.toBeInstanceOf(NoAddressBookError)
})

it('merges with the person chosen, after saying what it does', async () => {
  const onSubmit = vi.fn()
  render(
    <TranslationProvider>
      <ToastProvider>
        <MergePersonDialog
          person={group({}) as never}
          others={
            [group({ id: 'group-2', contactName: '' }), group({ id: 'group-3', contactName: 'Third Person' })] as never
          }
          onSubmit={onSubmit}
          onClose={vi.fn()}
        />
      </ToastProvider>
    </TranslationProvider>,
  )
  const dialog = screen.getByRole('dialog')
  expect(within(dialog).getByRole('button', { name: 'Merge' }).hasAttribute('disabled')).toBe(true)
  fireEvent.click(within(dialog).getByRole('radio', { name: /Third Person/ }))
  expect(within(dialog).getByText('Merge Third Person into Sample Person? This cannot be undone.')).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Merge' }))
  expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ id: 'group-3' }))
})

it('tags a hidden person and offers the way to name one who is nobody yet', async () => {
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ReadPhotoLibrary')) return { ReadPhotoLibrary: photoLibrary() } as never
    if (document.includes('ListPhotoFaceGroups'))
      return {
        ListPhotoFaceGroups: [
          group({ id: 'group-2', contactId: '', contactName: '' }),
          group({ isHidden: true, contactName: 'Hidden Person' }),
        ],
      } as never
    if (document.includes('ListAddressBooks')) return { ListAddressBooks: [{ id: 'book-1' }] } as never
    if (document.includes('ListContacts'))
      return { ListContacts: [{ id: 'contact-1', name: 'Hidden Person', organization: '', emails: [] }] } as never
    throw new Error(`unexpected document ${document} ${JSON.stringify(variables)}`)
  })
  showPeoplePage()
  fireEvent.click(await screen.findByLabelText('Show hidden people'))
  expect(await screen.findByText('Hidden')).toBeTruthy()
})

it('marks a contact that already names another person, and leaves it choosable', async () => {
  serveContacts([
    { id: 'contact-1', name: 'Sample Person' },
    { id: 'contact-2', name: 'Other Person' },
  ])
  render(
    <TranslationProvider>
      <ToastProvider>
        <NamePersonDialog
          currentName=""
          linkedContactIds={new Set(['contact-1'])}
          onSubmit={vi.fn()}
          onClose={vi.fn()}
        />
      </ToastProvider>
    </TranslationProvider>,
  )
  const marked = (await screen.findByRole('radio', { name: /Sample Person/ })).closest('label') as HTMLElement
  expect(marked.textContent).toContain('Already names another person')
  const plain = screen.getByRole('radio', { name: /Other Person/ }).closest('label') as HTMLElement
  expect(plain.textContent).not.toContain('Already names')
})
