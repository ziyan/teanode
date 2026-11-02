import { useState } from 'react'

import { failureText, graphql } from '../../api'
import { ErrorMessage, Loading, Tag } from '../../components/common'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { PencilIcon, TrashIcon } from '../../components/icons'
import { personName } from '../../components/personLabel'
import { Select } from '../../components/select'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { Key, useTranslation } from '../../i18n/i18n'
import {
  ALL_FINANCE_SHARE_LEVELS,
  AllFinanceShare,
  AllFinanceShareLevel,
  LIST_FINANCE_SHARES,
  SHARE_FINANCE,
  UNSHARE_FINANCE,
} from './financeShareApi'
import { ShareChoices, useGranteeOptions } from './financeShareChoices'

// The "Share all your finance" panel of the Sharing section: a share that
// covers every account the person holds now and links later, at the spending
// level or the transactions level, and shows the reader one combined view
// with no account names or balances. Starting or widening one is the
// person's own tap, which the dashboard is; the agent has no part in it.
// Per-account shares are the panel under this one.

function granteeName(share: AllFinanceShare, everyoneIn: (name: string) => string): string {
  if (share.granteeCircleId) return everyoneIn(share.granteeCircleName)
  return share.granteeName || share.granteeUsername
}

export function allLevelName(t: (key: Key) => string, level: string): string {
  return t(`finance.shareLevelName.${level}` as Key)
}

export function FinanceShareAllSection({
  canUseCircles,
  choices,
  choicesError,
}: {
  canUseCircles: boolean
  choices: ShareChoices | null | undefined
  choicesError?: unknown
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const shares = useQuery(
    async () =>
      (await graphql<{ ListFinanceShares: AllFinanceShare[] | null }>(LIST_FINANCE_SHARES)).ListFinanceShares ?? [],
    [],
    { refresh: false },
  )
  // The dialog: closed, or open to share anew, or to change the level of a
  // share that is there.
  const [dialog, setDialog] = useState<{ share: AllFinanceShare | null } | null>(null)
  const [isBusy, setIsBusy] = useState(false)
  const [stopping, setStopping] = useState<AllFinanceShare | null>(null)
  const [stopProblem, setStopProblem] = useState<string | null>(null)
  const everyoneIn = (name: string) => t('finance.shareEveryoneIn', { name })
  const current = shares.data ?? []

  async function stop(share: AllFinanceShare) {
    const name = granteeName(share, everyoneIn)
    setIsBusy(true)
    setStopProblem(null)
    try {
      await graphql(UNSHARE_FINANCE, { financeShareId: share.id })
      toast.done(t('sharing.stopped', { name }))
      setStopping(null)
      await shares.reload()
    } catch (failure) {
      setStopProblem(failureText(failure, t('finance.failed')))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <SettingsSection
      card
      title={t('finance.shareAllTitle')}
      description={t('finance.shareAllIntro')}
      action={
        canUseCircles ? (
          <button type="button" onClick={() => setDialog({ share: null })} disabled={!choices}>
            {t('finance.shareAllAction')}
          </button>
        ) : undefined
      }
    >
      <ErrorMessage error={shares.error ?? choicesError} />
      {shares.loading && !shares.data ? <Loading /> : null}
      {shares.data && current.length === 0 ? <SettingsEmpty>{t('finance.shareAllNobody')}</SettingsEmpty> : null}
      {current.map((share) => {
        const name = granteeName(share, everyoneIn)
        return (
          <SettingsRow
            key={share.id}
            title={name}
            subtitle={t('finance.shareAllCovers')}
            badge={<Tag value={allLevelName(t, share.financeShareLevel)} />}
            actions={
              <div className="row-actions">
                {canUseCircles ? (
                  <Tooltip label={t('finance.shareAllChange', { name })}>
                    <button
                      type="button"
                      className="icon-action"
                      aria-label={t('finance.shareAllChange', { name })}
                      disabled={isBusy}
                      onClick={() => setDialog({ share })}
                    >
                      <PencilIcon size={16} />
                    </button>
                  </Tooltip>
                ) : null}
                <Tooltip label={t('finance.shareAllStop', { name })}>
                  <button
                    type="button"
                    className="icon-action danger"
                    aria-label={t('finance.shareAllStop', { name })}
                    disabled={isBusy}
                    onClick={() => {
                      setStopProblem(null)
                      setStopping(share)
                    }}
                  >
                    <TrashIcon size={16} />
                  </button>
                </Tooltip>
              </div>
            }
          />
        )
      })}
      {stopping ? (
        <ConfirmDialog
          title={t('sharing.stopTitle', { name: granteeName(stopping, everyoneIn) })}
          body={t('sharing.stopBody', {
            name: granteeName(stopping, everyoneIn),
            what: t('finance.shareAllWhat'),
          })}
          confirmLabel={t('sharing.stop')}
          busy={isBusy}
          error={stopProblem}
          onConfirm={() => void stop(stopping)}
          onClose={() => setStopping(null)}
        />
      ) : null}
      {dialog && choices ? (
        <FinanceShareAllDialog
          choices={choices}
          share={dialog.share}
          onClose={() => setDialog(null)}
          onSaved={async () => {
            setDialog(null)
            await shares.reload()
          }}
        />
      ) : null}
    </SettingsSection>
  )
}

// FinanceShareAllDialog shares all of the person's finance with a person or a
// circle at a level, or changes the level of a share that is there, where who
// it is with is fixed.
function FinanceShareAllDialog({
  choices,
  share,
  onClose,
  onSaved,
}: {
  choices: ShareChoices
  share: AllFinanceShare | null
  onClose: () => void
  onSaved: () => Promise<void>
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [grantee, setGrantee] = useState(
    share ? (share.granteeCircleId ? `circle:${share.granteeCircleId}` : `user:${share.granteeUserId ?? ''}`) : '',
  )
  const [financeShareLevel, setFinanceShareLevel] = useState<AllFinanceShareLevel>(
    share?.financeShareLevel ?? 'spending',
  )
  const [isBusy, setIsBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const granteeOptions = useGranteeOptions(choices.people, choices.circles)

  async function save() {
    const [kind, id] = grantee.split(':')
    if (!id) return
    const person = choices.people.find((candidate) => candidate.userId === id)
    const name =
      kind === 'circle'
        ? t('finance.shareEveryoneIn', { name: choices.circles.find((candidate) => candidate.id === id)?.name ?? '' })
        : person
          ? personName(person)
          : (share?.granteeName ?? '')
    setIsBusy(true)
    setProblem(null)
    try {
      await graphql(SHARE_FINANCE, {
        granteeUserId: kind === 'user' ? id : null,
        granteeCircleId: kind === 'circle' ? id : null,
        financeShareLevel,
      })
      toast.done(t('finance.shareAllSaved', { name }))
      await onSaved()
    } catch (failure) {
      // The dialog is open, so the failure is said in it, once, and the dialog
      // stays up with what was chosen.
      setProblem(failureText(failure, t('finance.failed')))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <FormDialog
      title={share ? t('finance.shareChangeTitle') : t('finance.shareAllDialogTitle')}
      submitLabel={share ? t('common.save') : t('finance.shareAdd')}
      busy={isBusy}
      error={problem}
      canSubmit={grantee !== ''}
      onClose={onClose}
      onSubmit={() => void save()}
    >
      <label>
        <span>{t('finance.shareWith')}</span>
        <Select
          block
          value={grantee}
          label={t('finance.shareWith')}
          placeholder={t('finance.shareChoose')}
          disabled={share !== null}
          options={granteeOptions}
          onChange={setGrantee}
        />
      </label>
      <fieldset className="check-list">
        <legend>{t('finance.shareLevelLabel')}</legend>
        <div className="check-list-items" role="radiogroup">
          {ALL_FINANCE_SHARE_LEVELS.map((level) => (
            <label key={level} className={financeShareLevel === level ? 'check-list-item chosen' : 'check-list-item'}>
              <input
                type="radio"
                name="finance-share-all-level"
                checked={financeShareLevel === level}
                onChange={() => setFinanceShareLevel(level)}
              />
              <span>
                {allLevelName(t, level)}
                <span>{t(`finance.shareAllLevelHint.${level}` as Key)}</span>
              </span>
            </label>
          ))}
        </div>
      </fieldset>
      <p className="muted">{t('finance.shareNever')}</p>
    </FormDialog>
  )
}
