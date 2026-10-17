import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { TranslationProvider } from '../../i18n/i18n'
import type { Photo } from '../../photos/api'
import { PhotoGrid } from './grid'

afterEach(cleanup)

const photoFixture = (overrides: Partial<Photo>): Photo =>
  ({
    id: 'photo',
    createdAt: '2030-01-01T00:00:00Z',
    takenAt: '2030-07-12T09:00:00Z',
    takenAtOffsetMinutes: 0,
    photoKind: 'picture',
    photoProcessingState: 'ready',
    originalFilename: 'one.jpg',
    smallPreviewUrl: '/small',
    durationMS: 0,
    isFavorite: false,
    processingErrorMessage: '',
    ...overrides,
  }) as Photo

const show = (photos: Photo[], selectedIds: string[] = []) => {
  const onOpen = vi.fn()
  const onToggle = vi.fn()
  render(
    <TranslationProvider>
      <PhotoGrid photos={photos} selectedIds={new Set(selectedIds)} onOpen={onOpen} onToggle={onToggle} />
    </TranslationProvider>,
  )
  return { onOpen, onToggle }
}

it('puts a heading over each day and the photos of that day under it', () => {
  show([
    photoFixture({ id: 'a', originalFilename: 'a.jpg', takenAt: '2030-07-13T09:00:00Z' }),
    photoFixture({ id: 'b', originalFilename: 'b.jpg' }),
    photoFixture({ id: 'c', originalFilename: 'c.jpg' }),
  ])
  const headings = screen.getAllByRole('heading', { level: 2 })
  expect(headings.map((heading) => heading.textContent)).toEqual(['Saturday, July 13, 2030', 'Friday, July 12, 2030'])
  const secondDay = headings[1].closest('section') as HTMLElement
  expect(within(secondDay).getAllByRole('button')).toHaveLength(2)
})

it('shows a video length, a favorite, a waiting photo and a failed one', () => {
  show([
    photoFixture({ id: 'video', originalFilename: 'clip.mp4', photoKind: 'video', durationMS: 65_000 }),
    photoFixture({ id: 'favorite', isFavorite: true, originalFilename: 'fav.jpg' }),
    photoFixture({ id: 'waiting', photoProcessingState: 'waiting', smallPreviewUrl: '', originalFilename: 'wait.jpg' }),
    photoFixture({
      id: 'failed',
      photoProcessingState: 'failed',
      smallPreviewUrl: '',
      processingErrorMessage: 'not a picture',
      originalFilename: 'bad.jpg',
    }),
  ])
  expect(screen.getByText('Video, 1:05')).toBeTruthy()
  expect(screen.getByRole('img', { name: 'Getting this photo ready' })).toBeTruthy()
  expect(screen.getByTitle('This photo could not be read: not a picture')).toBeTruthy()
})

it('opens a photo when nothing is selected and selects it when something is', () => {
  const { onOpen, onToggle } = show([photoFixture({ id: 'a', originalFilename: 'a.jpg' })])
  fireEvent.click(screen.getByRole('button', { name: 'Open a.jpg' }))
  expect(onOpen).toHaveBeenCalledTimes(1)
  expect(onToggle).not.toHaveBeenCalled()
  cleanup()

  const selecting = show(
    [photoFixture({ id: 'a', originalFilename: 'a.jpg' }), photoFixture({ id: 'b', originalFilename: 'b.jpg' })],
    ['a'],
  )
  fireEvent.click(screen.getByRole('button', { name: 'Open b.jpg' }), { shiftKey: true })
  expect(selecting.onOpen).not.toHaveBeenCalled()
  expect(selecting.onToggle).toHaveBeenCalledWith(1, true)
})

it('selects with the checkbox, and extends a range with shift', () => {
  const { onToggle } = show([
    photoFixture({ id: 'a', originalFilename: 'a.jpg' }),
    photoFixture({ id: 'b', originalFilename: 'b.jpg' }),
  ])
  fireEvent.click(screen.getByRole('checkbox', { name: 'Select a.jpg' }))
  expect(onToggle).toHaveBeenLastCalledWith(0, false)
  fireEvent.click(screen.getByRole('checkbox', { name: 'Select b.jpg' }), { shiftKey: true })
  expect(onToggle).toHaveBeenLastCalledWith(1, true)
})
