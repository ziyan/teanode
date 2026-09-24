import { useEffect, useMemo, useState } from 'react'

import { graphql } from '../api'
import { ErrorMessage, Loading, Tag, formatMoney, formatTime } from './common'
import { Column, DataTable } from './dataTable'
import { FormDialog } from './dialog'
import { PencilIcon, TrashIcon } from './icons'
import { RelativeTime } from './relativeTime'
import { Select } from './select'
import { SettingsEmpty, SettingsSection } from './settingsList'
import { useToast } from './toast'
import { Tooltip } from './tooltip'
import { useQuery } from './useQuery'
import { Key, useTranslation } from '../i18n/i18n'

// The memory check on the agent's Memory tab: the questions the person has
// answered about themselves, how the agent scored against them, and what it
// said to each. The conversation is where a check is held, a question at a
// time and corrected in the person's own words; this is where what was
// recorded is looked at and put right.

export type EvaluationQuestion = {
  id: string
  createdAt: string
  modifiedAt: string
  questionKind: string
  questionText: string
  expectedAnswer: string
  outdatedAnswer: string | null
  questionState: string
  sourceFactIds: string[]
  isAnswerFiledAfter: boolean
  conversationId: string | null
  answeredAt: string | null
}

export type EvaluationSourceScore = {
  answerFrom: string
  answeredCount: number
  scorePercent: number
  scorePercentWithoutFiledAfter: number
  filedAfterCount: number
  verdictCounts: Record<string, number> | null
}

export type EvaluationRun = {
  id: string
  startedAt: string
  finishedAt: string | null
  questionCount: number
  cost: number
  sourceScores: EvaluationSourceScore[]
}

export type EvaluationAnswer = {
  id: string
  runId: string
  questionId: string
  createdAt: string
  answerFrom: string
  answerVerdict: string
  verdictReason: string
  answerText: string
  cost: number
}

const QUESTION_FIELDS = `{
  id createdAt modifiedAt questionKind questionText expectedAnswer outdatedAnswer questionState sourceFactIds
  isAnswerFiledAfter conversationId answeredAt
}`

// Every state, dropped ones included: a run's answers name their questions
// by id, and a question dropped since the run is still the one it answered.
const QUESTIONS = `query { ListAgentEvaluationQuestions ${QUESTION_FIELDS} }`

const RUNS = `
  query ($first: Int) {
    ListAgentEvaluationRuns(first: $first) {
      id startedAt finishedAt questionCount cost
      sourceScores { answerFrom answeredCount scorePercent scorePercentWithoutFiledAfter filedAfterCount verdictCounts }
    }
  }`

const ANSWERS = `
  query ($runId: String!) {
    ListAgentEvaluationAnswers(runId: $runId) {
      id runId questionId createdAt answerFrom answerVerdict verdictReason answerText cost
    }
  }`

const UPDATE_QUESTION = `
  mutation ($questionId: String!, $questionKind: String, $questionText: String, $expectedAnswer: String,
    $outdatedAnswer: String, $questionState: String) {
    UpdateAgentEvaluationQuestion(questionId: $questionId, questionKind: $questionKind, questionText: $questionText,
      expectedAnswer: $expectedAnswer, outdatedAnswer: $outdatedAnswer, questionState: $questionState) ${QUESTION_FIELDS}
  }`

const SPEAK_FIRST_NOW = `mutation ($speakFirstReason: String!) { SpeakFirstNow(speakFirstReason: $speakFirstReason) }`

const EVALUATE_NOW = `mutation { EvaluateAgentMemoryNow { id startedAt } }`

// How many runs the chart and the table go back: a quarter of weekly runs,
// with room for the ones asked for by hand.
const RUN_COUNT = 16

const QUESTION_KINDS = ['direct', 'paraphrase', 'changed', 'multihop', 'abstain'] as const
const QUESTION_STATES = ['asked', 'confirmed', 'corrected', 'unsure', 'dropped'] as const
// In the order the server reports them: what memory knows, what the
// documents say, and the two together as a turn that searches has them.
const ANSWER_SOURCES = ['memory', 'sources', 'both'] as const

const KIND_LABELS: Record<string, Key> = {
  direct: 'memoryCheck.kindDirect',
  paraphrase: 'memoryCheck.kindParaphrase',
  changed: 'memoryCheck.kindChanged',
  multihop: 'memoryCheck.kindMultihop',
  abstain: 'memoryCheck.kindAbstain',
}

const STATE_LABELS: Record<string, Key> = {
  asked: 'memoryCheck.stateAsked',
  confirmed: 'memoryCheck.stateConfirmed',
  corrected: 'memoryCheck.stateCorrected',
  dropped: 'memoryCheck.stateDropped',
  unsure: 'memoryCheck.stateUnsure',
}

const SOURCE_LABELS: Record<string, Key> = {
  memory: 'memoryCheck.sourceMemory',
  sources: 'memoryCheck.sourceSources',
  both: 'memoryCheck.sourceBoth',
}

const VERDICT_LABELS: Record<string, Key> = {
  correct: 'memoryCheck.verdictCorrect',
  partial: 'memoryCheck.verdictPartial',
  not_known: 'memoryCheck.verdictNotKnown',
  missed: 'memoryCheck.verdictMissed',
  stale: 'memoryCheck.verdictStale',
  invented: 'memoryCheck.verdictInvented',
  wrong: 'memoryCheck.verdictWrong',
  ungraded: 'memoryCheck.verdictUngraded',
}

// The verdicts worth reading first: memory did not carry the fact, or the
// answer said something that is not so. "Not known" is left out because
// the grader gives it only to a question memory should not know, where it
// is the right answer.
const MISSED_OR_WRONG_VERDICTS = new Set(['missed', 'wrong', 'invented', 'stale'])

export function isMissedOrWrong(answer: Pick<EvaluationAnswer, 'answerVerdict'>): boolean {
  return MISSED_OR_WRONG_VERDICTS.has(answer.answerVerdict)
}

// The questions the table shows. A dropped question is kept on record, so
// that a run which answered it can still name it, but it is out of every
// run since, and a list that kept showing it would read as though it
// counted.
export function visibleQuestions(questions: EvaluationQuestion[], isShowingDropped: boolean): EvaluationQuestion[] {
  return isShowingDropped ? questions : questions.filter((question) => question.questionState !== 'dropped')
}

// The score of one source in each finished run, oldest first, which is
// the order a line over time is read in. A run still going has no score
// yet, and a run that did not answer from a source is a gap rather than
// a zero.
export function scoreHistory(
  runs: EvaluationRun[],
  answerFrom: string,
): { runId: string; startedAt: string; scorePercent: number }[] {
  return runs
    .filter((run) => run.finishedAt)
    .map((run) => ({ run, score: run.sourceScores.find((candidate) => candidate.answerFrom === answerFrom) }))
    .filter(({ score }) => score !== undefined && score.answeredCount > 0)
    .map(({ run, score }) => ({ runId: run.id, startedAt: run.startedAt, scorePercent: score!.scorePercent }))
    .sort((first, second) => first.startedAt.localeCompare(second.startedAt))
}

// Where each score sits in a chart of the given size. The scale is the
// whole of zero to a hundred rather than the range of the scores, so that
// a line which moved from 70 to 72 looks as flat as it is. A lone score is
// drawn in the middle, where a line would begin.
export function sparklinePoints(
  scorePercents: number[],
  widthPixels: number,
  heightPixels: number,
  paddingPixels: number,
): { xPixels: number; yPixels: number }[] {
  const innerWidthPixels = widthPixels - paddingPixels * 2
  const innerHeightPixels = heightPixels - paddingPixels * 2
  return scorePercents.map((scorePercent, index) => {
    const clampedPercent = Math.min(100, Math.max(0, scorePercent))
    const xPixels =
      scorePercents.length === 1
        ? widthPixels / 2
        : paddingPixels + (innerWidthPixels * index) / (scorePercents.length - 1)
    const yPixels = paddingPixels + innerHeightPixels * (1 - clampedPercent / 100)
    return { xPixels, yPixels }
  })
}

function formatPercent(scorePercent: number): string {
  return `${Math.round(scorePercent)}%`
}

function messageOf(caught: unknown): string {
  return caught instanceof Error ? caught.message : String(caught)
}

function verdictTone(answerVerdict: string): 'good' | 'bad' | 'warn' | undefined {
  if (answerVerdict === 'correct' || answerVerdict === 'not_known') return 'good'
  if (answerVerdict === 'partial') return 'warn'
  if (MISSED_OR_WRONG_VERDICTS.has(answerVerdict)) return 'bad'
  return undefined
}

function stateTone(questionState: string): 'good' | 'bad' | 'warn' | undefined {
  if (questionState === 'confirmed' || questionState === 'corrected') return 'good'
  if (questionState === 'asked' || questionState === 'unsure') return 'warn'
  return undefined
}

const SPARKLINE_WIDTH_PIXELS = 160
const SPARKLINE_HEIGHT_PIXELS = 40
const SPARKLINE_PADDING_PIXELS = 5

// A line of one source's scores. Small and without axes: it is there to
// say whether the score is rising, and the number beside it says where it
// is. Each point names its run and its score on hover.
function ScoreSparkline({
  history,
  label,
}: {
  history: { runId: string; startedAt: string; scorePercent: number }[]
  label: string
}) {
  const points = sparklinePoints(
    history.map((entry) => entry.scorePercent),
    SPARKLINE_WIDTH_PIXELS,
    SPARKLINE_HEIGHT_PIXELS,
    SPARKLINE_PADDING_PIXELS,
  )
  const description = `${label}: ${history.map((entry) => formatPercent(entry.scorePercent)).join(', ')}`
  return (
    <svg
      className="memory-check-sparkline"
      width={SPARKLINE_WIDTH_PIXELS}
      height={SPARKLINE_HEIGHT_PIXELS}
      viewBox={`0 0 ${SPARKLINE_WIDTH_PIXELS} ${SPARKLINE_HEIGHT_PIXELS}`}
      role="img"
      aria-label={description}
    >
      <line
        className="memory-check-sparkline-baseline"
        x1={0}
        x2={SPARKLINE_WIDTH_PIXELS}
        y1={SPARKLINE_HEIGHT_PIXELS - SPARKLINE_PADDING_PIXELS}
        y2={SPARKLINE_HEIGHT_PIXELS - SPARKLINE_PADDING_PIXELS}
      />
      {points.length > 1 ? (
        <polyline
          className="memory-check-sparkline-line"
          points={points.map((point) => `${point.xPixels},${point.yPixels}`).join(' ')}
        />
      ) : null}
      {points.map((point, index) => (
        <circle
          key={history[index].runId}
          className="memory-check-sparkline-point"
          cx={point.xPixels}
          cy={point.yPixels}
          r={index === points.length - 1 ? 4 : 3}
        >
          <title>{`${formatTime(history[index].startedAt)}: ${formatPercent(history[index].scorePercent)}`}</title>
        </circle>
      ))}
    </svg>
  )
}

type QuestionDraft = {
  questionKind: string
  questionText: string
  expectedAnswer: string
  outdatedAnswer: string
  questionState: string
}

export function MemoryCheckSection() {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const questionsQuery = useQuery(() => graphql<{ ListAgentEvaluationQuestions: EvaluationQuestion[] }>(QUESTIONS))
  const questions = useMemo(() => questionsQuery.data?.ListAgentEvaluationQuestions ?? [], [questionsQuery.data])
  // Read again every few seconds only while a run is grading, so the row
  // gets its scores when it finishes without the page being asked about
  // runs that cannot change.
  const [isRunInProgress, setIsRunInProgress] = useState(false)
  const runsQuery = useQuery(
    () => graphql<{ ListAgentEvaluationRuns: EvaluationRun[] }>(RUNS, { first: RUN_COUNT }),
    [],
    { refresh: isRunInProgress },
  )
  const runs = useMemo(() => runsQuery.data?.ListAgentEvaluationRuns ?? [], [runsQuery.data])
  const hasRunInProgress = runs.some((run) => !run.finishedAt)
  useEffect(() => setIsRunInProgress(hasRunInProgress), [hasRunInProgress])

  const [isShowingDropped, setIsShowingDropped] = useState(false)
  const [editing, setEditing] = useState<{ question: EvaluationQuestion; draft: QuestionDraft } | null>(null)
  const [isSaving, setIsSaving] = useState(false)
  const [isAskingForCheck, setIsAskingForCheck] = useState(false)
  const [isAskingForGrading, setIsAskingForGrading] = useState(false)
  const [openRun, setOpenRun] = useState<EvaluationRun | null>(null)
  const [isOnlyMissedOrWrong, setIsOnlyMissedOrWrong] = useState(true)

  const answersQuery = useQuery(
    () =>
      openRun
        ? graphql<{ ListAgentEvaluationAnswers: EvaluationAnswer[] }>(ANSWERS, { runId: openRun.id })
        : Promise.resolve({ ListAgentEvaluationAnswers: [] as EvaluationAnswer[] }),
    [openRun?.id],
    { refresh: false },
  )
  const questionsById = useMemo(() => new Map(questions.map((question) => [question.id, question])), [questions])

  const kindLabel = (questionKind: string) => (KIND_LABELS[questionKind] ? t(KIND_LABELS[questionKind]) : questionKind)
  const stateLabel = (questionState: string) =>
    STATE_LABELS[questionState] ? t(STATE_LABELS[questionState]) : questionState
  const sourceLabel = (answerFrom: string) => (SOURCE_LABELS[answerFrom] ? t(SOURCE_LABELS[answerFrom]) : answerFrom)
  const verdictLabel = (answerVerdict: string) =>
    VERDICT_LABELS[answerVerdict] ? t(VERDICT_LABELS[answerVerdict]) : answerVerdict

  // Asking for a check starts one in the main conversation at once,
  // outside the rules about when the agent may speak first, which are for
  // the times nobody asked. The drawer opens on it by itself.
  const askForCheck = async () => {
    setIsAskingForCheck(true)
    try {
      await graphql(SPEAK_FIRST_NOW, { speakFirstReason: 'memory_check' })
      toast.done(t('memoryCheck.checkNowAsked'))
    } catch (caught) {
      toast.failed(messageOf(caught))
    } finally {
      setIsAskingForCheck(false)
    }
  }

  const askForGrading = async () => {
    setIsAskingForGrading(true)
    try {
      await graphql(EVALUATE_NOW, {})
      toast.done(t('memoryCheck.gradeNowAsked'))
      await runsQuery.reload()
    } catch (caught) {
      toast.failed(messageOf(caught))
    } finally {
      setIsAskingForGrading(false)
    }
  }

  const updateQuestion = async (questionId: string, changes: Partial<QuestionDraft>) => {
    await graphql(UPDATE_QUESTION, { questionId, ...changes })
    await questionsQuery.reload()
  }

  // Dropping keeps the row and can be taken back, so it asks nothing
  // first; the toast carries the way back instead.
  const drop = async (question: EvaluationQuestion) => {
    try {
      await updateQuestion(question.id, { questionState: 'dropped' })
      toast.done(t('memoryCheck.dropped'), {
        label: t('common.undo'),
        run: async () => {
          try {
            await updateQuestion(question.id, { questionState: question.questionState })
          } catch (caught) {
            toast.failed(messageOf(caught))
          }
        },
      })
    } catch (caught) {
      toast.failed(messageOf(caught))
    }
  }

  // Only what changed is sent: a field left alone is left out, so an edit
  // does not write back an answer another tab changed a moment before.
  const save = async () => {
    if (!editing) return
    const { question, draft } = editing
    const changes: Partial<QuestionDraft> = {}
    if (draft.questionKind !== question.questionKind) changes.questionKind = draft.questionKind
    if (draft.questionText !== question.questionText) changes.questionText = draft.questionText
    if (draft.expectedAnswer !== question.expectedAnswer) changes.expectedAnswer = draft.expectedAnswer
    if (draft.outdatedAnswer !== (question.outdatedAnswer ?? '')) changes.outdatedAnswer = draft.outdatedAnswer
    if (draft.questionState !== question.questionState) changes.questionState = draft.questionState
    setIsSaving(true)
    try {
      if (Object.keys(changes).length > 0) await updateQuestion(question.id, changes)
      toast.done(t('memoryCheck.saved'))
      setEditing(null)
    } catch (caught) {
      // The dialog stays open with what was typed still in it.
      toast.failed(messageOf(caught))
    } finally {
      setIsSaving(false)
    }
  }

  const openEditor = (question: EvaluationQuestion) =>
    setEditing({
      question,
      draft: {
        questionKind: question.questionKind,
        questionText: question.questionText,
        expectedAnswer: question.expectedAnswer,
        outdatedAnswer: question.outdatedAnswer ?? '',
        questionState: question.questionState,
      },
    })

  const questionColumns: Column<EvaluationQuestion>[] = [
    {
      key: 'kind',
      header: t('memoryCheck.kind'),
      width: '8rem',
      filter: 'select',
      value: (question) => kindLabel(question.questionKind),
      render: (question) => <Tag value={kindLabel(question.questionKind)} />,
    },
    {
      key: 'question',
      header: t('memoryCheck.question'),
      truncate: true,
      filter: 'text',
      value: (question) => question.questionText,
      render: (question) => (
        <Tooltip label={question.questionText}>
          <span>
            {question.questionText}
            {question.isAnswerFiledAfter ? (
              <>
                {' '}
                <Tag value={t('memoryCheck.filedAfter')} tone="warn" />
              </>
            ) : null}
          </span>
        </Tooltip>
      ),
    },
    {
      key: 'answer',
      header: t('memoryCheck.answer'),
      truncate: true,
      value: (question) => question.expectedAnswer,
      render: (question) => {
        const outdated =
          question.questionKind === 'changed' && question.outdatedAnswer
            ? t('memoryCheck.outdatedAnswerWas', { answer: question.outdatedAnswer })
            : ''
        const whole = outdated ? `${question.expectedAnswer} · ${outdated}` : question.expectedAnswer
        return (
          <Tooltip label={whole}>
            <span>
              {question.expectedAnswer}
              {outdated ? <span className="muted"> · {outdated}</span> : null}
            </span>
          </Tooltip>
        )
      },
    },
    {
      key: 'state',
      header: t('memoryCheck.state'),
      width: '8rem',
      filter: 'select',
      value: (question) => stateLabel(question.questionState),
      render: (question) => <Tag value={stateLabel(question.questionState)} tone={stateTone(question.questionState)} />,
    },
    {
      key: 'answeredAt',
      header: t('memoryCheck.answeredAt'),
      width: '9rem',
      optional: true,
      value: (question) => (question.answeredAt ? formatTime(question.answeredAt) : ''),
      sort: (first, second) => (first.answeredAt ?? '').localeCompare(second.answeredAt ?? ''),
      render: (question) => (question.answeredAt ? <RelativeTime value={question.answeredAt} /> : null),
    },
    {
      key: 'actions',
      header: '',
      width: '5rem',
      render: (question) => (
        <div className="row-actions">
          <Tooltip label={t('common.edit')}>
            <button
              type="button"
              className="icon-action"
              aria-label={`${question.questionText}: ${t('common.edit')}`}
              onClick={() => openEditor(question)}
            >
              <PencilIcon size={16} />
            </button>
          </Tooltip>
          {question.questionState !== 'dropped' ? (
            <Tooltip label={t('memoryCheck.drop')}>
              <button
                type="button"
                className="icon-action danger"
                aria-label={`${question.questionText}: ${t('memoryCheck.drop')}`}
                onClick={() => void drop(question)}
              >
                <TrashIcon size={16} />
              </button>
            </Tooltip>
          ) : null}
        </div>
      ),
    },
  ]

  const sourceScoreOf = (run: EvaluationRun, answerFrom: string) =>
    run.sourceScores.find((candidate) => candidate.answerFrom === answerFrom)

  const runColumns: Column<EvaluationRun>[] = [
    { key: 'startedAt', header: t('agent.when'), width: '11rem', value: (run) => formatTime(run.startedAt) },
    {
      key: 'questionCount',
      header: t('memoryCheck.questionCount'),
      width: '7rem',
      optional: true,
      value: (run) => String(run.questionCount),
      render: (run) => <span className="muted numeric">{run.questionCount}</span>,
    },
    ...ANSWER_SOURCES.map((answerFrom): Column<EvaluationRun> => ({
      key: answerFrom,
      header: sourceLabel(answerFrom),
      width: '7rem',
      value: (run) => String(sourceScoreOf(run, answerFrom)?.scorePercent ?? ''),
      render: (run) => {
        if (!run.finishedAt)
          return answerFrom === 'memory' ? <Tag value={t('memoryCheck.grading')} tone="good" /> : null
        const score = sourceScoreOf(run, answerFrom)
        return score && score.answeredCount > 0 ? (
          <span className="numeric">{formatPercent(score.scorePercent)}</span>
        ) : null
      },
    })),
    {
      key: 'cost',
      header: t('agent.dreamCost'),
      width: '7rem',
      optional: true,
      value: (run) => String(run.cost),
      render: (run) => <span className="muted numeric">{run.cost > 0 ? formatMoney(run.cost) : ''}</span>,
    },
    {
      key: 'answers',
      header: '',
      width: '7rem',
      render: (run) =>
        run.finishedAt ? (
          <button type="button" onClick={() => setOpenRun(run)}>
            {t('memoryCheck.answers')}
          </button>
        ) : null,
    },
  ]

  const answerColumns: Column<EvaluationAnswer>[] = [
    {
      key: 'question',
      header: t('memoryCheck.question'),
      truncate: true,
      filter: 'text',
      value: (answer) => questionsById.get(answer.questionId)?.questionText ?? '',
      render: (answer) => {
        const questionText = questionsById.get(answer.questionId)?.questionText ?? ''
        return (
          <Tooltip label={questionText}>
            <span>{questionText}</span>
          </Tooltip>
        )
      },
    },
    {
      key: 'answerFrom',
      header: t('memoryCheck.answerFrom'),
      width: '7rem',
      filter: 'select',
      value: (answer) => sourceLabel(answer.answerFrom),
    },
    {
      key: 'verdict',
      header: t('memoryCheck.verdict'),
      width: '8rem',
      filter: 'select',
      value: (answer) => verdictLabel(answer.answerVerdict),
      render: (answer) => <Tag value={verdictLabel(answer.answerVerdict)} tone={verdictTone(answer.answerVerdict)} />,
    },
    {
      key: 'answerText',
      header: t('memoryCheck.agentAnswer'),
      truncate: true,
      value: (answer) => answer.answerText,
      render: (answer) => (
        <Tooltip label={answer.answerText}>
          <span>{answer.answerText}</span>
        </Tooltip>
      ),
    },
    {
      key: 'verdictReason',
      header: t('memoryCheck.verdictReason'),
      truncate: true,
      optional: true,
      value: (answer) => answer.verdictReason,
      render: (answer) => (
        <Tooltip label={answer.verdictReason}>
          <span className="muted">{answer.verdictReason}</span>
        </Tooltip>
      ),
    },
  ]

  const shownQuestions = visibleQuestions(questions, isShowingDropped)
  const droppedCount = questions.length - visibleQuestions(questions, false).length
  const answers = answersQuery.data?.ListAgentEvaluationAnswers ?? []
  const shownAnswers = isOnlyMissedOrWrong ? answers.filter(isMissedOrWrong) : answers
  const latestFinishedRun = runs.find((run) => run.finishedAt)
  const hasQuestions = questions.length > 0

  return (
    <SettingsSection
      card
      title={t('memoryCheck.title')}
      description={t('memoryCheck.hint')}
      action={
        // One group: on a phone the section's head pushes each of its
        // actions to the right on its own, which spread two buttons across
        // the width with the line's whole slack between them.
        <div className="row-actions">
          <button type="button" disabled={isAskingForCheck} onClick={() => void askForCheck()}>
            {t('memoryCheck.checkNow')}
          </button>
          {hasQuestions ? (
            <button
              type="button"
              disabled={isAskingForGrading || hasRunInProgress}
              onClick={() => void askForGrading()}
            >
              {hasRunInProgress ? t('memoryCheck.grading') : t('memoryCheck.gradeNow')}
            </button>
          ) : null}
        </div>
      }
    >
      <ErrorMessage error={questionsQuery.error} />
      {questionsQuery.loading && !questionsQuery.data ? <Loading /> : null}
      {questionsQuery.data && !hasQuestions ? <SettingsEmpty>{t('memoryCheck.empty')}</SettingsEmpty> : null}
      {hasQuestions ? (
        <>
          <label className="checkbox">
            <input
              type="checkbox"
              checked={isShowingDropped}
              onChange={(event) => setIsShowingDropped(event.target.checked)}
            />
            {t('memoryCheck.showDropped', { count: droppedCount })}
          </label>
          <DataTable
            columns={questionColumns}
            rows={shownQuestions}
            rowKey={(question) => question.id}
            emptyMessage={t('memoryCheck.noQuestionsShown')}
            countLabel={(count) =>
              plural(count, { one: 'memoryCheck.questionsOne', other: 'memoryCheck.questionsOther' })
            }
          />
        </>
      ) : null}
      {hasQuestions || runs.length > 0 ? (
        <div className="settings-subform">
          <h4>{t('memoryCheck.scores')}</h4>
          <p className="muted">{t('memoryCheck.scoresHint')}</p>
          <ErrorMessage error={runsQuery.error} />
          {latestFinishedRun ? (
            <div className="memory-check-scores">
              {ANSWER_SOURCES.map((answerFrom) => {
                const history = scoreHistory(runs, answerFrom)
                const latestScore = sourceScoreOf(latestFinishedRun, answerFrom)
                return (
                  <div key={answerFrom} className="memory-check-score">
                    <div className="memory-check-score-head">
                      <span>{sourceLabel(answerFrom)}</span>
                      <strong className="numeric">
                        {latestScore && latestScore.answeredCount > 0
                          ? formatPercent(latestScore.scorePercent)
                          : t('common.none')}
                      </strong>
                    </div>
                    {history.length > 0 ? <ScoreSparkline history={history} label={sourceLabel(answerFrom)} /> : null}
                    {latestScore && latestScore.filedAfterCount > 0 ? (
                      <p className="muted memory-check-score-note">
                        {t('memoryCheck.withoutFiledAfter', {
                          score: formatPercent(latestScore.scorePercentWithoutFiledAfter),
                          count: latestScore.filedAfterCount,
                        })}
                      </p>
                    ) : null}
                  </div>
                )
              })}
            </div>
          ) : null}
          {latestFinishedRun ? (
            <p className="muted">
              {t('memoryCheck.latestRun', {
                when: formatTime(latestFinishedRun.startedAt),
                count: latestFinishedRun.questionCount,
                cost: formatMoney(latestFinishedRun.cost),
              })}
            </p>
          ) : null}
          <DataTable
            columns={runColumns}
            rows={runs}
            rowKey={(run) => run.id}
            emptyMessage={t('memoryCheck.noRuns')}
            countLabel={(count) => plural(count, { one: 'memoryCheck.runsOne', other: 'memoryCheck.runsOther' })}
          />
        </div>
      ) : null}
      {openRun ? (
        <div className="settings-subform">
          <div className="settings-section-head">
            <div>
              <h4>{t('memoryCheck.answersOf', { when: formatTime(openRun.startedAt) })}</h4>
              <p className="muted">{t('memoryCheck.answersHint')}</p>
            </div>
            <button type="button" onClick={() => setOpenRun(null)}>
              {t('common.close')}
            </button>
          </div>
          <label className="checkbox">
            <input
              type="checkbox"
              checked={isOnlyMissedOrWrong}
              onChange={(event) => setIsOnlyMissedOrWrong(event.target.checked)}
            />
            {t('memoryCheck.onlyMissedOrWrong')}
          </label>
          <ErrorMessage error={answersQuery.error} />
          <DataTable
            key={openRun.id}
            columns={answerColumns}
            rows={shownAnswers}
            rowKey={(answer) => answer.id}
            loading={answersQuery.loading && !answersQuery.data}
            emptyMessage={isOnlyMissedOrWrong ? t('memoryCheck.noMissedOrWrong') : t('memoryCheck.noAnswers')}
            countLabel={(count) => plural(count, { one: 'memoryCheck.answersOne', other: 'memoryCheck.answersOther' })}
          />
        </div>
      ) : null}
      {editing ? (
        <FormDialog
          title={t('memoryCheck.editTitle')}
          submitLabel={t('common.save')}
          busy={isSaving}
          canSubmit={editing.draft.questionText.trim() !== ''}
          onClose={() => setEditing(null)}
          onSubmit={() => void save()}
        >
          <label>
            <span>{t('memoryCheck.question')}</span>
            <textarea
              rows={2}
              value={editing.draft.questionText}
              onChange={(event) =>
                setEditing({ ...editing, draft: { ...editing.draft, questionText: event.target.value } })
              }
            />
          </label>
          <label>
            <span>{t('memoryCheck.answer')}</span>
            <textarea
              rows={2}
              value={editing.draft.expectedAnswer}
              onChange={(event) =>
                setEditing({ ...editing, draft: { ...editing.draft, expectedAnswer: event.target.value } })
              }
            />
          </label>
          <label>
            <span>{t('memoryCheck.kind')}</span>
            <Select
              block
              value={editing.draft.questionKind}
              label={t('memoryCheck.kind')}
              options={QUESTION_KINDS.map((questionKind) => ({ value: questionKind, label: kindLabel(questionKind) }))}
              onChange={(value) => setEditing({ ...editing, draft: { ...editing.draft, questionKind: value } })}
            />
          </label>
          {/* What used to be true matters only to a question about a
              change: an answer that gives it is graded stale. */}
          {editing.draft.questionKind === 'changed' ? (
            <label>
              <span>{t('memoryCheck.outdatedAnswer')}</span>
              <textarea
                rows={2}
                value={editing.draft.outdatedAnswer}
                onChange={(event) =>
                  setEditing({ ...editing, draft: { ...editing.draft, outdatedAnswer: event.target.value } })
                }
              />
            </label>
          ) : null}
          <label>
            <span>{t('memoryCheck.state')}</span>
            <Select
              block
              value={editing.draft.questionState}
              label={t('memoryCheck.state')}
              options={QUESTION_STATES.map((questionState) => ({
                value: questionState,
                label: stateLabel(questionState),
              }))}
              onChange={(value) => setEditing({ ...editing, draft: { ...editing.draft, questionState: value } })}
            />
          </label>
          <p className="muted field-hint">{t('memoryCheck.stateHint')}</p>
        </FormDialog>
      ) : null}
    </SettingsSection>
  )
}
