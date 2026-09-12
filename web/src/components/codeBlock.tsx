import { ReactNode, useState } from 'react'

import { CheckIcon, CopyIcon } from './icons'
import { useTranslation } from '../i18n/i18n'

// A block of code as the agent's answers and its tools carry it: coloured
// when it is JSON, with a copy mark in the corner. No library: what is
// coloured is a handful of token kinds, and everything stays a text node.

// pretty is a tool's text as it reads best: JSON laid out, the wrapper
// that marks a result as data left off, anything else as it came.
export function pretty(text: string | undefined): string {
  if (!text) return ''
  let inner = text.trim()
  const wrapped = inner.match(/^<untrusted-data>\n?([\s\S]*?)\n?<\/untrusted-data>$/)
  if (wrapped) inner = wrapped[1].trim()
  try {
    return JSON.stringify(JSON.parse(inner), null, 2)
  } catch {
    return inner
  }
}

const TOKEN = /("(?:\\.|[^"\\])*")(\s*:)?|\b(?:true|false|null)\b|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/g

// highlight colours JSON the way an editor does: keys, strings, numbers
// and the three words, each a span; text that is not JSON is left as is.
function highlight(text: string): ReactNode[] {
  const parts: ReactNode[] = []
  let last = 0
  let index = 0
  for (const match of text.matchAll(TOKEN)) {
    const at = match.index ?? 0
    if (at > last) parts.push(text.slice(last, at))
    const [whole, quoted, colon] = match
    if (quoted !== undefined) {
      parts.push(
        <span key={index++} className={colon ? 'json-key' : 'json-string'}>
          {quoted}
        </span>,
      )
      if (colon) parts.push(colon)
    } else if (/^(true|false|null)$/.test(whole)) {
      parts.push(
        <span key={index++} className="json-literal">
          {whole}
        </span>,
      )
    } else {
      parts.push(
        <span key={index++} className="json-number">
          {whole}
        </span>,
      )
    }
    last = at + whole.length
  }
  if (last < text.length) parts.push(text.slice(last))
  return parts
}

function looksLikeJSON(text: string): boolean {
  const trimmed = text.trim()
  return (trimmed.startsWith('{') && trimmed.endsWith('}')) || (trimmed.startsWith('[') && trimmed.endsWith(']'))
}

// CodeBlock is one block, with its language named when the writer named
// it, and a copy mark that shows under the pointer.
export function CodeBlock({ text, language, tidy }: { text: string; language?: string; tidy?: boolean }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const shown = tidy ? pretty(text) : text
  const json = language === 'json' || (!language && looksLikeJSON(shown))
  return (
    <div className="agent-code">
      {language ? <span className="agent-code-language muted">{language}</span> : null}
      <button
        type="button"
        className="icon-button agent-code-copy"
        aria-label={copied ? t('common.copied') : t('common.copy')}
        title={copied ? t('common.copied') : t('common.copy')}
        onClick={() => {
          if (!navigator.clipboard) return
          void navigator.clipboard.writeText(shown).then(() => {
            setCopied(true)
            setTimeout(() => setCopied(false), 1500)
          })
        }}
      >
        {copied ? <CheckIcon size={13} /> : <CopyIcon size={13} />}
      </button>
      <pre>{json ? highlight(shown) : shown}</pre>
    </div>
  )
}
