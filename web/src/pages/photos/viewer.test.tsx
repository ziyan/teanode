import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { graphql } from '../../api'
import { TranslationProvider } from '../../i18n/i18n'
import type { Photo, PhotoLibrary } from '../../photos/api'
import { PhotoViewer } from './viewer'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))

const photo = {
  id: 'photo-1',
  createdAt: '2030-01-01T00:00:00Z',
  takenAt: '2030-07-12T09:00:00Z',
  takenAtOffsetMinutes: 0,
  photoKind: 'picture',
  photoProcessingState: 'ready',
  originalFilename: 'one.jpg',
  smallPreviewUrl: '/small',
  largePreviewUrl: '/large',
  videoPreviewUrl: '',
  durationMS: 0,
  isFavorite: false,
  processingErrorMessage: '',
  placeName: '',
  cameraMake: '',
  cameraModel: '',
  widthPixels: 0,
  heightPixels: 0,
} as Photo

beforeEach(() => {
  // A wide window, where the details sit beside the picture.
  vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener() {}, removeEventListener() {} }))
  vi.mocked(graphql).mockReset()
  vi.mocked(graphql).mockResolvedValue({
    ReadPhoto: { photo, photoFiles: [], albums: [{ id: 'album-1', albumTitle: 'Picnic' }] },
  } as never)
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const describedPhoto = {
  ...photo,
  photoDescription: 'A red kite over a field.',
  descriptionVisibleText: 'KITE FESTIVAL',
  describedAt: '2030-07-13T10:00:00Z',
  isDescriptionRequested: false,
}
const undescribedPhoto = {
  ...photo,
  photoDescription: '',
  descriptionVisibleText: '',
  describedAt: null,
  isDescriptionRequested: false,
}
const library = (overrides: Partial<PhotoLibrary> = {}) =>
  ({ isDescriptionAvailable: true, descriptionUnavailableMessage: '', ...overrides }) as PhotoLibrary
const serveRead = (read: Record<string, unknown>) =>
  vi.mocked(graphql).mockResolvedValue({
    ReadPhoto: { photoFiles: [], albums: [], faces: [], ...read },
  } as never)

const show = (onClose = vi.fn(), extra: Partial<React.ComponentProps<typeof PhotoViewer>> = {}) => {
  render(
    <TranslationProvider>
      <MemoryRouter>
        <PhotoViewer
          photo={photo}
          previousPhoto={null}
          nextPhoto={null}
          isTrashed={false}
          isPaused={false}
          onNavigate={vi.fn()}
          onClose={onClose}
          onToggleFavorite={vi.fn()}
          onTrash={vi.fn()}
          onRestore={vi.fn()}
          onDeleteForever={vi.fn()}
          onAddToAlbum={vi.fn()}
          onNewAlbum={vi.fn()}
          {...extra}
        />
      </MemoryRouter>
    </TranslationProvider>,
  )
  return onClose
}

it('names the albums the photo is in, each a link to the album', async () => {
  show()
  const link = await screen.findByRole('link', { name: 'Picnic' })
  expect(link.getAttribute('href')).toBe('/photos/albums/album-1')
})

it('takes Escape before a listener that was on the page first, such as the agent drawer', async () => {
  let wasTakenWhenHeard: boolean | null = null
  // On the document before the viewer is there, as the drawer's is.
  const earlier = (event: KeyboardEvent) => {
    if (event.key === 'Escape') wasTakenWhenHeard = event.defaultPrevented
  }
  document.addEventListener('keydown', earlier)
  const onClose = show()
  await waitFor(() => expect(screen.getByRole('dialog', { name: 'Photo viewer' })).toBeTruthy())
  fireEvent.keyDown(document.body, { key: 'Escape' })
  document.removeEventListener('keydown', earlier)
  expect(onClose).toHaveBeenCalledTimes(1)
  expect(wasTakenWhenHeard).toBe(true)
})

it('leaves Escape to an open menu', async () => {
  const onClose = show()
  fireEvent.click(await screen.findByRole('button', { name: 'Add to album' }))
  await screen.findByRole('menu')
  fireEvent.keyDown(document.body, { key: 'Escape' })
  expect(onClose).not.toHaveBeenCalled()
})

it('shows the description and the words read in the picture, with the day it was written', async () => {
  serveRead({ photo: describedPhoto })
  show(vi.fn(), { photoLibrary: library(), onDescribe: vi.fn() })
  expect(await screen.findByText('A red kite over a field.')).toBeTruthy()
  expect(screen.getByText('KITE FESTIVAL')).toBeTruthy()
  expect(screen.getByText('Described on July 13, 2030')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Describe again' })).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Describe' })).toBeNull()
})

it('offers Describe for a photo nobody described, and asks for it', async () => {
  serveRead({ photo: undescribedPhoto })
  const onDescribe = vi.fn().mockResolvedValue(undefined)
  show(vi.fn(), { photoLibrary: library(), onDescribe })
  fireEvent.click(await screen.findByRole('button', { name: 'Describe' }))
  await waitFor(() => expect(onDescribe).toHaveBeenCalledWith(expect.objectContaining({ id: 'photo-1' }), false))
})

it('asks for a new description with Describe again', async () => {
  serveRead({ photo: describedPhoto })
  const onDescribe = vi.fn().mockResolvedValue(undefined)
  show(vi.fn(), { photoLibrary: library(), onDescribe })
  fireEvent.click(await screen.findByRole('button', { name: 'Describe again' }))
  await waitFor(() => expect(onDescribe).toHaveBeenCalledWith(expect.objectContaining({ id: 'photo-1' }), true))
})

it("says why a photo cannot be described, in the server's words, and offers nothing to press", async () => {
  serveRead({ photo: undescribedPhoto })
  show(vi.fn(), {
    photoLibrary: library({ isDescriptionAvailable: false, descriptionUnavailableMessage: 'The assistant is off.' }),
    onDescribe: vi.fn(),
  })
  expect(await screen.findByText('The assistant is off.')).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Describe' })).toBeNull()
})

it('reads the photo again every three seconds while a description is on its way', async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true })
  try {
    serveRead({ photo: { ...undescribedPhoto, isDescriptionRequested: true } })
    show(vi.fn(), { photoLibrary: library(), onDescribe: vi.fn() })
    expect(await screen.findByText('Describing…')).toBeTruthy()
    const reads = () => vi.mocked(graphql).mock.calls.length
    const before = reads()
    await vi.advanceTimersByTimeAsync(3100)
    expect(reads()).toBe(before + 1)
    serveRead({ photo: describedPhoto })
    await vi.advanceTimersByTimeAsync(3100)
    expect(await screen.findByText('A red kite over a field.')).toBeTruthy()
    expect(screen.queryByText('Describing…')).toBeNull()
  } finally {
    vi.useRealTimers()
  }
})

it('stops looking for a description after two minutes and says it goes on in the background', async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true })
  try {
    serveRead({ photo: { ...undescribedPhoto, isDescriptionRequested: true } })
    show(vi.fn(), { photoLibrary: library(), onDescribe: vi.fn() })
    await screen.findByText('Describing…')
    await vi.advanceTimersByTimeAsync(2 * 60 * 1000 + 3100)
    expect(await screen.findByText(/taking longer than usual/)).toBeTruthy()
    const reads = vi.mocked(graphql).mock.calls.length
    await vi.advanceTimersByTimeAsync(10_000)
    expect(vi.mocked(graphql).mock.calls.length).toBe(reads)
  } finally {
    vi.useRealTimers()
  }
})

it('opens the assistant on this photo', async () => {
  serveRead({ photo: describedPhoto })
  const onAskAssistant = vi.fn()
  show(vi.fn(), { photoLibrary: library(), onAskAssistant })
  fireEvent.click(await screen.findByRole('button', { name: 'Ask the assistant about this photo' }))
  expect(onAskAssistant).toHaveBeenCalledWith(expect.objectContaining({ id: 'photo-1' }))
})

const face = (overrides: Record<string, unknown>) => ({
  id: 'face-1',
  photoId: 'photo-1',
  boxLeftFraction: 0.1,
  boxTopFraction: 0.1,
  boxWidthFraction: 0.2,
  boxHeightFraction: 0.2,
  photoFaceGroupId: 'group-1',
  contactId: 'contact-1',
  contactName: 'Sample Person',
  facePictureUrl: '/face-1',
  ...overrides,
})

it('lists the people in the photo, each a link to the person, and takes a face out of a group', async () => {
  serveRead({
    photo: describedPhoto,
    faces: [face({}), face({ id: 'face-2', photoFaceGroupId: 'group-2', contactName: '', contactId: '' })],
  })
  const onRemoveFaceFromGroup = vi.fn().mockResolvedValue(undefined)
  show(vi.fn(), { photoLibrary: library(), onRemoveFaceFromGroup })
  const link = await screen.findByRole('link', { name: 'Sample Person' })
  expect(link.getAttribute('href')).toBe('/photos/people/group-1')
  expect(screen.getByRole('link', { name: 'Unnamed person' }).getAttribute('href')).toBe('/photos/people/group-2')
  fireEvent.click(screen.getByRole('button', { name: 'Not Sample Person' }))
  await waitFor(() => expect(onRemoveFaceFromGroup).toHaveBeenCalledWith(expect.objectContaining({ id: 'face-1' })))
  expect(screen.getByRole('button', { name: 'Not this person' })).toBeTruthy()
})

it('leaves out a face that is in no group, which has no person to link to', async () => {
  serveRead({ photo: describedPhoto, faces: [face({ photoFaceGroupId: '' })] })
  show(vi.fn(), { photoLibrary: library() })
  await screen.findByText('A red kite over a field.')
  expect(screen.queryByText('People')).toBeNull()
})
