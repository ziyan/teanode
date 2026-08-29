import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { Domain, Rendered, Template, graphql } from '../api'
import { ErrorMessage, Loading, formatBytes } from '../components/common'
import { PaperclipIcon } from '../components/icons'
import { RenderedPreview, useDebounced } from '../components/preview'
import { RichTextEditor, htmlToText, textToHtml } from '../components/richText'
import { useQuery } from '../components/useQuery'
import { Trans, useTranslation } from '../i18n/i18n'

const DOMAINS = `{ ListDomains { id domain } }`

const TEMPLATES = `
  query ($domainId: String!) {
    ListTemplates(domainId: $domainId) {
      id name comment locale variables
      translations { locale }
    }
  }`

const RENDER = `
  query ($domainId: String!, $templateId: String!, $locale: String, $variables: Any) {
    RenderTemplate(domainId: $domainId, templateId: $templateId, locale: $locale, variables: $variables) {
      subject htmlContent textContent locale variables
    }
  }`

const SEND = `
  mutation ($domainId: String!, $messageParameters: MessageParametersInput!) {
    SendMail(domainId: $domainId, messageParameters: $messageParameters) {
      mail { id subject }
    }
  }`

type Mode = 'template' | 'write'
type Editor = 'rich' | 'plain'

// splitAddresses reads a recipient field: addresses separated by commas,
// semicolons or newlines, each of which may carry a display name.
function splitAddresses(value: string): string[] {
  return value
    .split(/[;\n]|,(?![^<]*>)/)
    .map((entry) => entry.trim())
    .filter(Boolean)
}

// The file as base64, which is how the Data scalar carries bytes. A data URL
// is what the browser gives, and the bytes start after its comma.
function readAsBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(reader.error)
    reader.onload = () => {
      const result = String(reader.result)
      resolve(result.slice(result.indexOf(',') + 1))
    }
    reader.readAsDataURL(file)
  })
}

// The locale to offer first: the browser's, when the template has it, else
// the template's default. Somebody writing to a reader in their own language
// usually is one.
function preferredLocale(template: Template | undefined): string {
  if (!template) {
    return ''
  }
  const offered = (template.translations ?? []).map((translation) => translation.locale)
  for (const tag of navigator.languages ?? [navigator.language]) {
    const wanted = tag.toLowerCase()
    const exact = offered.find((locale) => locale.toLowerCase() === wanted)
    if (exact) {
      return exact
    }
    const language = wanted.split('-')[0]
    if ((template.locale ?? '').toLowerCase().split('-')[0] === language) {
      return ''
    }
    const close = offered.find((locale) => locale.toLowerCase().split('-')[0] === language)
    if (close) {
      return close
    }
  }
  return ''
}

export function ComposePage() {
  const { t } = useTranslation()
  const [search, setSearch] = useSearchParams()

  const domainQuery = useQuery(() => graphql<{ ListDomains: Domain[] }>(DOMAINS), [], { refresh: false })
  const domains = useMemo(() => domainQuery.data?.ListDomains ?? [], [domainQuery.data])

  // The domain to send as, from the address bar when a page linked here
  // with one, else the first there is.
  const [domainId, setDomainId] = useState(search.get('domain') ?? '')
  useEffect(() => {
    if (!domainId && domains.length > 0) {
      setDomainId(domains[0].id)
    }
  }, [domains, domainId])
  const domain = domains.find((each) => each.id === domainId)

  const templateQuery = useQuery(
    () =>
      domainId
        ? graphql<{ ListTemplates: Template[] }>(TEMPLATES, { domainId })
        : Promise.resolve({ ListTemplates: [] }),
    [domainId],
    { refresh: false },
  )
  const templates = useMemo(() => templateQuery.data?.ListTemplates ?? [], [templateQuery.data])

  const [mode, setMode] = useState<Mode>(search.get('template') ? 'template' : 'write')
  const [templateId, setTemplateId] = useState(search.get('template') ?? '')
  const [locale, setLocale] = useState('')
  const [values, setValues] = useState<Record<string, string>>({})
  const template = templates.find((each) => each.id === templateId)

  // When the templates arrive, or the one chosen goes away with a change of
  // domain, settle on one.
  useEffect(() => {
    if (templates.length === 0) {
      return
    }
    if (!templates.some((each) => each.id === templateId)) {
      setTemplateId(templates[0].id)
      setLocale(preferredLocale(templates[0]))
    }
  }, [templates, templateId])

  const [fromName, setFromName] = useState('')
  const [fromLocal, setFromLocal] = useState('')
  const [to, setTo] = useState('')
  const [cc, setCc] = useState('')
  const [bcc, setBcc] = useState('')
  const [subject, setSubject] = useState('')
  const [editor, setEditor] = useState<Editor>('rich')
  const [html, setHtml] = useState('')
  const [text, setText] = useState('')
  const [files, setFiles] = useState<File[]>([])
  const fileInput = useRef<HTMLInputElement>(null)

  const [sending, setSending] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const [sent, setSent] = useState<{ id: string; subject?: string } | null>(null)

  // The preview of a template, rendered by the server with the values typed
  // so far. What it shows is what will be sent.
  const [rendered, setRendered] = useState<Rendered | null>(null)
  const [renderProblem, setRenderProblem] = useState<string | null>(null)
  const request = useDebounced(
    useMemo(() => ({ domainId, templateId, locale, values, mode }), [domainId, templateId, locale, values, mode]),
  )
  useEffect(() => {
    if (request.mode !== 'template' || !request.domainId || !request.templateId) {
      setRendered(null)
      return
    }
    let cancelled = false
    graphql<{ RenderTemplate: Rendered }>(RENDER, {
      domainId: request.domainId,
      templateId: request.templateId,
      locale: request.locale || null,
      variables: request.values,
    })
      .then((result) => {
        if (!cancelled) {
          setRendered(result.RenderTemplate)
          setRenderProblem(null)
        }
      })
      .catch((caught) => {
        if (!cancelled) {
          setRenderProblem(caught instanceof Error ? caught.message : String(caught))
        }
      })
    return () => {
      cancelled = true
    }
  }, [request])

  const variables = rendered?.variables ?? template?.variables ?? []
  const totalSize = files.reduce((sum, file) => sum + file.size, 0)

  const ready =
    Boolean(domain) &&
    fromLocal.trim() !== '' &&
    splitAddresses(to).length + splitAddresses(cc).length + splitAddresses(bcc).length > 0 &&
    (mode === 'template' ? Boolean(templateId) : html.trim() !== '' || text.trim() !== '' || files.length > 0)

  async function send() {
    if (!domain) {
      return
    }
    setSending(true)
    setProblem(null)
    setSent(null)
    try {
      const attachments = await Promise.all(
        files.map(async (file) => ({
          filename: file.name,
          contentType: file.type || null,
          content: await readAsBase64(file),
        })),
      )
      const parameters: Record<string, unknown> = {
        from: `${fromLocal.trim()}@${domain.domain}`,
        fromName: fromName.trim() || null,
        to: splitAddresses(to),
        cc: splitAddresses(cc),
        bcc: splitAddresses(bcc),
        attachments,
      }
      if (mode === 'template') {
        parameters.templateId = templateId
        parameters.locale = locale || null
        parameters.variables = values
      } else {
        parameters.subject = subject
        // A message written as rich text carries a text alternative derived
        // from it, so a client that shows only text, and every filter, has
        // something to read. One written as plain text is only that.
        parameters.htmlContent = editor === 'rich' ? html : ''
        parameters.textContent = editor === 'rich' ? htmlToText(html) : text
      }
      const result = await graphql<{ SendMail: { mail?: { id: string; subject?: string } } }>(SEND, {
        domainId,
        messageParameters: parameters,
      })
      setSent(result.SendMail.mail ?? { id: '' })
      // The notice is at the top of the form and the Send button at the
      // bottom, so on anything but a tall screen the page has to go back
      // up to show what happened.
      document.querySelector('.content')?.scrollTo({ top: 0, behavior: 'smooth' })
      setTo('')
      setCc('')
      setBcc('')
      setSubject('')
      setHtml('')
      setText('')
      setFiles([])
      setValues({})
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : t('compose.failed'))
    } finally {
      setSending(false)
    }
  }

  if (domainQuery.loading && !domainQuery.data) {
    return <Loading />
  }
  if (domainQuery.error) {
    return <ErrorMessage error={domainQuery.error} />
  }
  if (domains.length === 0) {
    return (
      <p className="muted">
        <Trans k="mail.noDomains" nodes={{ link: <Link to="/domains">{t('nav.domains')}</Link> }} />
      </p>
    )
  }

  return (
    // Two columns while there is a preview to show beside the form; one,
    // held to a readable width, while there is not.
    <div className={mode === 'template' ? 'editor' : 'editor editor-single'}>
      <form
        className="card editor-form"
        onSubmit={(event) => {
          event.preventDefault()
          void send()
        }}
      >
        {problem && <p className="error">{problem}</p>}
        {sent && (
          <div className="banner">
            {sent.id ? (
              <Trans
                k="compose.sent"
                nodes={{ link: <Link to={`/mail/${sent.id}`}>{sent.subject || t('mail.noSubject')}</Link> }}
              />
            ) : (
              t('compose.sentUnseen')
            )}
          </div>
        )}

        <div className="row fields">
          <label style={{ margin: 0 }}>
            <span>{t('compose.fromName')}</span>
            <input
              value={fromName}
              placeholder={t('compose.fromNamePlaceholder')}
              onChange={(event) => setFromName(event.target.value)}
            />
          </label>
          {/* The address is typed on the left of the @ and chosen on the
              right of it: the domain is a choice among those configured,
              and this is where a reader looks for it. */}
          <label style={{ margin: 0, flex: 2 }}>
            <span>{t('compose.from')}</span>
            <span className="address-input">
              <input
                className="mono"
                value={fromLocal}
                placeholder="hello"
                spellCheck={false}
                autoComplete="off"
                onChange={(event) => setFromLocal(event.target.value.replace(/@.*$/, ''))}
              />
              <span className="address-domain">@</span>
              <select
                className="mono"
                aria-label={t('compose.domain')}
                value={domainId}
                onChange={(event) => {
                  setDomainId(event.target.value)
                  setSearch({ domain: event.target.value }, { replace: true })
                }}
              >
                {domains.map((each) => (
                  <option key={each.id} value={each.id}>
                    {each.domain}
                  </option>
                ))}
              </select>
            </span>
          </label>
        </div>

        <label>
          <span>{t('compose.to')}</span>
          <input
            value={to}
            placeholder={t('compose.toPlaceholder')}
            spellCheck={false}
            onChange={(event) => setTo(event.target.value)}
          />
        </label>
        <div className="row fields">
          <label style={{ margin: 0 }}>
            <span>{t('compose.carbonCopy')}</span>
            <input value={cc} spellCheck={false} onChange={(event) => setCc(event.target.value)} />
          </label>
          <label style={{ margin: 0 }}>
            <span>{t('compose.blindCarbonCopy')}</span>
            <input value={bcc} spellCheck={false} onChange={(event) => setBcc(event.target.value)} />
          </label>
        </div>

        <div className="tabs" style={{ marginTop: 16 }}>
          <button type="button" className={mode === 'write' ? 'active' : ''} onClick={() => setMode('write')}>
            {t('compose.modeWrite')}
          </button>
          <button
            type="button"
            className={mode === 'template' ? 'active' : ''}
            onClick={() => setMode('template')}
            disabled={templates.length === 0}
            title={templates.length === 0 ? t('compose.noTemplates') : undefined}
          >
            {t('compose.modeTemplate')}
          </button>
        </div>

        {mode === 'template' && (
          <>
            <div className="row fields">
              <label style={{ margin: 0 }}>
                <span>
                  {t('compose.template')}
                  {template && (
                    <>
                      {' · '}
                      <Link to={`/domains/${domainId}/templates/${template.id}`}>{t('compose.editTemplate')}</Link>
                    </>
                  )}
                </span>
                <select
                  value={templateId}
                  onChange={(event) => {
                    setTemplateId(event.target.value)
                    setValues({})
                    setLocale(preferredLocale(templates.find((each) => each.id === event.target.value)))
                  }}
                >
                  {templates.map((each) => (
                    <option key={each.id} value={each.id}>
                      {each.name}
                      {each.comment ? ` — ${each.comment}` : ''}
                    </option>
                  ))}
                </select>
              </label>
              <label style={{ margin: 0, maxWidth: 200 }}>
                <span>{t('compose.language')}</span>
                <select value={locale} onChange={(event) => setLocale(event.target.value)}>
                  <option value="">
                    {template?.locale ? t('locale.defaultNamed', { locale: template.locale }) : t('locale.default')}
                  </option>
                  {(template?.translations ?? []).map((translation) => (
                    <option key={translation.locale} value={translation.locale}>
                      {translation.locale}
                    </option>
                  ))}
                </select>
              </label>
            </div>
            {variables.length > 0 ? (
              <div className="variables">
                {variables.map((variable) => (
                  <label key={variable} style={{ margin: 0 }}>
                    <span className="mono">{variable}</span>
                    <input
                      value={values[variable] ?? ''}
                      onChange={(event) => setValues({ ...values, [variable]: event.target.value })}
                    />
                  </label>
                ))}
              </div>
            ) : (
              <p className="muted">{t('compose.noVariables')}</p>
            )}
          </>
        )}

        {mode === 'write' && (
          <>
            <label>
              <span>{t('compose.subject')}</span>
              <input value={subject} onChange={(event) => setSubject(event.target.value)} />
            </label>
            <div className="editor-switch">
              <span className="muted">{t('compose.editorLabel')}</span>
              <button
                type="button"
                className={editor === 'rich' ? 'active' : ''}
                onClick={() => {
                  if (editor === 'plain') {
                    setHtml(textToHtml(text))
                    setEditor('rich')
                  }
                }}
              >
                {t('compose.richText')}
              </button>
              <button
                type="button"
                className={editor === 'plain' ? 'active' : ''}
                onClick={() => {
                  if (editor === 'rich') {
                    // Formatting is lost on the way to plain text, which is
                    // what plain text means; the words all survive.
                    setText(htmlToText(html))
                    setEditor('plain')
                  }
                }}
              >
                {t('compose.plainText')}
              </button>
            </div>
            {editor === 'rich' ? (
              <RichTextEditor value={html} onChange={setHtml} placeholder={t('compose.bodyPlaceholder')} />
            ) : (
              <textarea
                className="plain-editor"
                value={text}
                onChange={(event) => setText(event.target.value)}
                placeholder={t('compose.bodyPlaceholder')}
              />
            )}
          </>
        )}

        <div className="attachments">
          <input
            ref={fileInput}
            type="file"
            multiple
            hidden
            onChange={(event) => {
              const chosen = Array.from(event.target.files ?? [])
              setFiles((previous) => [...previous, ...chosen])
              event.target.value = ''
            }}
          />
          {files.map((file, index) => (
            <div className="attachment" key={`${file.name}-${index}`}>
              <PaperclipIcon size={14} />
              <span className="attachment-name">{file.name}</span>
              <span className="muted">{formatBytes(file.size)}</span>
              <button
                className="link"
                type="button"
                onClick={() => setFiles(files.filter((_, position) => position !== index))}
              >
                {t('common.remove')}
              </button>
            </div>
          ))}
          <div className="attachment-actions">
            <button type="button" onClick={() => fileInput.current?.click()}>
              {t('compose.attach')}
            </button>
            {files.length > 0 && (
              <span className="muted">{t('compose.attachmentTotal', { size: formatBytes(totalSize) })}</span>
            )}
          </div>
        </div>

        <div className="page-actions">
          <button className="primary" type="submit" disabled={sending || !ready}>
            {sending ? t('compose.sending') : t('compose.send')}
          </button>
        </div>
      </form>

      {mode === 'template' && (
        <div className="card editor-preview">
          <h3>{t('editor.preview')}</h3>
          {rendered?.locale && (
            <p className="muted" style={{ marginTop: 0 }}>
              {t('compose.renderedIn', { locale: rendered.locale })}
            </p>
          )}
          <RenderedPreview rendered={rendered} error={renderProblem} />
        </div>
      )}
    </div>
  )
}
