// The password generator. Every character comes from crypto.getRandomValues,
// and an index into an alphabet is drawn by rejection sampling so no
// character is likelier than another (a plain remainder favors the first
// ones whenever the alphabet does not divide 2^32).
//
// No passphrase-of-words option: the repository has no word list, and this
// feature does not add one.

import { PasswordsCryptoError } from './crypto'

export interface PasswordOptions {
  length: number
  shouldIncludeLetters: boolean
  shouldIncludeDigits: boolean
  shouldIncludeSymbols: boolean
  // shouldAvoidLookAlikes leaves out the characters that read as one another
  // in many typefaces: I l 1 | O 0 o.
  shouldAvoidLookAlikes: boolean
}

export const DEFAULT_PASSWORD_OPTIONS: PasswordOptions = {
  length: 20,
  shouldIncludeLetters: true,
  shouldIncludeDigits: true,
  shouldIncludeSymbols: true,
  shouldAvoidLookAlikes: false,
}

export const MAXIMUM_PASSWORD_LENGTH = 128

const LOWERCASE_LETTERS = 'abcdefghijklmnopqrstuvwxyz'
const UPPERCASE_LETTERS = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ'
const DIGITS = '0123456789'
const SYMBOLS = '!@#$%^&*()-_=+[]{}.,:;?'
const LOOK_ALIKES = 'Il1|O0o'

// RandomFiller fills an array with random 32 bit numbers. Tests pass their
// own to count draws; the default is the browser's.
export type RandomFiller = (values: Uint32Array<ArrayBuffer>) => void

function fillWithBrowserRandomness(values: Uint32Array<ArrayBuffer>): void {
  crypto.getRandomValues(values)
}

// randomIndex returns a number from 0 up to but not including limit, every
// one equally likely.
export function randomIndex(limit: number, fillRandom: RandomFiller = fillWithBrowserRandomness): number {
  if (!Number.isInteger(limit) || limit < 1 || limit > 0x100000000)
    throw new RangeError('The limit must be a whole number from 1 to 2^32')
  // Numbers at or above the largest multiple of limit would make the low
  // indexes likelier, so they are drawn again.
  const largestMultiple = Math.floor(0x100000000 / limit) * limit
  const draw = new Uint32Array(1)
  for (;;) {
    fillRandom(draw)
    if (draw[0] < largestMultiple) return draw[0] % limit
  }
}

function withoutLookAlikes(alphabet: string, shouldAvoidLookAlikes: boolean): string {
  if (!shouldAvoidLookAlikes) return alphabet
  return [...alphabet].filter((character) => !LOOK_ALIKES.includes(character)).join('')
}

// generatePassword returns a password of options.length characters. Each kind
// asked for appears at least once; the order is then shuffled.
export function generatePassword(
  options: PasswordOptions = DEFAULT_PASSWORD_OPTIONS,
  fillRandom: RandomFiller = fillWithBrowserRandomness,
): string {
  const alphabets: string[] = []
  if (options.shouldIncludeLetters) {
    alphabets.push(withoutLookAlikes(LOWERCASE_LETTERS, options.shouldAvoidLookAlikes))
    alphabets.push(withoutLookAlikes(UPPERCASE_LETTERS, options.shouldAvoidLookAlikes))
  }
  if (options.shouldIncludeDigits) alphabets.push(withoutLookAlikes(DIGITS, options.shouldAvoidLookAlikes))
  if (options.shouldIncludeSymbols) alphabets.push(withoutLookAlikes(SYMBOLS, options.shouldAvoidLookAlikes))
  if (alphabets.length === 0) throw new PasswordsCryptoError('Choose at least one kind of character')
  if (
    !Number.isInteger(options.length) ||
    options.length < alphabets.length ||
    options.length > MAXIMUM_PASSWORD_LENGTH
  ) {
    throw new RangeError(`The length must be a whole number from ${alphabets.length} to ${MAXIMUM_PASSWORD_LENGTH}`)
  }

  const everyCharacter = [...alphabets.join('')]
  const characters: string[] = alphabets.map((alphabet) => alphabet[randomIndex(alphabet.length, fillRandom)])
  while (characters.length < options.length) {
    characters.push(everyCharacter[randomIndex(everyCharacter.length, fillRandom)])
  }

  // Fisher-Yates, so the characters guaranteed above do not sit at the front.
  for (let index = characters.length - 1; index > 0; index--) {
    const otherIndex = randomIndex(index + 1, fillRandom)
    const held = characters[index]
    characters[index] = characters[otherIndex]
    characters[otherIndex] = held
  }
  return characters.join('')
}
