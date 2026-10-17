import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import { AddToAlbumMenu } from './albumControls'
import { PhotoAlbumsPage } from './albums'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))
vi.mock('../../session', () => ({ useSession: () => ({ userId: 'person-1' }) }))

beforeEach(() => {
  vi.mocked(graphql).mockReset()
})
afterEach(cleanup)

const album = (overrides: Record<string, unknown>) => ({
  id: 'album-1',
  albumTitle: 'Picnic',
  createdAt: '2030-07-14T00:00:00Z',
  updatedAt: '2030-07-14T00:00:00Z',
  coverPhotoId: null,
  photoCount: 3,
  firstTakenAt: '2030-07-12T09:00:00Z',
  lastTakenAt: '2030-07-14T09:00:00Z',
  coverPreviewUrl: '/cover-1',
  ownerUserId: 'person-1',
  circleId: null,
  ownerUsername: 'ana',
  ownerName: 'Ana',
  ownerAvatarUrl: null,
  ownerAvatarColorIndex: 0,
  circleName: '',
  isOwnedByCaller: true,
  shareLevel: '',
  canCallerContribute: true,
  canCallerManage: true,
  isLocationIncluded: true,
  ...overrides,
})

const suggestion = {
  takenAfter: '2030-08-01T08:00:00Z',
  takenBefore: '2030-08-03T20:00:00Z',
  placeNames: ['Fixtureville'],
  albumTitle: 'Fixtureville',
  photoCount: 2,
  samplePhotoIds: ['photo-1', 'photo-2'],
}

function serve(suggestions = [suggestion]) {
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ListPhotoAlbums'))
      return {
        ListPhotoAlbums: [album({}), album({ id: 'album-2', albumTitle: 'Hike', coverPreviewUrl: '' })],
      } as never
    if (document.includes('ListCircles')) return { ListCircles: [] } as never
    if (document.includes('SuggestPhotoAlbums')) return { SuggestPhotoAlbums: suggestions } as never
    if (document.includes('sample0:'))
      return {
        sample0: { photo: { id: 'photo-1', smallPreviewUrl: '/small-1' } },
        sample1: { photo: { id: 'photo-2', smallPreviewUrl: '/small-2' } },
      } as never
    if (document.includes('ListPhotos(')) {
      expect(variables?.takenAfter).toBe(suggestion.takenAfter)
      expect(variables?.takenBefore).toBe(suggestion.takenBefore)
      return { ListPhotos: [{ id: 'photo-1' }, { id: 'photo-2' }] } as never
    }
    if (document.includes('CreatePhotoAlbum'))
      return { CreatePhotoAlbum: album({ id: 'album-3', albumTitle: 'Trip' }) } as never
    throw new Error(`unexpected document ${document}`)
  })
}

const showPage = () =>
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter initialEntries={['/photos/albums']}>
          <Routes>
            <Route path="/photos/albums" element={<PhotoAlbumsPage />} />
            <Route path="/photos/albums/:photoAlbumId" element={<p>The album page</p>} />
          </Routes>
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )

it('shows each album with its title, its photo count and the dates it spans', async () => {
  serve([])
  showPage()
  const link = await screen.findByRole('link', { name: /Picnic/ })
  expect(link.getAttribute('href')).toBe('/photos/albums/album-1')
  expect(link.textContent).toContain('3 photos')
  expect(link.textContent).toContain('Jul 12')
  expect(screen.getByRole('link', { name: /Hike/ })).toBeTruthy()
  expect(screen.queryByText('Suggested')).toBeNull()
})

it('makes an album from a suggestion with the title the person confirms and every photo of its dates', async () => {
  serve()
  showPage()
  const panel = (await screen.findByText('Suggested')).closest('section') as HTMLElement
  expect(within(panel).getByText('2 photos', { exact: false })).toBeTruthy()
  fireEvent.click(within(panel).getByRole('button', { name: 'Create album' }))

  const field = (await screen.findByLabelText('Title')) as HTMLInputElement
  expect(field.value).toBe('Fixtureville')
  fireEvent.change(field, { target: { value: '  Trip  ' } })
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Create' }))

  await screen.findByText('The album page')
  const created = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('CreatePhotoAlbum'))
  expect(created?.[1]).toEqual({ albumTitle: 'Trip', photoIds: ['photo-1', 'photo-2'], circleId: null })
})

it('says the places of a suggestion once: in its title, or under the dates when the title is dates', async () => {
  serve([
    suggestion,
    {
      ...suggestion,
      takenAfter: '2030-09-01T08:00:00Z',
      takenBefore: '2030-09-03T20:00:00Z',
      placeNames: ['Exampleton'],
      albumTitle: 'September 1 to 3, 2030',
    },
  ])
  showPage()
  const panel = (await screen.findByText('Suggested')).closest('section') as HTMLElement
  // The place that is the title is not said again under the dates.
  expect(within(panel).getAllByText('Fixtureville')).toHaveLength(1)
  // A title of dates keeps the line that names where.
  expect(within(panel).getByText('Exampleton')).toBeTruthy()
})

it('does not make an album when the title is blank', async () => {
  serve()
  showPage()
  fireEvent.click(await screen.findByRole('button', { name: 'New album' }))
  const dialog = await screen.findByRole('dialog')
  expect(within(dialog).getByRole('button', { name: 'Create' }).hasAttribute('disabled')).toBe(true)
  fireEvent.change(within(dialog).getByLabelText('Title'), { target: { value: 'Hike' } })
  await waitFor(() =>
    expect(within(dialog).getByRole('button', { name: 'Create' }).hasAttribute('disabled')).toBe(false),
  )
})

it('lists the other albums in the add menu, newest change first, with New album at its head', async () => {
  serve([])
  const onChooseAlbum = vi.fn()
  const onNewAlbum = vi.fn()
  render(
    <TranslationProvider>
      <AddToAlbumMenu hiddenAlbumId="album-2" onChooseAlbum={onChooseAlbum} onNewAlbum={onNewAlbum} />
    </TranslationProvider>,
  )
  fireEvent.click(screen.getByRole('button', { name: 'Add to album' }))
  const items = await screen.findAllByRole('menuitem')
  await waitFor(() => expect(screen.getAllByRole('menuitem').length).toBe(2))
  expect(screen.getAllByRole('menuitem')[0].textContent).toBe('New album…')
  expect(items[0].textContent).toBe('New album…')
  expect(screen.queryByText('Hike')).toBeNull()
  fireEvent.click(screen.getByRole('menuitem', { name: /Picnic/ }))
  expect(onChooseAlbum).toHaveBeenCalledWith(expect.objectContaining({ id: 'album-1' }))
  expect(onNewAlbum).not.toHaveBeenCalled()
})

// ---- albums of circles, and albums shared with the person ----

const circleAlbum = album({
  id: 'album-3',
  albumTitle: 'Holiday',
  ownerUserId: null,
  circleId: 'circle-1',
  ownerUsername: '',
  ownerName: '',
  circleName: 'Family',
  isOwnedByCaller: false,
  shareLevel: 'contribute',
  canCallerManage: false,
})
const sharedAlbum = album({
  id: 'album-4',
  albumTitle: 'Wedding',
  ownerUserId: 'person-2',
  ownerUsername: 'ben',
  ownerName: 'Ben',
  isOwnedByCaller: false,
  shareLevel: 'view',
  canCallerContribute: false,
  canCallerManage: false,
})

function serveSharing() {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('ListPhotoAlbums')) return { ListPhotoAlbums: [album({}), circleAlbum, sharedAlbum] } as never
    if (document.includes('ListCircles')) return { ListCircles: [{ id: 'circle-1', name: 'Family' }] } as never
    if (document.includes('SuggestPhotoAlbums')) return { SuggestPhotoAlbums: [] } as never
    if (document.includes('CreatePhotoAlbum'))
      return { CreatePhotoAlbum: album({ id: 'album-5', albumTitle: 'Trip' }) } as never
    throw new Error(`unexpected document ${document}`)
  })
}

it('lists the person’s albums, then their circles’, then those shared with them, each marked with whose it is', async () => {
  serveSharing()
  showPage()
  expect(await screen.findByRole('heading', { name: 'Your albums' })).toBeTruthy()
  const circles = screen.getByRole('heading', { name: 'Circle albums' }).closest('section') as HTMLElement
  expect(within(circles).getByRole('link', { name: /Holiday/ }).textContent).toContain('Family')
  const shared = screen.getByRole('heading', { name: 'Shared with you' }).closest('section') as HTMLElement
  expect(within(shared).getByRole('link', { name: /Wedding/ }).textContent).toContain('Ben')
  // The person's own albums carry no mark of an owner.
  const own = screen.getByRole('heading', { name: 'Your albums' }).closest('section') as HTMLElement
  expect(within(own).getByRole('link', { name: /Picnic/ }).textContent).not.toContain('Ana')
})

it('names no sections when every album is the person’s own', async () => {
  serve([])
  showPage()
  await screen.findByRole('link', { name: /Picnic/ })
  expect(screen.queryByRole('heading', { name: 'Your albums' })).toBeNull()
})

it('offers to make the album for a circle, and sends the circle with the title', async () => {
  serveSharing()
  showPage()
  fireEvent.click(await screen.findByRole('button', { name: 'New album' }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.change(within(dialog).getByLabelText('Title'), { target: { value: 'Trip' } })
  fireEvent.click(await within(dialog).findByRole('combobox', { name: 'Whose album' }))
  fireEvent.click(await screen.findByRole('option', { name: 'For Family' }))
  expect(within(dialog).getByText('Everyone in Family can see it and add their own photos.')).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Create' }))
  await screen.findByText('The album page')
  const created = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('CreatePhotoAlbum'))
  expect(created?.[1]).toEqual({ albumTitle: 'Trip', photoIds: [], circleId: 'circle-1' })
})

it('does not offer an album that is only to view in the add menu', async () => {
  serveSharing()
  render(
    <TranslationProvider>
      <AddToAlbumMenu onChooseAlbum={vi.fn()} onNewAlbum={vi.fn()} />
    </TranslationProvider>,
  )
  fireEvent.click(screen.getByRole('button', { name: 'Add to album' }))
  await screen.findByRole('menuitem', { name: /Holiday/ })
  // The circle's album says whose it is; the album shared to view is not there.
  expect(screen.getByRole('menuitem', { name: /Holiday/ }).textContent).toContain('Family')
  expect(screen.queryByRole('menuitem', { name: /Wedding/ })).toBeNull()
})

it('lets a suggestion be put off, and keeps it off for this person in this browser', async () => {
  window.localStorage.clear()
  serve()
  const first = showPage()
  const panel = (await screen.findByText('Suggested')).closest('section') as HTMLElement
  fireEvent.click(within(panel).getByRole('button', { name: 'Fixtureville: Not now' }))
  expect(screen.queryByText('Suggested')).toBeNull()
  expect(window.localStorage.getItem('teanode.photos.dismissedSuggestions.person-1')).toBe(
    JSON.stringify([`${suggestion.takenAfter}|${suggestion.takenBefore}`]),
  )
  first.unmount()
  showPage()
  await screen.findByRole('link', { name: /Picnic/ })
  expect(screen.queryByText('Suggested')).toBeNull()
  window.localStorage.clear()
})
