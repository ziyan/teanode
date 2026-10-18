import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { graphql, openAgentConversation, startAgentConversation } from '../api'
import { useAgentIdentity } from '../agentPreferences'
import { ChatAvatars, UnreadCount, chatTitleOf } from '../components/agentChat'
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
import { useOptionalSession } from '../session'

// Which of the conversations the page lists: all of them, the ones with the
// person's agent, or the chats with other people.
type KindFilter = 'all' | 'agent' | 'chats'

// The person's conversations with their agent, as tiles: the list the
// drawer's picker holds, on a page wide enough to read what each was about.
// The page lists and the drawer talks. A tile's title opens the conversation
// in the drawer, and a new one is started there too, so there is one place a
// conversation is carried on.
export function AgentConversationsPage() {
  const { t, language } = useTranslation()
  const toast = useToast()
  const navigate = useNavigate()
  const agent = useAgentIdentity()
  const ownUserId = useOptionalSession()?.userId ?? ''
  const deletedNote = t('chat.deletedAccount')
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
  const [kindFilter, setKindFilter] = useState<KindFilter>('all')
  // A conversation nobody has named is known by when it last moved, which is
  // all a list of them has to tell two of them apart by.
  const untitledOf = (conversation: Conversation) =>
    t('agentConversations.untitledOn', {
      date: new Date(conversation.lastAt).toLocaleString(language, {
        month: 'short',
        day: 'numeric',
        hour: 'numeric',
        minute: '2-digit',
      }),
    })
  // What a conversation is called here: a chat by the people in it.
  const titleOf = (conversation: Conversation) =>
    conversation.kind === 'main'
      ? t('agentDrawer.main')
      : conversation.chat
        ? chatTitleOf(conversation.chat, ownUserId, deletedNote, conversation.title || untitledOf(conversation))
        : conversation.title
  const conversations = useMemo(() => {
    const listed = (data?.ListAgentConversations ?? []).filter(
      (conversation) => kindFilter === 'all' || (kindFilter === 'chats') === (conversation.kind === 'chat'),
    )
    const words = filter.trim().toLowerCase()
    const matching = words
      ? listed.filter((conversation) =>
          [titleOf(conversation), conversation.summary ?? ''].join(' ').toLowerCase().includes(words),
        )
      : listed
    // The main chat first, as the picker has it, then by when each was last
    // spoken in, because that is how somebody looks for one.
    return [...matching].sort((first, second) => {
      if (first.kind === 'main' || second.kind === 'main') return first.kind === 'main' ? -1 : 1
      return (second.lastAt ?? '').localeCompare(first.lastAt ?? '')
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data, filter, kindFilter, ownUserId, t])

  // Turning it on is on the agent's own settings page, where each person
  // does it for themselves.
  const turnOnAction = { label: t('agentConversations.turnOnAction'), run: () => navigate('/settings/agent') }

  const open = (conversationId: string) => {
    if (!openAgentConversation(conversationId)) toast.failed(t('agentConversations.unavailable'), turnOnAction)
  }

  // A link from the agent's memory names the conversation in the address
  // ("?conversation=...&message=..."): it opens in the drawer once, and the
  // address forgets it, so a reload does not open it again.
  const [parameters, setParameters] = useSearchParams()
  const linkedConversationId = parameters.get('conversation') ?? ''
  const openedLink = useRef('')
  useEffect(() => {
    if (!linkedConversationId || openedLink.current === linkedConversationId) return
    openedLink.current = linkedConversationId
    if (!openAgentConversation(linkedConversationId)) toast.failed(t('agentConversations.unavailable'), turnOnAction)
    setParameters(
      (previous) => {
        const next = new URLSearchParams(previous)
        next.delete('conversation')
        next.delete('message')
        return next
      },
      { replace: true },
    )
    // The toast and the translation are made again on every render; the
    // conversation is opened once.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [linkedConversationId])

  const startNew = () => {
    if (!startAgentConversation(() => void reload(true)))
      toast.failed(t('agentConversations.unavailable'), turnOnAction)
  }

  const hasConversations = (data?.ListAgentConversations.length ?? 0) > 0
  const hasChats = (data?.ListAgentConversations ?? []).some((conversation) => conversation.kind === 'chat')

  return (
    <>
      {/* Starting one first, then finding one: the button leads the row
          and the search box takes the rest of it. */}
      <div className="conversation-toolbar">
        <button type="button" className="primary with-icon" onClick={startNew}>
          <PlusIcon size={16} />
          {t('agentConversations.new')}
        </button>
        {hasChats && (
          <select
            value={kindFilter}
            aria-label={t('agentConversations.kindLabel')}
            onChange={(event) => setKindFilter(event.target.value as KindFilter)}
          >
            <option value="all">{t('agentConversations.kindAll')}</option>
            <option value="agent">{t('agentConversations.kindAgent')}</option>
            <option value="chats">{t('agentConversations.kindChats')}</option>
          </select>
        )}
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
                  ) : conversation.chat ? (
                    <>
                      <ChatAvatars chat={conversation.chat} ownUserId={ownUserId} deleted={deletedNote} size={20} />
                      <HighlightText text={titleOf(conversation) || untitledOf(conversation)} search={filter} />
                    </>
                  ) : (
                    <HighlightText text={conversation.title || untitledOf(conversation)} search={filter} />
                  )}
                </span>
                {/* What it is about: the agent's summary, else what it is
                    working toward, so no tile is a bare title. */}
                {/* A direct chat is titled by the person it is with, so a
                    line saying it is a chat with them repeats the title. */}
                {conversation.chat?.chatKind === 'direct' && !conversation.summary ? null : (
                <span
                  className={conversation.summary ? 'conversation-tile-summary' : 'conversation-tile-summary muted'}
                  id={`conversation-${conversation.id}-summary`}
                >
                  {conversation.summary ? (
                    <HighlightText text={conversation.summary} search={filter} />
                  ) : conversation.chat ? (
                    t('chat.withPeople', {
                      names: conversation.chat.members
                        .filter((member) => member.userId !== ownUserId && !member.isRemoved)
                        .map((member) => member.name?.trim() || member.username)
                        .join(', '),
                    })
                  ) : (
                    t('agentConversations.noSummary')
                  )}
                </span>
                )}
                <span className="tile-detail">
                  <RelativeTime value={conversation.lastAt} />
                  {conversation.chat ? <UnreadCount count={conversation.chat.unreadCount} /> : null}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </>
  )
}
