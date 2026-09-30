import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'

import { graphql, openAgentConversation } from '../api'
import { useTranslation } from '../i18n/i18n'
import { CloseIcon, GraphIcon, MailIcon, SparkIcon } from './icons'
import { useToast } from './toast'
import { useQuery } from './useQuery'

// An idea is an offer of work the agent can do for the person, with what
// became of it. The Ideas tab lists them all; the drawer shows the first
// few in an empty conversation. Both draw them with IdeaRow and act on them
// with the operations below, the same ones the command line and the agent's
// own idea tool call.

export type IdeaStatus = 'open' | 'started' | 'done' | 'dismissed' | 'expired'

export interface IdeaEvidence {
  evidenceKind: 'message' | 'page' | 'conversation'
  evidenceId: string
  evidenceSummary: string
}

export interface Idea {
  id: string
  ideaKind: 'catalog' | 'personal'
  ideaCategory: string
  emoji: string
  headline: string
  body: string
  openingRequest: string
  suggestionReason: string
  evidence: IdeaEvidence[]
  ideaStatus: IdeaStatus
  startedConversationId: string
  createdAt: string
  shownAt: string | null
  startedAt: string | null
  closedAt: string | null
}

export interface IdeaCategory {
  ideaCategory: string
  emojis: string[]
}

export const LIST_IDEAS = `
  query ($ideaStatuses: [String!], $language: String) {
    ListAgentIdeas(ideaStatuses: $ideaStatuses, language: $language) {
      ideas {
        id ideaKind ideaCategory emoji headline body openingRequest suggestionReason
        evidence { evidenceKind evidenceId evidenceSummary }
        ideaStatus startedConversationId createdAt shownAt startedAt closedAt
      }
      ideaCategories { ideaCategory emojis }
    }
  }`

export type IdeaList = { ListAgentIdeas: { ideas: Idea[]; ideaCategories: IdeaCategory[] } }

const START_IDEA = `
  mutation ($ideaId: String!, $conversationId: String, $language: String) {
    StartAgentIdea(ideaId: $ideaId, conversationId: $conversationId, language: $language) {
      conversation { id }
      openingRequest
    }
  }`

const SET_IDEA_STATUS = `
  mutation ($ideaId: String!, $ideaStatus: String!) {
    SetAgentIdeaStatus(ideaId: $ideaId, ideaStatus: $ideaStatus) { id ideaStatus }
  }`

const MARK_IDEAS_SHOWN = `mutation ($ideaIds: [String!]!) { MarkAgentIdeasShown(ideaIds: $ideaIds) }`

// startIdea records that a conversation carries the idea out: the one
// given, or a new one named after it. Nothing is sent here; what it answers
// is the conversation and the request the caller sends there as the
// person's first message, through the drawer.
export async function startIdea(
  idea: Idea,
  language: string,
  conversationId?: string,
): Promise<{ conversationId: string; openingRequest: string }> {
  const started = await graphql<{ StartAgentIdea: { conversation: { id: string }; openingRequest: string } }>(
    START_IDEA,
    { ideaId: idea.id, conversationId: conversationId || null, language },
  )
  return {
    conversationId: started.StartAgentIdea.conversation.id,
    openingRequest: started.StartAgentIdea.openingRequest,
  }
}

// setIdeaStatus says what became of an idea: done, dismissed, or open.
export async function setIdeaStatus(idea: Idea, ideaStatus: 'done' | 'dismissed' | 'open'): Promise<void> {
  await graphql(SET_IDEA_STATUS, { ideaId: idea.id, ideaStatus })
}

// Being shown is reported for an idea when most of its row has been on the
// screen, not when it was drawn: a tab of thirty ideas is not thirty seen,
// and the agent offers on its own only the ones nobody has seen. The ids
// are gathered for a moment and reported together.
const reportedShown = new Set<string>()
const waitingToReport = new Set<string>()
let reportTimer: ReturnType<typeof setTimeout> | undefined

function reportShown(id: string) {
  if (reportedShown.has(id)) return
  reportedShown.add(id)
  waitingToReport.add(id)
  if (reportTimer) return
  reportTimer = setTimeout(() => {
    reportTimer = undefined
    const ideaIds = [...waitingToReport]
    waitingToReport.clear()
    void graphql(MARK_IDEAS_SHOWN, { ideaIds }).catch(() => ideaIds.forEach((id) => reportedShown.delete(id)))
  }, 1000)
}

let seenObserver: IntersectionObserver | undefined

function observeSeen(element: Element, id: string): () => void {
  if (typeof IntersectionObserver === 'undefined') return () => {}
  seenObserver ??= new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        const seen = (entry.target as HTMLElement).dataset.ideaId
        if (entry.isIntersecting && seen) {
          reportShown(seen)
          seenObserver?.unobserve(entry.target)
        }
      }
    },
    { threshold: 0.6 },
  )
  ;(element as HTMLElement).dataset.ideaId = id
  seenObserver.observe(element)
  return () => seenObserver?.unobserve(element)
}

// evidenceLink is where a piece of evidence opens: a message where it is,
// a memory page in the explorer; a conversation opens in the drawer.
function evidenceLink(evidence: IdeaEvidence): string | null {
  switch (evidence.evidenceKind) {
    case 'message':
      return `/mailbox/starred/${encodeURIComponent(evidence.evidenceId)}`
    case 'page':
      return `/settings/knowledge/explore?from=${encodeURIComponent(evidence.evidenceId)}`
  }
  return null
}

// IdeaRow is one idea: its emoji, the offer, what happens, and for one
// found in the person's own data, why and from what. The row is the button
// that starts it; the mark in its corner dismisses it. A word there would
// take a line of its own under every idea.
export function IdeaRow({
  idea,
  busy,
  onStart,
  onDismiss,
}: {
  idea: Idea
  busy?: boolean
  onStart: (idea: Idea) => void
  onDismiss?: (idea: Idea) => void
}) {
  const { t } = useTranslation()
  const row = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (idea.shownAt || !row.current) return
    return observeSeen(row.current, idea.id)
  }, [idea.id, idea.shownAt])
  return (
    <div className="idea-row" ref={row}>
      <button type="button" className="idea-start" disabled={busy} onClick={() => onStart(idea)}>
        <span className="idea-emoji" aria-hidden="true">
          {idea.emoji}
        </span>
        <span className="idea-text">
          <span className="idea-headline">{idea.headline}</span>
          <span className="idea-body muted">{idea.body}</span>
        </span>
      </button>
      {onDismiss ? (
        <button
          type="button"
          className="icon-action idea-dismiss"
          disabled={busy}
          title={t('ideas.notInterested')}
          aria-label={`${idea.headline}: ${t('ideas.notInterested')}`}
          onClick={() => onDismiss(idea)}
        >
          <CloseIcon size={14} />
        </button>
      ) : null}
      {idea.suggestionReason || idea.evidence.length > 0 ? (
        <div className="idea-foot">
          {idea.suggestionReason ? <p className="idea-reason muted">{idea.suggestionReason}</p> : null}
          {idea.evidence.length > 0 ? (
            <ul className="idea-sources">
              {idea.evidence.map((evidence) => {
                const link = evidenceLink(evidence)
                const Icon =
                  evidence.evidenceKind === 'message'
                    ? MailIcon
                    : evidence.evidenceKind === 'page'
                      ? GraphIcon
                      : SparkIcon
                const label = (
                  <>
                    <Icon size={13} />
                    <span>{evidence.evidenceSummary}</span>
                  </>
                )
                return (
                  <li key={evidence.evidenceKind + evidence.evidenceId}>
                    {link ? (
                      <Link className="idea-source" to={link} title={evidence.evidenceSummary}>
                        {label}
                      </Link>
                    ) : (
                      <button
                        type="button"
                        className="idea-source"
                        title={evidence.evidenceSummary}
                        onClick={() => openAgentConversation(evidence.evidenceId)}
                      >
                        {label}
                      </button>
                    )}
                  </li>
                )
              })}
            </ul>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

// ideaSuggestionCount is how many ideas an empty conversation offers: a
// start, not the whole list, which is on the Ideas tab.
const ideaSuggestionCount = 3

// IdeaSuggestions is the best few open ideas, offered in an empty
// conversation. Choosing one starts it there and hands its request to
// onStarted, which sends it.
export function IdeaSuggestions({
  conversationId,
  onStarted,
}: {
  conversationId: string
  onStarted: (conversationId: string, openingRequest: string) => void
}) {
  const { t, language } = useTranslation()
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const { data } = useQuery(() => graphql<IdeaList>(LIST_IDEAS, { ideaStatuses: ['open'], language }), [language])
  const ideas = (data?.ListAgentIdeas.ideas ?? []).slice(0, ideaSuggestionCount)
  if (ideas.length === 0) return null
  const onStart = async (chosen: Idea) => {
    setBusy(true)
    try {
      const started = await startIdea(chosen, language, conversationId)
      onStarted(started.conversationId, started.openingRequest)
    } catch (caught) {
      toast.failure(caught, t('ideas.failed'))
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="idea-suggestions">
      <p className="muted idea-suggestions-title">{t('ideas.suggestions')}</p>
      <div className="idea-list">
        {ideas.map((idea) => (
          <IdeaRow key={idea.id} idea={idea} busy={busy} onStart={(chosen) => void onStart(chosen)} />
        ))}
      </div>
    </div>
  )
}
