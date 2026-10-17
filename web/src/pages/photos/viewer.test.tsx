import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { graphql } from '../../api'
import { TranslationProvider } from '../../i18n/i18n'
import type { Photo } from '../../photos/api'
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

const show = (onClose = vi.fn()) => {
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
