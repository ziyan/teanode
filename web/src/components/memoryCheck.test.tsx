import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import {
  EvaluationQuestion,
  EvaluationRun,
  MemoryCheckSection,
  isMissedOrWrong,
  scoreHistory,
  sparklinePoints,
  visibleQuestions,
} from './memoryCheck'

const notices = vi.hoisted(() => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }))
vi.mock('../api', () => ({ graphql: vi.fn() }))
vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    plural: (_count: number, forms: { one: string }) => forms.one,
    language: 'en',
  }),
}))
vi.mock('./toast', () => ({ useToast: () => notices }))
const execute = vi.mocked(graphql)

beforeEach(() => {
  execute.mockReset()
  notices.done.mockReset()
  notices.failed.mockReset()
})
afterEach(cleanup)

function question(identifier: string, questionState: string): EvaluationQuestion {
  return {
    id: identifier,
    createdAt: '2030-01-01T00:00:00Z',
    modifiedAt: '2030-01-01T00:00:00Z',
    questionKind: 'direct',
    questionText: `Which color is the bicycle ${identifier}?`,
    expectedAnswer: 'Green',
    outdatedAnswer: null,
    questionState,
    sourceFactIds: [],
    isAnswerFiledAfter: false,
    conversationId: null,
    answeredAt: null,
  }
}

function run(identifier: string, startedAt: string, memoryPercent: number | null, isFinished = true): EvaluationRun {
  return {
    id: identifier,
    startedAt,
    finishedAt: isFinished ? startedAt : null,
    questionCount: 4,
    cost: 0.02,
    sourceScores:
      memoryPercent === null
        ? []
        : [
            {
              answerFrom: 'memory',
              answeredCount: 4,
              scorePercent: memoryPercent,
              scorePercentWithoutFiledAfter: memoryPercent,
              filedAfterCount: 0,
              verdictCounts: { correct: 2 },
            },
          ],
  }
}

it('draws the whole range of percents, not just the range of the scores', () => {
  const points = sparklinePoints([0, 50, 100], 110, 50, 5)
  expect(points).toEqual([
    { xPixels: 5, yPixels: 45 },
    { xPixels: 55, yPixels: 25 },
    { xPixels: 105, yPixels: 5 },
  ])
})

it('puts a lone score in the middle and keeps a score past a hundred inside the chart', () => {
  expect(sparklinePoints([140], 100, 50, 5)).toEqual([{ xPixels: 50, yPixels: 5 }])
})

it('orders the history oldest first and leaves out running runs and runs without the source', () => {
  const runs = [
    run('latest', '2030-01-15T00:00:00Z', 80),
    run('running', '2030-01-20T00:00:00Z', null, false),
    run('without', '2030-01-10T00:00:00Z', null),
    run('earliest', '2030-01-01T00:00:00Z', 60),
  ]
  expect(scoreHistory(runs, 'memory').map((entry) => entry.runId)).toEqual(['earliest', 'latest'])
})

it('counts missed, wrong, invented and stale as the ones to read, and not known as right', () => {
  expect(['missed', 'wrong', 'invented', 'stale'].every((answerVerdict) => isMissedOrWrong({ answerVerdict }))).toBe(
    true,
  )
  expect(
    ['correct', 'partial', 'not_known', 'ungraded'].some((answerVerdict) => isMissedOrWrong({ answerVerdict })),
  ).toBe(false)
})

it('hides dropped questions unless asked to show them', () => {
  const questions = [question('first', 'confirmed'), question('second', 'dropped')]
  expect(visibleQuestions(questions, false).map((shown) => shown.id)).toEqual(['first'])
  expect(visibleQuestions(questions, true)).toHaveLength(2)
})

it('offers a check when there are no questions, and says when the agent refuses', async () => {
  execute.mockImplementation(async (document: string) => {
    if (document.includes('ListAgentEvaluationQuestions')) return { ListAgentEvaluationQuestions: [] }
    if (document.includes('ListAgentEvaluationRuns')) return { ListAgentEvaluationRuns: [] }
    throw new Error('no agent worker runs on this server')
  })
  render(<MemoryCheckSection />)
  await screen.findByText('memoryCheck.empty')
  expect(screen.queryByRole('button', { name: 'memoryCheck.gradeNow' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'memoryCheck.checkNow' }))
  await waitFor(() => expect(notices.failed).toHaveBeenCalledWith('no agent worker runs on this server'))
  const speakFirstCall = execute.mock.calls.find(([document]) => document.includes('SpeakFirstNow'))
  expect(speakFirstCall?.[1]).toEqual({ speakFirstReason: 'memory_check' })
})
