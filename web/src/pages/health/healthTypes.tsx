import { useMemo } from 'react'

import { SettingsSection } from '../../components/settingsList'
import { useTranslation } from '../../i18n/i18n'
import { HEALTH_CATEGORIES, HealthType, amountText, categoryName, parseDay, summaryValue, typeName } from './healthApi'

// The kinds of data the person has, in the groups the Health app puts them in,
// each with its latest day's value.

export function groupByCategory(types: HealthType[]): { healthCategory: string; types: HealthType[] }[] {
  const known = HEALTH_CATEGORIES as readonly string[]
  const unknown = [...new Set(types.map((each) => each.healthCategory))].filter((each) => !known.includes(each))
  return [...known, ...unknown.sort()]
    .map((healthCategory) => ({
      healthCategory,
      types: types.filter((each) => each.healthCategory === healthCategory),
    }))
    .filter((group) => group.types.length > 0)
}

export function HealthTypeList({
  types,
  chosenType,
  onChoose,
}: {
  types: HealthType[]
  chosenType: string | null
  onChoose: (healthType: string) => void
}) {
  const { t, language } = useTranslation()
  const groups = useMemo(() => groupByCategory(types), [types])
  const dayFormat = useMemo(() => new Intl.DateTimeFormat(language, { month: 'short', day: 'numeric' }), [language])
  return (
    <SettingsSection card title={t('health.typesTitle')}>
      <div className="health-types" role="group" aria-label={t('health.typesLabel')}>
        {groups.map((group) => (
          <section key={group.healthCategory}>
            <h4>{categoryName(t, group.healthCategory)}</h4>
            {group.types.map((type) => {
              const latest = type.latestDaySummary
              const value = latest ? summaryValue(latest, type.summaryKind) : null
              return (
                <button
                  key={type.healthType}
                  type="button"
                  className={type.healthType === chosenType ? 'health-type-row is-selected' : 'health-type-row'}
                  aria-pressed={type.healthType === chosenType}
                  onClick={() => onChoose(type.healthType)}
                >
                  <span className="health-type-name">
                    <strong>{typeName(t, type.healthType, type.typeName)}</strong>
                    {latest && <span className="muted">{dayFormat.format(parseDay(latest.summaryDay))}</span>}
                  </span>
                  <span className="health-type-value">
                    {latest ? amountText(value, type.summaryKind, type.quantityUnit, language, t) : ''}
                  </span>
                </button>
              )
            })}
          </section>
        ))}
      </div>
    </SettingsSection>
  )
}
