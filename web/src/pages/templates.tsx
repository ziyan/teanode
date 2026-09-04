import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { Domain, Layout, Template, graphql } from '../api'
import { ErrorMessage, Loading, Tag } from '../components/common'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { TrashIcon } from '../components/icons'
import { RelativeTime } from '../components/relativeTime'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../components/settingsList'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'

const LIST = `
  query ($domainId: String!) {
    GetDomain(domainId: $domainId) { id domain }
    ListTemplates(domainId: $domainId) {
      id name comment locale layoutId modifiedAt variables
      translations { locale }
    }
    ListLayouts(domainId: $domainId) {
      id comment locale modifiedAt
      translations { locale }
    }
  }`

// A new template starts empty and opens in the editor; the dialog asks only
// for the one thing it cannot do without, the name a caller sends it by.
const CREATE_TEMPLATE = `
  mutation ($domainId: String!, $name: String!) {
    CreateTemplate(
      domainId: $domainId
      templateParameters: { layoutId: "", name: $name, comment: "", subject: "", htmlContent: "", textContent: "" }
    ) {
      template { id }
    }
  }`

const CREATE_LAYOUT = `
  mutation ($domainId: String!, $comment: String!) {
    CreateLayout(domainId: $domainId, layoutParameters: { comment: $comment, htmlContent: "", textContent: "" }) {
      layout { id }
    }
  }`

const DELETE_TEMPLATE = `mutation ($templateId: String!) { DeleteTemplate(templateId: $templateId) }`
const DELETE_LAYOUT = `mutation ($layoutId: String!) { DeleteLayout(layoutId: $layoutId) }`

type Response = {
  GetDomain: Domain
  ListTemplates: Template[]
  ListLayouts: Layout[]
}

// Names a template's name may take: what fits in a URL path and a shell
// argument without quoting, because that is where it is typed.
const TEMPLATE_NAME = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,31}$/

export function TemplatesTab() {
  const { t, plural } = useTranslation()
  const { domainId = '' } = useParams()
  const navigate = useNavigate()
  const { data, error, loading, reload } = useQuery(() => graphql<Response>(LIST, { domainId }), [domainId], {
    refresh: false,
  })

  const [addingTemplate, setAddingTemplate] = useState(false)
  const [addingLayout, setAddingLayout] = useState(false)
  const [name, setName] = useState('')
  const [comment, setComment] = useState('')
  const [removingTemplate, setRemovingTemplate] = useState<Template | null>(null)
  const [removingLayout, setRemovingLayout] = useState<Layout | null>(null)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  async function run(work: () => Promise<unknown>) {
    setBusy(true)
    setProblem(null)
    try {
      await work()
      await reload()
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : t('domain.failed'))
    } finally {
      setBusy(false)
    }
  }

  if (loading && !data) {
    return <Loading />
  }
  if (error) {
    return <ErrorMessage error={error} />
  }
  if (!data?.GetDomain) {
    return <p className="muted">{t('common.notFound')}</p>
  }

  const templates = data.ListTemplates ?? []
  const layouts = data.ListLayouts ?? []
  const layoutsById = new Map(layouts.map((layout) => [layout.id, layout]))
  const usage = new Map<string, number>()
  for (const template of templates) {
    if (template.layoutId) {
      usage.set(template.layoutId, (usage.get(template.layoutId) ?? 0) + 1)
    }
  }

  return (
    <>
      {problem && <p className="error">{problem}</p>}

      <SettingsSection
        title={t('templates.title')}
        description={t('templates.intro')}
        action={
          <button className="primary" type="button" onClick={() => setAddingTemplate(true)}>
            {t('templates.new')}
          </button>
        }
      >
        {templates.length === 0 && <SettingsEmpty>{t('templates.empty')}</SettingsEmpty>}
        {templates.map((template) => (
          <SettingsRow
            key={template.id}
            title={<Link to={`/domains/${domainId}/templates/${template.id}`}>{template.name}</Link>}
            badge={
              <Languages
                defaultLocale={template.locale}
                locales={(template.translations ?? []).map((each) => each.locale)}
              />
            }
            subtitle={
              <>
                {template.comment && <div>{template.comment}</div>}
                <div>
                  {template.layoutId
                    ? t('templates.inLayout', { layout: describeLayout(layoutsById.get(template.layoutId), t) })
                    : t('templates.noLayout')}
                  {' · '}
                  {template.variables?.length
                    ? t('templates.variables', { variables: template.variables.join(', ') })
                    : t('templates.noVariables')}
                  {template.modifiedAt && (
                    <>
                      {' · '}
                      {t('templates.modified')} <RelativeTime value={template.modifiedAt} />
                    </>
                  )}
                </div>
              </>
            }
            actions={
              <>
                <Link
                  className="button"
                  to={`/mail/compose?domain=${encodeURIComponent(domainId)}&template=${encodeURIComponent(template.id)}`}
                >
                  {t('templates.send')}
                </Link>
                <button
                  className="icon-button danger"
                  type="button"
                  aria-label={t('common.remove')}
                  title={t('common.remove')}
                  onClick={() => setRemovingTemplate(template)}
                >
                  <TrashIcon />
                </button>
              </>
            }
          />
        ))}
      </SettingsSection>

      <SettingsSection
        title={t('layouts.title')}
        description={t('layouts.intro')}
        action={
          <button className="primary" type="button" onClick={() => setAddingLayout(true)}>
            {t('layouts.new')}
          </button>
        }
      >
        {layouts.length === 0 && <SettingsEmpty>{t('layouts.empty')}</SettingsEmpty>}
        {layouts.map((layout) => (
          <SettingsRow
            key={layout.id}
            title={<Link to={`/domains/${domainId}/layouts/${layout.id}`}>{describeLayout(layout, t)}</Link>}
            badge={
              <Languages
                defaultLocale={layout.locale}
                locales={(layout.translations ?? []).map((each) => each.locale)}
              />
            }
            subtitle={
              <div>
                {plural(usage.get(layout.id) ?? 0, { one: 'layouts.usedByOne', other: 'layouts.usedBy' })}
                {layout.modifiedAt && (
                  <>
                    {' · '}
                    {t('templates.modified')} <RelativeTime value={layout.modifiedAt} />
                  </>
                )}
              </div>
            }
            actions={
              <button
                className="icon-button danger"
                type="button"
                aria-label={t('common.remove')}
                title={t('common.remove')}
                onClick={() => setRemovingLayout(layout)}
              >
                <TrashIcon />
              </button>
            }
          />
        ))}
      </SettingsSection>

      {addingTemplate && (
        <FormDialog
          title={t('templates.new')}
          submitLabel={t('common.create')}
          busy={busy}
          error={problem}
          canSubmit={TEMPLATE_NAME.test(name)}
          onClose={() => {
            setAddingTemplate(false)
            setProblem(null)
          }}
          onSubmit={() =>
            void run(async () => {
              const result = await graphql<{ CreateTemplate: { template: { id: string } } }>(CREATE_TEMPLATE, {
                domainId,
                name,
              })
              setAddingTemplate(false)
              setName('')
              navigate(`/domains/${domainId}/templates/${result.CreateTemplate.template.id}`)
            })
          }
        >
          <label>
            <span>{t('templates.name')}</span>
            <input
              className="mono"
              value={name}
              placeholder="welcome"
              spellCheck={false}
              autoComplete="off"
              onChange={(event) => setName(event.target.value)}
            />
          </label>
          <p className="muted field-hint">{t('templates.nameHint')}</p>
        </FormDialog>
      )}

      {addingLayout && (
        <FormDialog
          title={t('layouts.new')}
          submitLabel={t('common.create')}
          busy={busy}
          error={problem}
          onClose={() => {
            setAddingLayout(false)
            setProblem(null)
          }}
          onSubmit={() =>
            void run(async () => {
              const result = await graphql<{ CreateLayout: { layout: { id: string } } }>(CREATE_LAYOUT, {
                domainId,
                comment,
              })
              setAddingLayout(false)
              setComment('')
              navigate(`/domains/${domainId}/layouts/${result.CreateLayout.layout.id}`)
            })
          }
        >
          <label>
            <span>{t('layouts.comment')}</span>
            <input
              value={comment}
              placeholder={t('layouts.commentPlaceholder')}
              onChange={(event) => setComment(event.target.value)}
            />
          </label>
        </FormDialog>
      )}

      {removingTemplate && (
        <ConfirmDialog
          title={t('templates.removeTitle', { name: removingTemplate.name })}
          body={t('templates.removeBody')}
          confirmLabel={t('common.remove')}
          busy={busy}
          onConfirm={() => {
            const templateId = removingTemplate.id
            setRemovingTemplate(null)
            void run(() => graphql(DELETE_TEMPLATE, { templateId }))
          }}
          onClose={() => setRemovingTemplate(null)}
        />
      )}

      {removingLayout && (
        <ConfirmDialog
          title={t('layouts.removeTitle')}
          body={
            usage.get(removingLayout.id)
              ? t('layouts.removeBodyUsed', { count: usage.get(removingLayout.id) ?? 0 })
              : t('layouts.removeBody')
          }
          confirmLabel={t('common.remove')}
          busy={busy}
          onConfirm={() => {
            const layoutId = removingLayout.id
            setRemovingLayout(null)
            void run(() => graphql(DELETE_LAYOUT, { layoutId }))
          }}
          onClose={() => setRemovingLayout(null)}
        />
      )}
    </>
  )
}

// A layout has no name of its own: what it is for is its comment, and a
// layout with none is named by when it was made, which is in its identifier.
export function describeLayout(layout: Layout | undefined, t: (key: 'layouts.untitled') => string): string {
  if (!layout) {
    return t('layouts.untitled')
  }
  return layout.comment || t('layouts.untitled')
}

// The languages something is written in, as chips: the default first, then
// each translation. A template with nothing said about its language shows
// nothing, which is what is true of it.
function Languages({ defaultLocale, locales }: { defaultLocale?: string; locales: string[] }) {
  const all = [defaultLocale, ...locales].filter(Boolean) as string[]
  if (all.length === 0) {
    return null
  }
  return (
    <span className="languages">
      {all.map((locale) => (
        <Tag key={locale} value={locale} />
      ))}
    </span>
  )
}
