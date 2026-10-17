import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { graphql, isNotFound } from '../api'
import { Avatar } from '../components/avatar'
import { PageHeadingActions, useBreadcrumbDetail } from '../components/breadcrumb'
import { announceCirclesChanged } from '../components/circleInvitations'
import { ErrorMessage, Loading, Tag } from '../components/common'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { CloseIcon, MoreIcon, PencilIcon, TrashIcon } from '../components/icons'
import { MenuButton } from '../components/menuButton'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../components/settingsList'
import { useToast } from '../components/toast'
import { Tooltip } from '../components/tooltip'
import { useQuery } from '../components/useQuery'
import { Key, useTranslation } from '../i18n/i18n'
import { useSession } from '../session'

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

// The most faces a card draws before the count says the rest.
const MOST_FACES = 5

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
  const navigate = useNavigate()

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

  const circles = data?.circles ?? []
  const invitations = data?.invitations ?? []

  function answer(invitation: CircleInvitation, isAccepted: boolean) {
    void act(
      async () => {
        await graphql(ANSWER_CIRCLE_INVITATION, { invitationId: invitation.id, isAccepted })
        announceCirclesChanged()
      },
      isAccepted ? t('circles.joined', { name: invitation.circleName }) : t('circles.declined'),
    )
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
          <SettingsEmpty>{t('circles.empty')}</SettingsEmpty>
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
                    {circle.members.slice(0, MOST_FACES).map((member) => (
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

  const [inviteUsername, setInviteUsername] = useState('')
  const [isInviteAdmin, setInviteAdmin] = useState(false)
  const [isRenaming, setRenaming] = useState(false)
  const [renameDraft, setRenameDraft] = useState('')
  const [isDeleting, setDeleting] = useState(false)
  const [isLeaving, setLeaving] = useState(false)
  const [removingMember, setRemovingMember] = useState<CircleMember | null>(null)

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
            <div className="row-actions">
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
      >
        <div className="page-actions">
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
      </SettingsSection>

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
              subtitle={isYou ? `${member.username} · ${t('circles.you')}` : member.username}
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

      {isAdmin && (
        <SettingsSection card title={t('circles.invite.title')} description={t('circles.invite.intro')}>
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
