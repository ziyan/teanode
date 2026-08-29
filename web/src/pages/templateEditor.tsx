import { useRef, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { Domain, Layout, Rendered, Template, TemplateTranslation, graphql } from '../api'
import { ErrorMessage, Loading } from '../components/common'
import { ConfirmDialog } from '../components/dialog'
import { LocaleTabs } from '../components/localeTabs'
import { RenderedPreview, useDebounced } from '../components/preview'
import { useQuery } from '../components/useQuery'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { MediaButton, imageTag } from '../components/media'
import { useTranslation } from '../i18n/i18n'
import { describeLayout } from './templates'

const TEMPLATE = `
  query ($domainId: String!, $templateId: String!) {
    GetDomain(domainId: $domainId) { id domain }
    GetTemplate(templateId: $templateId) {
      id domainId layoutId name comment locale subject htmlContent textContent variables modifiedAt
      translations { locale subject htmlContent textContent }
    }
    ListLayouts(domainId: $domainId) { id comment locale }
  }`

// The preview renders what is in the form, saved or not, so what is on the
// right is always what is on the left.
const RENDER = `
  query ($domainId: String!, $templateParameters: TemplateParametersInput!, $locale: String, $variables: Any) {
    RenderTemplate(domainId: $domainId, templateParameters: $templateParameters, locale: $locale, variables: $variables) {
      subject htmlContent textContent locale variables
    }
  }`

const SAVE = `
  mutation ($templateId: String!, $templateParameters: TemplateParametersInput!) {
    ModifyTemplate(templateId: $templateId, templateParameters: $templateParameters) {
      template { id modifiedAt }
    }
  }`

const DELETE = `mutation ($templateId: String!) { DeleteTemplate(templateId: $templateId) }`

type Response = {
  GetDomain: Domain
  GetTemplate: Template
  ListLayouts: Layout[]
}

// The form is exactly what ModifyTemplate takes, so saving is sending it.
type Form = {
  layoutId: string
  name: string
  comment: string
  locale: string
  subject: string
  htmlContent: string
  textContent: string
  translations: TemplateTranslation[]
}

function formOf(template: Template): Form {
  return {
    layoutId: template.layoutId ?? '',
    name: template.name,
    comment: template.comment ?? '',
    locale: template.locale ?? '',
    subject: template.subject ?? '',
    htmlContent: template.htmlContent ?? '',
    textContent: template.textContent ?? '',
    translations: (template.translations ?? []).map((translation) => ({
      locale: translation.locale,
      subject: translation.subject ?? '',
      htmlContent: translation.htmlContent ?? '',
      textContent: translation.textContent ?? '',
    })),
  }
}

export function TemplateEditorPage() {
  const { t } = useTranslation()
  const htmlEditor = useRef<HTMLTextAreaElement>(null)
  const { domainId = '', templateId = '' } = useParams()
  const navigate = useNavigate()
  const { data, error, loading, reload } = useQuery(
    () => graphql<Response>(TEMPLATE, { domainId, templateId }),
    [domainId, templateId],
    { refresh: false },
  )

  const [form, setForm] = useState<Form | null>(null)
  const [saved, setSaved] = useState('')
  const [active, setActive] = useState('')
  const [samples, setSamples] = useState<Record<string, string>>({})
  const [rendered, setRendered] = useState<Rendered | null>(null)
  const [renderProblem, setRenderProblem] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [removing, setRemoving] = useState(false)

  useBreadcrumbDetail(data?.GetDomain?.domain, data?.GetTemplate?.name)

  // The form is filled from the server once. A background reload must not
  // overwrite what is being typed.
  useEffect(() => {
    if (data?.GetTemplate && form === null) {
      const initial = formOf(data.GetTemplate)
      setForm(initial)
      setSaved(JSON.stringify(initial))
    }
  }, [data, form])

  const request = useDebounced(useMemo(() => ({ form, active, samples }), [form, active, samples]))

  useEffect(() => {
    if (!request.form) {
      return
    }
    let cancelled = false
    graphql<{ RenderTemplate: Rendered }>(RENDER, {
      domainId,
      templateParameters: request.form,
      locale: request.active || request.form.locale || null,
      variables: request.samples,
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
  }, [domainId, request])

  if (loading && !data) {
    return <Loading />
  }
  if (error) {
    return <ErrorMessage error={error} />
  }
  if (!data?.GetTemplate || !form) {
    return <p className="muted">{t('common.notFound')}</p>
  }

  const dirty = JSON.stringify(form) !== saved
  const variables = rendered?.variables ?? data.GetTemplate.variables ?? []

  // Which content the tabs are showing: the template's own, or one
  // translation's. Editing goes to the same place.
  const translation = active ? form.translations.find((each) => each.locale === active) : undefined
  const content = translation ?? form
  function setContent(patch: Partial<Pick<Form, 'subject' | 'htmlContent' | 'textContent'>>) {
    setForm((previous) => {
      if (!previous) {
        return previous
      }
      if (!active) {
        return { ...previous, ...patch }
      }
      return {
        ...previous,
        translations: previous.translations.map((each) => (each.locale === active ? { ...each, ...patch } : each)),
      }
    })
  }

  async function save() {
    if (!form) {
      return
    }
    setBusy(true)
    setProblem(null)
    setNotice(null)
    try {
      await graphql(SAVE, { templateId, templateParameters: form })
      setSaved(JSON.stringify(form))
      setNotice(t('editor.saved'))
      await reload()
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : t('domain.failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      {problem && <p className="error">{problem}</p>}
      {notice && <p className="muted">{notice}</p>}

      <div className="editor">
        <div className="card editor-form">
          <div className="row fields">
            <label style={{ margin: 0 }}>
              <span>{t('templates.name')}</span>
              <input
                className="mono"
                value={form.name}
                spellCheck={false}
                autoComplete="off"
                onChange={(event) => setForm({ ...form, name: event.target.value })}
              />
            </label>
            <label className="shrink" style={{ margin: 0, width: 140 }}>
              <span>{t('editor.defaultLocale')}</span>
              <input
                className="mono"
                value={form.locale}
                placeholder="en"
                spellCheck={false}
                autoComplete="off"
                onChange={(event) => setForm({ ...form, locale: event.target.value })}
              />
            </label>
          </div>
          <label>
            <span>{t('editor.layout')}</span>
            <select value={form.layoutId} onChange={(event) => setForm({ ...form, layoutId: event.target.value })}>
              <option value="">{t('editor.noLayout')}</option>
              {(data.ListLayouts ?? []).map((layout) => (
                <option key={layout.id} value={layout.id}>
                  {describeLayout(layout, t)}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>{t('editor.comment')}</span>
            <input value={form.comment} onChange={(event) => setForm({ ...form, comment: event.target.value })} />
          </label>

          <LocaleTabs
            defaultLocale={form.locale}
            locales={form.translations.map((each) => each.locale)}
            active={active}
            onSelect={setActive}
            onAdd={(locale) => {
              setForm({
                ...form,
                translations: [...form.translations, { locale, subject: '', htmlContent: '', textContent: '' }],
              })
              setActive(locale)
            }}
          />

          <label>
            <span>{t('editor.subject')}</span>
            <input value={content.subject ?? ''} onChange={(event) => setContent({ subject: event.target.value })} />
          </label>
          <label>
            <span>{t('editor.html')}</span>
            <textarea
              ref={htmlEditor}
              className="code-editor"
              value={content.htmlContent ?? ''}
              spellCheck={false}
              onChange={(event) => setContent({ htmlContent: event.target.value })}
            />
          </label>
          <p className="editor-actions">
            <MediaButton
              domainId={data?.GetDomain?.id}
              label={t('editor.insertPicture')}
              onUploaded={(media) =>
                insertAtCaret(htmlEditor.current, imageTag(media), (value) => setContent({ htmlContent: value }))
              }
            />
            <span className="muted">{t('editor.pictureHint')}</span>
          </p>
          <label>
            <span>{t('editor.text')}</span>
            <textarea
              className="code-editor"
              value={content.textContent ?? ''}
              spellCheck={false}
              onChange={(event) => setContent({ textContent: event.target.value })}
            />
          </label>
          <p className="muted field-hint">{t('editor.syntaxHint')}</p>
          {active && (
            <p>
              <button
                className="link danger"
                type="button"
                onClick={() => {
                  setForm({ ...form, translations: form.translations.filter((each) => each.locale !== active) })
                  setActive('')
                }}
              >
                {t('editor.removeTranslation', { locale: active })}
              </button>
            </p>
          )}

          <div className="page-actions">
            <button className="primary" type="button" disabled={busy || !dirty} onClick={() => void save()}>
              {t('common.save')}
            </button>
            <Link
              className="button"
              to={`/mail/compose?domain=${encodeURIComponent(domainId)}&template=${encodeURIComponent(templateId)}`}
            >
              {t('templates.send')}
            </Link>
            <button className="link danger" type="button" onClick={() => setRemoving(true)}>
              {t('common.remove')}
            </button>
          </div>
        </div>

        <div className="card editor-preview">
          <h3>{t('editor.preview')}</h3>
          {variables.length > 0 ? (
            <>
              <p className="muted" style={{ marginTop: 0 }}>
                {t('editor.sampleValues')}
              </p>
              <div className="variables">
                {variables.map((variable) => (
                  <label key={variable} style={{ margin: 0 }}>
                    <span className="mono">{variable}</span>
                    <input
                      value={samples[variable] ?? ''}
                      onChange={(event) => setSamples({ ...samples, [variable]: event.target.value })}
                    />
                  </label>
                ))}
              </div>
            </>
          ) : (
            <p className="muted" style={{ marginTop: 0 }}>
              {t('editor.noVariablesYet')}
            </p>
          )}
          <RenderedPreview rendered={rendered} error={renderProblem} />
        </div>
      </div>

      {removing && (
        <ConfirmDialog
          title={t('templates.removeTitle', { name: form.name })}
          body={t('templates.removeBody')}
          confirmLabel={t('common.remove')}
          busy={busy}
          onConfirm={() => {
            setRemoving(false)
            void (async () => {
              setBusy(true)
              try {
                await graphql(DELETE, { templateId })
                navigate(`/domains/${domainId}/templates`)
              } catch (caught) {
                setProblem(caught instanceof Error ? caught.message : t('domain.failed'))
              } finally {
                setBusy(false)
              }
            })()
          }}
          onClose={() => setRemoving(false)}
        />
      )}
    </>
  )
}

// insertAtCaret writes text where the caret is in a textarea, or at the end
// when it has not been focused, and leaves the caret after what it wrote.
// Done through the element rather than by rebuilding the value from state so
// that the caret does not jump to the start, which mid-template is unusable.
function insertAtCaret(element: HTMLTextAreaElement | null, text: string, apply: (value: string) => void) {
  if (!element) {
    return
  }
  const start = element.selectionStart ?? element.value.length
  const end = element.selectionEnd ?? start
  const value = element.value.slice(0, start) + text + element.value.slice(end)
  apply(value)
  // After the state has been written back, not before: React replaces the
  // value and would put the caret at the end.
  window.setTimeout(() => {
    element.focus()
    element.setSelectionRange(start + text.length, start + text.length)
  }, 0)
}
