import { useState } from 'react'

import { graphql, openAgentConversation, sendToAgentConversation } from '../api'
import { ErrorMessage, Loading, Tag, formatTime } from '../components/common'
import { Column, DataTable } from '../components/dataTable'
import { CheckIcon, RestartIcon } from '../components/icons'
import { Idea, IdeaList, IdeaRow, LIST_IDEAS, setIdeaStatus, startIdea } from '../components/ideaRow'
import { SettingsEmpty, SettingsSection } from '../components/settingsList'
import { useToast } from '../components/toast'
import { useQuery } from '../components/useQuery'
import { Key, useTranslation } from '../i18n/i18n'

// The Ideas tab: what the agent offers to do for the person, the ones found
// in their own mail and memory first and then the rest by area, and below
// them the history of every idea taken up, finished, dismissed or gone
// stale. The same list the command line's agent idea shows and the agent
// manages with its idea tool.

type ClosedStatus = 'started' | 'done' | 'dismissed' | 'expired'

const HISTORY_STATUSES: ClosedStatus[] = ['started', 'done', 'dismissed', 'expired']

export function IdeasTab() {
  const { t, language } = useTranslation()
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const { data, error, loading, reload } = useQuery(() => graphql<IdeaList>(LIST_IDEAS, { language }), [language])
  const ideas = data?.ListAgentIdeas.ideas ?? []
  const categories = data?.ListAgentIdeas.ideaCategories ?? []
  const open = ideas.filter((idea) => idea.ideaStatus === 'open')
  const history = ideas.filter((idea) => idea.ideaStatus !== 'open')

  if (loading && !data) return <Loading />
  if (error) return <ErrorMessage error={error} />

  async function act(work: () => Promise<void>, done?: string) {
    setBusy(true)
    try {
      await work()
      if (done) toast.done(done)
      await reload()
    } catch (caught) {
      toast.failure(caught, t('ideas.failed'))
    } finally {
      setBusy(false)
    }
  }
  const onStart = (idea: Idea) =>
    void act(async () => {
      const started = await startIdea(idea, language)
      sendToAgentConversation(started.conversationId, started.openingRequest)
    })
  const onDismiss = (idea: Idea) => void act(() => setIdeaStatus(idea, 'dismissed'), t('ideas.dismissed'))

  const personal = open.filter((idea) => idea.ideaKind === 'personal')
  const sections = categories
    .map((category) => ({
      category: category.ideaCategory,
      ideas: open.filter((idea) => idea.ideaKind === 'catalog' && idea.ideaCategory === category.ideaCategory),
    }))
    .filter((section) => section.ideas.length > 0)

  return (
    <>
      <SettingsSection card title={t('ideas.forYou')} description={t('ideas.forYouHint')}>
        {personal.length === 0 ? (
          <SettingsEmpty>{t('ideas.noneForYou')}</SettingsEmpty>
        ) : (
          <div className="idea-list idea-list-personal">
            {personal.map((idea) => (
              <IdeaRow key={idea.id} idea={idea} busy={busy} onStart={onStart} onDismiss={onDismiss} />
            ))}
          </div>
        )}
      </SettingsSection>
      {sections.map((section) => (
        <SettingsSection key={section.category} card title={t(`ideas.category.${section.category}` as Key)}>
          <div className="idea-list">
            {section.ideas.map((idea) => (
              <IdeaRow key={idea.id} idea={idea} busy={busy} onStart={onStart} onDismiss={onDismiss} />
            ))}
          </div>
        </SettingsSection>
      ))}
      <IdeaHistory
        ideas={history}
        busy={busy}
        onMarkDone={(idea) => void act(() => setIdeaStatus(idea, 'done'), t('ideas.markedDone'))}
        onBringBack={(idea) => void act(() => setIdeaStatus(idea, 'open'), t('ideas.broughtBack'))}
      />
    </>
  )
}

// IdeaHistory is every idea that is no longer only on offer: taken up,
// finished, dismissed or gone stale, the latest first.
function IdeaHistory({
  ideas,
  busy,
  onMarkDone,
  onBringBack,
}: {
  ideas: Idea[]
  busy: boolean
  onMarkDone: (idea: Idea) => void
  onBringBack: (idea: Idea) => void
}) {
  const { t, plural } = useTranslation()
  const when = (idea: Idea) => idea.closedAt ?? idea.startedAt ?? idea.createdAt
  const rows = [...ideas].sort((first, second) => when(second).localeCompare(when(first)))
  const tone = (idea: Idea) =>
    idea.ideaStatus === 'done' ? 'good' : idea.ideaStatus === 'started' ? 'warn' : undefined
  const columns: Column<Idea>[] = [
    {
      key: 'headline',
      header: t('ideas.idea'),
      truncate: true,
      value: (idea) => idea.headline,
      render: (idea) =>
        idea.startedConversationId ? (
          <button
            type="button"
            className="idea-history-title"
            title={idea.headline}
            onClick={() => openAgentConversation(idea.startedConversationId)}
          >
            {idea.emoji} {idea.headline}
          </button>
        ) : (
          <span className="idea-history-title" title={idea.headline}>
            {idea.emoji} {idea.headline}
          </span>
        ),
    },
    {
      key: 'ideaStatus',
      header: t('ideas.status'),
      width: '8rem',
      filter: 'select',
      options: HISTORY_STATUSES.map((status) => ({ value: status, label: t(`ideas.status.${status}` as Key) })),
      value: (idea) => idea.ideaStatus,
      render: (idea) => <Tag value={t(`ideas.status.${idea.ideaStatus}` as Key)} tone={tone(idea)} />,
    },
    {
      key: 'when',
      header: t('ideas.when'),
      width: '11rem',
      value: (idea) => formatTime(when(idea)),
      sort: (first, second) => when(first).localeCompare(when(second)),
    },
    {
      key: 'actions',
      header: '',
      width: '3rem',
      render: (idea) =>
        idea.ideaStatus === 'started' ? (
          <button
            type="button"
            className="icon-action"
            disabled={busy}
            title={t('ideas.markDone')}
            aria-label={`${idea.headline}: ${t('ideas.markDone')}`}
            onClick={() => onMarkDone(idea)}
          >
            <CheckIcon size={16} />
          </button>
        ) : idea.ideaStatus === 'dismissed' || idea.ideaStatus === 'expired' ? (
          <button
            type="button"
            className="icon-action"
            disabled={busy}
            title={t('ideas.bringBack')}
            aria-label={`${idea.headline}: ${t('ideas.bringBack')}`}
            onClick={() => onBringBack(idea)}
          >
            <RestartIcon size={16} />
          </button>
        ) : null,
    },
  ]
  return (
    <SettingsSection card title={t('ideas.history')} description={t('ideas.historyHint')}>
      <DataTable
        columns={columns}
        rows={rows}
        rowKey={(idea) => idea.id}
        emptyMessage={t('ideas.noHistory')}
        countLabel={(count) => plural(count, { one: 'ideas.countOne', other: 'ideas.countOther' })}
      />
    </SettingsSection>
  )
}
