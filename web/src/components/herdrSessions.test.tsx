import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { uploadFiles } from '../upload'
import {
  HERDR_DOCUMENTS,
  HerdrQuestionAnswer,
  HerdrSendForm,
  HerdrTurnsList,
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
  execute.mockResolvedValue({ SendAgentHerdrSession: idle })
  const sent = vi.fn()
  const { container } = render(<HerdrSendForm session={idle} onSent={sent} />)
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
  expect(sent).toHaveBeenCalledWith(idle)
  expect(screen.queryByText('error.png')).toBeNull()
})

it('sends on Ctrl with Enter, and not while the session asks', async () => {
  const idle: HerdrSession = { ...session, herdrSessionState: 'idle' }
  execute.mockResolvedValue({ SendAgentHerdrSession: idle })
  const { rerender } = render(<HerdrSendForm session={idle} onSent={vi.fn()} />)
  const box = screen.getByLabelText('herdr.sendPlaceholder')
  fireEvent.change(box, { target: { value: 'run the tests' } })
  fireEvent.keyDown(box, { key: 'Enter', ctrlKey: true })
  await waitFor(() => expect(execute).toHaveBeenCalled())
  expect(execute.mock.calls[0][1]).toMatchObject({ text: 'run the tests' })
  rerender(<HerdrSendForm session={session} onSent={vi.fn()} />)
  expect((screen.getByLabelText('herdr.sendPlaceholder') as HTMLTextAreaElement).disabled).toBe(true)
})

it('draws person turns as bubbles, agent turns as text and tool turns as one line', () => {
  const { container } = render(
    <HerdrTurnsList
      turns={[
        { herdrTurnRole: 'person', turnText: 'fix the build' },
        { herdrTurnRole: 'agent', turnText: 'Looking at **it** now' },
        { herdrTurnRole: 'tool', turnText: 'Edit(main.go)' },
      ]}
    />,
  )
  expect(container.querySelector('.agent-line.user')?.textContent).toBe('fix the build')
  expect(container.querySelector('.agent-line.assistant strong')?.textContent).toBe('it')
  expect(container.querySelector('.agent-line.tool')?.textContent).toBe('Edit(main.go)')
})

it('opens a tool call to its whole input and what it answered', () => {
  const { container } = render(
    <HerdrTurnsList
      turns={[
        {
          herdrTurnRole: 'tool',
          turnText: 'Bash: npm test',
          toolInputText: '{\n  "command": "npm test"\n}',
          toolResultText: '4 passed',
        },
      ]}
    />,
  )
  const details = container.querySelector('details.herdr-tool') as HTMLDetailsElement
  expect(details.querySelector('summary')?.textContent).toBe('Bash: npm test')
  expect(details.open).toBe(false)
  fireEvent.click(details.querySelector('summary') as HTMLElement)
  expect([...details.querySelectorAll('pre')].map((pre) => pre.textContent)).toEqual([
    '{\n  "command": "npm test"\n}',
    '4 passed',
  ])
})

it('attaches a pasted picture, and leaves pasted text to the box', () => {
  render(<HerdrSendForm session={session} onSent={vi.fn()} />)
  const box = screen.getByLabelText('herdr.sendPlaceholder')
  const picture = new File(['png bytes'], 'image.png', { type: 'image/png' })
  fireEvent.paste(box, { clipboardData: { files: [picture], getData: () => '' } })
  expect(screen.getByText(/^pasted-\d+-1\.png$/)).toBeTruthy()
  fireEvent.paste(box, { clipboardData: { files: [], getData: () => 'some text' } })
  expect(screen.getAllByText(/^pasted-/)).toHaveLength(1)
})
