import { act, fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import type { DictationCallbacks } from '../../voice/dictation'
import { answerWith, calls, renderAt, setUpJournalTestWorld } from './journalFixtures'

beforeEach(() => {
  if (typeof window.matchMedia !== 'function') {
    vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener() {}, removeEventListener() {} }))
  }
})

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
  openAgentConversation: vi.fn(),
}))
vi.mock('../../components/locationMap', () => ({
  LocationMap: () => <div data-testid="map" />,
}))

// The socket is Dictation's own, tested apart: here it is told words.
const sessions: { callbacks: DictationCallbacks; place: string; abort: ReturnType<typeof vi.fn> }[] = []
vi.mock('../../voice/dictation', () => ({
  Dictation: class {
    callbacks: DictationCallbacks
    place = ''
    start = vi.fn(async (_sampleRate: number, place: string) => {
      this.place = place
    })
    stop = vi.fn()
    abort = vi.fn()
    constructor(callbacks: DictationCallbacks) {
      this.callbacks = callbacks
      sessions.push(this)
    }
  },
}))

setUpJournalTestWorld()

function offerVoice(isVoiceAvailable: boolean) {
  answerWith()
  const answered = vi.mocked(graphql).getMockImplementation()
  vi.mocked(graphql).mockImplementation(async (document, variables) =>
    document.includes('ReadAgentVoice')
      ? ({
          ReadAgentVoice: {
            isVoiceAvailable,
            isVoiceMessageAvailable: false,
            sampleRate: isVoiceAvailable ? 24000 : 0,
          },
        } as never)
      : (answered as NonNullable<typeof answered>)(document, variables),
  )
}

it('dictates into the journal words at the cursor, and saving stops it', async () => {
  sessions.length = 0
  offerVoice(true)
  renderAt('/journal/2026-10-22')

  const box = (await screen.findByLabelText('What you wrote')) as HTMLTextAreaElement
  fireEvent.change(box, { target: { value: 'Went to the market.' } })
  box.setSelectionRange(7, 7)
  fireEvent.click(await screen.findByRole('button', { name: 'Dictate' }))
  expect(sessions[0].place).toBe('journal')
  act(() => sessions[0].callbacks.onWords('with Tobias'))
  // Inserted at the cursor, unsaved.
  expect(box.value).toBe('Went to with Tobias the market.')
  expect(calls('SetJournalWords')).toHaveLength(0)

  fireEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(calls('SetJournalWords')).toHaveLength(1))
  expect(sessions[0].abort).toHaveBeenCalled()
  expect(calls('SetJournalWords')[0][1]).toMatchObject({ words: 'Went to with Tobias the market.' })
})

it('shows no microphone in the journal when the server has no voice', async () => {
  offerVoice(false)
  renderAt('/journal/2026-10-22')
  await screen.findByLabelText('What you wrote')
  await waitFor(() => expect(calls('ReadAgentVoice').length).toBeGreaterThan(0))
  expect(screen.queryByRole('button', { name: 'Dictate' })).toBeNull()
})
