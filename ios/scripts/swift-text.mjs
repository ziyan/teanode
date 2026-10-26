// The text of Swift sources as the iPhone checks read it, without a parser:
// the files, comments and strings blanked out, and balanced brackets
// (check-button-colors.mjs, check-presentations.mjs).

import { readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'

export function swiftFiles(directory) {
  return readdirSync(directory).flatMap((name) => {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) return name === 'build' ? [] : swiftFiles(path)
    return path.endsWith('.swift') ? [path] : []
  })
}

// stringEnd returns the index just past the string literal that starts at
// `start`: plain (`"`), multi-line (`"""`) or raw (`#"`, `##"""`), stepping
// over escapes and `\( ... )` interpolations, which may hold strings of
// their own.
export function stringEnd(text, start) {
  let hashCount = 0
  while (text[start + hashCount] === '#') hashCount++
  const quoteStart = start + hashCount
  const isMultiline = text.startsWith('"""', quoteStart)
  const closing = (isMultiline ? '"""' : '"') + '#'.repeat(hashCount)
  const escape = '\\' + '#'.repeat(hashCount)
  let index = quoteStart + (isMultiline ? 3 : 1)
  while (index < text.length) {
    if (text.startsWith(escape, index)) {
      const after = index + escape.length
      if (text[after] === '(') {
        index = interpolationEnd(text, after)
        continue
      }
      index = after + 1
      continue
    }
    if (text.startsWith(closing, index)) return index + closing.length
    if (!isMultiline && text[index] === '\n') return index
    index++
  }
  return index
}

// interpolationEnd returns the index just past the `)` that closes the
// interpolation opened at `start`.
export function interpolationEnd(text, start) {
  let depth = 0
  let index = start
  while (index < text.length) {
    if (isStringStart(text, index)) {
      index = stringEnd(text, index)
      continue
    }
    if (text[index] === '(') depth++
    else if (text[index] === ')') {
      depth--
      if (depth === 0) return index + 1
    }
    index++
  }
  return index
}

export function isStringStart(text, index) {
  if (text[index] === '"') return true
  if (text[index] !== '#') return false
  let next = index
  while (text[next] === '#') next++
  return text[next] === '"'
}

// codeOnly is the text with every comment and the inside of every string
// replaced by spaces, the same length and on the same lines, so that offsets
// into it are offsets into the file.
export function codeOnly(text) {
  const characters = text.split('')
  const blank = (from, to) => {
    for (let index = from; index < to; index++) if (characters[index] !== '\n') characters[index] = ' '
  }
  let index = 0
  while (index < text.length) {
    if (text.startsWith('//', index)) {
      const lineEnd = text.indexOf('\n', index)
      const end = lineEnd === -1 ? text.length : lineEnd
      blank(index, end)
      index = end
      continue
    }
    if (text.startsWith('/*', index)) {
      // Swift's block comments nest.
      let depth = 0
      let end = index
      while (end < text.length) {
        if (text.startsWith('/*', end)) {
          depth++
          end += 2
        } else if (text.startsWith('*/', end)) {
          depth--
          end += 2
          if (depth === 0) break
        } else end++
      }
      blank(index, end)
      index = end
      continue
    }
    if (isStringStart(text, index)) {
      const end = stringEnd(text, index)
      // The quotes stay, so `Button("x"` still reads as a call with an argument.
      blank(index + 1, end - 1)
      index = end
      continue
    }
    index++
  }
  return characters.join('')
}

// skipBalanced returns the index just past the bracket that closes the one
// at `start`, in text that has no comments or strings left in it.
export function skipBalanced(code, start) {
  const opening = code[start]
  const closing = { '(': ')', '{': '}', '[': ']' }[opening]
  let depth = 0
  for (let index = start; index < code.length; index++) {
    if (code[index] === opening) depth++
    else if (code[index] === closing) {
      depth--
      if (depth === 0) return index + 1
    }
  }
  return code.length
}
