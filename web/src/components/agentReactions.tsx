import { useEffect, useId, useRef, useState } from 'react'
import { useTranslation, type Key } from '../i18n/i18n'
import { EMOJI_CATEGORIES, reactionEntryProblem, type ReactionEntryProblem } from './emojiChoices'
import { PlusIcon } from './icons'

// Reactions on the messages of a conversation: an emoji the person or the
// agent put on one, as in a chat app. One each per message; reacting again
// replaces it, and the same one again takes it away.

export interface AgentMessageReaction {
  emoji: string
  reactorKind: 'person' | 'agent' | string
  reactedAt?: string
  // Who, where more than one person can react: a chat. A conversation with
  // an agent has the one person, who these leave out or name alike.
  reactorUserId?: string | null
  reactorName?: string | null
}

// isOwnReaction is whether a person's reaction is the signed-in person's.
// Without their id to compare, which is where nobody else can react, every
// person's reaction is theirs.
function isOwnReaction(reaction: AgentMessageReaction, ownUserId?: string): boolean {
  if (reaction.reactorKind !== 'person') return false
  return !ownUserId || !reaction.reactorUserId || reaction.reactorUserId === ownUserId
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
  reactor?: { userId?: string | null; name?: string | null },
): AgentMessageReaction[] {
  // With a person named, only theirs is replaced: in a chat the others'
  // stay. Without, the kind is the reactor, as it is with one person.
  const others = (reactions ?? []).filter(
    (reaction) =>
      reaction.reactorKind !== reactorKind ||
      Boolean(reactor?.userId && reaction.reactorUserId && reaction.reactorUserId !== reactor.userId),
  )
  const next = emoji
    ? [
        ...others,
        reactor?.userId
          ? { emoji, reactorKind, reactedAt, reactorUserId: reactor.userId, reactorName: reactor.name }
          : { emoji, reactorKind, reactedAt },
      ]
    : others
  return next.sort((left, right) =>
    left.reactorKind === right.reactorKind ? 0 : left.reactorKind === 'person' ? -1 : 1,
  )
}

// personReaction is the emoji the person has on a message, or empty. In a
// chat, the person is named by their id.
export function personReaction(reactions: AgentMessageReaction[] | undefined, ownUserId?: string): string {
  return reactions?.find((reaction) => isOwnReaction(reaction, ownUserId))?.emoji ?? ''
}

// ReactionBadges sit on the bubble's lower corner: the person's is a button
// that takes it away, the agent's says who put it there.
export function ReactionBadges({
  reactions,
  onRemove,
  ownUserId,
}: {
  reactions: AgentMessageReaction[] | undefined
  onRemove?: () => void
  ownUserId?: string
}) {
  const { t } = useTranslation()
  if (!reactions || reactions.length === 0) return null
  // Who put an emoji there, in words: by name where the server names the
  // person, which is what tells two people's apart in a chat.
  const reactionWords = (reaction: AgentMessageReaction) =>
    reaction.reactorKind === 'agent'
      ? t('agentDrawer.reactionAgent', { emoji: reaction.emoji })
      : reaction.reactorName
        ? t('chat.reactionBy', { emoji: reaction.emoji, name: reaction.reactorName })
        : t('agentDrawer.reactionPerson', { emoji: reaction.emoji })
  return (
    <span className="agent-reactions">
      {reactions.map((reaction) =>
        isOwnReaction(reaction, ownUserId) && onRemove ? (
          <button
            key={`${reaction.reactorKind}-${reaction.reactorUserId ?? ''}`}
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
            key={`${reaction.reactorKind}-${reaction.reactorUserId ?? ''}`}
            className="agent-reaction"
            title={reactionWords(reaction)}
            aria-label={reactionWords(reaction)}
          >
            {reaction.emoji}
          </span>
        ),
      )}
    </span>
  )
}

// ReactionPicker is the row of quick reactions over a message, opened from
// its React button, and a last button, More, that opens every other emoji:
// a grid by category and a box to type or paste any one. The reaction the
// person already has is marked, shown at the end of the row when it is not
// one of the quick ones, and choosing it again takes it away. A click
// anywhere else closes it; Escape closes More first, then the row.
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
  const moreButton = useRef<HTMLButtonElement>(null)
  const [isMoreOpen, setIsMoreOpen] = useState(false)
  const hasOtherCurrent = current !== '' && !QUICK_REACTIONS.includes(current)
  const choices = hasOtherCurrent ? [...QUICK_REACTIONS, current] : QUICK_REACTIONS
  useEffect(() => {
    const opener = document.activeElement
    if (canMovePillFocus()) picker.current?.querySelector<HTMLButtonElement>('button')?.focus()
    return () => {
      if (opener instanceof HTMLElement && opener.isConnected) opener.focus()
    }
  }, [])
  useLinePillDismissal(
    picker,
    () => {
      if (isMoreOpen) {
        setIsMoreOpen(false)
        moreButton.current?.focus()
        return
      }
      onClose()
    },
    onClose,
  )
  return (
    <div className="agent-reaction-picker-group" ref={picker}>
      <div className="agent-reaction-picker" role="menu" aria-label={t('agentDrawer.react')}>
        {choices.map((emoji) => {
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
        <button
          ref={moreButton}
          type="button"
          role="menuitem"
          className="agent-reaction-choice agent-reaction-more-button"
          title={t('agentDrawer.reactMore')}
          aria-label={t('agentDrawer.reactMore')}
          aria-haspopup="dialog"
          aria-expanded={isMoreOpen}
          onClick={() => setIsMoreOpen((previous) => !previous)}
        >
          <PlusIcon size={16} />
        </button>
      </div>
      {isMoreOpen && <EmojiChooser current={current} onPick={onPick} />}
    </div>
  )
}

// EmojiChooser is every emoji beyond the quick row, under it: a box that
// takes one typed or pasted (the system's emoji picker types into it) and
// the common ones by category. The box says what is wrong with an entry
// once there is one, and does not react until it is right.
function EmojiChooser({ current, onPick }: { current: string; onPick: (emoji: string) => void }) {
  const { t } = useTranslation()
  const [entry, setEntry] = useState('')
  const entryInput = useRef<HTMLInputElement>(null)
  const hintId = useId()
  const problem = reactionEntryProblem(entry)
  const hintKeys: Record<ReactionEntryProblem, Key | null> = {
    empty: null,
    notEmoji: 'agentDrawer.emojiEntryHintNotEmoji',
    tooMany: 'agentDrawer.emojiEntryHintTooMany',
    tooLong: 'agentDrawer.emojiEntryHintTooLong',
  }
  const hintKey = problem ? hintKeys[problem] : null
  const choose = (emoji: string) => onPick(emoji === current ? '' : emoji)
  // A dialog takes focus when it opens, so a keyboard or screen reader user
  // lands in it instead of staying on the button behind it. Escape closes it
  // and the picker gives focus back to that button.
  useEffect(() => {
    entryInput.current?.focus()
  }, [])
  return (
    // A drag across it is not a swipe of the message under it.
    <div
      className="agent-reaction-more"
      role="dialog"
      aria-label={t('agentDrawer.reactMoreTitle')}
      onPointerDown={(event) => event.stopPropagation()}
    >
      <form
        className="agent-reaction-entry"
        onSubmit={(event) => {
          event.preventDefault()
          if (!problem) choose(entry.trim())
        }}
      >
        <input
          ref={entryInput}
          type="text"
          value={entry}
          autoComplete="off"
          placeholder={t('agentDrawer.emojiEntryLabel')}
          aria-label={t('agentDrawer.emojiEntryLabel')}
          aria-invalid={hintKey !== null}
          aria-describedby={hintKey ? hintId : undefined}
          onChange={(event) => setEntry(event.target.value)}
        />
        <button type="submit" disabled={problem !== null}>
          {t('agentDrawer.emojiEntrySubmit')}
        </button>
      </form>
      {hintKey && (
        <p className="agent-reaction-entry-hint" id={hintId} role="status">
          {t(hintKey)}
        </p>
      )}
      <div className="agent-reaction-categories">
        {EMOJI_CATEGORIES.map((category) => (
          <section key={category.id} aria-label={t(category.titleKey)}>
            <h4>{t(category.titleKey)}</h4>
            <div className="agent-reaction-grid">
              {category.emoji.map((emoji) => {
                const isCurrent = emoji === current
                const label = isCurrent
                  ? t('agentDrawer.reactionYours', { emoji })
                  : t('agentDrawer.reactWith', { emoji })
                return (
                  <button
                    key={emoji}
                    type="button"
                    className={['agent-reaction-choice', isCurrent ? 'current' : ''].filter(Boolean).join(' ')}
                    title={label}
                    aria-label={label}
                    aria-pressed={isCurrent}
                    onClick={() => choose(emoji)}
                  >
                    {emoji}
                  </button>
                )
              })}
            </div>
          </section>
        ))}
      </div>
    </div>
  )
}

// canMovePillFocus says whether a pill that opens takes the focus to its first
// button, which a keyboard needs; on a touch screen it would only draw a ring
// around a button nobody chose.
function canMovePillFocus(): boolean {
  return !window.matchMedia?.('(hover: none)').matches
}

// useLinePillDismissal closes a pill over a message: a press outside it, or
// Escape (onEscape, which may close something inside it first). A press on
// the message it belongs to is left to the message, whose tap opens and
// closes the pill on a phone.
export function useLinePillDismissal(
  pill: React.RefObject<HTMLElement | null>,
  onEscape: () => void,
  onClose: () => void,
) {
  useEffect(() => {
    const closeOutside = (event: PointerEvent) => {
      const line = pill.current?.closest('.agent-line')
      if (pill.current && !pill.current.contains(event.target as Node) && !line?.contains(event.target as Node))
        onClose()
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onEscape()
    }
    document.addEventListener('pointerdown', closeOutside)
    document.addEventListener('keydown', closeOnEscape)
    return () => {
      document.removeEventListener('pointerdown', closeOutside)
      document.removeEventListener('keydown', closeOnEscape)
    }
  }, [pill, onEscape, onClose])
}

export interface LineAction {
  actionKey: string
  label: string
  icon: React.ReactNode
  onAct: () => void
}

// LineActionPill is the pill a tap opens over a message that is acted on
// rather than reacted to, drawn as the reactions are: the same row, the same
// place, the same way to close it.
export function LineActionPill({
  label,
  actions,
  onClose,
}: {
  label: string
  actions: LineAction[]
  onClose: () => void
}) {
  const pill = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const opener = document.activeElement
    if (canMovePillFocus()) pill.current?.querySelector<HTMLButtonElement>('button')?.focus()
    return () => {
      if (opener instanceof HTMLElement && opener.isConnected) opener.focus()
    }
  }, [])
  useLinePillDismissal(pill, onClose, onClose)
  return (
    <div className="agent-reaction-picker-group" ref={pill}>
      <div className="agent-reaction-picker" role="menu" aria-label={label}>
        {actions.map((action) => (
          <button
            key={action.actionKey}
            type="button"
            role="menuitem"
            className="agent-reaction-choice"
            title={action.label}
            aria-label={action.label}
            onClick={() => {
              onClose()
              action.onAct()
            }}
          >
            {action.icon}
          </button>
        ))}
      </div>
    </div>
  )
}
