import { useMemo } from 'react'

import { SettingsSection } from '../../components/settingsList'
import { useTranslation } from '../../i18n/i18n'
import { HEALTH_CATEGORIES, HealthType, amountText, categoryName, parseDay, summaryValue, typeName } from './healthApi'

// The kinds of data the person has, in the groups the Health app puts them in,
// each with its latest day's value.

export function groupByCategory(healthTypes: HealthType[]): { healthCategory: string; healthTypes: HealthType[] }[] {
  const known = HEALTH_CATEGORIES as readonly string[]
  const unknown = [...new Set(healthTypes.map((each) => each.healthCategory))].filter((each) => !known.includes(each))
  return [...known, ...unknown.sort()]
    .map((healthCategory) => ({
      healthCategory,
      healthTypes: healthTypes.filter((each) => each.healthCategory === healthCategory),
    }))
    .filter((group) => group.healthTypes.length > 0)
}

export function HealthTypeList({
  healthTypes,
  chosenType,
  onChoose,
}: {
  healthTypes: HealthType[]
  chosenType: string | null
  onChoose: (healthType: string) => void
}) {
  const { t, language } = useTranslation()
  const groups = useMemo(() => groupByCategory(healthTypes), [healthTypes])
  const dayFormat = useMemo(() => new Intl.DateTimeFormat(language, { month: 'short', day: 'numeric' }), [language])
  return (
    <SettingsSection card title={t('health.typesTitle')}>
      <div className="health-types" role="group" aria-label={t('health.typesLabel')}>
        {groups.map((group) => (
          <section key={group.healthCategory}>
            <h4>{categoryName(t, group.healthCategory)}</h4>
            {group.healthTypes.map((each) => {
              const latest = each.latestDaySummary
              const latestAmount = latest ? summaryValue(latest, each.summaryKind) : null
              return (
                <button
                  key={each.healthType}
                  type="button"
                  className={each.healthType === chosenType ? 'health-type-row is-selected' : 'health-type-row'}
                  aria-current={each.healthType === chosenType ? 'true' : undefined}
                  onClick={() => onChoose(each.healthType)}
                >
                  <span className="health-type-name">
                    <strong>{typeName(t, each.healthType, each.typeName)}</strong>
                    {latest && <span className="muted">{dayFormat.format(parseDay(latest.summaryDay))}</span>}
                  </span>
                  <span className="health-type-value">
                    {latest ? amountText(latestAmount, each.summaryKind, each.quantityUnit, language, t) : ''}
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
