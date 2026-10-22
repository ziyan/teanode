import { useState } from 'react'
import { Link } from 'react-router-dom'

import { failureText, graphql } from '../../api'
import { ErrorMessage, Loading, Tag } from '../../components/common'
import { FormDialog } from '../../components/dialog'
import { CloseIcon, PencilIcon } from '../../components/icons'
import { personLabel, personName } from '../../components/personLabel'
import { Select } from '../../components/select'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { Key, useTranslation } from '../../i18n/i18n'
import { hasPermission, useSession } from '../../session'
import { FINANCE_ACCOUNTS, FinanceAccount } from './financeApi'
import { accountLabel } from './financeCommon'
import {
  FINANCE_SHARE_LEVELS,
  FinanceShare,
  FinanceShareLevel,
  LIST_FINANCE_ACCOUNT_SHARES,
  SHARE_FINANCE_ACCOUNT,
  UNSHARE_FINANCE_ACCOUNT,
} from './financeShareApi'

// The Sharing panel of the agent page's Finance tab: which of the person's
// finance accounts are shared with whom, and at what level. Sharing an
// account is the account's owner's alone, and starting a share or showing
// more of an account is the person's own tap, which the dashboard is. The
// agent has no part in it: it neither shares an account nor reads what is
// shared with the person. What each level shows is in docs/subsystems/finance.md.

type Person = { userId: string; username: string; name: string }
type CircleChoice = { id: string; name: string; members: { userId: string }[] }

const LIST_PEOPLE = `query { ListCirclePeople { userId username name } }`
const LIST_CIRCLES = `query { ListCircles { id name members { userId } } }`

// What a share is with, in a sentence and in a list.
function granteeName(share: FinanceShare, everyoneIn: (name: string) => string): string {
  if (share.granteeCircleId) return everyoneIn(share.granteeCircleName)
  return share.granteeName || share.granteeUsername
}

export function levelName(t: (key: Key) => string, level: string): string {
  return t(`finance.shareLevelName.${level}` as Key)
}

export function FinanceSharingSection() {
  const { t } = useTranslation()
  const toast = useToast()
  const session = useSession()
  // Who can be shared with is found through circles, which a person without
  // circle:use cannot read: they get no list and no failure, and the shares
  // they already have are shown as they are.
  const canUseCircles = hasPermission(session.permissions, 'circle:use')
  const shares = useQuery(
    async () =>
      (await graphql<{ ListFinanceAccountShares: FinanceShare[] }>(LIST_FINANCE_ACCOUNT_SHARES))
        .ListFinanceAccountShares,
    [],
    { refresh: false },
  )
  const accounts = useQuery(
    async () => (await graphql<{ FinanceAccounts: FinanceAccount[] }>(FINANCE_ACCOUNTS)).FinanceAccounts,
    [],
    { refresh: false },
  )
  const choices = useQuery(
    async () => {
      if (!canUseCircles) return { people: [] as Person[], circles: [] as CircleChoice[] }
      const [people, circles] = await Promise.all([
        graphql<{ ListCirclePeople: Person[] }>(LIST_PEOPLE),
        graphql<{ ListCircles: CircleChoice[] }>(LIST_CIRCLES),
      ])
      return { people: people.ListCirclePeople, circles: circles.ListCircles }
    },
    [canUseCircles],
    { refresh: false },
  )
  // The dialog: closed, or open to share an account anew, or to change the
  // level of a share that is there.
  const [dialog, setDialog] = useState<{ share: FinanceShare | null } | null>(null)
  const [isBusy, setIsBusy] = useState(false)
  const everyoneIn = (name: string) => t('finance.shareEveryoneIn', { name })

  const current = shares.data ?? []
  const accountList = accounts.data ?? []

  async function stop(share: FinanceShare) {
    const name = granteeName(share, everyoneIn)
    setIsBusy(true)
    try {
      await graphql(UNSHARE_FINANCE_ACCOUNT, { shareId: share.id })
      toast.done(t('finance.shareStopped', { account: accountLabel(share), name }))
      await shares.reload()
    } catch (failure) {
      toast.failure(failure, t('finance.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <SettingsSection
      card
      title={t('finance.shareTitle')}
      description={t('finance.shareIntro')}
      action={
        accountList.length > 0 && canUseCircles ? (
          <button type="button" onClick={() => setDialog({ share: null })}>
            {t('finance.shareAction')}
          </button>
        ) : undefined
      }
    >
      <ErrorMessage error={shares.error ?? accounts.error ?? choices.error} />
      {(shares.loading && !shares.data) || (accounts.loading && !accounts.data) ? <Loading /> : null}
      {shares.data && accounts.data && accountList.length === 0 ? (
        <SettingsEmpty>{t('finance.shareNoAccounts')}</SettingsEmpty>
      ) : null}
      {shares.data && accountList.length > 0 && current.length === 0 ? (
        <SettingsEmpty>{t('finance.shareNobody')}</SettingsEmpty>
      ) : null}
      {current.map((share) => {
        const name = granteeName(share, everyoneIn)
        const account = accountLabel(share)
        return (
          <SettingsRow
            key={share.id}
            title={account}
            subtitle={t('finance.shareWithName', { name })}
            badge={<Tag value={levelName(t, share.financeShareLevel)} />}
            actions={
              <div className="row-actions">
                {canUseCircles ? (
                  <Tooltip label={t('finance.shareChange', { account, name })}>
                    <button
                      type="button"
                      className="icon-action"
                      aria-label={t('finance.shareChange', { account, name })}
                      disabled={isBusy}
                      onClick={() => setDialog({ share })}
                    >
                      <PencilIcon size={16} />
                    </button>
                  </Tooltip>
                ) : null}
                <Tooltip label={t('finance.shareStop', { account, name })}>
                  <button
                    type="button"
                    className="icon-action danger"
                    aria-label={t('finance.shareStop', { account, name })}
                    disabled={isBusy}
                    onClick={() => void stop(share)}
                  >
                    <CloseIcon size={16} />
                  </button>
                </Tooltip>
              </div>
            }
          />
        )
      })}
      {!canUseCircles && accountList.length > 0 ? <p className="muted">{t('finance.shareNeedsCircles')}</p> : null}
      {choices.data &&
      accountList.length > 0 &&
      choices.data.people.length === 0 &&
      choices.data.circles.length === 0 ? (
        <>
          <p className="muted">{t('finance.shareNoPeople')}</p>
          <div className="page-actions">
            <Link className="button" to="/circles">
              {t('circles.open')}
            </Link>
          </div>
        </>
      ) : null}
      {dialog && choices.data ? (
        <FinanceShareDialog
          accounts={accountList}
          people={choices.data.people}
          circles={choices.data.circles}
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

// FinanceShareDialog shares an account with a person or a circle at a level,
// or changes the level of a share that is there, where the account and who it
// is with are fixed.
function FinanceShareDialog({
  accounts,
  people,
  circles,
  share,
  onClose,
  onSaved,
}: {
  accounts: FinanceAccount[]
  people: Person[]
  circles: CircleChoice[]
  share: FinanceShare | null
  onClose: () => void
  onSaved: () => Promise<void>
}) {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const [financeAccountId, setFinanceAccountId] = useState(share?.financeAccountId ?? '')
  const [grantee, setGrantee] = useState(
    share ? (share.granteeCircleId ? `circle:${share.granteeCircleId}` : `user:${share.granteeUserId ?? ''}`) : '',
  )
  const [financeShareLevel, setFinanceShareLevel] = useState<FinanceShareLevel>(share?.financeShareLevel ?? 'balance')
  const [isBusy, setIsBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  const everyoneIn = (name: string) => t('finance.shareEveryoneIn', { name })
  // A circle also says how many people it is, so two circles with one name
  // can be told apart, as they are on the command line.
  const circleOption = (circle: CircleChoice) =>
    t('finance.shareCircleOption', {
      name: circle.name,
      people: plural(
        circle.members.length,
        { one: 'circles.memberCountOne', other: 'circles.memberCount' },
        { count: circle.members.length },
      ),
    })
  const granteeOptions = [
    ...people.map((person) => ({
      value: `user:${person.userId}`,
      label: personLabel(person),
      group: t('finance.sharePeople'),
    })),
    ...circles.map((circle) => ({
      value: `circle:${circle.id}`,
      label: circleOption(circle),
      group: t('finance.shareCircles'),
    })),
  ]

  async function save() {
    const [kind, id] = grantee.split(':')
    if (!financeAccountId || !id) return
    const account = accounts.find((candidate) => candidate.id === financeAccountId)
    const person = people.find((candidate) => candidate.userId === id)
    const name =
      kind === 'circle'
        ? everyoneIn(circles.find((candidate) => candidate.id === id)?.name ?? '')
        : person
          ? personName(person)
          : (share?.granteeName ?? '')
    setIsBusy(true)
    setProblem(null)
    try {
      await graphql(SHARE_FINANCE_ACCOUNT, {
        financeAccountId,
        granteeUserId: kind === 'user' ? id : null,
        granteeCircleId: kind === 'circle' ? id : null,
        financeShareLevel,
      })
      toast.done(t('finance.shareSaved', { account: account ? accountLabel(account) : '', name }))
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
      title={share ? t('finance.shareChangeTitle') : t('finance.shareDialogTitle')}
      submitLabel={share ? t('common.save') : t('finance.shareAdd')}
      busy={isBusy}
      error={problem}
      canSubmit={financeAccountId !== '' && grantee !== ''}
      onClose={onClose}
      onSubmit={() => void save()}
    >
      <label>
        <span>{t('finance.shareAccount')}</span>
        <Select
          block
          value={financeAccountId}
          label={t('finance.shareAccount')}
          placeholder={t('finance.shareAccountChoose')}
          disabled={share !== null}
          options={accounts.map((account) => ({ value: account.id, label: accountLabel(account) }))}
          onChange={setFinanceAccountId}
        />
      </label>
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
      <label>
        <span>{t('finance.shareLevelLabel')}</span>
        <Select
          block
          value={financeShareLevel}
          label={t('finance.shareLevelLabel')}
          options={FINANCE_SHARE_LEVELS.map((level) => ({ value: level, label: levelName(t, level) }))}
          onChange={(next) => setFinanceShareLevel(next as FinanceShareLevel)}
        />
        <span className="muted">{t(`finance.shareLevelHint.${financeShareLevel}` as Key)}</span>
      </label>
      <p className="muted">{t('finance.shareNever')}</p>
    </FormDialog>
  )
}
