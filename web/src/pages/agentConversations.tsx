import { useEffect, useMemo, useState } from 'react'

import { graphql, openAgentConversation, startAgentConversation } from '../api'
import { useAgentIdentity } from '../agentPreferences'
import { CONVERSATIONS, type Conversation } from '../components/agentDrawer'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { Loading } from '../components/common'
import { PlusIcon, StarIcon, TargetIcon } from '../components/icons'
import { RelativeTime } from '../components/relativeTime'
import { SettingsEmpty } from '../components/settingsList'
import { useToast } from '../components/toast'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'

// The person's conversations with their agent, as tiles: the list the
// drawer's picker holds, on a page wide enough to read what each was about.
// The page lists and the drawer talks. A tile's title opens the conversation
// in the drawer, and a new one is started there too, so there is one place a
// conversation is carried on.
export function AgentConversationsPage() {
  const { t } = useTranslation()
  const toast = useToast()
  const agent = useAgentIdentity()
  const agentLabel = agent.name || t('nav.agent')
  useBreadcrumbDetail(agentLabel)

  // The same question the picker asks, and re-asked on the same interval as
  // any list, so a conversation started from a terminal or a phone appears.
  const { data, error, loading, reload } = useQuery(() =>
    graphql<{ ListAgentConversations: Conversation[] }>(CONVERSATIONS, { archived: false }),
  )
  useEffect(() => {
    if (error) toast.failure(error, t('agentConversations.loadFailed'))
    // Once per failure: the toast is not to come back with every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [error])

  // Typed words narrow the tiles at once, by what each conversation is
  // called and by the summary the agent wrote of it.
  const [filter, setFilter] = useState('')
  const conversations = useMemo(() => {
    const listed = data?.ListAgentConversations ?? []
    const words = filter.trim().toLowerCase()
    const matching = words
      ? listed.filter((conversation) =>
          [conversation.kind === 'main' ? t('agentDrawer.main') : conversation.title, conversation.summary ?? '']
            .join(' ')
            .toLowerCase()
            .includes(words),
        )
      : listed
    // The main chat first, as the picker has it, then by when each was last
    // spoken in, because that is how somebody looks for one.
    return [...matching].sort((first, second) => {
      if (first.kind === 'main' || second.kind === 'main') return first.kind === 'main' ? -1 : 1
      return (second.lastAt ?? '').localeCompare(first.lastAt ?? '')
    })
  }, [data, filter, t])

  const open = (conversationId: string) => {
    if (!openAgentConversation(conversationId)) toast.failed(t('agentConversations.unavailable'))
  }

  const startNew = () => {
    if (!startAgentConversation(() => void reload(true))) toast.failed(t('agentConversations.unavailable'))
  }

  const hasConversations = (data?.ListAgentConversations.length ?? 0) > 0

  return (
    <>
      <div className="page-actions">
        <p className="muted">{t('agentConversations.intro', { name: agentLabel })}</p>
        <button type="button" className="primary" onClick={startNew}>
          <PlusIcon size={16} />
          {t('agentConversations.new')}
        </button>
      </div>
      {hasConversations && (
        <div className="conversation-search">
          <input
            type="search"
            value={filter}
            placeholder={t('agentConversations.search')}
            aria-label={t('agentConversations.search')}
            onChange={(event) => setFilter(event.target.value)}
          />
        </div>
      )}
      {loading && !data ? (
        <Loading />
      ) : !data ? null : !hasConversations ? (
        <SettingsEmpty>{t('agentConversations.empty')}</SettingsEmpty>
      ) : conversations.length === 0 ? (
        <SettingsEmpty>{t('agentConversations.nothingFound')}</SettingsEmpty>
      ) : (
        <ul className="tile-grid conversation-tiles">
          {conversations.map((conversation) => (
            <li key={conversation.id} className="tile conversation-tile">
              <button type="button" className="conversation-tile-title" onClick={() => open(conversation.id)}>
                {conversation.kind === 'main' ? (
                  <>
                    <StarIcon size={14} /> {t('agentDrawer.main')}
                  </>
                ) : (
                  conversation.title || t('agentDrawer.untitled')
                )}
              </button>
              {/* What it is about: the agent's summary, else what it is
                  working toward, so no tile is a bare title. */}
              {conversation.summary || conversation.goal ? (
                <p className="conversation-tile-summary">{conversation.summary || conversation.goal}</p>
              ) : (
                <p className="conversation-tile-summary muted">{t('agentConversations.noSummary')}</p>
              )}
              <span className="tile-detail">
                {/* A conversation working toward something says so, in the
                    color of where it stands, the way the picker marks it. */}
                {conversation.goal ? (
                  <TargetIcon size={12} className={`agent-drawer-list-goal ${conversation.goalState || 'working'}`} />
                ) : null}
                {t('agentConversations.lastActive')} <RelativeTime value={conversation.lastAt} />
              </span>
            </li>
          ))}
        </ul>
      )}
    </>
  )
}
