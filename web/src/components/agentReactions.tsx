import { useEffect, useRef } from 'react'
import { useTranslation } from '../i18n/i18n'

// Reactions on the messages of a conversation: an emoji the person or the
// agent put on one, as in a chat app. One each per message; reacting again
// replaces it, and the same one again takes it away.

export interface AgentMessageReaction {
  emoji: string
  reactorKind: 'person' | 'agent' | string
  reactedAt?: string
}

// QUICK_REACTIONS are what the picker offers. The server takes any one or
// two emoji; the command line and the agent can use others.
export const QUICK_REACTIONS = ['👍', '❤️', '😂', '😮', '😢', '🙏', '🔥', '🎉']

// withReaction is the reactions with one reactor's replaced: its emoji now,
// or none when the emoji is empty. The person's come before the agent's,
// as the server orders them.
export function withReaction(
  reactions: AgentMessageReaction[] | undefined,
  reactorKind: string,
  emoji: string,
  reactedAt?: string,
): AgentMessageReaction[] {
  const others = (reactions ?? []).filter((reaction) => reaction.reactorKind !== reactorKind)
  const next = emoji ? [...others, { emoji, reactorKind, reactedAt }] : others
  return next.sort((left, right) =>
    left.reactorKind === right.reactorKind ? 0 : left.reactorKind === 'person' ? -1 : 1,
  )
}

// personReaction is the emoji the person has on a message, or empty.
export function personReaction(reactions: AgentMessageReaction[] | undefined): string {
  return reactions?.find((reaction) => reaction.reactorKind === 'person')?.emoji ?? ''
}

// ReactionBadges sit on the bubble's lower corner: the person's is a button
// that takes it away, the agent's says who put it there.
export function ReactionBadges({
  reactions,
  onRemove,
}: {
  reactions: AgentMessageReaction[] | undefined
  onRemove?: () => void
}) {
  const { t } = useTranslation()
  if (!reactions || reactions.length === 0) return null
  return (
    <span className="agent-reactions">
      {reactions.map((reaction) =>
        reaction.reactorKind === 'person' && onRemove ? (
          <button
            key={reaction.reactorKind}
            type="button"
            className="agent-reaction mine"
            title={t('agentDrawer.reactionYours', { emoji: reaction.emoji })}
            aria-label={t('agentDrawer.reactionYours', { emoji: reaction.emoji })}
            onClick={onRemove}
          >
            {reaction.emoji}
          </button>
        ) : (
          <span
            key={reaction.reactorKind}
            className="agent-reaction"
            title={
              reaction.reactorKind === 'agent'
                ? t('agentDrawer.reactionAgent', { emoji: reaction.emoji })
                : t('agentDrawer.reactionPerson', { emoji: reaction.emoji })
            }
            aria-label={
              reaction.reactorKind === 'agent'
                ? t('agentDrawer.reactionAgent', { emoji: reaction.emoji })
                : t('agentDrawer.reactionPerson', { emoji: reaction.emoji })
            }
          >
            {reaction.emoji}
          </span>
        ),
      )}
    </span>
  )
}

// ReactionPicker is the row of quick reactions over a message, opened from
// its React button. The one the person already has is marked, and choosing
// it again takes it away. A click anywhere else or Escape closes it.
export function ReactionPicker({
  current,
  onPick,
  onClose,
}: {
  current: string
  onPick: (emoji: string) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const picker = useRef<HTMLDivElement>(null)
  useEffect(() => {
    // A press on the message it belongs to is left to the message, whose
    // tap opens and closes the picker on a phone.
    const closeOutside = (event: PointerEvent) => {
      const line = picker.current?.closest('.agent-line')
      if (picker.current && !picker.current.contains(event.target as Node) && !line?.contains(event.target as Node))
        onClose()
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    document.addEventListener('pointerdown', closeOutside)
    document.addEventListener('keydown', closeOnEscape)
    picker.current?.querySelector<HTMLButtonElement>('button')?.focus()
    return () => {
      document.removeEventListener('pointerdown', closeOutside)
      document.removeEventListener('keydown', closeOnEscape)
    }
  }, [onClose])
  return (
    <div className="agent-reaction-picker" role="menu" aria-label={t('agentDrawer.react')} ref={picker}>
      {QUICK_REACTIONS.map((emoji) => {
        const isCurrent = emoji === current
        const label = isCurrent ? t('agentDrawer.reactionYours', { emoji }) : t('agentDrawer.reactWith', { emoji })
        return (
          <button
            key={emoji}
            type="button"
            role="menuitemcheckbox"
            className={['agent-reaction-choice', isCurrent ? 'current' : ''].filter(Boolean).join(' ')}
            title={label}
            aria-label={label}
            aria-checked={isCurrent}
            onClick={() => onPick(isCurrent ? '' : emoji)}
          >
            {emoji}
          </button>
        )
      })}
    </div>
  )
}
