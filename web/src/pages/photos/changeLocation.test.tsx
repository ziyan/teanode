import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import type { Photo } from '../../photos/api'
import { ChangeLocationDialog } from './changeLocation'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))

const placed = { id: 'photo-1', latitude: 38.72, longitude: -9.14, placeName: 'Lisbon, Portugal' } as Photo
const unplaced = { id: 'photo-2', latitude: null, longitude: null, placeName: '' } as Photo

const lisbon = { placeName: 'Lisbon, Portugal', latitude: 38.72, longitude: -9.14, distanceKilometers: 0 }
const porto = { placeName: 'Porto, Portugal', latitude: 41.15, longitude: -8.61, distanceKilometers: 273.4 }

let places: (query: string) => Promise<unknown[]>

beforeEach(() => {
  places = async () => [lisbon, porto]
  vi.mocked(graphql).mockReset()
  vi.mocked(graphql).mockImplementation((async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('SearchPhotoPlaces(')) return { SearchPhotoPlaces: await places(String(variables?.query)) }
    if (document.includes('SetPhotoLocation(')) return { SetPhotoLocation: (variables?.photoIds as string[]).length }
    if (document.includes('RemovePhotoLocation('))
      return { RemovePhotoLocation: (variables?.photoIds as string[]).length }
    throw new Error(`unexpected document ${document}`)
  }) as never)
})
afterEach(cleanup)

const callsTo = (name: string) =>
  vi
    .mocked(graphql)
    .mock.calls.filter(([document]) => document.includes(`{ ${name}(`))
    .map(([, variables]) => (variables ?? {}) as Record<string, unknown>)

const show = (photos: Photo[]) => {
  const onChanged = vi.fn()
  const onClose = vi.fn()
  render(
    <TranslationProvider>
      <ToastProvider>
        <ChangeLocationDialog photos={photos} onChanged={onChanged} onClose={onClose} />
      </ToastProvider>
    </TranslationProvider>,
  )
  return { onChanged, onClose }
}

const type = (text: string) =>
  fireEvent.change(screen.getByRole('searchbox', { name: 'Search for a city' }), { target: { value: text } })

it('puts the cursor in the search and asks nothing until something is typed', async () => {
  show([placed])
  expect(document.activeElement).toBe(screen.getByRole('searchbox', { name: 'Search for a city' }))
  await new Promise((resolve) => setTimeout(resolve, 400))
  expect(callsTo('SearchPhotoPlaces')).toHaveLength(0)
})

it('asks once typing has paused, near where the photo was taken, and lists the cities with their distance', async () => {
  show([placed])
  type('Lis')
  type('Lisbo')
  expect(callsTo('SearchPhotoPlaces')).toHaveLength(0)
  expect(await screen.findByRole('button', { name: /Porto, Portugal/ })).toBeTruthy()
  expect(callsTo('SearchPhotoPlaces')).toEqual([{ query: 'Lisbo', nearLatitude: 38.72, nearLongitude: -9.14 }])
  expect(screen.getByText('273 km away')).toBeTruthy()
})

it('asks for no nearer city when the photo has no position', async () => {
  show([unplaced])
  type('Lisbon')
  await screen.findByRole('button', { name: /Lisbon, Portugal/ })
  expect(callsTo('SearchPhotoPlaces')).toEqual([{ query: 'Lisbon', nearLatitude: null, nearLongitude: null }])
})

it('ignores the answer to words that are no longer the ones typed', async () => {
  let answerFirst: (found: unknown[]) => void = () => {}
  places = (query) => (query === 'Por' ? new Promise((resolve) => (answerFirst = resolve)) : Promise.resolve([lisbon]))
  show([unplaced])
  type('Por')
  await waitFor(() => expect(callsTo('SearchPhotoPlaces')).toHaveLength(1))
  type('Lisbon')
  expect(await screen.findByRole('button', { name: /Lisbon, Portugal/ })).toBeTruthy()
  answerFirst([porto])
  await new Promise((resolve) => setTimeout(resolve, 20))
  expect(screen.queryByRole('button', { name: /Porto, Portugal/ })).toBeNull()
})

it('sets the chosen city on every photo, says so, and closes', async () => {
  const { onChanged, onClose } = show([placed, unplaced])
  type('Por')
  fireEvent.click(await screen.findByRole('button', { name: /Porto, Portugal/ }))
  await waitFor(() => expect(onClose).toHaveBeenCalled())
  expect(callsTo('SetPhotoLocation')).toEqual([{ photoIds: ['photo-1', 'photo-2'], latitude: 41.15, longitude: -8.61 }])
  expect(onChanged).toHaveBeenCalledTimes(1)
  expect(await screen.findByText('Location changed for 2 photos')).toBeTruthy()
})

it('says "No cities match" when the search finds none', async () => {
  places = async () => []
  show([placed])
  type('Nowhereville')
  expect(await screen.findByText('No cities match')).toBeTruthy()
})

it('offers Remove location only while a photo has one', () => {
  show([unplaced])
  expect(screen.queryByRole('button', { name: 'Remove location' })).toBeNull()
  cleanup()
  show([placed])
  expect(screen.getByRole('button', { name: 'Remove location' })).toBeTruthy()
})

it('asks before removing the location, and removes it once confirmed', async () => {
  const { onChanged, onClose } = show([placed])
  fireEvent.click(screen.getByRole('button', { name: 'Remove location' }))
  expect(await screen.findByText('Remove the location of this photo?')).toBeTruthy()
  expect(screen.getByText("This can't be undone. Copies already shared or downloaded keep the location.")).toBeTruthy()
  expect(screen.queryByRole('searchbox')).toBeNull()
  expect(callsTo('RemovePhotoLocation')).toHaveLength(0)
  fireEvent.click(screen.getByRole('button', { name: 'Remove location' }))
  await waitFor(() => expect(onClose).toHaveBeenCalled())
  expect(callsTo('RemovePhotoLocation')).toEqual([{ photoIds: ['photo-1'] }])
  expect(onChanged).toHaveBeenCalledTimes(1)
  expect(await screen.findByText('Location removed')).toBeTruthy()
})

it('says how many photos the removal is for', () => {
  show([placed, unplaced])
  fireEvent.click(screen.getByRole('button', { name: 'Remove location' }))
  expect(screen.getByText('Remove the location of 2 photos?')).toBeTruthy()
})

it('goes back to the search on Cancel without removing anything', () => {
  const { onClose } = show([placed])
  fireEvent.click(screen.getByRole('button', { name: 'Remove location' }))
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  expect(screen.getByRole('searchbox', { name: 'Search for a city' })).toBeTruthy()
  expect(callsTo('RemovePhotoLocation')).toHaveLength(0)
  expect(onClose).not.toHaveBeenCalled()
})

it('says in a toast that a change failed, stays open, and still tells the caller', async () => {
  const { onChanged, onClose } = show([placed])
  type('Por')
  const choice = await screen.findByRole('button', { name: /Porto, Portugal/ })
  vi.mocked(graphql).mockRejectedValueOnce(new Error('The server is busy.'))
  fireEvent.click(choice)
  expect(await screen.findByText('The server is busy.')).toBeTruthy()
  expect(onClose).not.toHaveBeenCalled()
  // Earlier batches of photos may have changed before the one that failed.
  expect(onChanged).toHaveBeenCalledTimes(1)
})

it('tells the caller when a removal fails too, and stays on the question', async () => {
  const { onChanged, onClose } = show([placed])
  fireEvent.click(screen.getByRole('button', { name: 'Remove location' }))
  vi.mocked(graphql).mockRejectedValueOnce(new Error('The server is busy.'))
  fireEvent.click(screen.getByRole('button', { name: 'Remove location' }))
  expect(await screen.findByText('The server is busy.')).toBeTruthy()
  expect(onClose).not.toHaveBeenCalled()
  expect(onChanged).toHaveBeenCalledTimes(1)
})
