import { useState } from 'react'

import { graphql } from '../api'
import { Tag } from '../components/common'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { TrashIcon } from '../components/icons'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../components/settingsList'
import {
  LIST_SKILL_SHARE_CALLS,
  PaymentCard,
  SKILL_SHARE_FIELDS,
  formatMinor,
  limitsLine,
  parseMinor,
  type SkillShare,
  type SkillShareCall,
} from '../components/skillShare'
import { useToast } from '../components/toast'
import { Tooltip } from '../components/tooltip'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'

// Sharing a skill: the person's own skills, lent to somebody in a circle they
// share, each use run on their own computer as them and held to the limits they
// set. docs/decisions/20261016-a-payment-skill-is-shared-call-by-call.md says
// why, and docs/subsystems/skills.md ("Sharing a skill") how. The card is on
// the agent page's Connections tab, beside the values the skills ask for and
// the computers they run on; the tools it lends are ones the operator installed.

type SkillTool = {
  name: string
  description: string
  needsComputer: boolean
  isAmountDeclared: boolean
  isScoped: boolean
}

type InstalledSkill = {
  name: string
  description: string
  enabled: boolean
  readable: boolean
  tools: SkillTool[]
}

type Person = { userId: string; username: string; name: string }

const OVERVIEW = `query {
  ListSkillShares { ${SKILL_SHARE_FIELDS} }
  ListAgentSkills { name description enabled readable tools { name description needsComputer isAmountDeclared isScoped } }
  ListCirclePeople { userId username name }
}`

const SHARE_SKILL = `mutation ($skillName: String!, $toolNames: [String!]!, $granteeUserId: String!, $paymentLimitMinor: Int64, $monthlyLimitMinor: Int64, $currencyCode: String, $isApprovalRequired: Boolean) {
  ShareSkill(skillName: $skillName, toolNames: $toolNames, granteeUserId: $granteeUserId, paymentLimitMinor: $paymentLimitMinor, monthlyLimitMinor: $monthlyLimitMinor, currencyCode: $currencyCode, isApprovalRequired: $isApprovalRequired) { id }
}`

const UNSHARE_SKILL = `mutation ($shareId: String!) { UnshareSkill(shareId: $shareId) }`

// SkillSharesCard is the whole of it: the skills that can be lent, the shares
// the person made with the requests under each, and what others lent them.
export function SkillSharesCard() {
  const { t } = useTranslation()
  const toast = useToast()
  const overview = useQuery(
    () =>
      graphql<{ ListSkillShares: SkillShare[]; ListAgentSkills: InstalledSkill[]; ListCirclePeople: Person[] }>(
        OVERVIEW,
      ),
    [],
  )
  const [sharing, setSharing] = useState<InstalledSkill | null>(null)
  const [stopping, setStopping] = useState<SkillShare | null>(null)
  const [opened, setOpened] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)

  // Somebody who cannot use circles has nobody to lend to, and the question
  // is refused: no card at all rather than one that can only fail.
  if (overview.error || !overview.data) return null
  const shares = overview.data.ListSkillShares
  const mine = shares.filter((share) => share.isOwnedByCaller)
  const withMe = shares.filter((share) => !share.isOwnedByCaller)
  const people = overview.data.ListCirclePeople
  const lendable = overview.data.ListAgentSkills.filter(
    (skill) => skill.enabled && skill.readable && skill.tools.length > 0,
  )
  if (lendable.length === 0 && shares.length === 0) return null

  const toggle = (shareId: string) =>
    setOpened((before) => {
      const after = new Set(before)
      if (!after.delete(shareId)) after.add(shareId)
      return after
    })

  async function stop(share: SkillShare) {
    setBusy(true)
    try {
      await graphql(UNSHARE_SKILL, { shareId: share.id })
      toast.done(t('skillShare.stopped', { skill: share.skillName, name: share.granteeName || share.granteeUsername }))
      setStopping(null)
      await overview.reload()
    } catch (caught) {
      toast.failure(caught, t('skillShare.failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <SettingsSection card title={t('skillShare.title')} description={t('skillShare.intro')}>
        {people.length === 0 ? <SettingsEmpty>{t('skillShare.noPeople')}</SettingsEmpty> : null}
        {lendable.map((skill) => (
          <SettingsRow
            key={skill.name}
            title={skill.name}
            subtitle={[skill.description, skill.tools.map((tool) => tool.name).join(', ')].filter(Boolean).join(' · ')}
            actions={
              <button type="button" disabled={people.length === 0} onClick={() => setSharing(skill)}>
                {t('skillShare.share')}
              </button>
            }
          />
        ))}
      </SettingsSection>

      {mine.length > 0 ? (
        <SettingsSection card title={t('skillShare.byYou')} description={t('skillShare.byYouHint')}>
          {mine.map((share) => {
            const name = share.granteeName || share.granteeUsername
            return (
              <div key={share.id} className="skill-share">
                <SettingsRow
                  title={t('skillShare.rowTitle', { skill: share.skillName, name })}
                  avatar={name}
                  avatarUrl={share.granteeAvatarUrl}
                  avatarColorIndex={share.granteeAvatarColorIndex}
                  badge={
                    <>
                      {share.toolNames.map((tool) => (
                        <Tag key={tool} value={tool} />
                      ))}
                      {share.waitingCallCount > 0 ? (
                        <Tag tone="warn" value={t('skillShare.waiting', { count: share.waitingCallCount })} />
                      ) : null}
                    </>
                  }
                  subtitle={limitsAndMonth(share, t)}
                  actions={
                    <>
                      <button type="button" aria-expanded={opened.has(share.id)} onClick={() => toggle(share.id)}>
                        {t('skillShare.requests')}
                      </button>
                      <Tooltip label={t('skillShare.stop')}>
                        <button
                          type="button"
                          className="icon-action danger"
                          aria-label={`${share.skillName}, ${name}: ${t('skillShare.stop')}`}
                          disabled={busy}
                          onClick={() => setStopping(share)}
                        >
                          <TrashIcon size={16} />
                        </button>
                      </Tooltip>
                    </>
                  }
                />
                {opened.has(share.id) ? (
                  <ShareCalls share={share} isOwner onChanged={() => void overview.reload()} />
                ) : null}
              </div>
            )
          })}
        </SettingsSection>
      ) : null}

      {withMe.length > 0 ? (
        <SettingsSection card title={t('skillShare.withYou')} description={t('skillShare.withYouHint')}>
          {withMe.map((share) => {
            const name = share.ownerName || share.ownerUsername
            return (
              <div key={share.id} className="skill-share">
                <SettingsRow
                  title={t('skillShare.rowTitleFrom', { skill: share.skillName, name })}
                  avatar={name}
                  avatarUrl={share.ownerAvatarUrl}
                  avatarColorIndex={share.ownerAvatarColorIndex}
                  badge={
                    <>
                      {share.toolNames.map((tool) => (
                        <Tag key={tool} value={tool} />
                      ))}
                    </>
                  }
                  subtitle={limitsAndMonth(share, t)}
                  actions={
                    <button type="button" aria-expanded={opened.has(share.id)} onClick={() => toggle(share.id)}>
                      {t('skillShare.requests')}
                    </button>
                  }
                />
                {opened.has(share.id) ? <ShareCalls share={share} isOwner={false} /> : null}
              </div>
            )
          })}
        </SettingsSection>
      ) : null}

      {sharing ? (
        <ShareSkillDialog
          skill={sharing}
          people={people}
          onClose={() => setSharing(null)}
          onShared={() => {
            setSharing(null)
            void overview.reload()
          }}
        />
      ) : null}

      {stopping ? (
        <ConfirmDialog
          title={t('skillShare.stopTitle', {
            skill: stopping.skillName,
            name: stopping.granteeName || stopping.granteeUsername,
          })}
          body={t('skillShare.stopBody')}
          confirmLabel={t('skillShare.stop')}
          destructive
          busy={busy}
          onClose={() => setStopping(null)}
          onConfirm={() => void stop(stopping)}
        />
      ) : null}
    </>
  )
}

// limitsAndMonth is a share's limits, and what this month's payments through it
// have come to when it has a limit for the month.
function limitsAndMonth(share: SkillShare, t: ReturnType<typeof useTranslation>['t']): string {
  const parts = [limitsLine(share, t)]
  if (share.monthlyLimitMinor !== null && !share.isApprovalRequired)
    parts.push(t('skillShare.monthSoFar', { amount: formatMinor(share.monthTotalMinor, share.currencyCode) }))
  return parts.join(' · ')
}

// ShareCalls are the requests made through one share: the ones that wait first,
// with the buttons that answer them for the owner, then the rest as short rows.
function ShareCalls({ share, isOwner, onChanged }: { share: SkillShare; isOwner: boolean; onChanged?: () => void }) {
  const { t } = useTranslation()
  const calls = useQuery(
    () =>
      graphql<{ ListSkillShareCalls: SkillShareCall[] }>(LIST_SKILL_SHARE_CALLS, {
        shareId: share.id,
        callState: null,
      }),
    [share.id],
  )
  if (!calls.data) return <p className="muted skill-share-calls">{t('payment.loading')}</p>
  const rows = calls.data.ListSkillShareCalls
  if (rows.length === 0) return <SettingsEmpty>{t('skillShare.noRequests')}</SettingsEmpty>
  return (
    <div className="skill-share-calls">
      {rows.map((call) =>
        call.callState === 'waiting_for_approval' ? (
          <PaymentCard
            key={call.id}
            call={call}
            isOwner={isOwner}
            onAnswered={() => {
              void calls.reload()
              onChanged?.()
            }}
          />
        ) : (
          <SettingsRow
            key={call.id}
            title={call.toolName}
            badge={
              <Tag
                value={t(`skillShare.callState.${call.callState}` as Parameters<typeof t>[0])}
                tone={call.callState === 'ran' ? 'good' : 'bad'}
              />
            }
            subtitle={[
              call.amountMinor !== null ? formatMinor(call.amountMinor, call.currencyCode) : '',
              new Date(call.createdAt).toLocaleString(),
              call.requestReason,
            ]
              .filter(Boolean)
              .join(' · ')}
          />
        ),
      )}
    </div>
  )
}

// ShareSkillDialog lends tools of one skill to one person: which tools, how
// much one payment may be, how much a month, and whether to be asked every
// time. A tool that pays is preselected, since lending it is why anybody comes
// here, and is not lent without a limit or the question every time.
function ShareSkillDialog({
  skill,
  people,
  onClose,
  onShared,
}: {
  skill: InstalledSkill
  people: Person[]
  onClose: () => void
  onShared: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [personId, setPersonId] = useState('')
  const [chosen, setChosen] = useState<Set<string>>(
    () => new Set(skill.tools.filter((tool) => tool.isAmountDeclared).map((tool) => tool.name)),
  )
  const [paymentLimit, setPaymentLimit] = useState('')
  const [monthlyLimit, setMonthlyLimit] = useState('')
  const [currency, setCurrency] = useState('')
  const [isAskedEveryTime, setIsAskedEveryTime] = useState(false)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  const chosenTools = skill.tools.filter((tool) => chosen.has(tool.name))
  const pays = chosenTools.some((tool) => tool.isAmountDeclared)
  const code = currency.trim().toLowerCase()
  const hasLimit = paymentLimit.trim() !== '' || monthlyLimit.trim() !== ''
  const isCurrencyValid = /^[a-z]{3}$/.test(code)
  const paymentMinor = paymentLimit.trim() === '' ? null : parseMinor(paymentLimit, code)
  const monthlyMinor = monthlyLimit.trim() === '' ? null : parseMinor(monthlyLimit, code)
  const isLimitReadable =
    (paymentLimit.trim() === '' || paymentMinor !== null) && (monthlyLimit.trim() === '' || monthlyMinor !== null)
  const canSubmit =
    personId !== '' &&
    chosen.size > 0 &&
    (!pays || isAskedEveryTime || hasLimit) &&
    (!hasLimit || isCurrencyValid) &&
    isLimitReadable

  async function submit() {
    const person = people.find((each) => each.userId === personId)
    setBusy(true)
    setProblem(null)
    try {
      await graphql(SHARE_SKILL, {
        skillName: skill.name,
        toolNames: [...chosen],
        granteeUserId: personId,
        paymentLimitMinor: isAskedEveryTime ? null : paymentMinor,
        monthlyLimitMinor: isAskedEveryTime ? null : monthlyMinor,
        currencyCode: hasLimit && !isAskedEveryTime ? code : null,
        isApprovalRequired: isAskedEveryTime,
      })
      toast.done(t('skillShare.shared', { skill: skill.name, name: person?.name || person?.username || '' }))
      onShared()
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : String(caught))
      toast.failure(caught, t('skillShare.failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <FormDialog
      title={t('skillShare.shareTitle', { skill: skill.name })}
      submitLabel={t('skillShare.share')}
      busy={busy}
      error={problem}
      canSubmit={canSubmit}
      onClose={onClose}
      onSubmit={() => void submit()}
    >
      <p className="muted">{t('skillShare.shareIntro')}</p>
      <label>
        {t('skillShare.with')}
        <select value={personId} onChange={(event) => setPersonId(event.target.value)}>
          <option value="">{t('skillShare.choose')}</option>
          {people.map((person) => (
            <option key={person.userId} value={person.userId}>
              {person.name || person.username}
            </option>
          ))}
        </select>
      </label>
      <fieldset className="skill-share-tools">
        <legend>{t('skillShare.tools')}</legend>
        {skill.tools.map((tool) => (
          <label key={tool.name} className="checkbox">
            <input
              type="checkbox"
              checked={chosen.has(tool.name)}
              onChange={(event) =>
                setChosen((before) => {
                  const after = new Set(before)
                  if (event.target.checked) after.add(tool.name)
                  else after.delete(tool.name)
                  return after
                })
              }
            />
            <span>
              {tool.name}
              <span className="muted">
                {' · '}
                {tool.isAmountDeclared
                  ? t('skillShare.toolPays')
                  : tool.isScoped
                    ? t('skillShare.toolScoped')
                    : t('skillShare.toolAlwaysAsks')}
              </span>
            </span>
          </label>
        ))}
      </fieldset>
      <label className="checkbox">
        <input
          type="checkbox"
          checked={isAskedEveryTime}
          onChange={(event) => setIsAskedEveryTime(event.target.checked)}
        />
        <span>
          {t('skillShare.askEveryTime')}
          <span className="muted"> · {t('skillShare.askEveryTimeHint')}</span>
        </span>
      </label>
      {!isAskedEveryTime ? (
        <>
          <label>
            {t('skillShare.paymentLimit')}
            <input
              inputMode="decimal"
              value={paymentLimit}
              placeholder="100.00"
              aria-invalid={!isLimitReadable && paymentMinor === null && paymentLimit.trim() !== ''}
              onChange={(event) => setPaymentLimit(event.target.value)}
            />
          </label>
          <label>
            {t('skillShare.monthlyLimit')}
            <input
              inputMode="decimal"
              value={monthlyLimit}
              placeholder="400.00"
              aria-invalid={!isLimitReadable && monthlyMinor === null && monthlyLimit.trim() !== ''}
              onChange={(event) => setMonthlyLimit(event.target.value)}
            />
          </label>
          <label>
            {t('skillShare.currency')}
            <input
              value={currency}
              maxLength={3}
              placeholder="usd"
              autoCapitalize="none"
              onChange={(event) => setCurrency(event.target.value)}
            />
            <span className="muted">{t('skillShare.currencyHint')}</span>
          </label>
          <p className="muted">{pays && !hasLimit ? t('skillShare.needsBound') : t('skillShare.limitsHint')}</p>
        </>
      ) : null}
    </FormDialog>
  )
}
