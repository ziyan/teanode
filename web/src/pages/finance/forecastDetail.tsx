import { useId, useState } from 'react'

import { InfoIcon } from '../../components/icons'
import { Tooltip } from '../../components/tooltip'
import { useTranslation } from '../../i18n/i18n'

// ForecastDetail is the line under a budget's bar that says where the month
// is heading, with an icon button that opens, under it, how that number is
// worked out. Opened in the page rather than as a tooltip: the explanation
// carries a list of merchants, and a tooltip says one line and never opens
// to a tap. name is what the button explains, for a screen reader.
export function ForecastDetail({
  name,
  line,
  explanation,
}: {
  name: string
  line: React.ReactNode
  explanation: React.ReactNode
}) {
  const { t } = useTranslation()
  const [isOpen, setIsOpen] = useState(false)
  const explanationId = useId()
  return (
    <>
      <div className="muted finance-budget-row-detail finance-forecast-line">
        <span>{line}</span>
        <Tooltip label={t('finance.forecastExplain')}>
          <button
            type="button"
            className="icon-action finance-forecast-explain"
            aria-label={`${name}: ${t('finance.forecastExplain')}`}
            aria-expanded={isOpen}
            aria-controls={isOpen ? explanationId : undefined}
            onClick={() => setIsOpen((previous) => !previous)}
          >
            <InfoIcon size={14} />
          </button>
        </Tooltip>
      </div>
      {isOpen ? (
        <div id={explanationId} className="finance-forecast-explanation">
          {explanation}
        </div>
      ) : null}
    </>
  )
}
