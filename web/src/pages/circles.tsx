import { useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { graphql, isNotFound } from '../api'
import { Avatar } from '../components/avatar'
import { PageHeadingActions, useBreadcrumbDetail } from '../components/breadcrumb'
import { announceCirclesChanged } from '../components/circleInvitations'
import { ErrorMessage, Loading, Tag } from '../components/common'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { CloseIcon, MoreIcon, PencilIcon, TrashIcon } from '../components/icons'
import { MenuButton } from '../components/menuButton'
import { SHARE_LEVELS, GrantableLevel } from '../components/calendarShare'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../components/settingsList'
import { useToast } from '../components/toast'
import { Tooltip } from '../components/tooltip'
import { useQuery } from '../components/useQuery'
import { Key, useTranslation } from '../i18n/i18n'
import { hasPermission, useSession } from '../session'

// What a circle is for, which is how it is named on screen: a circle is called
// by its name and described by its kind. "Family", never "a family circle".
export type CircleKind = 'family' | 'friends' | 'colleagues'
export type MemberRole = 'admin' | 'member'

export type CircleMember = {
  userId: string
  username: string
  name: string
  avatarUrl: string | null
  avatarColorIndex: number
  memberRole: MemberRole
  joinedAt: string
}

export type CircleInvitation = {
  id: string
  circleId: string
  circleName: string
  circleKind: CircleKind
  invitedByName: string
  invitedUsername: string
  memberRole: MemberRole
  createdAt: string
  expiresAt: string
}

export type Circle = {
  id: string
  name: string
  circleKind: CircleKind
  createdAt: string
  // The caller's own role in it.
  memberRole: MemberRole
  members: CircleMember[]
  // Empty for somebody who is not an admin: the server answers only the
  // admins' question of who is still being waited for.
  pendingInvitations: CircleInvitation[]
}

type AccountDraft = { username: string; name: string; password: string; isAdmin: boolean }

const EMPTY_ACCOUNT: AccountDraft = { username: '', name: '', password: '', isAdmin: false }

const CIRCLE_KINDS: CircleKind[] = ['family', 'friends', 'colleagues']

const KIND_LABELS: Record<CircleKind, Key> = {
  family: 'circles.kind.family',
  friends: 'circles.kind.friends',
  colleagues: 'circles.kind.colleagues',
}

const MEMBER_FIELDS = '{ userId username name avatarUrl avatarColorIndex memberRole joinedAt }'
const INVITATION_FIELDS =
  '{ id circleId circleName circleKind invitedByName invitedUsername memberRole createdAt expiresAt }'
const CIRCLE_FIELDS = `{ id name circleKind createdAt memberRole members ${MEMBER_FIELDS} pendingInvitations ${INVITATION_FIELDS} }`

const LIST_CIRCLES = `query { ListCircles ${CIRCLE_FIELDS} }`
const LIST_CIRCLE_INVITATIONS = `query { ListCircleInvitations ${INVITATION_FIELDS} }`
const READ_CIRCLE = `query ($circleId: String!) { ReadCircle(circleId: $circleId) ${CIRCLE_FIELDS} }`

const CREATE_CIRCLE = `
  mutation ($name: String!, $circleKind: String!) {
    CreateCircle(name: $name, circleKind: $circleKind) { id }
  }`
const RENAME_CIRCLE = `mutation ($circleId: String!, $name: String!) { RenameCircle(circleId: $circleId, name: $name) { id } }`
const DELETE_CIRCLE = `mutation ($circleId: String!) { DeleteCircle(circleId: $circleId) }`
const INVITE_TO_CIRCLE = `
  mutation ($circleId: String!, $username: String!, $memberRole: String!) {
    InviteToCircle(circleId: $circleId, username: $username, memberRole: $memberRole)
  }`
const ANSWER_CIRCLE_INVITATION = `
  mutation ($invitationId: String!, $isAccepted: Boolean!) {
    AnswerCircleInvitation(invitationId: $invitationId, isAccepted: $isAccepted) { id }
  }`
const WITHDRAW_CIRCLE_INVITATION = `mutation ($invitationId: String!) { WithdrawCircleInvitation(invitationId: $invitationId) }`
const SET_CIRCLE_MEMBER_ROLE = `
  mutation ($circleId: String!, $userId: String!, $memberRole: String!) {
    SetCircleMemberRole(circleId: $circleId, userId: $userId, memberRole: $memberRole) { id }
  }`
const REMOVE_CIRCLE_MEMBER = `mutation ($circleId: String!, $userId: String!) { RemoveCircleMember(circleId: $circleId, userId: $userId) { id } }`
const LEAVE_CIRCLE = `mutation ($circleId: String!) { LeaveCircle(circleId: $circleId) }`
// The account an operator makes for somebody, and putting it in the circle
// without an invitation: the same two operations the Users tab and the
// command line use.
const CREATE_USER = `
  mutation ($username: String!, $password: String, $name: String) {
    CreateUser(username: $username, password: $password, name: $name) { id username }
  }`
const ADD_CIRCLE_MEMBER = `
  mutation ($circleId: String!, $username: String!, $memberRole: String!) {
    AddCircleMember(circleId: $circleId, username: $username, memberRole: $memberRole) { id }
  }`

// The calendars the person reaches, to know whether a circle has one of its
// own and which is the person's calendar to share.
const LIST_CALENDARS = `query { ListCalendars { id calendarKind shareLevel circleId isCircleCalendar } }`
const CREATE_CIRCLE_CALENDAR = `
  mutation ($circleId: String!) { CreateCircleCalendar(circleId: $circleId) { id } }`
const SHARE_CALENDAR = `
  mutation ($calendarId: String!, $granteeCircleId: String!, $shareLevel: String!) {
    ShareCalendar(calendarId: $calendarId, granteeCircleId: $granteeCircleId, shareLevel: $shareLevel) { id }
  }`

type CalendarOfCircle = {
  id: string
  calendarKind: string
  shareLevel?: string
  circleId?: string | null
  isCircleCalendar?: boolean
}

// The most pictures a card draws before the count says the rest.
const MOST_AVATARS = 5

function nameOf(member: Pick<CircleMember, 'name' | 'username'>): string {
  return member.name?.trim() || member.username
}

// useAct runs one change to circles the same way everywhere: it says what
// happened in a toast, and keeps the failure for a dialog that is open to show
// in its own words, so that the dialog stays up with what was typed in it.
function useAct(reload: () => Promise<unknown>) {
  const { t } = useTranslation()
  const toast = useToast()
  const [isBusy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  async function act(work: () => Promise<unknown>, successMessage: string | null, isInDialog = false) {
    setBusy(true)
    setProblem(null)
    try {
      await work()
      if (successMessage) {
        toast.done(successMessage)
      }
      await reload()
      return true
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : t('circles.failed'))
      if (!isInDialog) {
        toast.failure(caught, t('circles.failed'))
      }
      return false
    } finally {
      setBusy(false)
    }
  }

  return { act, isBusy, problem, clearProblem: () => setProblem(null) }
}

// CirclesPage is the circles a person is in, the invitations waiting for their
// answer above them, and the way to make a new one.
export function CirclesPage() {
  const { t, plural } = useTranslation()
  const session = useSession()
  const navigate = useNavigate()
  const managesUsers = hasPermission(session.permissions, 'user:manage')
  const canUseCalendar =
    hasPermission(session.permissions, 'calendar:use') && session.services?.isCalendarEnabled !== false

  const { data, error, loading, reload } = useQuery(async () => {
    const [circles, invitations] = await Promise.all([
      graphql<{ ListCircles: Circle[] }>(LIST_CIRCLES),
      graphql<{ ListCircleInvitations: CircleInvitation[] }>(LIST_CIRCLE_INVITATIONS),
    ])
    return { circles: circles.ListCircles, invitations: invitations.ListCircleInvitations }
  }, [])
  const { act, isBusy, problem, clearProblem } = useAct(reload)

  const [isCreating, setCreating] = useState(false)
  const [newName, setNewName] = useState('')
  const [newCircleKind, setNewCircleKind] = useState<CircleKind>('family')
  // Offered once, as the person joins a family or a group of colleagues: share
  // their calendar with it, at a level they choose, or not now. Never asked
  // again; the calendar page has the same choice whenever they want it.
  const [suggestion, setSuggestion] = useState<{ circleId: string; name: string } | null>(null)
  const [suggestedLevel, setSuggestedLevel] = useState<GrantableLevel>('busy')

  const circles = data?.circles ?? []
  const invitations = data?.invitations ?? []

  function answer(invitation: CircleInvitation, isAccepted: boolean) {
    void act(
      async () => {
        await graphql(ANSWER_CIRCLE_INVITATION, { invitationId: invitation.id, isAccepted })
        announceCirclesChanged()
      },
      isAccepted ? t('circles.joined', { name: invitation.circleName }) : t('circles.declined'),
    ).then((isDone) => {
      if (isDone && isAccepted && invitation.circleKind !== 'friends' && canUseCalendar) {
        setSuggestedLevel('busy')
        setSuggestion({ circleId: invitation.circleId, name: invitation.circleName })
      }
    })
  }

  return (
    <>
      <PageHeadingActions>
        <button
          type="button"
          className="primary"
          onClick={() => {
            clearProblem()
            setNewName('')
            setNewCircleKind('family')
            setCreating(true)
          }}
        >
          {t('circles.new')}
        </button>
      </PageHeadingActions>

      {loading && !data && <Loading />}
      <ErrorMessage error={error} />

      {invitations.length > 0 && (
        <SettingsSection card title={t('circles.invitations.title')} description={t('circles.invitations.intro')}>
          {invitations.map((invitation) => (
            <SettingsRow
              key={invitation.id}
              title={invitation.circleName}
              badge={<Tag value={t(KIND_LABELS[invitation.circleKind])} />}
              subtitle={t('circles.invitations.from', { name: invitation.invitedByName })}
              actions={
                <div className="row-actions">
                  <button type="button" disabled={isBusy} onClick={() => answer(invitation, false)}>
                    {t('circles.decline')}
                  </button>
                  <button type="button" className="primary" disabled={isBusy} onClick={() => answer(invitation, true)}>
                    {t('circles.accept')}
                  </button>
                </div>
              }
            />
          ))}
        </SettingsSection>
      )}

      {data && circles.length === 0 ? (
        <SettingsSection card title={t('circles.mine')} description={t('circles.intro')}>
          {/* An operator is told they can bring in people who have no
              account yet; anybody else can only invite accounts. */}
          <SettingsEmpty>{managesUsers ? t('circles.emptyOperator') : t('circles.empty')}</SettingsEmpty>
        </SettingsSection>
      ) : (
        circles.length > 0 && (
          <div className="circle-grid">
            {circles.map((circle) => (
              <Link key={circle.id} to={`/circles/${circle.id}`} className="card circle-card">
                <div className="circle-card-title">
                  <strong>{circle.name}</strong>
                  <Tag value={t(KIND_LABELS[circle.circleKind])} />
                </div>
                <div className="circle-card-members">
                  <div className="avatar-stack">
                    {circle.members.slice(0, MOST_AVATARS).map((member) => (
                      <Avatar
                        key={member.userId}
                        name={nameOf(member)}
                        avatarUrl={member.avatarUrl}
                        colorIndex={member.avatarColorIndex}
                        size={28}
                      />
                    ))}
                  </div>
                  <span className="muted">
                    {plural(
                      circle.members.length,
                      { one: 'circles.memberCountOne', other: 'circles.memberCount' },
                      { count: circle.members.length },
                    )}
                  </span>
                </div>
              </Link>
            ))}
          </div>
        )
      )}

      {suggestion && (
        <FormDialog
          title={t('circles.shareSuggestion.title', { name: suggestion.name })}
          submitLabel={t('circles.shareSuggestion.share')}
          closeLabel={t('circles.shareSuggestion.notNow')}
          busy={isBusy}
          error={problem}
          onClose={() => setSuggestion(null)}
          onSubmit={async () => {
            const isShared = await act(
              async () => {
                const calendars = await graphql<{ ListCalendars: CalendarOfCircle[] }>(LIST_CALENDARS)
                const own = calendars.ListCalendars.find(
                  (found) => found.calendarKind === 'events' && (!found.shareLevel || found.shareLevel === 'owner'),
                )
                if (!own) throw new Error(t('circles.failed'))
                await graphql(SHARE_CALENDAR, {
                  calendarId: own.id,
                  granteeCircleId: suggestion.circleId,
                  shareLevel: suggestedLevel,
                })
              },
              t('calendar.shareSaid', { name: t('calendar.shareEveryoneIn', { name: suggestion.name }) }),
              true,
            )
            if (isShared) setSuggestion(null)
          }}
        >
          <p className="muted">{t('circles.shareSuggestion.body', { name: suggestion.name })}</p>
          <label>
            {t('calendar.shareLevelLabel')}
            <select
              value={suggestedLevel}
              onChange={(event) => setSuggestedLevel(event.target.value as GrantableLevel)}
            >
              {SHARE_LEVELS.filter((level) => level !== 'edit').map((level) => (
                <option key={level} value={level}>
                  {t(`calendar.shareLevelName.${level}` as Key)}
                </option>
              ))}
            </select>
            <span className="muted">{t(`calendar.shareLevelHint.${suggestedLevel}` as Key)}</span>
          </label>
        </FormDialog>
      )}

      {isCreating && (
        <FormDialog
          title={t('circles.new')}
          submitLabel={t('common.create')}
          busy={isBusy}
          error={problem}
          canSubmit={newName.trim() !== ''}
          onClose={() => setCreating(false)}
          onSubmit={async () => {
            let createdCircleId = ''
            const isCreated = await act(
              async () => {
                const created = await graphql<{ CreateCircle: { id: string } }>(CREATE_CIRCLE, {
                  name: newName.trim(),
                  circleKind: newCircleKind,
                })
                createdCircleId = created.CreateCircle.id
              },
              t('circles.created'),
              true,
            )
            if (isCreated) {
              setCreating(false)
              // Straight to it: the next thing to do with a new circle is
              // invite somebody, and that is on its page.
              if (createdCircleId) {
                navigate(`/circles/${createdCircleId}`)
              }
            }
          }}
        >
          <label>
            {t('circles.name')}
            <input autoFocus value={newName} maxLength={100} onChange={(event) => setNewName(event.target.value)} />
          </label>
          <label>
            {t('circles.kind')}
            <select value={newCircleKind} onChange={(event) => setNewCircleKind(event.target.value as CircleKind)}>
              {CIRCLE_KINDS.map((kind) => (
                <option key={kind} value={kind}>
                  {t(KIND_LABELS[kind])}
                </option>
              ))}
            </select>
          </label>
        </FormDialog>
      )}
    </>
  )
}

// CirclePage is one circle: who is in it, and for an admin, who is still being
// waited for, the invitation form, and the changes to the circle itself. What
// is hidden from a member here is refused by the server as well.
export function CirclePage() {
  const { t } = useTranslation()
  const session = useSession()
  const navigate = useNavigate()
  const circleId = useParams().circleId ?? ''

  const { data, error, loading, reload } = useQuery(
    async () => (await graphql<{ ReadCircle: Circle }>(READ_CIRCLE, { circleId })).ReadCircle,
    [circleId],
  )
  const { act, isBusy, problem, clearProblem } = useAct(reload)

  // The circle's own calendar, if it has one. Asked only of somebody who can
  // use calendars at all.
  const canUseCalendar =
    hasPermission(session.permissions, 'calendar:use') && session.services?.isCalendarEnabled !== false
  const calendars = useQuery(
    async () =>
      canUseCalendar ? (await graphql<{ ListCalendars: CalendarOfCircle[] }>(LIST_CALENDARS)).ListCalendars : [],
    [canUseCalendar, circleId],
    { refresh: false },
  )

  const [inviteUsername, setInviteUsername] = useState('')
  const [isInviteAdmin, setInviteAdmin] = useState(false)
  const [isRenaming, setRenaming] = useState(false)
  const [renameDraft, setRenameDraft] = useState('')
  const [isDeleting, setDeleting] = useState(false)
  const [isLeaving, setLeaving] = useState(false)
  const [removingMember, setRemovingMember] = useState<CircleMember | null>(null)
  const [isMakingAccount, setMakingAccount] = useState(false)
  const [accountDraft, setAccountDraft] = useState<AccountDraft>(EMPTY_ACCOUNT)
  // The username of an account that was made but could not be put in the
  // circle, so that trying again adds it instead of failing on a username
  // that is taken now.
  const madeUsername = useRef('')
  const managesUsers = hasPermission(session.permissions, 'user:manage')

  const circle = data
  useBreadcrumbDetail(circle?.name)

  if (loading && !circle) {
    return <Loading />
  }
  if (!circle) {
    // A circle somebody is not in is answered not found, which is also what a
    // circle they just left is. Said as such rather than as a failure.
    return (
      <>
        {error && !isNotFound(error) ? <ErrorMessage error={error} /> : null}
        <SettingsEmpty>{t('circles.notFound')}</SettingsEmpty>
        <p>
          <Link to="/circles">{t('circles.title')}</Link>
        </p>
      </>
    )
  }

  const isAdmin = circle.memberRole === 'admin'

  const setRole = (member: CircleMember, memberRole: MemberRole) =>
    void act(
      () => graphql(SET_CIRCLE_MEMBER_ROLE, { circleId: circle.id, userId: member.userId, memberRole }),
      t(memberRole === 'admin' ? 'circles.nowAdmin' : 'circles.nowMember', { name: nameOf(member) }),
    )

  return (
    <>
      <SettingsSection
        card
        title={circle.name}
        description={t('circles.detail.intro', { kind: t(KIND_LABELS[circle.circleKind]) })}
        action={
          isAdmin ? (
            <div className="row-actions head-icons">
              <Tooltip label={t('circles.rename')}>
                <button
                  type="button"
                  className="icon-action"
                  aria-label={`${circle.name}: ${t('circles.rename')}`}
                  onClick={() => {
                    clearProblem()
                    setRenameDraft(circle.name)
                    setRenaming(true)
                  }}
                >
                  <PencilIcon size={16} />
                </button>
              </Tooltip>
              <Tooltip label={t('circles.delete')}>
                <button
                  type="button"
                  className="icon-action danger"
                  aria-label={`${circle.name}: ${t('circles.delete')}`}
                  onClick={() => {
                    clearProblem()
                    setDeleting(true)
                  }}
                >
                  <TrashIcon size={16} />
                </button>
              </Tooltip>
            </div>
          ) : undefined
        }
      />

      <SettingsSection card title={t('circles.members.title')}>
        {circle.members.map((member) => {
          const isYou = member.userId === session.userId
          return (
            <SettingsRow
              key={member.userId}
              avatar={nameOf(member)}
              avatarUrl={member.avatarUrl}
              avatarColorIndex={member.avatarColorIndex}
              title={nameOf(member)}
              badge={member.memberRole === 'admin' ? <Tag value={t('circles.admin')} tone="good" /> : undefined}
              // The username only where it says something the title did not:
              // somebody without a name is titled by their username already.
              subtitle={
                [member.name?.trim() ? member.username : '', isYou ? t('circles.you') : '']
                  .filter(Boolean)
                  .join(' · ') || undefined
              }
              actions={
                isAdmin && !isYou ? (
                  <MenuButton
                    className="icon-action"
                    label={`${nameOf(member)}: ${t('circles.memberMenu')}`}
                    icon={<MoreIcon size={16} />}
                    render={(close) => (
                      <>
                        <button
                          type="button"
                          role="menuitem"
                          disabled={isBusy}
                          onClick={() => {
                            close()
                            setRole(member, member.memberRole === 'admin' ? 'member' : 'admin')
                          }}
                        >
                          {member.memberRole === 'admin' ? t('circles.makeMember') : t('circles.makeAdmin')}
                        </button>
                        <button
                          type="button"
                          role="menuitem"
                          onClick={() => {
                            close()
                            clearProblem()
                            setRemovingMember(member)
                          }}
                        >
                          {t('circles.removeMember')}
                        </button>
                      </>
                    )}
                  />
                ) : undefined
              }
            />
          )
        })}
      </SettingsSection>

      {canUseCalendar && calendars.data && (
        <SettingsSection card title={t('circles.calendar.title')} description={t('circles.calendar.intro')}>
          {calendars.data.some((found) => found.isCircleCalendar && found.circleId === circle.id) ? (
            <>
              <p>{t('circles.calendar.exists')}</p>
              <p>
                <Link to="/calendar">{t('circles.calendar.open')}</Link>
              </p>
            </>
          ) : isAdmin ? (
            <button
              type="button"
              className="primary"
              disabled={isBusy}
              onClick={() =>
                void act(
                  () => graphql(CREATE_CIRCLE_CALENDAR, { circleId: circle.id }),
                  t('circles.calendar.made'),
                ).then((isMade) => {
                  if (isMade) void calendars.reload()
                })
              }
            >
              {t(circle.circleKind === 'family' ? 'circles.calendar.makeFamily' : 'circles.calendar.makeShared')}
            </button>
          ) : (
            <SettingsEmpty>{t('circles.calendar.none')}</SettingsEmpty>
          )}
        </SettingsSection>
      )}

      {isAdmin && (
        <SettingsSection
          card
          title={t('circles.invite.title')}
          description={managesUsers ? t('circles.invite.introOperator') : t('circles.invite.intro')}
        >
          <form
            onSubmit={(event) => {
              event.preventDefault()
              const username = inviteUsername.trim()
              if (!username) {
                return
              }
              void act(
                () =>
                  graphql(INVITE_TO_CIRCLE, {
                    circleId: circle.id,
                    username,
                    memberRole: isInviteAdmin ? 'admin' : 'member',
                  }),
                // The server answers the same whether or not the account
                // exists, so the page must not claim the person was found.
                t('circles.invite.sent'),
              ).then((isSent) => {
                if (isSent) {
                  setInviteUsername('')
                  setInviteAdmin(false)
                }
              })
            }}
          >
            <div className="form-narrow">
              <label>
                {t('circles.invite.username')}
                <input
                  value={inviteUsername}
                  autoCapitalize="none"
                  autoCorrect="off"
                  spellCheck={false}
                  onChange={(event) => setInviteUsername(event.target.value)}
                />
              </label>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={isInviteAdmin}
                  onChange={(event) => setInviteAdmin(event.target.checked)}
                />
                {t('circles.invite.isAdmin')}
              </label>
              <div>
                <button type="submit" className="primary" disabled={isBusy || inviteUsername.trim() === ''}>
                  {t('circles.invite.submit')}
                </button>
              </div>
            </div>
          </form>
          {/* The other way in, for an operator: the person has no account
              yet, so one is made and they are in the circle at once, with
              nothing for them to accept. */}
          {managesUsers && (
            <div className="circle-account">
              <button
                type="button"
                disabled={isBusy}
                onClick={() => {
                  clearProblem()
                  madeUsername.current = ''
                  setAccountDraft(EMPTY_ACCOUNT)
                  setMakingAccount(true)
                }}
              >
                {t('circles.account.title')}
              </button>
              <p className="muted">{t('circles.account.intro')}</p>
            </div>
          )}
        </SettingsSection>
      )}

      {isAdmin && (
        <SettingsSection card title={t('circles.pending.title')} description={t('circles.pending.intro')}>
          {circle.pendingInvitations.length === 0 && <SettingsEmpty>{t('circles.pending.empty')}</SettingsEmpty>}
          {circle.pendingInvitations.map((invitation) => (
            <SettingsRow
              key={invitation.id}
              avatar={invitation.invitedUsername}
              title={invitation.invitedUsername}
              badge={invitation.memberRole === 'admin' ? <Tag value={t('circles.admin')} tone="good" /> : undefined}
              subtitle={t('circles.pending.detail', {
                name: invitation.invitedByName,
                date: new Date(invitation.expiresAt).toLocaleDateString(),
              })}
              actions={
                <div className="row-actions">
                  <Tooltip label={t('circles.withdraw')}>
                    <button
                      type="button"
                      className="icon-action"
                      disabled={isBusy}
                      aria-label={`${invitation.invitedUsername}: ${t('circles.withdraw')}`}
                      onClick={() =>
                        void act(
                          () => graphql(WITHDRAW_CIRCLE_INVITATION, { invitationId: invitation.id }),
                          t('circles.withdrawn'),
                        )
                      }
                    >
                      <CloseIcon size={16} />
                    </button>
                  </Tooltip>
                </div>
              }
            />
          ))}
        </SettingsSection>
      )}

      {/* Leaving is not what this page is for, so it is the last thing on
          it, quiet, where a destructive action that is not the page's
          purpose goes. */}
      <div className="page-actions page-actions-end">
        <button
          type="button"
          className="danger"
          onClick={() => {
            clearProblem()
            setLeaving(true)
          }}
        >
          {t('circles.leave')}
        </button>
      </div>

      {isMakingAccount && (
        <FormDialog
          title={t('circles.account.title')}
          submitLabel={t('common.create')}
          busy={isBusy}
          error={problem}
          canSubmit={accountDraft.username.trim() !== '' && accountDraft.password !== ''}
          onClose={() => setMakingAccount(false)}
          onSubmit={async () => {
            const username = accountDraft.username.trim()
            const name = accountDraft.name.trim()
            const isDone = await act(
              async () => {
                if (madeUsername.current !== username) {
                  await graphql(CREATE_USER, { username, password: accountDraft.password, name })
                  madeUsername.current = username
                }
                try {
                  await graphql(ADD_CIRCLE_MEMBER, {
                    circleId: circle.id,
                    username,
                    memberRole: accountDraft.isAdmin ? 'admin' : 'member',
                  })
                } catch (caught) {
                  // The account is there now; say so, so that the person
                  // does not make it a second time and meet "taken".
                  throw new Error(
                    t('circles.account.notAdded', {
                      username,
                      reason: caught instanceof Error ? caught.message : t('circles.failed'),
                    }),
                  )
                }
              },
              t('circles.account.made', { name: name || username }),
              true,
            )
            if (isDone) {
              madeUsername.current = ''
              setMakingAccount(false)
              setAccountDraft(EMPTY_ACCOUNT)
            }
          }}
        >
          <label>
            {t('access.users.username')}
            <input
              autoFocus
              value={accountDraft.username}
              autoCapitalize="none"
              autoCorrect="off"
              spellCheck={false}
              onChange={(event) => setAccountDraft({ ...accountDraft, username: event.target.value })}
            />
          </label>
          <label>
            {t('access.users.name')}
            <input
              value={accountDraft.name}
              onChange={(event) => setAccountDraft({ ...accountDraft, name: event.target.value })}
            />
          </label>
          <label>
            {t('access.users.password')}
            <input
              type="password"
              autoComplete="new-password"
              value={accountDraft.password}
              onChange={(event) => setAccountDraft({ ...accountDraft, password: event.target.value })}
            />
          </label>
          <label className="checkbox">
            <input
              type="checkbox"
              checked={accountDraft.isAdmin}
              onChange={(event) => setAccountDraft({ ...accountDraft, isAdmin: event.target.checked })}
            />
            {t('circles.invite.isAdmin')}
          </label>
        </FormDialog>
      )}

      {isRenaming && (
        <FormDialog
          title={t('circles.renameTitle', { name: circle.name })}
          submitLabel={t('common.save')}
          busy={isBusy}
          error={problem}
          canSubmit={renameDraft.trim() !== '' && renameDraft.trim() !== circle.name}
          onClose={() => setRenaming(false)}
          onSubmit={async () => {
            if (
              await act(
                () => graphql(RENAME_CIRCLE, { circleId: circle.id, name: renameDraft.trim() }),
                t('circles.renamed'),
                true,
              )
            ) {
              setRenaming(false)
            }
          }}
        >
          <label>
            {t('circles.name')}
            <input
              autoFocus
              value={renameDraft}
              maxLength={100}
              onChange={(event) => setRenameDraft(event.target.value)}
            />
          </label>
        </FormDialog>
      )}

      {isDeleting && (
        <ConfirmDialog
          title={t('circles.deleteTitle', { name: circle.name })}
          body={t('circles.deleteBody')}
          confirmLabel={t('circles.delete')}
          busy={isBusy}
          error={problem}
          onConfirm={async () => {
            if (await act(() => graphql(DELETE_CIRCLE, { circleId: circle.id }), t('circles.deleted'), true)) {
              setDeleting(false)
              navigate('/circles')
            }
          }}
          onClose={() => setDeleting(false)}
        />
      )}

      {isLeaving && (
        <ConfirmDialog
          title={t('circles.leaveTitle', { name: circle.name })}
          body={t('circles.leaveBody', { name: circle.name })}
          confirmLabel={t('circles.leave')}
          busy={isBusy}
          error={problem}
          onConfirm={async () => {
            if (
              await act(
                async () => {
                  await graphql(LEAVE_CIRCLE, { circleId: circle.id })
                  announceCirclesChanged()
                },
                t('circles.left', { name: circle.name }),
                true,
              )
            ) {
              setLeaving(false)
              navigate('/circles')
            }
          }}
          onClose={() => setLeaving(false)}
        />
      )}

      {removingMember && (
        <ConfirmDialog
          title={t('circles.removeTitle', { name: nameOf(removingMember) })}
          body={t('circles.removeBody', { name: nameOf(removingMember), circle: circle.name })}
          confirmLabel={t('circles.removeMember')}
          busy={isBusy}
          error={problem}
          onConfirm={async () => {
            if (
              await act(
                () => graphql(REMOVE_CIRCLE_MEMBER, { circleId: circle.id, userId: removingMember.userId }),
                t('circles.removed', { name: nameOf(removingMember) }),
                true,
              )
            ) {
              setRemovingMember(null)
            }
          }}
          onClose={() => setRemovingMember(null)}
        />
      )}
    </>
  )
}
