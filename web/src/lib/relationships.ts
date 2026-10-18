import type { Key, Values } from '../i18n/i18n'

// What somebody is to somebody else, in the reader's language. One place, so
// that the circles page and the notes in the conversation word a relationship
// the same way. The kinds, their opposites and which of them have gendered
// words are the server's (internal/models/relationship.go); the words are not
// translated word for word but chosen, which is why they are a table here.

export type TermGender = 'masculine' | 'feminine' | 'neutral'
export type Pronoun = 'he' | 'she' | 'they' | ''

export const PRONOUNS: Exclude<Pronoun, ''>[] = ['he', 'she', 'they']

export const PRONOUN_LABELS: Record<Exclude<Pronoun, ''>, Key> = {
  he: 'profile.pronounHe',
  she: 'profile.pronounShe',
  they: 'profile.pronounThey',
}

type KindWords = { neutral: Key; masculine?: Key; feminine?: Key }

const KIND_WORDS: Record<string, KindWords> = {
  spouse: {
    neutral: 'relationship.spouse.neutral',
    masculine: 'relationship.spouse.masculine',
    feminine: 'relationship.spouse.feminine',
  },
  partner: {
    neutral: 'relationship.partner.neutral',
    masculine: 'relationship.partner.masculine',
    feminine: 'relationship.partner.feminine',
  },
  parent: {
    neutral: 'relationship.parent.neutral',
    masculine: 'relationship.parent.masculine',
    feminine: 'relationship.parent.feminine',
  },
  child: {
    neutral: 'relationship.child.neutral',
    masculine: 'relationship.child.masculine',
    feminine: 'relationship.child.feminine',
  },
  sibling: {
    neutral: 'relationship.sibling.neutral',
    masculine: 'relationship.sibling.masculine',
    feminine: 'relationship.sibling.feminine',
  },
  grandparent: {
    neutral: 'relationship.grandparent.neutral',
    masculine: 'relationship.grandparent.masculine',
    feminine: 'relationship.grandparent.feminine',
  },
  grandchild: {
    neutral: 'relationship.grandchild.neutral',
    masculine: 'relationship.grandchild.masculine',
    feminine: 'relationship.grandchild.feminine',
  },
  aunt_or_uncle: {
    neutral: 'relationship.auntOrUncle.neutral',
    masculine: 'relationship.auntOrUncle.masculine',
    feminine: 'relationship.auntOrUncle.feminine',
  },
  niece_or_nephew: {
    neutral: 'relationship.nieceOrNephew.neutral',
    masculine: 'relationship.nieceOrNephew.masculine',
    feminine: 'relationship.nieceOrNephew.feminine',
  },
  cousin: { neutral: 'relationship.cousin.neutral' },
  parent_in_law: {
    neutral: 'relationship.parentInLaw.neutral',
    masculine: 'relationship.parentInLaw.masculine',
    feminine: 'relationship.parentInLaw.feminine',
  },
  child_in_law: {
    neutral: 'relationship.childInLaw.neutral',
    masculine: 'relationship.childInLaw.masculine',
    feminine: 'relationship.childInLaw.feminine',
  },
  sibling_in_law: {
    neutral: 'relationship.siblingInLaw.neutral',
    masculine: 'relationship.siblingInLaw.masculine',
    feminine: 'relationship.siblingInLaw.feminine',
  },
  step_parent: {
    neutral: 'relationship.stepParent.neutral',
    masculine: 'relationship.stepParent.masculine',
    feminine: 'relationship.stepParent.feminine',
  },
  step_child: {
    neutral: 'relationship.stepChild.neutral',
    masculine: 'relationship.stepChild.masculine',
    feminine: 'relationship.stepChild.feminine',
  },
  friend: { neutral: 'relationship.friend.neutral' },
  colleague: { neutral: 'relationship.colleague.neutral' },
}

// What the other person is, when this one is the given kind: the father's
// opposite is the child, a cousin's is a cousin.
const OPPOSITE_KINDS: Record<string, string> = {
  spouse: 'spouse',
  partner: 'partner',
  parent: 'child',
  child: 'parent',
  sibling: 'sibling',
  grandparent: 'grandchild',
  grandchild: 'grandparent',
  aunt_or_uncle: 'niece_or_nephew',
  niece_or_nephew: 'aunt_or_uncle',
  cousin: 'cousin',
  parent_in_law: 'child_in_law',
  child_in_law: 'parent_in_law',
  sibling_in_law: 'sibling_in_law',
  step_parent: 'step_child',
  step_child: 'step_parent',
  friend: 'friend',
  colleague: 'colleague',
}

export function oppositeKind(relationshipKind: string): string {
  return OPPOSITE_KINDS[relationshipKind] ?? relationshipKind
}

// Kinds with one word whatever the gender: asking how a person is described
// would change nothing about how they appear.
export function isGenderedKind(relationshipKind: string): boolean {
  return KIND_WORDS[relationshipKind]?.masculine !== undefined
}

// The gender of the word for somebody described by this pronoun. Without one
// the word is neutral (spouse, child): a gender is never guessed.
export function termGenderOfPronoun(pronoun: string): TermGender {
  if (pronoun === 'he') return 'masculine'
  if (pronoun === 'she') return 'feminine'
  return 'neutral'
}

// The group a kind is offered under: family first, then friends and
// colleagues, the same three words a circle is named by.
export function relationshipGroup(relationshipKind: string): 'family' | 'friends' | 'colleagues' {
  if (relationshipKind === 'friend') return 'friends'
  if (relationshipKind === 'colleague') return 'colleagues'
  return 'family'
}

// relationshipWord is the word, lower case, for use in a sentence ("Sam is
// your father"). The English the server sent stands in for a kind this
// dashboard has no words for, so a kind added on the server still reads.
export function relationshipWord(
  t: (key: Key, values?: Values) => string,
  relationshipKind: string,
  termGender: string,
  fallbackWord = '',
): string {
  const words = KIND_WORDS[relationshipKind]
  if (!words) return fallbackWord
  const key =
    (termGender === 'masculine' ? words.masculine : termGender === 'feminine' ? words.feminine : undefined) ??
    words.neutral
  return t(key)
}

// relationshipLabel is the same word as a label ("Father"): Chinese and
// Japanese have no capitals, so it changes nothing there.
export function relationshipLabel(
  t: (key: Key, values?: Values) => string,
  relationshipKind: string,
  termGender: string,
  fallbackWord = '',
): string {
  const word = relationshipWord(t, relationshipKind, termGender, fallbackWord)
  return word.charAt(0).toLocaleUpperCase() + word.slice(1)
}
