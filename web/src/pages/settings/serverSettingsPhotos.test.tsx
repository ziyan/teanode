import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import { PhotosForm } from './serverSettings'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const settings = { maxFileBytes: '8GB', maxAccountBytes: '0', trashRetention: '30d', isFaceRecognitionEnabled: true }

it('turns finding faces off for everybody, and says that is what it does', async () => {
  vi.mocked(graphql).mockResolvedValue({} as never)
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter>
          <PhotosForm settings={settings} onSaved={vi.fn()} />
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
  const faces = screen.getByRole('checkbox', { name: 'Find people in photos' }) as HTMLInputElement
  expect(faces.checked).toBe(true)
  expect(screen.getByText(/stops finding faces for everybody on this server/)).toBeTruthy()
  fireEvent.click(faces)
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))
  const saved = () => vi.mocked(graphql).mock.calls.find(([document]) => document.includes('UpdateSettings'))
  await waitFor(() => expect(saved()).toBeTruthy())
  expect(saved()?.[1]).toEqual({
    photos: { maxFileBytes: '8GB', maxAccountBytes: '0', trashRetention: '30d', isFaceRecognitionEnabled: false },
  })
})

it('says no limit in words for 0, and writes 0 back when the field is emptied', async () => {
  vi.mocked(graphql).mockResolvedValue({} as never)
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter>
          <PhotosForm settings={settings} onSaved={vi.fn()} />
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
  const field = screen.getByLabelText('Space per person') as HTMLInputElement
  expect(field.value).toBe('')
  expect(field.placeholder).toBe('No limit')
  fireEvent.change(field, { target: { value: '5GB' } })
  expect(field.value).toBe('5GB')
  fireEvent.change(field, { target: { value: '' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))
  const saved = () => vi.mocked(graphql).mock.calls.find(([document]) => document.includes('UpdateSettings'))
  await waitFor(() => expect(saved()).toBeTruthy())
  expect((saved()?.[1] as { photos: { maxAccountBytes: string } }).photos.maxAccountBytes).toBe('0')
})
