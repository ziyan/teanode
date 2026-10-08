import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { HERDR_DOCUMENTS, HerdrQuestionAnswer, type HerdrQuestion, type HerdrSession } from './herdrSessions'

const notices = vi.hoisted(() => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }))
vi.mock('../api', () => ({ graphql: vi.fn() }))
vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({ t: (key: string) => key, language: 'en' }),
  Trans: ({ k }: { k: string }) => k,
}))
vi.mock('./toast', () => ({ useToast: () => notices }))
const execute = vi.mocked(graphql)

beforeEach(() => {
  execute.mockReset()
  notices.done.mockClear()
  notices.failure.mockClear()
})
afterEach(cleanup)

// Each document calls the operation it is named for. That these are every
// operation the agent's tool and the command line have, and no other, is
// checked from the server's side, by TestHerdrParity, which reads this
// component's documents.
it('names each document after the operation it calls', () => {
  for (const [operation, document] of Object.entries(HERDR_DOCUMENTS)) {
    expect(document).toContain(`${operation}(`)
  }
})

const session: HerdrSession = {
  computer: 'laptop',
  paneId: 'w1:p2',
  codingAgentKind: 'claude',
  codingSessionId: 'example',
  herdrSessionState: 'asking',
  herdrAgentStatus: 'idle',
  paneTitle: 'Example',
  workingDirectory: '~/src/example',
  transcriptPath: '',
  isWatched: false,
}

const question: HerdrQuestion = {
  questionFingerprint: 'abc123',
  herdrQuestionKind: 'question',
  questionText: 'Which fruit should we pick?',
  isMultipleChoice: false,
  isFromTranscript: false,
  options: [
    { optionNumber: 1, optionLabel: 'Apple', optionDescription: '', herdrOptionKind: 'choice' },
    { optionNumber: 2, optionLabel: 'Banana', optionDescription: 'Soft', herdrOptionKind: 'choice' },
    { optionNumber: 3, optionLabel: 'Type something', optionDescription: '', herdrOptionKind: 'freeText' },
  ],
}

it('answers with the option tapped and the question it was shown', async () => {
  execute.mockResolvedValue({
    AnswerAgentHerdrQuestion: {
      herdrSession: { ...session, question: null },
      isAnswerAccepted: true,
      answeredWith: '2. Banana',
    },
  })
  const answered = vi.fn()
  render(<HerdrQuestionAnswer session={session} question={question} onAnswered={answered} />)
  fireEvent.click(screen.getByRole('button', { name: /Banana/ }))
  await waitFor(() => expect(answered).toHaveBeenCalled())
  expect(execute).toHaveBeenCalledWith(HERDR_DOCUMENTS.AnswerAgentHerdrQuestion, {
    computer: 'laptop',
    paneId: 'w1:p2',
    questionFingerprint: 'abc123',
    optionNumbers: [2],
    optionLabels: ['Banana'],
    freeText: null,
  })
  expect(notices.done).toHaveBeenCalled()
})

it('types an answer into the option that takes text', async () => {
  execute.mockResolvedValue({
    AnswerAgentHerdrQuestion: { herdrSession: session, isAnswerAccepted: true, answeredWith: '"a pear"' },
  })
  render(<HerdrQuestionAnswer session={session} question={question} onAnswered={vi.fn()} />)
  fireEvent.change(screen.getByLabelText('herdr.typeAnswer'), { target: { value: 'a pear' } })
  fireEvent.click(screen.getByRole('button', { name: 'herdr.sendAnswer' }))
  await waitFor(() => expect(execute).toHaveBeenCalled())
  expect(execute.mock.calls[0][1]).toMatchObject({ optionNumbers: [3], freeText: 'a pear' })
})

it('says so when the question was answered elsewhere first', async () => {
  execute.mockRejectedValue(new Error('this question was already answered or has changed'))
  const answered = vi.fn()
  render(<HerdrQuestionAnswer session={session} question={question} onAnswered={answered} />)
  fireEvent.click(screen.getByRole('button', { name: /Apple/ }))
  await waitFor(() => expect(notices.failure).toHaveBeenCalled())
  expect(answered).toHaveBeenCalledWith(null)
})
