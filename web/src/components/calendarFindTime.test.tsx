import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { TranslationProvider } from '../i18n/i18n'
import { FindATimeDialog } from './calendarFindTime'
import { ToastProvider } from './toast'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))

beforeEach(() => {
  vi.mocked(graphql).mockReset()
})
afterEach(cleanup)

function inApp(element: React.ReactElement) {
  return render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter>{element}</MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
}

const day = (hours: number, minutes = 0) => new Date(2030, 0, 2, hours, minutes).toISOString()

it('offers only the people who shared a calendar, and lists when everyone chosen is free', async () => {
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ListCirclePeople')) {
      return {
        ListCirclePeople: [
          { userId: 'ben-id', username: 'ben', name: 'Ben Example' },
          { userId: 'cora-id', username: 'cora', name: '' },
        ],
      }
    }
    const asked = (variables?.userIds as string[]) ?? []
    const isProbe = !asked.includes('ada-id')
    return {
      ListCalendarFreeBusy: asked.map((userId) => ({
        userId,
        username: userId.replace('-id', ''),
        name: '',
        // Cora has shared nothing; Ben's and Ada's busy times are the ones in the day.
        isShared: userId !== 'cora-id',
        periods: isProbe
          ? []
          : userId === 'ada-id'
            ? [{ startsAt: day(10), endsAt: day(11) }]
            : [{ startsAt: day(10, 30), endsAt: day(12) }],
      })),
    }
  })
  inApp(<FindATimeDialog ownUserId="ada-id" onClose={() => {}} />)

  expect(await screen.findByLabelText(/Ben Example/)).toBeTruthy()
  expect(screen.queryByLabelText(/cora/)).toBeNull()
  expect(screen.getByText(/have not shared their calendar with you/)).toBeTruthy()

  fireEvent.click(screen.getByLabelText(/Ben Example/))
  fireEvent.change(screen.getByLabelText('First day'), { target: { value: '2030-01-02' } })
  fireEvent.change(screen.getByLabelText('Last day'), { target: { value: '2030-01-02' } })
  fireEvent.click(screen.getByRole('button', { name: 'Find times' }))

  await waitFor(() => expect(screen.getByLabelText('When everyone is free')).toBeTruthy())
  const result = screen.getByLabelText('When everyone is free')
  // 09:00 to 10:00 and 12:00 to 17:00 in the browser's zone.
  expect(result.textContent).toMatch(/9:00\s?(AM)?\s-\s10:00|09:00\s-\s10:00/)
  expect(result.textContent).toMatch(/12:00\s?(PM)?\s-\s0?5:00|12:00\s-\s17:00/)
  const asks = vi.mocked(graphql).mock.calls.filter(([document]) => document.includes('ListCalendarFreeBusy'))
  expect(asks[asks.length - 1]?.[1]).toMatchObject({ userIds: ['ada-id', 'ben-id'] })
})

it('says so when nobody has shared a calendar, and does not offer to find anything', async () => {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('ListCirclePeople')) {
      return { ListCirclePeople: [{ userId: 'cora-id', username: 'cora', name: '' }] }
    }
    return { ListCalendarFreeBusy: [{ userId: 'cora-id', username: '', name: '', isShared: false, periods: [] }] }
  })
  inApp(<FindATimeDialog ownUserId="ada-id" onClose={() => {}} />)
  expect(await screen.findByText(/Nobody has shared their calendar with you yet/)).toBeTruthy()
  expect(screen.queryByLabelText('First day')).toBeNull()
})
