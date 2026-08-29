import { useRef, useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'

import { Domain, Layout, LayoutTranslation, Rendered, graphql } from '../api'
import { ErrorMessage, Loading } from '../components/common'
import { ConfirmDialog } from '../components/dialog'
import { LocaleTabs } from '../components/localeTabs'
import { RenderedPreview, useDebounced } from '../components/preview'
import { useQuery } from '../components/useQuery'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { MediaButton, imageTag } from '../components/media'
import { useTranslation } from '../i18n/i18n'
import { describeLayout } from './templates'

const LAYOUT = `
  query ($domainId: String!, $layoutId: String!) {
    GetDomain(domainId: $domainId) { id domain }
    GetLayout(layoutId: $layoutId) {
      id domainId comment locale htmlContent textContent modifiedAt
      translations { locale htmlContent textContent }
    }
  }`

const RENDER = `
  query ($domainId: String!, $layoutParameters: LayoutParametersInput!, $locale: String, $variables: Any) {
    RenderLayout(domainId: $domainId, layoutParameters: $layoutParameters, locale: $locale, variables: $variables) {
      subject htmlContent textContent locale variables
    }
  }`

const SAVE = `
  mutation ($layoutId: String!, $layoutParameters: LayoutParametersInput!) {
    ModifyLayout(layoutId: $layoutId, layoutParameters: $layoutParameters) {
      layout { id modifiedAt }
    }
  }`

const DELETE = `mutation ($layoutId: String!) { DeleteLayout(layoutId: $layoutId) }`

type Response = {
  GetDomain: Domain
  GetLayout: Layout
}

type Form = {
  comment: string
  locale: string
  htmlContent: string
  textContent: string
  translations: LayoutTranslation[]
}

function formOf(layout: Layout): Form {
  return {
    comment: layout.comment ?? '',
    locale: layout.locale ?? '',
    htmlContent: layout.htmlContent ?? '',
    textContent: layout.textContent ?? '',
    translations: (layout.translations ?? []).map((translation) => ({
      locale: translation.locale,
      htmlContent: translation.htmlContent ?? '',
      textContent: translation.textContent ?? '',
    })),
  }
}

// The layout editor is the template editor with less in it: no name, no
// subject, no layout to choose. The preview renders the layout by itself,
// with each block showing whatever the layout put there as a default.
export function LayoutEditorPage() {
  const { t } = useTranslation()
  const htmlEditor = useRef<HTMLTextAreaElement>(null)
  const { domainId = '', layoutId = '' } = useParams()
  const navigate = useNavigate()
  const { data, error, loading, reload } = useQuery(
    () => graphql<Response>(LAYOUT, { domainId, layoutId }),
    [domainId, layoutId],
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

  useBreadcrumbDetail(data?.GetDomain?.domain, data?.GetLayout ? describeLayout(data.GetLayout, t) : undefined)

  useEffect(() => {
    if (data?.GetLayout && form === null) {
      const initial = formOf(data.GetLayout)
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
    graphql<{ RenderLayout: Rendered }>(RENDER, {
      domainId,
      layoutParameters: request.form,
      locale: request.active || request.form.locale || null,
      variables: request.samples,
    })
      .then((result) => {
        if (!cancelled) {
          setRendered(result.RenderLayout)
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
  if (!data?.GetLayout || !form) {
    return <p className="muted">{t('common.notFound')}</p>
  }

  const dirty = JSON.stringify(form) !== saved
  const variables = rendered?.variables ?? []
  const translation = active ? form.translations.find((each) => each.locale === active) : undefined
  const content = translation ?? form

  function setContent(patch: Partial<Pick<Form, 'htmlContent' | 'textContent'>>) {
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
      await graphql(SAVE, { layoutId, layoutParameters: form })
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
              <span>{t('layouts.comment')}</span>
              <input
                value={form.comment}
                placeholder={t('layouts.commentPlaceholder')}
                onChange={(event) => setForm({ ...form, comment: event.target.value })}
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

          <LocaleTabs
            defaultLocale={form.locale}
            locales={form.translations.map((each) => each.locale)}
            active={active}
            onSelect={setActive}
            onAdd={(locale) => {
              setForm({ ...form, translations: [...form.translations, { locale, htmlContent: '', textContent: '' }] })
              setActive(locale)
            }}
          />

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
          <p className="muted field-hint">{t('editor.layoutHint')}</p>
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
            <button className="link danger" type="button" onClick={() => setRemoving(true)}>
              {t('common.remove')}
            </button>
          </div>
        </div>

        <div className="card editor-preview">
          <h3>{t('editor.preview')}</h3>
          {variables.length > 0 && (
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
          )}
          <RenderedPreview rendered={rendered} error={renderProblem} showSubject={false} />
        </div>
      </div>

      {removing && (
        <ConfirmDialog
          title={t('layouts.removeTitle')}
          body={t('layouts.removeBody')}
          confirmLabel={t('common.remove')}
          busy={busy}
          onConfirm={() => {
            setRemoving(false)
            void (async () => {
              setBusy(true)
              try {
                await graphql(DELETE, { layoutId })
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
