import { useState } from 'react'
import { Link } from 'react-router-dom'

import { failureText, graphql, isNotFound } from '../api'
import { Tag } from '../components/common'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { AddPersonIcon, ListIcon, TrashIcon } from '../components/icons'
import { personLabel, personName } from '../components/personLabel'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../components/settingsList'
import {
  LIST_ENDED_SKILL_SHARE_CALLS,
  LIST_SKILL_SHARE_CALLS,
  SkillShareCallCard,
  SKILL_SHARE_FIELDS,
  formatCallTime,
  formatMinor,
  limitsLine,
  parseMinor,
  typedExample,
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
  const [stopProblem, setStopProblem] = useState<string | null>(null)

  // Somebody who cannot use circles has nobody to lend to, and the question
  // is refused as not found: no card at all rather than one that can only
  // fail. Any other failure is kept and shown with a way to try again, so
  // that a dropped connection is not read as "nothing is shared".
  if (overview.error && !overview.data) {
    if (isNotFound(overview.error)) return null
    return (
      <SettingsSection card title={t('skillShare.title')} description={t('skillShare.intro')}>
        <LoadFailed onRetry={() => void overview.reload()} />
      </SettingsSection>
    )
  }
  if (!overview.data) return null
  const shares = overview.data.ListSkillShares
  const mine = shares.filter((share) => share.isOwnedByCaller)
  const withMe = shares.filter((share) => !share.isOwnedByCaller)
  const people = overview.data.ListCirclePeople
  const lendable = overview.data.ListAgentSkills.filter(
    (skill) => skill.enabled && skill.readable && skill.tools.length > 0,
  )
  if (lendable.length === 0 && shares.length === 0) return <EndedShares />

  const toggle = (shareId: string) =>
    setOpened((before) => {
      const after = new Set(before)
      if (!after.delete(shareId)) after.add(shareId)
      return after
    })

  async function stop(share: SkillShare) {
    setBusy(true)
    setStopProblem(null)
    try {
      await graphql(UNSHARE_SKILL, { shareId: share.id })
      toast.done(t('skillShare.stopped', { skill: share.skillName, name: share.granteeName || share.granteeUsername }))
      setStopping(null)
      await overview.reload()
    } catch (caught) {
      // The confirmation is open, so the failure is said in it.
      setStopProblem(failureText(caught, t('skillShare.stopFailed')))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <SettingsSection card title={t('skillShare.title')} description={t('skillShare.intro')}>
        {people.length === 0 ? (
          <SettingsEmpty>
            {t('skillShare.noPeople')} <Link to="/circles">{t('circles.open')}</Link>
          </SettingsEmpty>
        ) : null}
        {lendable.map((skill) => (
          <SettingsRow
            key={skill.name}
            title={skill.name}
            subtitle={[skill.description, skill.tools.map((tool) => tool.name).join(', ')].filter(Boolean).join(' · ')}
            actions={
              <Tooltip label={t('skillShare.share')}>
                <button
                  type="button"
                  className="icon-action"
                  aria-label={`${skill.name}: ${t('skillShare.share')}`}
                  disabled={people.length === 0}
                  onClick={() => setSharing(skill)}
                >
                  <AddPersonIcon size={16} />
                </button>
              </Tooltip>
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
                      <Tooltip label={t('skillShare.activity')}>
                        <button
                          type="button"
                          className="icon-action"
                          aria-label={`${share.skillName}, ${name}: ${t('skillShare.activity')}`}
                          aria-expanded={opened.has(share.id)}
                          onClick={() => toggle(share.id)}
                        >
                          <ListIcon size={16} />
                        </button>
                      </Tooltip>
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
                    <Tooltip label={t('skillShare.activity')}>
                      <button
                        type="button"
                        className="icon-action"
                        aria-label={`${share.skillName}, ${name}: ${t('skillShare.activity')}`}
                        aria-expanded={opened.has(share.id)}
                        onClick={() => toggle(share.id)}
                      >
                        <ListIcon size={16} />
                      </button>
                    </Tooltip>
                  }
                />
                {opened.has(share.id) ? <ShareCalls share={share} isOwner={false} /> : null}
              </div>
            )
          })}
        </SettingsSection>
      ) : null}

      <EndedShares />

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
          error={stopProblem}
          onClose={() => {
            setStopping(null)
            setStopProblem(null)
          }}
          onConfirm={() => void stop(stopping)}
        />
      ) : null}
    </>
  )
}

// LoadFailed says a read did not get through and offers to make it again. A
// failed read is not an empty answer: "nothing is shared" would be a claim.
function LoadFailed({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation()
  return (
    <SettingsEmpty>
      <span role="alert">{t('skillShare.loadFailed')}</span>{' '}
      <button type="button" onClick={onRetry}>
        {t('common.tryAgain')}
      </button>
    </SettingsEmpty>
  )
}

// EndedShares are the calls of shares that no longer exist: taken back, or
// ended when the two stopped sharing a circle. The owner keeps the record of
// what was asked of their money; the other person sees their own. Drawn only
// when there are any.
function EndedShares() {
  const { t } = useTranslation()
  const calls = useQuery(() => graphql<{ ListSkillShareCalls: SkillShareCall[] }>(LIST_ENDED_SKILL_SHARE_CALLS), [])
  const rows = calls.data?.ListSkillShareCalls ?? []
  if (calls.error && !calls.data) {
    return (
      <SettingsSection card title={t('skillShare.ended')} description={t('skillShare.endedHint')}>
        <LoadFailed onRetry={() => void calls.reload()} />
      </SettingsSection>
    )
  }
  if (rows.length === 0) return null
  return (
    <SettingsSection card title={t('skillShare.ended')} description={t('skillShare.endedHint')}>
      {rows.map((call) => (
        <SettingsRow
          key={call.id}
          title={t('skillShare.endedRow', {
            skill: call.skillName,
            tool: call.toolName,
            name: call.granteeName || call.granteeUsername,
          })}
          avatar={call.granteeName || call.granteeUsername}
          avatarUrl={call.granteeAvatarUrl}
          avatarColorIndex={call.granteeAvatarColorIndex}
          badge={
            <Tag
              value={t(`skillShare.callState.${call.callState}` as Parameters<typeof t>[0])}
              tone={call.callState === 'ran' ? 'good' : 'bad'}
            />
          }
          subtitle={[
            call.amountMinor !== null ? formatMinor(call.amountMinor, call.currencyCode) : '',
            formatCallTime(call.createdAt),
            call.requestReason,
          ]
            .filter(Boolean)
            .join(' · ')}
        />
      ))}
    </SettingsSection>
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

// ShareCalls are what was asked through one share: what waits first, with the
// buttons that answer it for the owner, then the rest as short rows.
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
  if (calls.error && !calls.data) {
    return (
      <div className="skill-share-calls">
        <LoadFailed onRetry={() => void calls.reload()} />
      </div>
    )
  }
  if (!calls.data) return <p className="muted skill-share-calls">{t('skillShare.callLoading')}</p>
  const rows = calls.data.ListSkillShareCalls
  if (rows.length === 0) return <SettingsEmpty>{t('skillShare.noActivity')}</SettingsEmpty>
  const waiting = rows.filter((call) => call.callState === 'waiting_for_approval')
  // The rest, which the owner did not necessarily answer: some were declined,
  // some expired and some were refused before there was anything to answer.
  const earlier = rows.filter((call) => call.callState !== 'waiting_for_approval')
  return (
    <div className="skill-share-calls">
      {waiting.length > 0 ? (
        <>
          <h4 className="skill-share-calls-heading">
            {isOwner ? t('skillShare.callsWaitingForYou') : t('skillShare.callState.waiting_for_approval')}
          </h4>
          {waiting.map((call) => (
            <SkillShareCallCard
              key={call.id}
              call={call}
              isOwner={isOwner}
              onAnswered={() => {
                void calls.reload()
                onChanged?.()
              }}
            />
          ))}
        </>
      ) : null}
      {earlier.length > 0 ? (
        <>
          <h4 className="skill-share-calls-heading">{t('skillShare.callsEarlier')}</h4>
          {earlier.map((call) => (
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
                formatCallTime(call.createdAt),
                call.requestReason,
              ]
                .filter(Boolean)
                .join(' · ')}
            />
          ))}
        </>
      ) : null}
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
  const isPaymentUnreadable = paymentLimit.trim() !== '' && paymentMinor === null
  const isMonthlyUnreadable = monthlyLimit.trim() !== '' && monthlyMinor === null
  const isLimitReadable = !isPaymentUnreadable && !isMonthlyUnreadable
  // A currency that is typed but is not three letters: said under it, since
  // the limits it belongs to are further down than a short window shows.
  const isCurrencyBad = currency.trim() !== '' && !isCurrencyValid
  // One payment above what a month may come to is a limit that cannot hold;
  // the server refuses it in the same words.
  const isPaymentAboveMonthly = paymentMinor !== null && monthlyMinor !== null && paymentMinor > monthlyMinor
  // Said under the field that could not be read, in the currency's own digits.
  const unreadableAmount = t('skillShare.amountUnreadable', {
    currency: isCurrencyValid ? code.toUpperCase() : t('skillShare.amountUnreadableCurrency'),
    example: typedExample(code),
  })
  // Asking every time sends no limits, so whatever is wrong with the limit
  // fields (which are not drawn then) does not hold the share back.
  const isEachLimitAcceptable =
    (!pays || hasLimit) && (!hasLimit || isCurrencyValid) && !isCurrencyBad && isLimitReadable && !isPaymentAboveMonthly
  const canSubmit = personId !== '' && chosen.size > 0 && (isAskedEveryTime || isEachLimitAcceptable)

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
      toast.done(t('skillShare.shared', { skill: skill.name, name: person ? personName(person) : '' }))
      onShared()
    } catch (caught) {
      // The dialog is open, so the failure is said in it, once.
      setProblem(failureText(caught, t('skillShare.failed')))
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
              {personLabel(person)}
            </option>
          ))}
        </select>
      </label>
      <fieldset className="skill-share-tools">
        <legend>{t('skillShare.tools')}</legend>
        {skill.tools.map((tool) => (
          <label key={tool.name} className="skill-share-choice">
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
              <span className="skill-share-choice-name">{tool.name}</span>
              <span className="skill-share-choice-hint">
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
      <label className="skill-share-choice">
        <input
          type="checkbox"
          checked={isAskedEveryTime}
          onChange={(event) => setIsAskedEveryTime(event.target.checked)}
        />
        <span>
          <span className="skill-share-choice-name">{t('skillShare.askEveryTime')}</span>
          <span className="skill-share-choice-hint">{t('skillShare.askEveryTimeHint')}</span>
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
              aria-invalid={isPaymentUnreadable}
              onChange={(event) => setPaymentLimit(event.target.value)}
            />
          </label>
          {isPaymentUnreadable ? (
            <span className="error" role="alert">
              {unreadableAmount}
            </span>
          ) : null}
          {isPaymentAboveMonthly ? (
            <span className="error" role="alert">
              {t('skillShare.paymentAboveMonthly')}
            </span>
          ) : null}
          <label>
            {t('skillShare.monthlyLimit')}
            <input
              inputMode="decimal"
              value={monthlyLimit}
              placeholder="400.00"
              aria-invalid={isMonthlyUnreadable}
              onChange={(event) => setMonthlyLimit(event.target.value)}
            />
          </label>
          {isMonthlyUnreadable ? (
            <span className="error" role="alert">
              {unreadableAmount}
            </span>
          ) : null}
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
          {isCurrencyBad ? (
            <span className="error" role="alert">
              {t('skillShare.currencyBad')}
            </span>
          ) : null}
          <p className="muted">{pays && !hasLimit ? t('skillShare.needsBound') : t('skillShare.limitsHint')}</p>
        </>
      ) : null}
    </FormDialog>
  )
}
