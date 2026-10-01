import { useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag, formatMoney } from '../../components/common'
import { MeterBar } from '../../components/budgetBar'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { CheckIcon, PencilIcon, RestartIcon } from '../../components/icons'
import { Select } from '../../components/select'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import {
  ASSETS,
  Asset,
  CLOSE_SAVINGS_TARGET,
  CREATE_SAVINGS_TARGET,
  SAVINGS_TARGETS,
  SavingsTarget,
  SavingsTargetView,
  UPDATE_SAVINGS_TARGET,
  amountOf,
  formatDay,
  isDecimal,
  personToday,
} from './financeApi'
import { CurrencyPicker, UnconvertedNote, useAct, useFinanceWords, useReportingCurrency } from './financeCommon'

// The Savings targets section: an amount to save by a day, measured by
// money not spent or by what chosen assets are worth, each with how far
// along it is and what it needs a month from now on to get there.
export function FinanceSavingsTargetsSection() {
  const { t } = useTranslation()
  const words = useFinanceWords()
  const targets = useQuery(() => graphql<{ SavingsTargets: SavingsTargetView[] }>(SAVINGS_TARGETS), [], {
    refresh: false,
  })
  const { busy, run } = useAct(targets.reload)
  const [editing, setEditing] = useState<SavingsTarget | 'new' | null>(null)
  const [closing, setClosing] = useState<SavingsTarget | null>(null)
  const [isClosedShown, setIsClosedShown] = useState(false)
  // Open ones, unless the closed ones are asked for: a closed savings
  // target is done with, until it is opened again.
  const all = targets.data?.SavingsTargets ?? []
  const list = all.filter((view) => isClosedShown || !view.savingsTarget.closedOn)

  return (
    <SettingsSection
      card
      title={t('finance.savingsTargetsTitle')}
      description={t('finance.savingsTargetsHint')}
      action={
        <button type="button" className="primary" onClick={() => setEditing('new')}>
          {t('finance.addSavingsTarget')}
        </button>
      }
    >
      <ErrorMessage error={targets.error} />
      {targets.loading && !targets.data ? <Loading /> : null}
      {targets.data && list.length === 0 ? <SettingsEmpty>{t('finance.noSavingsTargets')}</SettingsEmpty> : null}
      {list.map(({ savingsTarget: target, savingsTargetProgress: progressOf }) => {
        const goal = amountOf(target.targetAmount)
        const progress = amountOf(progressOf?.savedAmount)
        const said = t('finance.savedOfTarget', {
          saved: formatMoney(progress, target.currencyCode),
          target: formatMoney(goal, target.currencyCode),
        })
        return (
          <SettingsRow
            key={target.id}
            title={target.savingsTargetName}
            badge={
              <>
                <Tag value={words.targetMeasure(target.targetMeasure)} />
                {target.closedOn ? <Tag value={t('finance.closed')} /> : null}
                {progressOf?.isBehind && !target.closedOn ? <Tag value={t('finance.behind')} tone="warn" /> : null}
              </>
            }
            subtitle={
              <div className="finance-target">
                <div className="finance-budget-row-head">
                  <span>{said}</span>
                  <span className="finance-budget-row-said">
                    {t('finance.byDay', { day: formatDay(target.targetOn) })}
                  </span>
                </div>
                <MeterBar
                  fraction={goal > 0 ? progress / goal : 0}
                  tone={progressOf?.isBehind ? 'warn' : 'good'}
                  label={said}
                />
                {progressOf && amountOf(progressOf.remainingAmount) > 0 ? (
                  <span>
                    {t('finance.paceNeeded', {
                      amount: formatMoney(amountOf(progressOf.requiredMonthlyAmount), target.currencyCode),
                    })}
                  </span>
                ) : null}
                <UnconvertedNote currencyCodes={progressOf?.unconvertedCurrencyCodes} />
              </div>
            }
            actions={
              target.closedOn ? (
                <Tooltip label={t('finance.reopen')}>
                  <button
                    type="button"
                    className="icon-action"
                    disabled={busy}
                    aria-label={`${target.savingsTargetName}: ${t('finance.reopen')}`}
                    onClick={() =>
                      void run(
                        CLOSE_SAVINGS_TARGET,
                        { savingsTargetId: target.id, shouldReopen: true },
                        t('finance.savingsTargetReopened'),
                      )
                    }
                  >
                    <RestartIcon size={16} />
                  </button>
                </Tooltip>
              ) : (
              <div className="row-actions">
                <Tooltip label={t('common.edit')}>
                  <button
                    type="button"
                    className="icon-action"
                    aria-label={`${target.savingsTargetName}: ${t('common.edit')}`}
                    onClick={() => setEditing(target)}
                  >
                    <PencilIcon size={16} />
                  </button>
                </Tooltip>
                <Tooltip label={t('finance.closeSavingsTarget')}>
                  <button
                    type="button"
                    className="icon-action"
                    aria-label={`${target.savingsTargetName}: ${t('finance.closeSavingsTarget')}`}
                    onClick={() => setClosing(target)}
                  >
                    <CheckIcon size={16} />
                  </button>
                </Tooltip>
              </div>
              )
            }
          />
        )
      })}
      {all.some((view) => view.savingsTarget.closedOn) ? (
        <label className="checkbox">
          <input type="checkbox" checked={isClosedShown} onChange={(event) => setIsClosedShown(event.target.checked)} />
          {t('finance.showClosedSavingsTargets')}
        </label>
      ) : null}
      {editing ? (
        <SavingsTargetDialog
          target={editing === 'new' ? undefined : editing}
          onClose={() => setEditing(null)}
          onSaved={targets.reload}
        />
      ) : null}
      {closing ? (
        <ConfirmDialog
          title={t('finance.closeSavingsTarget')}
          body={t('finance.closeSavingsTargetBody', { name: closing.savingsTargetName })}
          confirmLabel={t('finance.closeSavingsTarget')}
          destructive={false}
          busy={busy}
          onClose={() => setClosing(null)}
          onConfirm={() => {
            void run(CLOSE_SAVINGS_TARGET, { savingsTargetId: closing.id }, t('finance.savingsTargetClosed')).then(() =>
              setClosing(null),
            )
          }}
        />
      ) : null}
    </SettingsSection>
  )
}

function SavingsTargetDialog({
  target,
  onClose,
  onSaved,
}: {
  target?: SavingsTarget
  onClose: () => void
  onSaved: () => Promise<void>
}) {
  const { t } = useTranslation()
  const words = useFinanceWords()
  const { reportingCurrencyCode } = useReportingCurrency()
  const assets = useQuery(() => graphql<{ Assets: Asset[] }>(ASSETS), [], {
    refresh: false,
  })
  const { busy, act } = useAct(onSaved)
  const [name, setName] = useState(target?.savingsTargetName ?? '')
  const [targetAmount, setTargetAmount] = useState(target ? String(amountOf(target.targetAmount)) : '')
  const [currencyCode, setCurrencyCode] = useState(target?.currencyCode ?? '')
  const [targetOn, setTargetOn] = useState(target?.targetOn ?? '')
  const [targetMeasure, setTargetMeasure] = useState<'cash_flow' | 'asset_value'>(target?.targetMeasure ?? 'cash_flow')
  const [startingAmount, setStartingAmount] = useState(target?.startingAmount ?? '')
  const [startedOn, setStartedOn] = useState(target?.startedOn ?? personToday())
  const [assetIds, setAssetIds] = useState<string[]>(target?.assetIds ?? [])
  const currency = currencyCode || reportingCurrencyCode
  const assetList = (assets.data?.Assets ?? []).filter((asset) => !asset.isLiability && !asset.closedOn)

  const canSubmit =
    name.trim() !== '' &&
    isDecimal(targetAmount) &&
    amountOf(targetAmount) > 0 &&
    targetOn !== '' &&
    (startingAmount.trim() === '' || isDecimal(startingAmount)) &&
    (targetMeasure === 'cash_flow' || assetIds.length > 0)

  return (
    <FormDialog
      title={target ? t('finance.editSavingsTarget') : t('finance.addSavingsTarget')}
      submitLabel={target ? t('common.save') : t('common.create')}
      busy={busy}
      canSubmit={canSubmit}
      onClose={onClose}
      onSubmit={() => {
        const shared = {
          savingsTargetName: name.trim(),
          targetAmount: targetAmount.trim(),
          targetOn,
          startingAmount: startingAmount.trim() || undefined,
          assetIds: targetMeasure === 'asset_value' ? assetIds : [],
        }
        void act(
          () =>
            target
              ? graphql(UPDATE_SAVINGS_TARGET, { ...shared, savingsTargetId: target.id })
              : graphql(CREATE_SAVINGS_TARGET, {
                  ...shared,
                  currencyCode: currency || undefined,
                  targetMeasure,
                  startedOn,
                }),
          t('finance.savingsTargetSaved'),
        ).then((isDone) => {
          if (isDone) onClose()
        })
      }}
    >
      <label>
        <span>{t('finance.savingsTargetName')}</span>
        <input value={name} onChange={(event) => setName(event.target.value)} />
      </label>
      <div className="row">
        <label>
          <span>{t('finance.targetAmount')}</span>
          <input inputMode="decimal" value={targetAmount} onChange={(event) => setTargetAmount(event.target.value)} />
        </label>
        {!target ? (
          <label>
            <span>{t('finance.currency')}</span>
            <CurrencyPicker value={currency} label={t('finance.currency')} onChange={setCurrencyCode} />
          </label>
        ) : null}
      </div>
      <label>
        <span>{t('finance.targetOn')}</span>
        {/* No earliest day on a target that exists: one already past its
            day must still be editable, to move the day on. */}
        <input
          type="date"
          value={targetOn}
          min={target ? undefined : personToday()}
          onChange={(event) => setTargetOn(event.target.value)}
        />
      </label>
      {!target ? (
        <>
          <label>
            <span>{t('finance.targetMeasureLabel')}</span>
            <Select
              block
              value={targetMeasure}
              label={t('finance.targetMeasureLabel')}
              options={(['cash_flow', 'asset_value'] as const).map((value) => ({
                value,
                label: words.targetMeasure(value),
              }))}
              onChange={(value) => setTargetMeasure(value as 'cash_flow' | 'asset_value')}
            />
          </label>
          <p className="muted field-hint">
            {targetMeasure === 'cash_flow' ? t('finance.cashFlowMeasureHint') : t('finance.assetValueMeasureHint')}
          </p>
          <label>
            <span>{t('finance.startedOn')}</span>
            <input type="date" value={startedOn} onChange={(event) => setStartedOn(event.target.value)} />
          </label>
        </>
      ) : null}
      {targetMeasure === 'asset_value' ? (
        <>
          <label>
            <span>{t('finance.startingAmount')}</span>
            <input
              inputMode="decimal"
              value={startingAmount}
              onChange={(event) => setStartingAmount(event.target.value)}
            />
          </label>
          <fieldset className="finance-asset-choices">
            <legend>{t('finance.targetAssets')}</legend>
            {assetList.length === 0 ? <p className="muted">{t('finance.noAssets')}</p> : null}
            {assetList.map((asset) => (
              <label key={asset.id} className="checkbox">
                <input
                  type="checkbox"
                  checked={assetIds.includes(asset.id)}
                  onChange={(event) =>
                    setAssetIds((previous) =>
                      event.target.checked ? [...previous, asset.id] : previous.filter((id) => id !== asset.id),
                    )
                  }
                />
                {asset.assetName}
              </label>
            ))}
          </fieldset>
        </>
      ) : null}
    </FormDialog>
  )
}
