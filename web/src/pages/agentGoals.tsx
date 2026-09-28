import { useState } from 'react'
import { Link } from 'react-router-dom'

import { graphql, openAgentConversation } from '../api'
import { ErrorMessage, Loading, Tag, formatTime } from '../components/common'
import { IdeaList, LIST_IDEAS } from '../components/ideaRow'
import { SettingsEmpty, SettingsSection } from '../components/settingsList'
import { useToast } from '../components/toast'
import { useQuery } from '../components/useQuery'
import { Key, useTranslation } from '../i18n/i18n'

// The Goals tab: what the agent is keeping track of for the person. Every
// goal still in progress, with the agent's latest word on it, which the
// person ticks when it is done; beside them what runs on its own; and a
// way to set a goal about one area of their life. The same goals the
// command line's agent conversation goal lists and the agent's goal tool
// lists and closes.

interface Goal {
  id: string
  title: string
  goal: string
  goalState: 'working' | 'waiting' | 'met' | ''
  goalNote: string
  goalNextAt: string | null
}

interface Schedule {
  id: string
  name: string
  cron: string
  enabled: boolean
  nextRunAt?: string | null
}

const GOALS = `
  query {
    ListAgentConversations(hasGoal: true) { id title goal goalState goalNote goalNextAt }
  }`

const SCHEDULES = `query { ListAgentSchedules { id name cron enabled nextRunAt } }`

const UPDATE_GOAL = `
  mutation ($conversationId: String!, $goal: String, $goalState: String) {
    UpdateAgentConversation(conversationId: $conversationId, goal: $goal, goalState: $goalState) { id }
  }`

const START = `
  mutation ($title: String) {
    StartAgentConversation(title: $title) { id }
  }`

export function GoalsTab() {
  const { t } = useTranslation()
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const goals = useQuery(() => graphql<{ ListAgentConversations: Goal[] }>(GOALS, {}), [], { refresh: true })
  const schedules = useQuery(() => graphql<{ ListAgentSchedules: Schedule[] }>(SCHEDULES, {}), [])
  const ideas = useQuery(() => graphql<IdeaList>(LIST_IDEAS, { ideaStatuses: ['open'] }), [])

  if (goals.loading && !goals.data) return <Loading />
  if (goals.error) return <ErrorMessage error={goals.error} />
  const inProgress = goals.data?.ListAgentConversations ?? []
  const running = (schedules.data?.ListAgentSchedules ?? []).filter((schedule) => schedule.enabled)
  // Every area of a life; the agent's own is for ideas about the agent.
  const categories = (ideas.data?.ListAgentIdeas.ideaCategories ?? []).filter(
    (category) => category.ideaCategory !== 'assistant',
  )

  // Ticking a goal marks it met, with the chance to take that back: the
  // same goal set again, which starts it working from now.
  async function markMet(goal: Goal) {
    setBusy(true)
    try {
      await graphql(UPDATE_GOAL, { conversationId: goal.id, goalState: 'met' })
      toast.done(t('goals.markedMet'), {
        label: t('common.undo'),
        run: async () => {
          await graphql(UPDATE_GOAL, { conversationId: goal.id, goal: goal.goal })
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

  // Setting a goal is a conversation, not a form: the agent asks what the
  // person is after and proposes a goal they can check off, and sets it
  // once they agree. The first line is drafted for them to send.
  async function startGoal(category: string) {
    setBusy(true)
    try {
      const area = t(`ideas.category.${category}` as Key)
      const started = await graphql<{ StartAgentConversation: { id: string } }>(START, {
        title: t('goals.newTitle', { area }),
      })
      openAgentConversation(started.StartAgentConversation.id, t('goals.draft', { area: area.toLowerCase() }))
    } catch (caught) {
      toast.failure(caught, t('goals.failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <SettingsSection card title={t('goals.inProgress')} description={t('goals.inProgressHint')}>
        {inProgress.length === 0 ? (
          <SettingsEmpty>{t('goals.none')}</SettingsEmpty>
        ) : (
          <div className="goal-list">
            {inProgress.map((goal) => (
              <div key={goal.id} className="goal-row">
                <input
                  type="checkbox"
                  className="goal-check"
                  disabled={busy}
                  checked={false}
                  title={t('goals.markMet')}
                  aria-label={`${goal.goal}: ${t('goals.markMet')}`}
                  onChange={() => void markMet(goal)}
                />
                <div className="goal-text">
                  <button type="button" className="goal-title" onClick={() => openAgentConversation(goal.id)}>
                    {goal.goal}
                  </button>
                  {goal.goalNote ? <span className="goal-note muted">{goal.goalNote}</span> : null}
                  <span className="goal-meta muted">
                    <Tag
                      value={t(goal.goalState === 'waiting' ? 'goals.waiting' : 'goals.working')}
                      tone={goal.goalState === 'waiting' ? 'warn' : undefined}
                    />
                    {goal.goalState !== 'waiting' && goal.goalNextAt ? (
                      <span>{t('goals.nextLook', { when: formatTime(goal.goalNextAt) })}</span>
                    ) : null}
                    {goal.title && goal.title !== goal.goal ? <span>{goal.title}</span> : null}
                  </span>
                </div>
              </div>
            ))}
          </div>
        )}
      </SettingsSection>
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
              <Link className="goal-title" to="/mailbox/settings/rules">
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
              onClick={() => void startGoal(category.ideaCategory)}
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
