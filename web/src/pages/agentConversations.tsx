import { useEffect, useMemo, useState } from 'react'

import { graphql, openAgentConversation, startAgentConversation } from '../api'
import { useAgentIdentity } from '../agentPreferences'
import { CONVERSATIONS, type Conversation } from '../components/agentDrawer'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { Loading } from '../components/common'
import { HighlightText } from '../components/highlightText'
import { PlusIcon, StarIcon } from '../components/icons'
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
      <p className="muted">{t('agentConversations.intro', { name: agentLabel })}</p>
      {/* Starting one first, then finding one: the button leads the row
          and the search box takes the rest of it. */}
      <div className="conversation-toolbar">
        <button type="button" className="primary with-icon" onClick={startNew}>
          <PlusIcon size={16} />
          {t('agentConversations.new')}
        </button>
        {hasConversations && (
          <input
            type="search"
            value={filter}
            placeholder={t('agentConversations.search')}
            aria-label={t('agentConversations.search')}
            onChange={(event) => setFilter(event.target.value)}
          />
        )}
      </div>
      {loading && !data ? (
        <Loading />
      ) : !data ? null : !hasConversations ? (
        <SettingsEmpty>{t('agentConversations.empty')}</SettingsEmpty>
      ) : conversations.length === 0 ? (
        <SettingsEmpty>{t('agentConversations.nothingFound')}</SettingsEmpty>
      ) : (
        <ul className="tile-grid conversation-tiles">
          {conversations.map((conversation) => (
            <li key={conversation.id}>
              {/* The whole tile is the button that opens the conversation.
                  It is named by its title alone, and its summary describes
                  it, so a screen reader hears the name first rather than
                  three lines of summary and a time. */}
              <button
                type="button"
                className="tile conversation-tile"
                aria-labelledby={`conversation-${conversation.id}-title`}
                aria-describedby={`conversation-${conversation.id}-summary`}
                onClick={() => open(conversation.id)}
              >
                <span className="conversation-tile-title" id={`conversation-${conversation.id}-title`}>
                  {conversation.kind === 'main' ? (
                    <>
                      <StarIcon size={14} /> <HighlightText text={t('agentDrawer.main')} search={filter} />
                    </>
                  ) : (
                    <HighlightText text={conversation.title || t('agentDrawer.untitled')} search={filter} />
                  )}
                </span>
                {/* What it is about: the agent's summary, else what it is
                    working toward, so no tile is a bare title. */}
                <span
                  className={conversation.summary ? 'conversation-tile-summary' : 'conversation-tile-summary muted'}
                  id={`conversation-${conversation.id}-summary`}
                >
                  {conversation.summary ? (
                    <HighlightText text={conversation.summary} search={filter} />
                  ) : (
                    t('agentConversations.noSummary')
                  )}
                </span>
                <span className="tile-detail">
                  <RelativeTime value={conversation.lastAt} />
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </>
  )
}
