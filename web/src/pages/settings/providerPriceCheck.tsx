import { useEffect, useState } from 'react'

import { graphql } from '../../api'
import { FormDialog } from '../../components/dialog'
import { formatRelative, hasTime } from '../../components/relativeTime'
import { useToast } from '../../components/toast'
import { Key, useTranslation } from '../../i18n/i18n'

// The model prices of a provider with a billing key, checked against what
// the provider billed: the line a provider's row shows, the button that
// checks now, and the table of what was compared. The server checks once a
// day on its own; this is the same check, asked for.

export type BilledPrice = {
  model: string
  tokenKind: string
  configuredPerMillion: number
  billedPerMillion: number
  dayCount: number
  isPattern: boolean
}

export type PriceChange = {
  model: string
  tokenKind: string
  beforePerMillion: number
  afterPerMillion: number
}

export type BillingCheck = {
  providerName: string
  checkedAt: string
  errorMessage: string
  compared: BilledPrice[]
  changed: PriceChange[]
}

const CHECK_FIELDS = `providerName checkedAt errorMessage
  compared { model tokenKind configuredPerMillion billedPerMillion dayCount isPattern }
  changed { model tokenKind beforePerMillion afterPerMillion }`
const LIST_CHECKS = `{ AgentBillingChecks { ${CHECK_FIELDS} } }`
const RUN_CHECK = `mutation { CheckAgentProviderPrices { ${CHECK_FIELDS} } }`

// useBillingChecks reads the last check of every provider that has a
// billing key. It reads nothing when no provider has one, so a page with
// none asks the server nothing. A failure is a toast, like every other
// failure on the settings page.
export function useBillingChecks(hasBillingKey: boolean) {
  const toast = useToast()
  const [checks, setChecks] = useState<Record<string, BillingCheck>>({})
  useEffect(() => {
    if (!hasBillingKey) return
    let isCurrent = true
    graphql<{ AgentBillingChecks: BillingCheck[] }>(LIST_CHECKS)
      .then((data) => {
        if (isCurrent) setChecks(byProvider(data.AgentBillingChecks))
      })
      .catch((caught: unknown) => {
        if (isCurrent) toast.failed(caught instanceof Error ? caught.message : String(caught))
      })
    return () => {
      isCurrent = false
    }
    // The toast is not a reason to read the checks again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hasBillingKey])
  return { checks, setChecks }
}

function byProvider(checks: BillingCheck[]): Record<string, BillingCheck> {
  return Object.fromEntries(checks.map((check) => [check.providerName, check]))
}

// runPriceCheck checks every provider with a billing key now. The server
// has no provider argument: one lock covers them all.
export async function runPriceCheck(): Promise<Record<string, BillingCheck>> {
  const data = await graphql<{ CheckAgentProviderPrices: BillingCheck[] }>(RUN_CHECK)
  return byProvider(data.CheckAgentProviderPrices)
}

const KIND_LABELS: Record<string, Key> = {
  input: 'agentSettings.priceKindInput',
  cachedInput: 'agentSettings.priceKindCachedInput',
  cacheWrite: 'agentSettings.priceKindCacheWrite',
  output: 'agentSettings.priceKindOutput',
  audioInput: 'agentSettings.priceKindAudioInput',
}

// PriceCheckLine says when the prices were last checked against the bill,
// or why the bill could not be read, in the danger tone.
export function PriceCheckLine({ check }: { check: BillingCheck | undefined }) {
  const { t, language } = useTranslation()
  if (!check || !hasTime(check.checkedAt)) {
    return <span>{t('agentSettings.priceCheckNever')}</span>
  }
  if (check.errorMessage) {
    return <span className="error">{check.errorMessage}</span>
  }
  return (
    <span>
      {t('agentSettings.priceCheckLine', {
        time: formatRelative(new Date(check.checkedAt), language),
        count: String(check.changed.length),
      })}
    </span>
  )
}

// PriceCheckDialog is what a check compared. Its one button checks again;
// closing it changes nothing.
export function PriceCheckDialog({
  check,
  busy,
  onCheck,
  onClose,
}: {
  check: BillingCheck
  busy: boolean
  onCheck: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const priceText = (price: number) => (price === 0 ? t('agentSettings.priceCheckNotSet') : String(price))
  const kindText = (tokenKind: string) => (KIND_LABELS[tokenKind] ? t(KIND_LABELS[tokenKind]) : tokenKind)
  return (
    <FormDialog
      title={t('agentSettings.priceCheckTitle', { name: check.providerName })}
      submitLabel={busy ? t('agentSettings.priceChecking') : t('agentSettings.priceCheckNow')}
      closeLabel={t('common.close')}
      busy={busy}
      wide
      onClose={onClose}
      onSubmit={onCheck}
    >
      <p className="muted">
        <PriceCheckLine check={check} />
      </p>
      {check.compared.length === 0 ? (
        <p className="muted">{t('agentSettings.priceCheckEmpty')}</p>
      ) : (
        <div className="table-wrap">
          <table className="numbers-table price-check-table">
            <thead>
              <tr>
                <th>{t('agentSettings.pricingModel')}</th>
                <th className="numeric">{t('agentSettings.priceCheckConfigured')}</th>
                <th className="numeric">{t('agentSettings.priceCheckBilled')}</th>
                <th className="numeric">{t('agentSettings.priceCheckDays')}</th>
              </tr>
            </thead>
            <tbody>
              {check.compared.map((compared) => (
                <tr key={`${compared.model}/${compared.tokenKind}`}>
                  {/* The kind of token under the model, as the iPhone shows it:
                      a column of its own made the table too wide for the
                      dialog on a desktop. */}
                  <td>
                    {compared.model}
                    <small className="muted price-check-kind">
                      {kindText(compared.tokenKind)}
                      {compared.isPattern ? ` · ${t('agentSettings.priceCheckPattern')}` : null}
                    </small>
                  </td>
                  <td className="numeric">{priceText(compared.configuredPerMillion)}</td>
                  <td className="numeric">{priceText(compared.billedPerMillion)}</td>
                  <td className="numeric">{compared.dayCount}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <p className="muted table-note">{t('agentSettings.priceCheckPerMillion')}</p>
      {check.changed.length > 0 ? (
        <ul>
          {check.changed.map((changed) => (
            <li key={`${changed.model}/${changed.tokenKind}`}>
              {t('agentSettings.priceCheckChange', {
                model: changed.model,
                kind: kindText(changed.tokenKind),
                before: priceText(changed.beforePerMillion),
                after: priceText(changed.afterPerMillion),
              })}
            </li>
          ))}
        </ul>
      ) : null}
    </FormDialog>
  )
}
