import { useState } from 'react'

import { useTranslation } from '../i18n/i18n'
import { FormDialog } from './dialog'

// The row of languages an editor switches between: the default content,
// then one tab per translation, then a way to add one. The empty string
// stands for the default, because that is what the server calls a template
// with no locale.

// The shape the server accepts for a locale: a two or three letter language,
// then dash separated subtags. Checked here too, so the dialog can say no
// before a round trip.
const LOCALE = /^[A-Za-z]{2,3}(-[A-Za-z0-9]{1,8})*$/

export function isLocale(value: string): boolean {
  return LOCALE.test(value)
}

export function LocaleTabs({
  defaultLocale,
  locales,
  active,
  onSelect,
  onAdd,
}: {
  defaultLocale: string
  locales: string[]
  active: string
  onSelect: (locale: string) => void
  onAdd: (locale: string) => void
}) {
  const { t } = useTranslation()
  const [adding, setAdding] = useState(false)
  const [draft, setDraft] = useState('')

  const taken = new Set([defaultLocale, ...locales].map((locale) => locale.toLowerCase()))
  const candidate = draft.trim()
  const acceptable = isLocale(candidate) && !taken.has(candidate.toLowerCase())

  return (
    <>
      <div className="tabs locale-tabs">
        <button type="button" className={active === '' ? 'active' : ''} onClick={() => onSelect('')}>
          {defaultLocale ? t('locale.defaultNamed', { locale: defaultLocale }) : t('locale.default')}
        </button>
        {locales.map((locale) => (
          <button
            key={locale}
            type="button"
            className={active === locale ? 'active' : ''}
            onClick={() => onSelect(locale)}
          >
            {locale}
          </button>
        ))}
        <button type="button" className="tab-action" onClick={() => setAdding(true)}>
          {t('locale.add')}
        </button>
      </div>

      {adding && (
        <FormDialog
          title={t('locale.addTitle')}
          submitLabel={t('locale.addButton')}
          canSubmit={acceptable}
          onClose={() => {
            setAdding(false)
            setDraft('')
          }}
          onSubmit={() => {
            onAdd(candidate)
            setAdding(false)
            setDraft('')
          }}
        >
          <label>
            <span>{t('locale.tag')}</span>
            <input
              className="mono"
              value={draft}
              placeholder="zh-CN"
              spellCheck={false}
              autoComplete="off"
              onChange={(event) => setDraft(event.target.value)}
            />
          </label>
          <p className="muted field-hint">{t('locale.tagHint')}</p>
          {candidate && !isLocale(candidate) && <p className="error">{t('locale.notATag')}</p>}
          {candidate && isLocale(candidate) && taken.has(candidate.toLowerCase()) && (
            <p className="error">{t('locale.alreadyThere')}</p>
          )}
        </FormDialog>
      )}
    </>
  )
}
