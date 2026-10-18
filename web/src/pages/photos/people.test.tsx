import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import { PhotoSections, forgetKnownPeople } from './albumControls'
import { FaceRecognitionPreferences, PhotoPeoplePage } from './people'
import { NoAddressBookError, createContact } from '../../photos/people'
import { MergePersonDialog, NamePersonDialog, NamePersonFlow } from './personControls'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))
// Whether the person has contacts, which is what lets them name a person.
let isContactsEnabled = false
vi.mock('../../session', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../session')>()),
  useSession: () => ({
    userId: 'person-1',
    services: { isPhotosEnabled: true, isContactsEnabled },
    permissions: { everywhere: ['photos:use', 'contacts:use'], byDomain: [] },
  }),
}))

beforeEach(() => {
  isContactsEnabled = false
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
  isOwnContact: false,
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

function Address() {
  const location = useLocation()
  return <output data-testid="address">{location.pathname + location.search}</output>
}

const showPeoplePage = (entry = '/photos/people') =>
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter initialEntries={[entry]}>
          <Address />
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

it('says "You" after the name of the person who is the reader, and after nobody else', async () => {
  serve([group({ id: 'group-own', contactName: 'Sample Reader', isOwnContact: true }), group({})])
  showPeoplePage()
  const own = await screen.findByRole('link', { name: /Sample Reader/ })
  expect(within(own).getByText('You').classList.contains('muted')).toBe(true)
  // After the name, on the same line.
  const nameLine = within(own).getByText('Sample Reader').parentElement!
  expect(nameLine.textContent).toBe('Sample ReaderYou')
  expect(within(screen.getByRole('link', { name: /Sample Person/ })).queryByText('You')).toBeNull()
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

it('keeps Show hidden in the address, so that a reload and a copied link keep it', async () => {
  serve([group({}), group({ id: 'group-2', contactName: 'Hidden Person', isHidden: true })])
  showPeoplePage()
  await screen.findByRole('link', { name: /Sample Person/ })
  fireEvent.click(screen.getByLabelText('Show hidden people'))
  await screen.findByRole('link', { name: /Hidden Person/ })
  expect(screen.getByTestId('address').textContent).toBe('/photos/people?hidden=1')
  fireEvent.click(screen.getByLabelText('Show hidden people'))
  await waitFor(() => expect(screen.queryByRole('link', { name: /Hidden Person/ })).toBeNull())
  expect(screen.getByTestId('address').textContent).toBe('/photos/people')
  cleanup()
  // A page opened on the address shows them, with the box ticked.
  showPeoplePage('/photos/people?hidden=1')
  expect(await screen.findByRole('link', { name: /Hidden Person/ })).toBeTruthy()
  expect((screen.getByLabelText('Show hidden people') as HTMLInputElement).checked).toBe(true)
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

function serveContacts(contacts: { id: string; name: string }[], books = 1) {
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ListAddressBooks')) {
      return { ListAddressBooks: Array.from({ length: books }, (_, index) => ({ id: `book-${index + 1}` })) } as never
    }
    if (document.includes('ListContacts')) {
      const query = String(variables?.query ?? '').toLowerCase()
      return {
        ListContacts: contacts
          .filter((contact) => contact.name.toLowerCase().includes(query))
          .slice(0, Number(variables?.first ?? contacts.length))
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

const manyContacts = (count: number) =>
  Array.from({ length: count }, (_, index) => ({
    id: `contact-${index}`,
    name: `Person ${String(index).padStart(2, '0')}`,
  }))
const showNamingDialog = () =>
  render(
    <TranslationProvider>
      <ToastProvider>
        <NamePersonDialog currentName="" onSubmit={vi.fn()} onClose={vi.fn()} />
      </ToastProvider>
    </TranslationProvider>,
  )

it('says the first 20 contacts are listed only when a book had more than that', async () => {
  serveContacts(manyContacts(21))
  showNamingDialog()
  expect(await screen.findByText('The first 20 contacts are listed. Type to narrow the list.')).toBeTruthy()
  expect(screen.getAllByRole('radio')).toHaveLength(20)
  cleanup()
  // A book with exactly 20 has nothing more to type for, and neither do two books of 11.
  serveContacts(manyContacts(20))
  showNamingDialog()
  await screen.findByLabelText('Person 19')
  expect(screen.queryByText(/The first 20 contacts are listed/)).toBeNull()
  cleanup()
  serveContacts(manyContacts(11), 2)
  showNamingDialog()
  await waitFor(() => expect(screen.getAllByRole('radio')).toHaveLength(22))
  expect(screen.queryByText(/The first 20 contacts are listed/)).toBeNull()
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
  expect(marked.textContent).toContain('Already names another person; choosing it merges the two')
  const plain = screen.getByRole('radio', { name: /Other Person/ }).closest('label') as HTMLElement
  expect(plain.textContent).not.toContain('Already names')
})

it('keeps the room for the Name button on every tile, so that the tiles of a row are one height', async () => {
  isContactsEnabled = true
  serve([group({}), group({ id: 'group-2', contactId: '', contactName: '' })])
  showPeoplePage()
  await screen.findByRole('link', { name: /Sample Person/ })
  const tiles = Array.from(document.querySelectorAll('.photo-person-cell'))
  expect(tiles).toHaveLength(2)
  for (const tile of tiles) expect(tile.querySelector('.photo-person-foot')).not.toBeNull()
  expect(screen.getAllByRole('button', { name: 'Name' })).toHaveLength(1)
  expect(tiles[0].querySelector('.photo-person-foot')?.querySelector('button')).toBeNull()
})

// The answer naming a group gives: the group left, the group that went into
// it, and whether the contact was given a picture.
const naming = (overrides: Record<string, unknown> = {}) => ({
  photoFaceGroup: group({ id: 'group-1' }),
  mergedPhotoFaceGroupId: '',
  isContactPictureSet: false,
  ...overrides,
})

function serveNaming(answer: unknown) {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('NamePhotoFaceGroup')) return { NamePhotoFaceGroup: answer } as never
    if (document.includes('RemoveContactPicture'))
      return { RemoveContactPicture: { id: 'contact-1', hasPhoto: false } } as never
    if (document.includes('ListPhotoFaceGroups')) return { ListPhotoFaceGroups: [] } as never
    if (document.includes('ListAddressBooks')) return { ListAddressBooks: [{ id: 'book-1' }] } as never
    if (document.includes('ListContacts'))
      return { ListContacts: [{ id: 'contact-1', name: 'Sample Person', organization: '', emails: [] }] } as never
    throw new Error(`unexpected document ${document}`)
  })
}

function showNamingFlow(entry: string, onNamed = vi.fn()) {
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter initialEntries={[entry]}>
          <Address />
          <Routes>
            <Route
              path="/photos/people/:photoFaceGroupId"
              element={
                <NamePersonFlow
                  group={group({ id: 'group-2', contactName: '' }) as never}
                  onClose={vi.fn()}
                  onNamed={onNamed}
                />
              }
            />
            <Route
              path="/photos/people/:photoFaceGroupId/:photoId"
              element={
                <NamePersonFlow
                  group={group({ id: 'group-2', contactName: '' }) as never}
                  onClose={vi.fn()}
                  onNamed={onNamed}
                />
              }
            />
            <Route path="/contacts" element={<p>The contacts page</p>} />
          </Routes>
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
  return onNamed
}

async function chooseSamplePerson() {
  const dialog = screen.getByRole('dialog')
  fireEvent.click(await within(dialog).findByLabelText('Sample Person'))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
}

it('says "Merged with" and goes to the group that is left when the open person was merged away', async () => {
  serveNaming(naming({ mergedPhotoFaceGroupId: 'group-2' }))
  const onNamed = showNamingFlow('/photos/people/group-2/photo-9')
  await chooseSamplePerson()
  expect(await screen.findByText('Merged with Sample Person')).toBeTruthy()
  await waitFor(() => expect(screen.getByTestId('address').textContent).toBe('/photos/people/group-1/photo-9'))
  expect(onNamed).toHaveBeenCalledWith(expect.objectContaining({ mergedPhotoFaceGroupId: 'group-2' }))
})

it('stays where it is when the merged group is not the one that is open', async () => {
  serveNaming(naming({ mergedPhotoFaceGroupId: 'group-7' }))
  showNamingFlow('/photos/people/group-2')
  await chooseSamplePerson()
  expect(await screen.findByText('Merged with Sample Person')).toBeTruthy()
  expect(screen.getByTestId('address').textContent).toBe('/photos/people/group-2')
})

it('says the person is now named when nothing was merged', async () => {
  serveNaming(naming())
  showNamingFlow('/photos/people/group-2')
  await chooseSamplePerson()
  expect(await screen.findByText('This person is now Sample Person.')).toBeTruthy()
  expect(screen.queryByText(/Used this face/)).toBeNull()
})

it("says a face was used as the contact's picture, and lets it be changed or removed", async () => {
  serveNaming(naming({ isContactPictureSet: true }))
  showNamingFlow('/photos/people/group-2')
  await chooseSamplePerson()
  expect(await screen.findByText('Used this face as their picture')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Remove' }))
  expect(await screen.findByText('Removed their picture')).toBeTruthy()
  expect(
    vi
      .mocked(graphql)
      .mock.calls.some(
        ([document, variables]) => document.includes('RemoveContactPicture') && variables?.id === 'contact-1',
      ),
  ).toBe(true)
})

it('opens the contact from the toast about its picture', async () => {
  serveNaming(naming({ isContactPictureSet: true }))
  showNamingFlow('/photos/people/group-2')
  await chooseSamplePerson()
  await screen.findByText('Used this face as their picture')
  fireEvent.click(screen.getByRole('button', { name: 'Change' }))
  await waitFor(() => expect(screen.getByTestId('address').textContent).toBe('/contacts?contact=contact-1'))
})
