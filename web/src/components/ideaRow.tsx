import { useEffect } from 'react'
import { Link } from 'react-router-dom'

import { graphql, openAgentConversation } from '../api'
import { useTranslation } from '../i18n/i18n'
import { CloseIcon } from './icons'
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
  query ($ideaStatuses: [String!]) {
    ListAgentIdeas(ideaStatuses: $ideaStatuses) {
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
  mutation ($ideaId: String!, $conversationId: String) {
    StartAgentIdea(ideaId: $ideaId, conversationId: $conversationId) { conversation { id } openingRequest }
  }`

const SET_IDEA_STATUS = `
  mutation ($ideaId: String!, $ideaStatus: String!) {
    SetAgentIdeaStatus(ideaId: $ideaId, ideaStatus: $ideaStatus) { id ideaStatus }
  }`

const MARK_IDEAS_SHOWN = `mutation ($ideaIds: [String!]!) { MarkAgentIdeasShown(ideaIds: $ideaIds) }`

// startIdea records that a conversation carries the idea out: the one
// given, or a new one named after it. Nothing is sent; what it answers is
// the conversation and the request to put in its reply box, for the person
// to send or change first.
export async function startIdea(
  idea: Idea,
  conversationId?: string,
): Promise<{ conversationId: string; openingRequest: string }> {
  const started = await graphql<{ StartAgentIdea: { conversation: { id: string }; openingRequest: string } }>(
    START_IDEA,
    { ideaId: idea.id, conversationId: conversationId || null },
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

// The ideas already reported shown in this tab, so that drawing a list
// again does not report them again.
const reportedShown = new Set<string>()

// markIdeasShown reports the ideas the person has in front of them.
export function markIdeasShown(ideas: Idea[]): void {
  const fresh = ideas.filter((idea) => !idea.shownAt && !reportedShown.has(idea.id)).map((idea) => idea.id)
  if (fresh.length === 0) return
  fresh.forEach((id) => reportedShown.add(id))
  void graphql(MARK_IDEAS_SHOWN, { ideaIds: fresh }).catch(() => fresh.forEach((id) => reportedShown.delete(id)))
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
  return (
    <div className="idea-row">
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
          {idea.suggestionReason ? <span className="idea-reason muted">{idea.suggestionReason}</span> : null}
          {idea.evidence.map((evidence) => {
            const link = evidenceLink(evidence)
            return link ? (
              <Link key={evidence.evidenceKind + evidence.evidenceId} className="idea-evidence" to={link}>
                {evidence.evidenceSummary}
              </Link>
            ) : (
              <button
                key={evidence.evidenceKind + evidence.evidenceId}
                type="button"
                className="link idea-evidence"
                onClick={() => openAgentConversation(evidence.evidenceId)}
              >
                {evidence.evidenceSummary}
              </button>
            )
          })}
        </div>
      ) : null}
    </div>
  )
}

// ideaSuggestionCount is how many ideas an empty conversation offers: a
// start, not the whole list, which is on the Ideas tab.
const ideaSuggestionCount = 3

// IdeaSuggestions is the best few open ideas, offered in an empty
// conversation. Choosing one starts it there and puts its request in the
// reply box through onDraft.
export function IdeaSuggestions({
  conversationId,
  onDraft,
}: {
  conversationId: string
  onDraft: (conversationId: string, openingRequest: string) => void
}) {
  const { t } = useTranslation()
  const { data } = useQuery(() => graphql<IdeaList>(LIST_IDEAS, { ideaStatuses: ['open'] }), [])
  const ideas = (data?.ListAgentIdeas.ideas ?? []).slice(0, ideaSuggestionCount)
  useEffect(() => {
    markIdeasShown(ideas)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data])
  if (ideas.length === 0) return null
  return (
    <div className="idea-suggestions">
      <p className="muted idea-suggestions-title">{t('ideas.suggestions')}</p>
      <div className="idea-list">
        {ideas.map((idea) => (
          <IdeaRow
            key={idea.id}
            idea={idea}
            onStart={(chosen) =>
              void startIdea(chosen, conversationId).then((started) =>
                onDraft(started.conversationId, started.openingRequest),
              )
            }
          />
        ))}
      </div>
    </div>
  )
}
