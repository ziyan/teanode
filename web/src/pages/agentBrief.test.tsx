import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { BriefCard } from './agent'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))

const brief = {
  id: 'brief',
  name: 'Daily brief',
  cron: '30 7 * * 1-5',
  prompt: 'What is on today.',
  deliver: 'drawer',
  enabled: true,
}

beforeEach(() => {
  vi.mocked(graphql).mockReset()
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('ListAgentSchedules')) return { ListAgentSchedules: [brief] } as never
    if (document.includes('SetAgentBrief')) return { SetAgentBrief: brief } as never
    return {} as never
  })
})
afterEach(cleanup)

function show() {
  render(
    <TranslationProvider>
      <ToastProvider>
        <BriefCard />
      </ToastProvider>
    </TranslationProvider>,
  )
}

function pressed() {
  return screen
    .getAllByRole('button')
    .filter((button) => button.hasAttribute('aria-pressed'))
    .filter((button) => button.getAttribute('aria-pressed') === 'true')
    .map((button) => button.getAttribute('aria-label'))
}

it('shows the brief’s days as toggle buttons and saves the days chosen', async () => {
  show()
  await waitFor(() => expect(pressed()).toEqual(['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday']))
  fireEvent.click(screen.getByRole('button', { name: 'Friday' }))
  fireEvent.click(screen.getByRole('button', { name: 'Saturday' }))
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => {
    const call = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('SetAgentBrief'))
    expect(call?.[1]).toEqual({ enabled: true, at: '07:30', days: [1, 2, 3, 4, 6] })
  })
})

it('cannot be saved with no day chosen, and says so', async () => {
  show()
  await waitFor(() => expect(pressed()).toHaveLength(5))
  for (const name of ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday']) {
    fireEvent.click(screen.getByRole('button', { name }))
  }
  expect(screen.getByText('Choose at least one day.')).toBeTruthy()
  expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
})
