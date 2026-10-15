import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { uploadFiles } from '../upload'
import {
  HERDR_DOCUMENTS,
  HerdrQuestionAnswer,
  HerdrSessionDialog,
  type HerdrQuestion,
  type HerdrSession,
} from './herdrSessions'

const notices = vi.hoisted(() => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }))
vi.mock('../api', () => ({ graphql: vi.fn() }))
vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({ t: (key: string) => key, language: 'en' }),
  Trans: ({ k }: { k: string }) => k,
}))
vi.mock('./toast', () => ({ useToast: () => notices }))
vi.mock('../upload', () => ({ uploadFiles: vi.fn() }))
const execute = vi.mocked(graphql)
const upload = vi.mocked(uploadFiles)

beforeEach(() => {
  execute.mockReset()
  upload.mockReset()
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
  paneName: 'example › Claude Code',
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

it('marks the option answered even when its label holds a comma', () => {
  const approval: HerdrQuestion = {
    ...question,
    questionText: 'Do you want to proceed?',
    options: [
      { optionNumber: 1, optionLabel: 'Yes', optionDescription: '', herdrOptionKind: 'choice' },
      { optionNumber: 2, optionLabel: "Yes, and don't ask again", optionDescription: '', herdrOptionKind: 'choice' },
      { optionNumber: 3, optionLabel: 'No', optionDescription: '', herdrOptionKind: 'choice' },
    ],
  }
  render(<HerdrQuestionAnswer session={session} question={approval} answeredWith="2. Yes, and don't ask again" />)
  const pressed = screen.getAllByRole('button').filter((button) => button.getAttribute('aria-pressed') === 'true')
  expect(pressed.map((button) => button.textContent)).toEqual(["2. Yes, and don't ask again"])
})

it('uploads the files chosen and sends them with the text', async () => {
  const idle: HerdrSession = { ...session, herdrSessionState: 'idle' }
  upload.mockReturnValue({
    promise: Promise.resolve({ attachments: [{ id: 'attachment-1' }] }),
    abort: vi.fn(),
  } as unknown as ReturnType<typeof uploadFiles>)
  execute.mockImplementation((document: string) =>
    Promise.resolve(
      document === HERDR_DOCUMENTS.SendAgentHerdrSession
        ? { SendAgentHerdrSession: idle }
        : { ReadAgentHerdrSession: { herdrSession: idle, turns: [], isTruncated: false } },
    ),
  )
  const { container } = render(<HerdrSessionDialog session={idle} onChanged={vi.fn()} onClose={vi.fn()} />)
  const picture = new File(['picture'], 'error.png', { type: 'image/png' })
  const picker = container.ownerDocument.querySelector('.herdr-send input[type="file"]') as HTMLInputElement
  fireEvent.change(picker, { target: { files: [picture] } })
  expect(screen.getByText('error.png')).toBeTruthy()
  // A file alone is something to send.
  fireEvent.click(screen.getByRole('button', { name: 'herdr.send' }))
  await waitFor(() => expect(notices.done).toHaveBeenCalled())
  expect(upload).toHaveBeenCalledWith('POST', '/api/v1/agent/attachments', [picture], expect.any(Function))
  expect(execute).toHaveBeenCalledWith(HERDR_DOCUMENTS.SendAgentHerdrSession, {
    computer: 'laptop',
    paneId: 'w1:p2',
    text: '',
    attachmentIds: ['attachment-1'],
  })
  expect(screen.queryByText('error.png')).toBeNull()
})
