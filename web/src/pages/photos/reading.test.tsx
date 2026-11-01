import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import type { PhotoLibrary } from '../../photos/api'
import { PhotoReadingPreferences, PhotoReadingSetting } from './reading'

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
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('SetPhotoReading')) return { SetPhotoReading: variables?.isEnabled } as never
    return {} as never
  })
})
afterEach(cleanup)

const library = (overrides: Partial<PhotoLibrary> = {}) =>
  ({
    isDescriptionAvailable: true,
    descriptionUnavailableMessage: '',
    isPhotoReadingEnabled: true,
    ...overrides,
  }) as PhotoLibrary

function show(info: PhotoLibrary, onChanged = vi.fn()) {
  render(
    <TranslationProvider>
      <ToastProvider>
        <PhotoReadingSetting info={info} onChanged={onChanged} />
      </ToastProvider>
    </TranslationProvider>,
  )
  return onChanged
}

const switchLabel = 'Describe new photos and record the receipts, statements and tax documents in them'

it('shows the switch as the library says it is', () => {
  show(library({ isPhotoReadingEnabled: false }))
  expect(screen.getByText('Read new photos')).toBeTruthy()
  expect((screen.getByLabelText(switchLabel) as HTMLInputElement).checked).toBe(false)
  cleanup()
  show(library())
  expect((screen.getByLabelText(switchLabel) as HTMLInputElement).checked).toBe(true)
})

it('turns reading off at once, without asking, and says so', async () => {
  const onChanged = show(library())
  fireEvent.click(screen.getByLabelText(switchLabel))
  await waitFor(() => {
    const call = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('SetPhotoReading'))
    expect(call?.[1]).toEqual({ isEnabled: false })
  })
  expect(screen.queryByRole('alertdialog')).toBeNull()
  expect(await screen.findByText('New photos are no longer read')).toBeTruthy()
  expect(onChanged).toHaveBeenCalled()
})

it('turns reading on and says so', async () => {
  show(library({ isPhotoReadingEnabled: false }))
  fireEvent.click(screen.getByLabelText(switchLabel))
  await waitFor(() => {
    const call = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('SetPhotoReading'))
    expect(call?.[1]).toEqual({ isEnabled: true })
  })
  expect(await screen.findByText('New photos are read')).toBeTruthy()
})

it("disables the switch and gives the server's words when describing is not available", () => {
  show(library({ isDescriptionAvailable: false, descriptionUnavailableMessage: 'No model is set up.' }))
  expect((screen.getByLabelText(switchLabel) as HTMLInputElement).disabled).toBe(true)
  expect(screen.getByText('No model is set up.')).toBeTruthy()
  expect(screen.queryByText(/Each picture taken in the last 30 days/)).toBeNull()
})

it('reads the library for the preferences card', async () => {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('ReadPhotoLibrary')) return { ReadPhotoLibrary: library() } as never
    return {} as never
  })
  render(
    <TranslationProvider>
      <ToastProvider>
        <PhotoReadingPreferences />
      </ToastProvider>
    </TranslationProvider>,
  )
  expect(((await screen.findByLabelText(switchLabel)) as HTMLInputElement).checked).toBe(true)
})
