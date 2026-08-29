import { useEffect, useRef, useState } from 'react'

import { useTranslation } from '../i18n/i18n'
import { EraserIcon, LinkIcon, ListIcon, NumberedListIcon, QuoteIcon } from './icons'
import { MediaButton, imageTag } from './media'

// A rich text editor for writing a message: a contentEditable element with
// a toolbar over it. No library, for the same reason nothing else here has
// one — and because what a message needs is bold, italic, a list and a link,
// which is what the browser's own editing commands do.
//
// execCommand is deprecated on paper and implemented by every browser in
// practice. Should one ever remove it, the toolbar stops working and typing
// does not; a message can still be written and sent.

type Command = { command: string; argument?: string; label: React.ReactNode; title: string; className?: string }

export function RichTextEditor({
  value,
  onChange,
  placeholder,
  domainId,
}: {
  value: string
  onChange: (html: string) => void
  placeholder?: string
  // The domain a picture uploaded here belongs to. Without one the button is
  // not shown: a picture has to belong to a domain, because it is served from
  // that domain's own name and may only appear in its templates.
  domainId?: string
}) {
  const { t } = useTranslation()
  const editor = useRef<HTMLDivElement>(null)
  // What the editor last told the page, so a value coming back down is not
  // written into the element under the cursor. Setting innerHTML moves the
  // caret to the start, which mid-word is unusable.
  const emitted = useRef<string>('')
  const [linking, setLinking] = useState<string | null>(null)
  const savedRange = useRef<Range | null>(null)

  useEffect(() => {
    const element = editor.current
    if (element && value !== emitted.current) {
      element.innerHTML = value
      emitted.current = value
    }
  }, [value])

  useEffect(() => {
    // Paragraphs rather than divs, which is what mail clients and the text
    // alternative both expect.
    document.execCommand('defaultParagraphSeparator', false, 'p')
  }, [])

  function emit() {
    const html = editor.current?.innerHTML ?? ''
    emitted.current = html
    onChange(html)
  }

  function run(command: string, argument?: string) {
    editor.current?.focus()
    document.execCommand(command, false, argument)
    emit()
  }

  // The link box replaces window.prompt, which cannot be styled and which
  // some browsers suppress. The selection is saved first: clicking into the
  // box takes the selection away from the editor.
  function beginLink() {
    const selection = window.getSelection()
    savedRange.current = selection && selection.rangeCount > 0 ? selection.getRangeAt(0).cloneRange() : null
    setLinking('')
  }

  function finishLink() {
    const url = safeLinkTarget(linking ?? '')
    setLinking(null)
    const element = editor.current
    if (!element || !url) {
      return
    }
    element.focus()
    const selection = window.getSelection()
    if (selection && savedRange.current) {
      selection.removeAllRanges()
      selection.addRange(savedRange.current)
    }
    // A selection that is only a caret gets the address itself as its text,
    // which is what people expect a link to say when they typed nothing.
    if (selection && selection.isCollapsed) {
      document.execCommand('insertHTML', false, `<a href="${escapeAttribute(url)}">${escapeText(url)}</a>`)
    } else {
      document.execCommand('createLink', false, url)
    }
    emit()
  }

  const commands: Command[] = [
    { command: 'bold', label: 'B', title: t('richText.bold'), className: 'richtext-bold' },
    { command: 'italic', label: 'I', title: t('richText.italic'), className: 'richtext-italic' },
    { command: 'underline', label: 'U', title: t('richText.underline'), className: 'richtext-underline' },
    { command: 'formatBlock', argument: 'h2', label: 'H', title: t('richText.heading') },
    { command: 'formatBlock', argument: 'p', label: '¶', title: t('richText.paragraph') },
    { command: 'insertUnorderedList', label: <ListIcon size={16} />, title: t('richText.bullets') },
    { command: 'insertOrderedList', label: <NumberedListIcon size={16} />, title: t('richText.numbers') },
    { command: 'formatBlock', argument: 'blockquote', label: <QuoteIcon size={16} />, title: t('richText.quote') },
  ]

  return (
    <div className="richtext">
      <div className="richtext-toolbar" role="toolbar" aria-label={t('richText.toolbar')}>
        {commands.map((entry) => (
          <button
            key={entry.command + (entry.argument ?? '')}
            type="button"
            className={entry.className}
            title={entry.title}
            aria-label={entry.title}
            // mousedown rather than click, and prevented, so the editor
            // keeps its selection: a click would move focus to the button
            // and the command would apply to nothing.
            onMouseDown={(event) => {
              event.preventDefault()
              run(entry.command, entry.argument)
            }}
          >
            {entry.label}
          </button>
        ))}
        <button
          type="button"
          title={t('richText.link')}
          aria-label={t('richText.link')}
          onMouseDown={(event) => {
            event.preventDefault()
            beginLink()
          }}
        >
          <LinkIcon size={16} />
        </button>
        <MediaButton
          domainId={domainId}
          onMouseDown={() => {
            const selection = window.getSelection()
            savedRange.current = selection && selection.rangeCount > 0 ? selection.getRangeAt(0).cloneRange() : null
          }}
          onUploaded={(media) => {
            const element = editor.current
            if (!element) {
              return
            }
            element.focus()
            const selection = window.getSelection()
            if (selection && savedRange.current) {
              selection.removeAllRanges()
              selection.addRange(savedRange.current)
            }
            document.execCommand('insertHTML', false, imageTag(media))
            emit()
          }}
        />
        <button
          type="button"
          title={t('richText.clear')}
          aria-label={t('richText.clear')}
          onMouseDown={(event) => {
            event.preventDefault()
            run('removeFormat')
            run('unlink')
          }}
        >
          <EraserIcon size={16} />
        </button>
        {linking !== null && (
          <form
            className="richtext-link"
            onSubmit={(event) => {
              event.preventDefault()
              finishLink()
            }}
          >
            <input
              autoFocus
              value={linking}
              placeholder="https://"
              onChange={(event) => setLinking(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Escape') {
                  setLinking(null)
                }
              }}
            />
            <button type="submit">{t('richText.insertLink')}</button>
          </form>
        )}
      </div>
      <div
        ref={editor}
        className="richtext-editor"
        contentEditable
        suppressContentEditableWarning
        data-placeholder={placeholder}
        onInput={emit}
        onBlur={emit}
      />
    </div>
  )
}

// safeLinkTarget is the address to link to, or the empty string for one that
// must not be linked to at all.
//
// A link in a message is a link somebody clicks, and the schemes that carry
// code are not addresses: "javascript:" runs it, and "data:" can be a whole
// document. Escaping the value stops it breaking out of the attribute, which
// is a different problem and was already handled; nothing stopped the address
// itself from being an instruction.
//
// Nothing here is load-bearing against a determined operator — they can write
// the HTML by hand, and the preview refuses to run it either way — but a
// dashboard should not be the thing that puts it there, and a message that
// leaves with such a link is one every receiver strips and some judge the
// sender for.
//
// An address with no scheme is the ordinary case: somebody types
// "example.com" and means a web page, so it gets https. A relative link is
// meaningless in mail, which is why it is not an exception.
export function safeLinkTarget(value: string): string {
  const url = value.trim()
  if (!url) {
    return ''
  }

  // An address that begins with two slashes takes the scheme of the page it
  // is on, which in a message is nothing. It means a web address.
  if (url.startsWith('//')) {
    return `https:${url}`
  }

  // A scheme is letters, digits, "+" or "-" before the first colon. A dot
  // rules it out: "example.com:8080/path" is a host and a port, and every
  // scheme worth allowing or refusing has no dot in it. Reading it as a
  // scheme refused the link instead of making one, which is how this was
  // found.
  const scheme = /^([A-Za-z][A-Za-z0-9+-]*):/.exec(url)
  if (!scheme) {
    return `https://${url}`
  }
  switch (scheme[1].toLowerCase()) {
    case 'http':
    case 'https':
    case 'mailto':
    case 'tel':
      return url
    default:
      return ''
  }
}

function escapeText(value: string): string {
  return value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

function escapeAttribute(value: string): string {
  return escapeText(value).replace(/"/g, '&quot;')
}

// htmlToText derives the plain text alternative of a message written in the
// editor. A recipient whose client shows only text, and every spam filter,
// reads this rather than the HTML; a message with no text part at all is a
// message some filters score for it.
//
// Block elements become line breaks, lists become lines with a marker, and a
// link whose text is not its address gets the address after it in brackets,
// so nothing that was in the message is lost.
export function htmlToText(html: string): string {
  const parsed = new DOMParser().parseFromString(html, 'text/html')
  const pieces: string[] = []

  function walk(node: Node, listMarker: () => string) {
    if (node.nodeType === Node.TEXT_NODE) {
      pieces.push((node.textContent ?? '').replace(/\s+/g, ' '))
      return
    }
    if (node.nodeType !== Node.ELEMENT_NODE) {
      return
    }
    const element = node as HTMLElement
    const tag = element.tagName.toLowerCase()

    if (tag === 'br') {
      pieces.push('\n')
      return
    }
    if (tag === 'hr') {
      pieces.push('\n----\n')
      return
    }
    if (tag === 'a') {
      const href = element.getAttribute('href') ?? ''
      const text = (element.textContent ?? '').trim()
      pieces.push(text)
      if (href && href !== text && !href.startsWith('mailto:' + text)) {
        pieces.push(` (${href})`)
      }
      return
    }

    const block = [
      'p',
      'div',
      'h1',
      'h2',
      'h3',
      'h4',
      'h5',
      'h6',
      'blockquote',
      'pre',
      'tr',
      'li',
      'ul',
      'ol',
      'table',
    ].includes(tag)
    if (block) {
      pieces.push('\n')
    }
    if (tag === 'li') {
      pieces.push(listMarker())
    }

    let counter = 0
    const childMarker =
      tag === 'ol'
        ? () => {
            counter += 1
            return `${counter}. `
          }
        : tag === 'ul'
          ? () => '- '
          : listMarker
    if (tag === 'blockquote') {
      const start = pieces.length
      element.childNodes.forEach((child) => walk(child, childMarker))
      const quoted = pieces
        .splice(start)
        .join('')
        .split('\n')
        .map((line) => (line ? `> ${line}` : line))
        .join('\n')
      pieces.push(quoted)
    } else {
      element.childNodes.forEach((child) => walk(child, childMarker))
    }
    if (block) {
      pieces.push('\n')
    }
  }

  parsed.body.childNodes.forEach((child) => walk(child, () => '- '))

  return pieces
    .join('')
    .split('\n')
    .map((line) => line.replace(/[ \t]+$/g, '').replace(/^ +/, (match) => match))
    .join('\n')
    .replace(/\n{3,}/g, '\n\n')
    .trim()
}

// textToHtml is the other direction, for switching a message written as
// plain text into the editor: paragraphs at blank lines, line breaks within.
export function textToHtml(text: string): string {
  return text
    .split(/\n{2,}/)
    .map((paragraph) => paragraph.trim())
    .filter(Boolean)
    .map((paragraph) => `<p>${escapeText(paragraph).replace(/\n/g, '<br>')}</p>`)
    .join('')
}
