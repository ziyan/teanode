// Syntax highlighting for the HTML behind a rendered message.
//
// Written here rather than pulled in. A highlighter is a tokeniser and a
// stylesheet; the libraries that do this are hundreds of kilobytes because
// they do it for ninety languages, and this needs one. It also has to run
// under a content security policy of script-src 'self', so a CDN is not an
// option even if it were a good idea.
//
// Not a parser. It never builds a tree, never validates and never rewrites —
// it walks the string once and labels runs of it, which is all colouring
// needs. Anything it cannot classify comes out as plain text, so malformed
// markup degrades to unhighlighted markup rather than to a mess. That matters:
// this is mail from strangers, and a good deal of it is malformed.
//
// The output is text in spans. Nothing here is ever set as HTML.

type Token = { text: string; kind?: Kind }
type Kind = 'comment' | 'doctype' | 'tag' | 'name' | 'attribute' | 'value' | 'entity'

export function HighlightedHtml({ source }: { source: string }) {
  return (
    <pre className="message-text code">
      {tokenize(source).map((token, index) =>
        token.kind ? (
          <span key={index} className={`tok-${token.kind}`}>
            {token.text}
          </span>
        ) : (
          // A fragment rather than a bare string, so React has a key for it.
          <span key={index}>{token.text}</span>
        ),
      )}
    </pre>
  )
}

// The scanner. Position-based rather than regular expressions over the whole
// document: an attribute value can contain a > and a comment can contain
// anything at all, and a pattern that ignores that colours half a message as
// one tag.
function tokenize(source: string): Token[] {
  const tokens: Token[] = []
  let index = 0
  let text = ''

  const flushText = () => {
    if (text) {
      tokens.push({ text })
      text = ''
    }
  }

  while (index < source.length) {
    const next = source.indexOf('<', index)
    if (next < 0) {
      text += source.slice(index)
      break
    }

    text += source.slice(index, next)

    if (source.startsWith('<!--', next)) {
      const end = source.indexOf('-->', next + 4)
      const stop = end < 0 ? source.length : end + 3
      flushText()
      tokens.push({ text: source.slice(next, stop), kind: 'comment' })
      index = stop
      continue
    }

    if (source.startsWith('<!', next)) {
      const end = source.indexOf('>', next)
      const stop = end < 0 ? source.length : end + 1
      flushText()
      tokens.push({ text: source.slice(next, stop), kind: 'doctype' })
      index = stop
      continue
    }

    const tag = readTag(source, next)
    if (!tag) {
      // A bare < in text, which mail is full of. Not a tag, so not coloured.
      text += '<'
      index = next + 1
      continue
    }

    flushText()
    tokens.push(...tag.tokens)
    index = tag.end
  }

  flushText()
  return tokens
}

// readTag reads one element, from its < to its >, and returns the tokens it
// is made of. Returns undefined when what follows the < is not a tag name, so
// the caller can treat the character as text.
function readTag(source: string, start: number): { tokens: Token[]; end: number } | undefined {
  let index = start + 1
  let closing = ''
  if (source[index] === '/') {
    closing = '/'
    index += 1
  }

  const nameStart = index
  while (index < source.length && /[A-Za-z0-9:_.-]/.test(source[index])) {
    index += 1
  }
  if (index === nameStart) {
    return undefined
  }

  const tokens: Token[] = [
    { text: `<${closing}`, kind: 'tag' },
    { text: source.slice(nameStart, index), kind: 'name' },
  ]

  // Attributes, until the tag closes or the document runs out.
  while (index < source.length) {
    const whitespaceStart = index
    while (index < source.length && /\s/.test(source[index])) {
      index += 1
    }
    if (index > whitespaceStart) {
      tokens.push({ text: source.slice(whitespaceStart, index) })
    }

    if (index >= source.length) {
      break
    }

    if (source[index] === '>' || source.startsWith('/>', index)) {
      const close = source[index] === '>' ? '>' : '/>'
      tokens.push({ text: close, kind: 'tag' })
      return { tokens, end: index + close.length }
    }

    const attributeStart = index
    while (index < source.length && !/[\s=>/]/.test(source[index])) {
      index += 1
    }
    if (index === attributeStart) {
      // Something that is neither whitespace, an attribute nor a close: a
      // stray character. Take it as text and keep going rather than looping.
      tokens.push({ text: source[index] })
      index += 1
      continue
    }
    tokens.push({ text: source.slice(attributeStart, index), kind: 'attribute' })

    if (source[index] !== '=') {
      continue
    }
    tokens.push({ text: '=', kind: 'tag' })
    index += 1

    const quote = source[index]
    if (quote === '"' || quote === "'") {
      const end = source.indexOf(quote, index + 1)
      const stop = end < 0 ? source.length : end + 1
      tokens.push({ text: source.slice(index, stop), kind: 'value' })
      index = stop
      continue
    }

    // An unquoted value, which is legal and common in mail written by hand.
    const valueStart = index
    while (index < source.length && !/[\s>]/.test(source[index])) {
      index += 1
    }
    if (index > valueStart) {
      tokens.push({ text: source.slice(valueStart, index), kind: 'value' })
    }
  }

  // Ran off the end inside a tag. Everything read so far still colours.
  return { tokens, end: index }
}
