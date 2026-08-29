import { useEffect, useState } from 'react'

import { Rendered } from '../api'
import { useTranslation } from '../i18n/i18n'
import { HighlightedHtml } from './highlightHtml'
import { MessageFrame } from './messageFrame'

// What a message will look like, as the server rendered it. Shared by the
// two editors and the compose page, so all three show the same thing: the
// subject, then the HTML in the same sandboxed frame stored mail is shown in,
// the text alternative, or the markup.

// useDebounced hands back a value only once it has stopped changing for a
// moment. A preview that re-renders on every keystroke asks the server a
// question per letter, and the answer to most of them is never seen.
export function useDebounced<T>(value: T, delay = 400): T {
  const [settled, setSettled] = useState(value)
  useEffect(() => {
    const timer = window.setTimeout(() => setSettled(value), delay)
    return () => window.clearTimeout(timer)
  }, [value, delay])
  return settled
}

type Tab = 'rendered' | 'text' | 'html'

export function RenderedPreview({
  rendered,
  error,
  showSubject = true,
}: {
  rendered: Rendered | null
  error: string | null
  showSubject?: boolean
}) {
  const { t } = useTranslation()
  const [chosen, setChosen] = useState<Tab | null>(null)

  const hasHtml = Boolean(rendered?.htmlContent?.trim())
  const tab = chosen ?? (hasHtml ? 'rendered' : 'text')

  return (
    <div className="preview">
      {error && <p className="error">{error}</p>}
      {showSubject && rendered && (
        <div className="preview-subject">
          <span className="muted">{t('preview.subject')}</span>{' '}
          {rendered.subject || <span className="muted">{t('mail.noSubject')}</span>}
        </div>
      )}
      <div className="tabs">
        <button
          type="button"
          className={tab === 'rendered' ? 'active' : ''}
          onClick={() => setChosen('rendered')}
          disabled={!hasHtml}
        >
          {t('mailDetail.rendered')}
        </button>
        <button type="button" className={tab === 'text' ? 'active' : ''} onClick={() => setChosen('text')}>
          {t('mailDetail.text')}
        </button>
        <button
          type="button"
          className={tab === 'html' ? 'active' : ''}
          onClick={() => setChosen('html')}
          disabled={!hasHtml}
        >
          {t('mailDetail.html')}
        </button>
      </div>
      {tab === 'rendered' && hasHtml && (
        <MessageFrame document={previewDocument(rendered?.htmlContent ?? '')} title={t('preview.title')} />
      )}
      {tab === 'text' && (
        <pre className="message-text">
          {rendered?.textContent || <span className="muted">{t('preview.noText')}</span>}
        </pre>
      )}
      {tab === 'html' && <HighlightedHtml source={rendered?.htmlContent ?? ''} />}
    </div>
  )
}

// previewDocument wraps HTML for the frame. The policy is as strict as the
// one stored mail is shown under, with one difference: images from anywhere
// are allowed, because this is the operator's own content and a logo on
// their own site is the ordinary case, not a tracking pixel.
export function previewDocument(html: string): string {
  const policy = [
    "default-src 'none'",
    'img-src data: https: http:',
    "style-src 'unsafe-inline'",
    'font-src data:',
  ].join('; ')

  return `<!DOCTYPE html><html><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="${policy}">
<style>#teanode-content{overflow:hidden}body{margin:0;padding:14px;font:15px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;color:#16161a;background:#fff;word-wrap:break-word}img{max-width:100%;height:auto}table{max-width:100%}</style>
</head><body><div id="teanode-content">${html}</div></body></html>`
}
