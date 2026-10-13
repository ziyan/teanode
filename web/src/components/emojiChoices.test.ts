import { expect, it } from 'vitest'

import { EMOJI_CATEGORIES, reactionEntryProblem } from './emojiChoices'
import { QUICK_REACTIONS } from './agentReactions'

// Every emoji the picker offers is one the server takes, and none is
// offered twice: a duplicate would be two buttons with one name.
it('offers only single emoji, each once', () => {
  const everyEmoji = [...QUICK_REACTIONS, ...EMOJI_CATEGORIES.flatMap((category) => category.emoji)]
  for (const emoji of everyEmoji) expect(reactionEntryProblem(emoji), emoji).toBeNull()
  const inCategories = EMOJI_CATEGORIES.flatMap((category) => category.emoji)
  expect(new Set(inCategories).size).toBe(inCategories.length)
  expect(EMOJI_CATEGORIES.map((category) => category.id)).toEqual([
    'smileys',
    'animals',
    'food',
    'activities',
    'travel',
    'objects',
    'symbols',
  ])
  for (const category of EMOJI_CATEGORIES) expect(category.emoji.length).toBeGreaterThanOrEqual(30)
})

// One or two emoji pass, whatever they are made of: a flag, a skin tone, a
// family joined by zero width joiners, a keycap.
it('accepts one or two emoji of any make', () => {
  for (const entry of [
    '👍',
    ' 👍 ',
    '👍🏽',
    '🇯🇵',
    '👨‍👩‍👧‍👦',
    '1️⃣',
    '❤️‍🔥',
    '🏳️‍🌈',
    '🔥🎉',
    '\u270C\u{1F3FD}',
    '\u261D\u{1F3FD}',
    '\u{1F575}\u{1F3FD}',
    '\u{1F3CB}\u{1F3FD}',
    '\u270D\u{1F3FD}',
    '\u{1F590}\u{1F3FD}',
    '\u26F9\u{1F3FD}',
  ]) {
    expect(reactionEntryProblem(entry), entry).toBeNull()
  }
})

// Anything else is told apart by why: nothing, words, too many, too long.
it('says why an entry cannot be a reaction', () => {
  expect(reactionEntryProblem('')).toBe('empty')
  expect(reactionEntryProblem('   ')).toBe('empty')
  expect(reactionEntryProblem('a')).toBe('notEmoji')
  expect(reactionEntryProblem('👍a')).toBe('notEmoji')
  expect(reactionEntryProblem('7')).toBe('notEmoji')
  expect(reactionEntryProblem('🇯')).toBe('notEmoji')
  expect(reactionEntryProblem('👍🔥🎉')).toBe('tooMany')
  expect(reactionEntryProblem('👨‍👩‍👧‍👦👨‍👩‍👧‍👦')).toBe('tooLong')
})
