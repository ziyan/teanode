import { useState } from 'react'
import { Link, Navigate, useNavigate, useParams } from 'react-router-dom'

import { graphql } from '../api'
import { PageHeadingActions } from '../components/breadcrumb'
import { ErrorMessage, Loading, formatTime } from '../components/common'
import { Column, DataTable } from '../components/dataTable'
import { ConfirmDialog } from '../components/dialog'
import { TrashIcon } from '../components/icons'
import { SettingsEmpty, SettingsSection } from '../components/settingsList'
import { TabItem, Tabs } from '../components/tabs'
import { useToast } from '../components/toast'
import { Tooltip } from '../components/tooltip'
import { useQuery } from '../components/useQuery'
import { Key, useTranslation } from '../i18n/i18n'
import { personPath } from '../lib/circlePeople'
import { accountLabel } from './finance/financeCommon'
import { hasPermission, useSession } from '../session'

// Everything the signed-in person shares, and everything shared with them, in
// one place. Each kind is still made and changed on its own page; this lists
// them all, takes you to each, and stops a share or removes one made to you by
// name with the kind's own operation. docs/subsystems/circles.md ("Sharing, in
// one place") says what is listed and what is left out.

export type ShareKind =
  'calendar' | 'location' | 'health' | 'memory_page' | 'photo_album' | 'recording' | 'finance' | 'skill' | 'passwords'

export type SharingMade = {
  shareKind: ShareKind
  // Empty for passwords, whose shares are one for each item.
  shareId: string
  subjectId: string
  subjectName: string
  // The last digits of a finance account's number; empty for every other kind.
  subjectMask: string
  // True for a share of a memory page that no longer exists; subjectName is then its path.
  isSubjectDeleted: boolean
  shareLevel: string
  shareScopes: string[]
  passwordItemCount: number
  granteeUserId: string | null
  granteeUsername: string
  granteeName: string
  granteeCircleId: string | null
  granteeCircleName: string
  createdAt: string
}

export type SharingReceived = {
  shareKind: ShareKind
  shareId: string
  subjectId: string
  subjectName: string
  // The last digits of a finance account's number; empty for every other kind.
  subjectMask: string
  shareLevel: string
  shareScopes: string[]
  passwordItemCount: number
  ownerUserId: string
  ownerUsername: string
  ownerName: string
  granteeCircleId: string | null
  granteeCircleName: string
  isDirect: boolean
  canDecline: boolean
  createdAt: string
}

export const LIST_SHARING = `query { ListSharing {
  sharesMade { shareKind shareId subjectId subjectName subjectMask isSubjectDeleted shareLevel shareScopes passwordItemCount
    granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName createdAt }
  sharesReceived { shareKind shareId subjectId subjectName subjectMask shareLevel shareScopes passwordItemCount
    ownerUserId ownerUsername ownerName granteeCircleId granteeCircleName isDirect canDecline createdAt }
} }`

// What stops a share is the kind's own operation, with the arguments it takes.
const UNSHARE_CALENDAR = `mutation ($shareId: String!) { UnshareCalendar(shareId: $shareId) }`
const UNSHARE_LOCATION = `mutation ($shareId: String!) { UnshareLocation(shareId: $shareId) }`
const UNSHARE_AGENT_PAGE = `mutation ($shareId: String!) { UnshareAgentPage(shareId: $shareId) }`
const UNSHARE_SKILL = `mutation ($shareId: String!) { UnshareSkill(shareId: $shareId) }`
const REMOVE_PHOTO_ALBUM_SHARE = `mutation ($photoAlbumShareId: String!) { RemovePhotoAlbumShare(photoAlbumShareId: $photoAlbumShareId) }`
// Health is shared by category, and taken back by sharing no categories.
const SET_HEALTH_SHARE = `mutation ($granteeUserId: String, $granteeCircleId: String, $healthCategories: [String!]!) {
  SetHealthShare(granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, healthCategories: $healthCategories) { id }
}`
const UNSHARE_RECORDING = `mutation ($recordingShareId: String!) { UnshareRecording(recordingShareId: $recordingShareId) }`
const UNSHARE_FINANCE_ACCOUNT = `mutation ($shareId: String!) { UnshareFinanceAccount(shareId: $shareId) }`
const DECLINE_SHARE = `mutation ($shareKind: String!, $shareId: String!) { DeclineShare(shareKind: $shareKind, shareId: $shareId) }`

const KIND_NAMES: Record<ShareKind, Key> = {
  calendar: 'sharing.kind.calendar',
  location: 'sharing.kind.location',
  health: 'sharing.kind.health',
  memory_page: 'sharing.kind.memoryPage',
  photo_album: 'sharing.kind.photoAlbum',
  recording: 'sharing.kind.recording',
  finance: 'sharing.kind.finance',
  skill: 'sharing.kind.skill',
  passwords: 'sharing.kind.passwords',
}

const SHARE_KINDS = Object.keys(KIND_NAMES) as ShareKind[]

// The words of a level, where the kind's own page already has them.
const LEVEL_NAMES: Record<string, Key> = {
  busy: 'calendar.shareLevelName.busy',
  titles: 'calendar.shareLevelName.titles',
  details: 'calendar.shareLevelName.details',
  edit: 'calendar.shareLevelName.edit',
  current: 'location.shareLevelName.current',
  history: 'location.shareLevelName.history',
  view: 'photos.levelView',
  contribute: 'photos.levelContribute',
  summary: 'recordings.shareLevelName.summary',
  transcript: 'recordings.shareLevelName.transcript',
  balance: 'finance.shareLevelName.balance',
  spending: 'finance.shareLevelName.spending',
  transactions: 'finance.shareLevelName.transactions',
}

// What the person shared of their own location is worded as "where I am"; what
// is shared with them is where the other person is.
const RECEIVED_LEVEL_NAMES: Record<string, Key> = {
  current: 'sharing.level.current',
  history: 'sharing.level.history',
}

// What else a share gives, worded by the kind: a scope means something only
// for its own kind (a skill's tool may be called "quotes"), and Health's are
// categories.
const SCOPE_NAMES: Partial<Record<ShareKind, Record<string, Key>>> = {
  memory_page: { quotes: 'sharing.scope.quotes' },
  photo_album: { location: 'photos.sharePlacesIncluded', faceNames: 'photos.shareFaceNamesIncluded' },
}

const HEALTH_CATEGORY_PREFIX = 'health.category.'

type Tab = 'by-you' | 'with-you'

// Where a kind is managed: the page that shares it and can change it. A memory
// page shared with the person opens as theirs to read, with its owner named.
function managerAddress(
  shareKind: ShareKind,
  subjectId: string,
  received?: { ownerUserId: string; ownerUsername: string },
): string {
  switch (shareKind) {
    case 'calendar':
      return '/calendar'
    case 'location':
      return '/location'
    case 'health':
      return '/health'
    case 'memory_page': {
      const path = subjectId.split('/').map(encodeURIComponent).join('/')
      return received ? `/knowledge/${path}?owner=${encodeURIComponent(received.ownerUsername)}` : `/knowledge/${path}`
    }
    case 'photo_album':
      return `/photos/albums/${encodeURIComponent(subjectId)}`
    // A recording of the person's own opens in their library, one shared with
    // them in the page for what it shares.
    case 'recording':
      return received
        ? `/recordings/shared/${encodeURIComponent(subjectId)}`
        : `/recordings/${encodeURIComponent(subjectId)}`
    // An account is shared from the Sharing panel of the agent page's Finance
    // tab; what somebody shares with the person is on the Finance page, which
    // shows that person's accounts.
    case 'finance':
      return received ? `/finance?person=${encodeURIComponent(received.ownerUserId)}` : '/settings/agent/finance'
    case 'skill':
      return '/settings/agent/connections'
    case 'passwords':
      return '/passwords'
  }
}

// SharingPage is what the person shares and what is shared with them, one tab
// each, the tab in the address.
export function SharingPage() {
  const session = useSession()
  const { t } = useTranslation()
  if (!hasPermission(session.permissions, 'circle:use')) {
    return (
      <SettingsSection card title={t('sharing.title')}>
        <SettingsEmpty>{t('circles.off')}</SettingsEmpty>
      </SettingsSection>
    )
  }
  return <SharingTabs />
}

const TABS: TabItem[] = [
  { id: 'by-you', label: 'sharing.byYou' },
  { id: 'with-you', label: 'sharing.withYou' },
]

function SharingTabs() {
  const { t } = useTranslation()
  const { tab } = useParams()
  const navigate = useNavigate()
  const { data, error, loading, reload } = useQuery(
    () => graphql<{ ListSharing: { sharesMade: SharingMade[]; sharesReceived: SharingReceived[] } }>(LIST_SHARING),
    [],
  )

  // A path that names no tab lands on the first.
  if (tab !== undefined && !TABS.some((candidate) => candidate.id === tab)) {
    return <Navigate to="/sharing" replace />
  }
  const active = (tab ?? 'by-you') as Tab

  return (
    <>
      <PageHeadingActions>
        <Link className="button" to="/circles">
          {t('circles.title')}
        </Link>
      </PageHeadingActions>

      <Tabs items={TABS} active={active} onSelect={(id) => navigate(id === 'by-you' ? '/sharing' : `/sharing/${id}`)} />

      {loading && !data && <Loading />}
      <ErrorMessage error={error} />
      {data && active === 'by-you' && <SharedByYou shares={data.ListSharing.sharesMade} reload={reload} />}
      {data && active === 'with-you' && <SharedWithYou shares={data.ListSharing.sharesReceived} reload={reload} />}
    </>
  )
}

// What a share is of, in words: the thing's name or "3 items" for passwords,
// and for a sentence ("Memory page: Kim") the kind before it.
function useShareWords() {
  const { t, plural } = useTranslation()
  function kindName(shareKind: ShareKind): string {
    return t(KIND_NAMES[shareKind])
  }
  // Location and Health are shared whole and have no name of their own, so
  // they are called by their kind.
  function subject(share: {
    shareKind: ShareKind
    subjectName: string
    subjectMask: string
    isSubjectDeleted?: boolean
    passwordItemCount: number
  }): string {
    if (share.isSubjectDeleted) {
      return t('sharing.pageDeleted')
    }
    if (share.shareKind === 'passwords') {
      return plural(
        share.passwordItemCount,
        { one: 'sharing.itemsOne', other: 'sharing.itemsOther' },
        { count: share.passwordItemCount },
      )
    }
    if (share.shareKind === 'finance' && share.subjectName) {
      return accountLabel({ accountName: share.subjectName, accountMask: share.subjectMask })
    }
    return share.subjectName || kindName(share.shareKind)
  }
  function what(share: {
    shareKind: ShareKind
    subjectName: string
    subjectMask: string
    isSubjectDeleted?: boolean
    passwordItemCount: number
  }): string {
    return share.shareKind === 'passwords' || !share.subjectName
      ? subject(share)
      : `${kindName(share.shareKind)}: ${subject(share)}`
  }
  // The level and what else it gives, as the kind's own page words them.
  function level(
    share: { shareKind: ShareKind; shareLevel: string; shareScopes: string[] },
    isReceived: boolean,
  ): string {
    const parts: string[] = []
    if (share.shareLevel) {
      const levelName = (isReceived && RECEIVED_LEVEL_NAMES[share.shareLevel]) || LEVEL_NAMES[share.shareLevel]
      parts.push(levelName ? t(levelName) : share.shareLevel)
    }
    for (const scope of share.shareScopes) {
      if (share.shareKind === 'health') {
        parts.push(t(`${HEALTH_CATEGORY_PREFIX}${scope}` as Key))
      } else {
        const scopeName = SCOPE_NAMES[share.shareKind]?.[scope]
        parts.push(scopeName ? t(scopeName) : scope)
      }
    }
    return parts.join(', ')
  }
  return { kindName, subject, what, level }
}

function countWords(plural: ReturnType<typeof useTranslation>['plural'], count: number): string {
  return plural(count, { one: 'sharing.countOne', other: 'sharing.countOther' }, { count })
}

function useStop(reload: () => Promise<unknown>) {
  const { t } = useTranslation()
  const toast = useToast()
  const [isBusy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  async function run(work: () => Promise<unknown>, successMessage: string): Promise<boolean> {
    setBusy(true)
    setProblem(null)
    try {
      await work()
      toast.done(successMessage)
      await reload()
      return true
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : t('circles.failed'))
      return false
    } finally {
      setBusy(false)
    }
  }
  return { run, isBusy, problem, clearProblem: () => setProblem(null) }
}

function SharedByYou({ shares, reload }: { shares: SharingMade[]; reload: () => Promise<unknown> }) {
  const { t, plural } = useTranslation()
  const words = useShareWords()
  const stop = useStop(reload)
  const [stopping, setStopping] = useState<SharingMade | null>(null)

  const granteeName = (share: SharingMade) =>
    share.granteeCircleId ? share.granteeCircleName : share.granteeName || share.granteeUsername

  async function stopShare(share: SharingMade) {
    let work: Promise<unknown>
    switch (share.shareKind) {
      case 'calendar':
        work = graphql(UNSHARE_CALENDAR, { shareId: share.shareId })
        break
      case 'location':
        work = graphql(UNSHARE_LOCATION, { shareId: share.shareId })
        break
      case 'health':
        work = graphql(SET_HEALTH_SHARE, {
          granteeUserId: share.granteeUserId,
          granteeCircleId: share.granteeCircleId,
          healthCategories: [],
        })
        break
      case 'memory_page':
        work = graphql(UNSHARE_AGENT_PAGE, { shareId: share.shareId })
        break
      case 'skill':
        work = graphql(UNSHARE_SKILL, { shareId: share.shareId })
        break
      case 'photo_album':
        work = graphql(REMOVE_PHOTO_ALBUM_SHARE, { photoAlbumShareId: share.shareId })
        break
      case 'recording':
        work = graphql(UNSHARE_RECORDING, { recordingShareId: share.shareId })
        break
      case 'finance':
        work = graphql(UNSHARE_FINANCE_ACCOUNT, { shareId: share.shareId })
        break
      default:
        return
    }
    if (await stop.run(() => work, t('sharing.stopped', { name: granteeName(share) }))) {
      setStopping(null)
    }
  }

  const columns: Column<SharingMade>[] = [
    {
      key: 'shareKind',
      header: t('sharing.kind'),
      width: '9rem',
      filter: 'select',
      options: SHARE_KINDS.map((shareKind) => ({ value: shareKind, label: words.kindName(shareKind) })),
      value: (share) => share.shareKind,
      render: (share) => words.kindName(share.shareKind),
    },
    {
      key: 'what',
      header: t('sharing.what'),
      filter: 'text',
      value: (share) => words.what(share),
      // A page that is gone has nowhere to open; it is listed so its share can be stopped.
      render: (share) =>
        share.isSubjectDeleted ? (
          words.subject(share)
        ) : (
          <Link to={managerAddress(share.shareKind, share.subjectId)}>{words.subject(share)}</Link>
        ),
    },
    {
      key: 'with',
      header: t('sharing.with'),
      filter: 'text',
      value: (share) => granteeName(share),
      sort: (first, second) => granteeName(first).localeCompare(granteeName(second)),
      render: (share) =>
        share.granteeUserId ? (
          <Link to={personPath(share.granteeUserId)}>{granteeName(share)}</Link>
        ) : (
          granteeName(share)
        ),
    },
    {
      key: 'level',
      header: t('sharing.level'),
      value: (share) => words.level(share, false),
      render: (share) => words.level(share, false),
    },
    {
      key: 'since',
      header: t('sharing.since'),
      width: '11rem',
      value: (share) => formatTime(share.createdAt),
      sort: (first, second) => first.createdAt.localeCompare(second.createdAt),
    },
    {
      key: 'actions',
      header: '',
      width: '4rem',
      render: (share) =>
        share.shareKind === 'passwords' ? null : (
          <div className="row-actions">
            <Tooltip label={t('sharing.stop')}>
              <button
                type="button"
                className="icon-action danger"
                aria-label={`${words.what(share)}: ${t('sharing.stopFor', { name: granteeName(share) })}`}
                disabled={stop.isBusy}
                onClick={() => {
                  stop.clearProblem()
                  setStopping(share)
                }}
              >
                <TrashIcon size={16} />
              </button>
            </Tooltip>
          </div>
        ),
    },
  ]

  return (
    <SettingsSection card title={t('sharing.byYou')} description={t('sharing.byYouHint')}>
      <DataTable
        columns={columns}
        rows={shares}
        rowKey={(share) => `${share.shareKind}:${share.shareId || share.granteeUserId || share.granteeCircleId}`}
        emptyMessage={t('sharing.emptyByYou')}
        countLabel={(count) => countWords(plural, count)}
      />
      {stopping && (
        <ConfirmDialog
          title={t('sharing.stopTitle', { name: granteeName(stopping) })}
          body={t('sharing.stopBody', { name: granteeName(stopping), what: words.what(stopping) })}
          confirmLabel={t('sharing.stop')}
          busy={stop.isBusy}
          error={stop.problem}
          onConfirm={() => void stopShare(stopping)}
          onClose={() => setStopping(null)}
        />
      )}
    </SettingsSection>
  )
}

function SharedWithYou({ shares, reload }: { shares: SharingReceived[]; reload: () => Promise<unknown> }) {
  const { t, plural } = useTranslation()
  const words = useShareWords()
  const decline = useStop(reload)
  const [removing, setRemoving] = useState<SharingReceived | null>(null)

  const ownerName = (share: SharingReceived) => share.ownerName || share.ownerUsername

  async function declineShare(share: SharingReceived) {
    if (
      await decline.run(
        () => graphql(DECLINE_SHARE, { shareKind: share.shareKind, shareId: share.shareId }),
        t('sharing.removed'),
      )
    ) {
      setRemoving(null)
    }
  }

  const columns: Column<SharingReceived>[] = [
    {
      key: 'shareKind',
      header: t('sharing.kind'),
      width: '9rem',
      filter: 'select',
      options: SHARE_KINDS.map((shareKind) => ({ value: shareKind, label: words.kindName(shareKind) })),
      value: (share) => share.shareKind,
      render: (share) => words.kindName(share.shareKind),
    },
    {
      key: 'what',
      header: t('sharing.what'),
      filter: 'text',
      value: (share) => words.what(share),
      render: (share) => (
        <Link to={managerAddress(share.shareKind, share.subjectId, share)}>{words.subject(share)}</Link>
      ),
    },
    {
      key: 'from',
      header: t('sharing.from'),
      filter: 'text',
      value: (share) => ownerName(share),
      sort: (first, second) => ownerName(first).localeCompare(ownerName(second)),
      render: (share) => <Link to={personPath(share.ownerUserId)}>{ownerName(share)}</Link>,
    },
    {
      key: 'through',
      header: t('sharing.through'),
      value: (share) => (share.isDirect ? t('sharing.byName') : share.granteeCircleName),
      render: (share) => (
        <>
          {share.isDirect ? t('sharing.byName') : share.granteeCircleName}
          {/* Said in the row, because there is no button: it ends with the
              circle, or with whoever shared it. */}
          {!share.canDecline && (
            <div className="muted">{share.isDirect ? t('sharing.removedByOwner') : t('sharing.endsWithCircle')}</div>
          )}
        </>
      ),
    },
    {
      key: 'level',
      header: t('sharing.level'),
      value: (share) => words.level(share, true),
      render: (share) => words.level(share, true),
    },
    {
      key: 'since',
      header: t('sharing.since'),
      width: '11rem',
      value: (share) => formatTime(share.createdAt),
      sort: (first, second) => first.createdAt.localeCompare(second.createdAt),
    },
    {
      key: 'actions',
      header: '',
      width: '4rem',
      render: (share) =>
        share.canDecline ? (
          <div className="row-actions">
            <Tooltip label={t('sharing.remove')}>
              <button
                type="button"
                className="icon-action danger"
                aria-label={`${words.what(share)}: ${t('sharing.removeFor', { name: ownerName(share) })}`}
                disabled={decline.isBusy}
                onClick={() => {
                  decline.clearProblem()
                  setRemoving(share)
                }}
              >
                <TrashIcon size={16} />
              </button>
            </Tooltip>
          </div>
        ) : null,
    },
  ]

  return (
    <SettingsSection card title={t('sharing.withYou')} description={t('sharing.withYouHint')}>
      <DataTable
        columns={columns}
        rows={shares}
        rowKey={(share) => `${share.shareKind}:${share.shareId || share.ownerUserId}:${share.granteeCircleId ?? ''}`}
        emptyMessage={t('sharing.emptyWithYou')}
        countLabel={(count) => countWords(plural, count)}
      />
      {removing && (
        <ConfirmDialog
          title={t('sharing.removeTitle', { name: ownerName(removing) })}
          body={t('sharing.removeBody', { name: ownerName(removing), what: words.what(removing) })}
          confirmLabel={t('sharing.remove')}
          busy={decline.isBusy}
          error={decline.problem}
          onConfirm={() => void declineShare(removing)}
          onClose={() => setRemoving(null)}
        />
      )}
    </SettingsSection>
  )
}
