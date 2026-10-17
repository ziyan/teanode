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
  expect(created?.[1]).toEqual({ albumTitle: 'Trip', photoIds: ['photo-1', 'photo-2'] })
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
