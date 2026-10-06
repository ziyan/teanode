import { FormEvent, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { graphql, openAgentConversation, sendToAgentConversation } from '../api'
import { ErrorMessage, Loading, Tag, formatTime } from '../components/common'
import { IdeaList, LIST_IDEAS } from '../components/ideaRow'
import { SettingsEmpty, SettingsSection } from '../components/settingsList'
import { useToast } from '../components/toast'
import { useQuery } from '../components/useQuery'
import { Key, useTranslation } from '../i18n/i18n'

// The Goals tab: what the agent keeps at in the background for the person,
// between conversations. Each goal runs in a conversation of its own and
// is heard from in the main conversation only when it needs them; here it
// is listed by where it stands, with its one-line status, and opens to what
// it is for, what happened on it and what it made. Beside them what runs on
// its own, and a way to start one. The same goals `teanode agent goal` and
// the agent's goal tool list, start and close.

type GoalState = 'working' | 'waiting' | 'met' | 'dropped'

interface Goal {
  conversationId: string
  goalTitle: string
  goalDescription: string
  goalState: GoalState
  goalStatus: string
  goalSetAt: string | null
  goalNextAt: string | null
  lastAt: string
}

interface GoalActivity {
  id: string
  createdAt: string
  goalActivityKind: string
  activityHeadline: string
  activityDetail?: string
}

interface GoalDetails extends Goal {
  goalOriginConversationId: string
  goalOriginTitle: string
  isGoalOriginMain: boolean
  activity: GoalActivity[]
  schedules: { id: string; name: string; cron: string; enabled: boolean; nextRunAt?: string | null }[]
  backgroundWork: { id: string; workKind: string; workStatus: string; title: string }[]
  artifacts: { id: string; goalArtifactKind: string; artifactReference: string; artifactTitle: string }[]
}

interface Schedule {
  id: string
  name: string
  cron: string
  enabled: boolean
  conversationId?: string
  nextRunAt?: string | null
}

const GOAL_FIELDS = 'conversationId goalTitle goalDescription goalState goalStatus goalSetAt goalNextAt lastAt'

const GOALS = `query { ListAgentGoals { ${GOAL_FIELDS} } }`

const GOAL = `
  query ($conversationId: String!) {
    GetAgentGoal(conversationId: $conversationId) {
      ${GOAL_FIELDS}
      goalOriginConversationId goalOriginTitle isGoalOriginMain
      activity { id createdAt goalActivityKind activityHeadline activityDetail }
      schedules { id name cron enabled nextRunAt }
      backgroundWork { id workKind workStatus title }
      artifacts { id goalArtifactKind artifactReference artifactTitle }
    }
  }`

const SCHEDULES = `query { ListAgentSchedules { id name cron enabled conversationId nextRunAt } }`

const START_GOAL = `
  mutation ($goalTitle: String!, $goalDescription: String!) {
    StartAgentGoal(goalTitle: $goalTitle, goalDescription: $goalDescription) { conversationId }
  }`

const TELL_GOAL = `
  mutation ($conversationId: String!, $text: String!) {
    TellAgentGoal(conversationId: $conversationId, text: $text) { conversationId }
  }`

const SET_GOAL_STATE = `
  mutation ($conversationId: String!, $goalState: String!) {
    SetAgentGoalState(conversationId: $conversationId, goalState: $goalState) { conversationId }
  }`

const START = `
  mutation ($title: String) {
    StartAgentConversation(title: $title) { id }
  }`

// What each state is called in a list, and the tag it wears.
const STATE_LABEL: Record<GoalState, Key> = {
  working: 'goals.working',
  waiting: 'goals.waiting',
  met: 'goals.met',
  dropped: 'goals.dropped',
}

export function GoalsTab() {
  const [search, setSearch] = useSearchParams()
  const opened = search.get('goal')
  if (opened) {
    return (
      <GoalPage
        conversationId={opened}
        onBack={() => {
          search.delete('goal')
          setSearch(search)
        }}
      />
    )
  }
  return (
    <GoalList
      onOpen={(conversationId) => {
        search.set('goal', conversationId)
        setSearch(search)
      }}
    />
  )
}

function GoalList({ onOpen }: { onOpen: (conversationId: string) => void }) {
  const { t } = useTranslation()
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const [isShowingDone, setShowingDone] = useState(false)
  const goals = useQuery(() => graphql<{ ListAgentGoals: Goal[] }>(GOALS, {}), [], { refresh: true })
  const schedules = useQuery(() => graphql<{ ListAgentSchedules: Schedule[] }>(SCHEDULES, {}), [])
  const ideas = useQuery(() => graphql<IdeaList>(LIST_IDEAS, { ideaStatuses: ['open'] }), [])

  if (goals.loading && !goals.data) return <Loading />
  if (goals.error) return <ErrorMessage error={goals.error} />
  const every = goals.data?.ListAgentGoals ?? []
  const needsYou = every.filter((goal) => goal.goalState === 'waiting')
  const tracking = every.filter((goal) => goal.goalState === 'working')
  const done = every.filter((goal) => goal.goalState === 'met' || goal.goalState === 'dropped')
  const goalIds = new Set(every.map((goal) => goal.conversationId))
  // The next run of each goal's own schedule, for the goals that run on one.
  const scheduledNext = new Map<string, string>()
  for (const schedule of schedules.data?.ListAgentSchedules ?? []) {
    if (!schedule.enabled || !schedule.conversationId || !goalIds.has(schedule.conversationId)) continue
    const earlier = scheduledNext.get(schedule.conversationId)
    if (schedule.nextRunAt && (!earlier || schedule.nextRunAt < earlier))
      scheduledNext.set(schedule.conversationId, schedule.nextRunAt)
  }
  // A schedule a goal made is shown on the goal; here only those that
  // stand on their own.
  const running = (schedules.data?.ListAgentSchedules ?? []).filter(
    (schedule) => schedule.enabled && !goalIds.has(schedule.conversationId ?? ''),
  )
  // Every area of a life; the agent's own is for ideas about the agent.
  const categories = (ideas.data?.ListAgentIdeas.ideaCategories ?? []).filter(
    (category) => category.ideaCategory !== 'assistant',
  )

  // Ticking a goal marks it done, with the chance to take that back.
  async function markDone(goal: Goal) {
    setBusy(true)
    try {
      await graphql(SET_GOAL_STATE, { conversationId: goal.conversationId, goalState: 'met' })
      toast.done(t('goals.markedMet'), {
        label: t('common.undo'),
        run: async () => {
          await graphql(SET_GOAL_STATE, { conversationId: goal.conversationId, goalState: 'working' })
          await goals.reload()
        },
      })
      await goals.reload()
    } catch (caught) {
      toast.failure(caught, t('goals.failed'))
    } finally {
      setBusy(false)
    }
  }

  // Starting a goal about an area is a conversation, not a form: the agent
  // asks what the person is after and proposes a goal, and starts it once
  // they agree. The first line is sent for them.
  async function startAbout(category: string) {
    setBusy(true)
    try {
      const area = t(`ideas.category.${category}` as Key)
      const started = await graphql<{ StartAgentConversation: { id: string } }>(START, {
        title: t('goals.newTitle', { area }),
      })
      sendToAgentConversation(
        started.StartAgentConversation.id,
        t('goals.openingRequest', { area: area.toLowerCase() }),
      )
    } catch (caught) {
      toast.failure(caught, t('goals.failed'))
    } finally {
      setBusy(false)
    }
  }

  const rows = (list: Goal[]) => (
    <div className="goal-list">
      {list.map((goal) => (
        <GoalRow
          key={goal.conversationId}
          goal={goal}
          scheduledNextAt={scheduledNext.get(goal.conversationId)}
          busy={busy}
          onOpen={onOpen}
          onDone={markDone}
        />
      ))}
    </div>
  )

  return (
    <>
      {needsYou.length > 0 ? (
        <SettingsSection card title={t('goals.needsYou')} description={t('goals.needsYouHint')}>
          {rows(needsYou)}
        </SettingsSection>
      ) : null}
      <SettingsSection card title={t('goals.tracking')} description={t('goals.trackingHint')}>
        {tracking.length === 0 ? <SettingsEmpty>{t('goals.none')}</SettingsEmpty> : rows(tracking)}
      </SettingsSection>
      {done.length > 0 ? (
        <SettingsSection card title={t('goals.done')}>
          {isShowingDone ? (
            rows(done)
          ) : (
            <button type="button" onClick={() => setShowingDone(true)}>
              {t('goals.showDone', { count: String(done.length) })}
            </button>
          )}
        </SettingsSection>
      ) : null}
      <StartGoalCard onStarted={() => void goals.reload()} />
      <SettingsSection card title={t('goals.onItsOwn')} description={t('goals.onItsOwnHint')}>
        <div className="goal-list">
          {running.map((schedule) => (
            <div key={schedule.id} className="goal-row">
              <span className="goal-mark" aria-hidden="true">
                🗓️
              </span>
              <div className="goal-text">
                <Link className="goal-title" to="/settings/agent/schedules">
                  {schedule.name}
                </Link>
                <span className="goal-meta muted">
                  <code>{schedule.cron}</code>
                  {schedule.nextRunAt ? (
                    <span>{t('goals.nextRun', { when: formatTime(schedule.nextRunAt) })}</span>
                  ) : null}
                </span>
              </div>
            </div>
          ))}
          <div className="goal-row">
            <span className="goal-mark" aria-hidden="true">
              📬
            </span>
            <div className="goal-text">
              <Link className="goal-title" to="/settings/mailbox/rules">
                {t('goals.mailRules')}
              </Link>
              <span className="goal-meta muted">{t('goals.mailRulesHint')}</span>
            </div>
          </div>
        </div>
      </SettingsSection>
      <SettingsSection card title={t('goals.set')} description={t('goals.setHint')}>
        <div className="goal-areas">
          {categories.map((category) => (
            <button
              key={category.ideaCategory}
              type="button"
              className="goal-area"
              disabled={busy}
              onClick={() => void startAbout(category.ideaCategory)}
            >
              <span aria-hidden="true">{category.emojis[0]}</span>
              <span>{t(`ideas.category.${category.ideaCategory}` as Key)}</span>
            </button>
          ))}
        </div>
      </SettingsSection>
    </>
  )
}

// GoalRow is one goal in a list: the box that marks it done, its title,
// and its one-line status under it.
function GoalRow({
  goal,
  scheduledNextAt,
  busy,
  onOpen,
  onDone,
}: {
  goal: Goal
  scheduledNextAt?: string
  busy: boolean
  onOpen: (conversationId: string) => void
  onDone: (goal: Goal) => void
}) {
  const { t } = useTranslation()
  const isOpen = goal.goalState === 'working' || goal.goalState === 'waiting'
  return (
    <div className="goal-row">
      <input
        type="checkbox"
        className="goal-check"
        disabled={busy || !isOpen}
        checked={!isOpen}
        title={t('goals.markMet')}
        aria-label={`${goal.goalTitle}: ${t('goals.markMet')}`}
        onChange={() => onDone(goal)}
      />
      <div className="goal-text">
        <button type="button" className="goal-title" onClick={() => onOpen(goal.conversationId)}>
          {goal.goalTitle}
        </button>
        {goal.goalStatus ? <span className="goal-note muted">{goal.goalStatus}</span> : null}
        <span className="goal-meta muted">
          <Tag value={t(STATE_LABEL[goal.goalState])} tone={goal.goalState === 'waiting' ? 'warn' : undefined} />
          {goal.goalState === 'working' && scheduledNextAt ? (
            <span>{t('goals.nextScheduled', { when: formatTime(scheduledNextAt) })}</span>
          ) : goal.goalState === 'working' && goal.goalNextAt ? (
            <span>{t('goals.nextLook', { when: formatTime(goal.goalNextAt) })}</span>
          ) : null}
        </span>
      </div>
    </div>
  )
}

// StartGoalCard starts a goal straight away, from a title and what it is
// for, as `teanode agent goal start` does.
function StartGoalCard({ onStarted }: { onStarted: () => void }) {
  const { t } = useTranslation()
  const toast = useToast()
  const [goalTitle, setGoalTitle] = useState('')
  const [goalDescription, setGoalDescription] = useState('')
  const [busy, setBusy] = useState(false)
  async function start(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    try {
      await graphql(START_GOAL, { goalTitle: goalTitle.trim(), goalDescription: goalDescription.trim() })
      setGoalTitle('')
      setGoalDescription('')
      toast.done(t('goals.started'))
      onStarted()
    } catch (caught) {
      toast.failure(caught, t('goals.failed'))
    } finally {
      setBusy(false)
    }
  }
  return (
    <SettingsSection card title={t('goals.start')} description={t('goals.startHint')}>
      <form className="goal-start" onSubmit={(event) => void start(event)}>
        <input
          type="text"
          value={goalTitle}
          placeholder={t('goals.titlePlaceholder')}
          aria-label={t('goals.titleLabel')}
          onChange={(event) => setGoalTitle(event.target.value)}
        />
        <textarea
          rows={3}
          value={goalDescription}
          placeholder={t('goals.descriptionPlaceholder')}
          aria-label={t('goals.descriptionLabel')}
          onChange={(event) => setGoalDescription(event.target.value)}
        />
        <div>
          <button type="submit" className="primary" disabled={busy || goalDescription.trim() === ''}>
            {t('goals.startButton')}
          </button>
        </div>
      </form>
    </SettingsSection>
  )
}

// GoalPage is one goal: what it is for, where it stands, a box to answer
// it, what it made, and what happened on it.
function GoalPage({ conversationId, onBack }: { conversationId: string; onBack: () => void }) {
  const { t } = useTranslation()
  const toast = useToast()
  const [answer, setAnswer] = useState('')
  const [busy, setBusy] = useState(false)
  const goal = useQuery(() => graphql<{ GetAgentGoal: GoalDetails }>(GOAL, { conversationId }), [conversationId], {
    refresh: true,
  })
  if (goal.loading && !goal.data) return <Loading />
  if (goal.error) return <ErrorMessage error={goal.error} />
  const found = goal.data?.GetAgentGoal
  if (!found) return null
  const isOpen = found.goalState === 'working' || found.goalState === 'waiting'

  async function act(run: () => Promise<unknown>, done: Key) {
    setBusy(true)
    try {
      await run()
      toast.done(t(done))
      await goal.reload()
    } catch (caught) {
      toast.failure(caught, t('goals.failed'))
    } finally {
      setBusy(false)
    }
  }

  async function tell(event: FormEvent) {
    event.preventDefault()
    const text = answer.trim()
    if (!text) return
    await act(() => graphql(TELL_GOAL, { conversationId, text }), 'goals.told')
    setAnswer('')
  }

  const madeCount = found.schedules.length + found.backgroundWork.length + found.artifacts.length
  return (
    <>
      <div className="goal-page-back">
        <button type="button" onClick={onBack}>
          ← {t('goals.back')}
        </button>
      </div>
      <SettingsSection card title={found.goalTitle}>
        <div className="goal-page">
          <span className="goal-meta muted">
            <Tag value={t(STATE_LABEL[found.goalState])} tone={found.goalState === 'waiting' ? 'warn' : undefined} />
            {found.goalSetAt ? <span>{t('goals.since', { when: formatTime(found.goalSetAt) })}</span> : null}
            {found.goalState === 'working' && found.schedules.some((schedule) => schedule.enabled) ? (
              <span>{t('goals.runsOnSchedule')}</span>
            ) : found.goalState === 'working' && found.goalNextAt ? (
              <span>{t('goals.nextLook', { when: formatTime(found.goalNextAt) })}</span>
            ) : null}
          </span>
          {found.goalOriginConversationId ? (
            <span className="goal-origin muted">
              {t('goals.askedIn')}{' '}
              <button
                /* link-button: names a conversation inline in a sentence */
                type="button"
                className="link"
                onClick={() => openAgentConversation(found.goalOriginConversationId)}
              >
                {found.isGoalOriginMain ? t('goals.mainChat') : found.goalOriginTitle || t('goals.aConversation')}
              </button>
            </span>
          ) : null}
          {found.goalStatus ? <p className="goal-status">{found.goalStatus}</p> : null}
          <p className="goal-description">{found.goalDescription}</p>
          {isOpen ? (
            <form className="goal-tell" onSubmit={(event) => void tell(event)}>
              <textarea
                rows={2}
                value={answer}
                placeholder={t(found.goalState === 'waiting' ? 'goals.answerPlaceholder' : 'goals.tellPlaceholder')}
                aria-label={t('goals.tellLabel')}
                onChange={(event) => setAnswer(event.target.value)}
              />
              <div>
                <button type="submit" className="primary" disabled={busy || answer.trim() === ''}>
                  {t('goals.tell')}
                </button>
              </div>
            </form>
          ) : null}
          <div className="goal-actions">
            {isOpen ? (
              <>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() =>
                    void act(() => graphql(SET_GOAL_STATE, { conversationId, goalState: 'met' }), 'goals.markedMet')
                  }
                >
                  {t('goals.markMet')}
                </button>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() =>
                    void act(
                      () => graphql(SET_GOAL_STATE, { conversationId, goalState: 'dropped' }),
                      'goals.markedDropped',
                    )
                  }
                >
                  {t('goals.drop')}
                </button>
              </>
            ) : (
              <button
                type="button"
                disabled={busy}
                onClick={() =>
                  void act(() => graphql(SET_GOAL_STATE, { conversationId, goalState: 'working' }), 'goals.reopened')
                }
              >
                {t('goals.reopen')}
              </button>
            )}
            <button type="button" onClick={() => openAgentConversation(conversationId)}>
              {t('goals.openConversation')}
            </button>
          </div>
        </div>
      </SettingsSection>
      {madeCount > 0 ? (
        <SettingsSection card title={t('goals.artifacts')} description={t('goals.artifactsHint')}>
          <div className="goal-list">
            {found.schedules.map((schedule) => (
              <div key={schedule.id} className="goal-row">
                <span className="goal-mark" aria-hidden="true">
                  🗓️
                </span>
                <div className="goal-text">
                  <Link className="goal-title" to="/settings/agent/schedules">
                    {schedule.name}
                  </Link>
                  <span className="goal-meta muted">
                    <code>{schedule.cron}</code>
                    {schedule.enabled && schedule.nextRunAt ? (
                      <span>{t('goals.nextRun', { when: formatTime(schedule.nextRunAt) })}</span>
                    ) : null}
                  </span>
                </div>
              </div>
            ))}
            {found.backgroundWork.map((work) => (
              <div key={work.id} className="goal-row">
                <span className="goal-mark" aria-hidden="true">
                  ⏳
                </span>
                <div className="goal-text">
                  <span className="goal-title-text">{work.title}</span>
                  <span className="goal-meta muted">
                    <span>{work.workKind}</span>
                    <span>{work.workStatus}</span>
                  </span>
                </div>
              </div>
            ))}
            {found.artifacts.map((artifact) => (
              <div key={artifact.id} className="goal-row">
                <span className="goal-mark" aria-hidden="true">
                  {artifact.goalArtifactKind === 'mail_rule'
                    ? '📬'
                    : artifact.goalArtifactKind === 'reminder'
                      ? '✅'
                      : '🔕'}
                </span>
                <div className="goal-text">
                  <span className="goal-title-text">{artifact.artifactTitle}</span>
                </div>
              </div>
            ))}
          </div>
        </SettingsSection>
      ) : null}
      <SettingsSection card title={t('goals.activity')}>
        {found.activity.length === 0 ? (
          <SettingsEmpty>{t('goals.noActivity')}</SettingsEmpty>
        ) : (
          <ol className="goal-activity">
            {found.activity.map((activity) => (
              <li key={activity.id} className={`goal-activity-item goal-activity-${activity.goalActivityKind}`}>
                <span className="goal-activity-headline">{activity.activityHeadline}</span>
                {activity.activityDetail ? (
                  <span className="goal-activity-detail muted">{activity.activityDetail}</span>
                ) : null}
                <span className="goal-activity-when muted">{formatTime(activity.createdAt)}</span>
              </li>
            ))}
          </ol>
        )}
      </SettingsSection>
    </>
  )
}
