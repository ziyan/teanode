// The emoji the reaction picker offers beyond its quick row, and the check
// on what a person types or pastes into its box. The server takes one or
// two emoji (internal/models/reaction.go); this refuses the rest before the
// round trip, with a hint, rather than after it, with a failure.

import type { Key } from '../i18n/i18n'

export interface EmojiCategory {
  id: string
  // The catalogue key of the heading.
  titleKey: Key
  emoji: string[]
}

// The most emoji one reaction holds, and the most bytes the server stores.
export const REACTION_EMOJI_MAXIMUM_COUNT = 2
export const REACTION_EMOJI_MAXIMUM_BYTES = 32

// emojiOf is the emoji written together in rows, which keeps the lists
// below readable, as the list they are.
function emojiOf(...rows: string[]): string[] {
  return graphemesOf(rows.join(''))
}

export const EMOJI_CATEGORIES: EmojiCategory[] = [
  {
    id: 'smileys',
    titleKey: 'agentDrawer.emojiSmileys',
    emoji: emojiOf(
      '😀😃😄😁😆😅😂🤣😊😇🙂😉',
      '😍🥰😘😋😎🤩🥳😏😢😭😮😱',
      '😴🤔🙄😬😡🤯🥺🤗👍👎👏🙌',
      '🙏🤝💪👋🤞✌️👌👀',
    ),
  },
  {
    id: 'animals',
    titleKey: 'agentDrawer.emojiAnimals',
    emoji: emojiOf('🐶🐱🐭🐹🐰🦊🐻🐼🐨🐯🦁🐮', '🐷🐸🐵🐔🐧🐦🦆🦉🦄🐝🦋🐢', '🐍🐙🐬🐳🦈🐘🦒🌵🌲🌸🌻🍀'),
  },
  {
    id: 'food',
    titleKey: 'agentDrawer.emojiFood',
    emoji: emojiOf('🍎🍐🍊🍋🍌🍉🍇🍓🍒🍑🍍🥑', '🍅🥕🌽🍞🧀🍳🥓🍔🍟🍕🌭🌮', '🍣🍜🍝🍩🍪🎂🍰🍫🍿☕🍺🍷'),
  },
  {
    id: 'activities',
    titleKey: 'agentDrawer.emojiActivities',
    emoji: emojiOf('⚽🏀🏈⚾🎾🏐🏉🎱🏓🏸🥊⛳', '🎯🎮🎲🎨🎬🎤🎧🎸🎹🥁🏆🥇', '🥈🥉🏅🎉🎊🎈🏄🚴🏊🧘'),
  },
  {
    id: 'travel',
    titleKey: 'agentDrawer.emojiTravel',
    emoji: emojiOf('🚗🚕🚌🚑🚒🚓🚚🚜🚲✈️🚀🚁', '⛵🚢🚂🚄🏠🏢🏥🏫🏰🗼🗽⛰️', '🌋🏖️🏝️🌅🌃🌉🌍🗺️⛺🎡🎢'),
  },
  {
    id: 'objects',
    titleKey: 'agentDrawer.emojiObjects',
    emoji: emojiOf('⌚📱💻⌨️🖥️📷🎥📺📻⏰💡🔋', '🔌💰💳💎🔧🔨⚙️🔒🔑📚📖✏️', '📝📎📌📅📧📦🎁🔔'),
  },
  {
    id: 'symbols',
    titleKey: 'agentDrawer.emojiSymbols',
    emoji: emojiOf('❤️🧡💛💚💙💜🖤🤍💔💯✅❌', '⭕❗❓⚠️✨⭐🌟🔥💥💫💤💢', '➕➖➡️⬅️⬆️⬇️🔁🆗🆕🔴🟢🔵'),
  },
]

interface GraphemeSegmenter {
  segment(text: string): Iterable<{ segment: string }>
}

// graphemesOf is the text split into what a reader sees as one character
// each. Intl.Segmenter keeps a flag, a family or a skin tone whole; where a
// browser lacks it the code points stand in, which refuses a joined
// sequence rather than letting a wrong one through.
function graphemesOf(text: string): string[] {
  const Segmenter = (Intl as unknown as { Segmenter?: new (locale?: string, options?: object) => GraphemeSegmenter })
    .Segmenter
  if (!Segmenter) return Array.from(text)
  return Array.from(new Segmenter(undefined, { granularity: 'grapheme' }).segment(text), (part) => part.segment)
}

// One emoji: a flag, a keycap, or pictographs drawn in color (by default,
// or by the selector that asks for it) joined by zero width joiners, each
// with an optional skin tone, and the tags that name a regional flag. A
// regional indicator alone is a letter in a box, not an emoji. A skin tone
// may follow a text-presentation base directly (a victory hand written
// without the selector), as the server's validator and the iPhone app allow.
const PICTOGRAPH =
  '(?:(?!\\p{Regional_Indicator})\\p{Emoji_Presentation}|\\p{Extended_Pictographic}\\uFE0F|\\p{Extended_Pictographic}(?=\\p{Emoji_Modifier}))\\p{Emoji_Modifier}?'
const ONE_EMOJI = new RegExp(
  `^(?:\\p{Regional_Indicator}{2}|[0-9#*]\\uFE0F?\\u20E3|${PICTOGRAPH}(?:\\u200D${PICTOGRAPH})*[\\u{E0020}-\\u{E007F}]*)$`,
  'u',
)

export type ReactionEntryProblem = 'empty' | 'notEmoji' | 'tooMany' | 'tooLong'

// reactionEntryProblem says why typed or pasted text cannot be a reaction,
// or null when it can: one or two emoji and nothing else.
export function reactionEntryProblem(entry: string): ReactionEntryProblem | null {
  const text = entry.trim()
  if (text === '') return 'empty'
  if (new TextEncoder().encode(text).length > REACTION_EMOJI_MAXIMUM_BYTES) return 'tooLong'
  const parts = graphemesOf(text)
  if (!parts.every((part) => ONE_EMOJI.test(part))) return 'notEmoji'
  if (parts.length > REACTION_EMOJI_MAXIMUM_COUNT) return 'tooMany'
  return null
}
