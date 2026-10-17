import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { graphql, isNotFound } from '../api'
import { Avatar } from '../components/avatar'
import { ErrorMessage, Loading, Tag } from '../components/common'
import { FormDialog } from '../components/dialog'
import { ChevronLeftIcon, CloseIcon } from '../components/icons'
import { Markdown } from '../components/markdown'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../components/settingsList'
import { useToast } from '../components/toast'
import { Tooltip } from '../components/tooltip'
import { useQuery } from '../components/useQuery'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { useTranslation } from '../i18n/i18n'

// Pages other people shared with the person, and sharing the person's own.
//
// What is shared is read where it lives: the owner's rows as they are now,
// read only, never a copy. docs/decisions/20261016-shared-knowledge-is-read-
// where-it-lives.md says why, and docs/subsystems/memory.md ("Sharing") what a
// reader is shown. The server decides all of it (internal/agent/
// shared_memory.go); everything here draws what it was given. A shared page
// has no edit controls because there is nothing here that could write it.

export type SharedRoot = {
  ownerUserId: string
  ownerUsername: string
  ownerName: string
  ownerAvatarUrl: string | null
  ownerAvatarColorIndex: number
  path: string
  name: string
  nodeKind: string
  opening: string
  isQuoteShared: boolean
}

export type SharedFact = {
  number: number
  factKind: string
  text: string
  happenedAt?: string | null
  isInferred: boolean
  evidenceSource: string
  evidenceQuote: string
}

type SharedEdge = {
  fromPath: string
  toPath: string
  fromName: string
  toName: string
  relation: string
  note: string
  isProposed: boolean
}

type SharedChild = { path: string; name: string; nodeKind: string; opening: string }

export type SharedPage = {
  ownerUserId: string
  ownerUsername: string
  ownerName: string
  ownerAvatarUrl: string | null
  ownerAvatarColorIndex: number
  path: string
  name: string
  nodeKind: string
  aliases: string[]
  opening: string
  overview: string
  overviewWrittenAt?: string | null
  isQuoteShared: boolean
  facts: SharedFact[]
  edges: SharedEdge[]
  children: SharedChild[]
}

const PAGE_FIELDS = `ownerUserId ownerUsername ownerName ownerAvatarUrl ownerAvatarColorIndex
    path name nodeKind aliases opening overview overviewWrittenAt isQuoteShared
    facts { number factKind text happenedAt isInferred evidenceSource evidenceQuote }
    edges { fromPath toPath fromName toName relation note isProposed }
    children { path name nodeKind opening }`

const LIST_SHARED = `query { ListSharedAgentPages {
  ownerUserId ownerUsername ownerName ownerAvatarUrl ownerAvatarColorIndex path name nodeKind opening isQuoteShared
} }`

const READ_SHARED = `query ($ownerUserId: String!, $path: String!) {
  ReadSharedAgentPage(ownerUserId: $ownerUserId, path: $path) { ${PAGE_FIELDS} }
}`

const LIST_SHARES = `query { ListAgentPageShares {
  id path pageName granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName isQuoteShared
} }`

const LIST_PEOPLE = `query { ListCirclePeople { userId username name avatarUrl avatarColorIndex } }`
const LIST_CIRCLES = `query { ListCircles { id name } }`

const SHARE = `mutation ($path: String!, $granteeUserId: String, $granteeCircleId: String, $isQuoteShared: Boolean) {
  ShareAgentPage(path: $path, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, isQuoteShared: $isQuoteShared) { id }
}`

const UNSHARE = `mutation ($shareId: String!) { UnshareAgentPage(shareId: $shareId) }`

const PREVIEW = `query ($path: String!, $granteeUserId: String, $granteeCircleId: String, $isQuoteShared: Boolean) {
  PreviewAgentPageShare(path: $path, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, isQuoteShared: $isQuoteShared) {
    page { ${PAGE_FIELDS} }
    reachedPageCount isReachedPageCountCut
  }
}`

// sharedAddress is where a shared page opens: the Knowledge page with the
// owner named, which is how it is told from a page of the person's own at the
// same path.
export function sharedAddress(path: string, ownerUsername: string): string {
  return `/knowledge/${path.split('/').map(encodeURIComponent).join('/')}?owner=${encodeURIComponent(ownerUsername)}`
}

function messageOf(caught: unknown): string {
  return caught instanceof Error ? caught.message : String(caught)
}

function cut(text: string, length: number): string {
  const characters = Array.from(text)
  return characters.length <= length ? text : characters.slice(0, length).join('').trimEnd() + '…'
}

type OwnerGroup = {
  ownerUserId: string
  ownerUsername: string
  ownerName: string
  ownerAvatarUrl: string | null
  ownerAvatarColorIndex: number
  roots: SharedRoot[]
}

// groupByOwner gathers the roots under whoever shared them, in the order the
// server listed them (by owner, then path).
export function groupByOwner(roots: SharedRoot[]): OwnerGroup[] {
  const groups: OwnerGroup[] = []
  for (const root of roots) {
    let group = groups.find((each) => each.ownerUserId === root.ownerUserId)
    if (!group) {
      group = {
        ownerUserId: root.ownerUserId,
        ownerUsername: root.ownerUsername,
        ownerName: root.ownerName,
        ownerAvatarUrl: root.ownerAvatarUrl,
        ownerAvatarColorIndex: root.ownerAvatarColorIndex,
        roots: [],
      }
      groups.push(group)
    }
    group.roots.push(root)
  }
  return groups
}

// SharedList is the pages shared with the person, grouped by who shared them:
// the owner's picture and name, then a row for each root with what it says.
// It draws nothing while it reads, when it fails (the person's own pages are
// still the thing being looked at) and when nothing is shared, so a person in
// no circle carries no empty heading.
export function SharedWithYou({
  selectedOwner = '',
  selectedPath = '',
}: {
  selectedOwner?: string
  selectedPath?: string
}) {
  const { t } = useTranslation()
  const shared = useQuery(() => graphql<{ ListSharedAgentPages: SharedRoot[] }>(LIST_SHARED), [], { refresh: false })
  const groups = groupByOwner(shared.data?.ListSharedAgentPages ?? [])
  if (groups.length === 0) return null
  return (
    <section className="knowledge-shared" aria-label={t('knowledge.shared.title')}>
      <div className="knowledge-list-heading">{t('knowledge.shared.title')}</div>
      {groups.map((group) => (
        <div key={group.ownerUserId} className="knowledge-shared-owner">
          <div className="knowledge-shared-person">
            <Avatar
              name={group.ownerName || group.ownerUsername}
              avatarUrl={group.ownerAvatarUrl}
              colorIndex={group.ownerAvatarColorIndex}
              size={20}
            />
            <span>{group.ownerName || group.ownerUsername}</span>
          </div>
          {group.roots.map((root) => {
            const isSelected = selectedOwner === root.ownerUsername && selectedPath === root.path
            return (
              <Link
                key={root.path}
                to={sharedAddress(root.path, root.ownerUsername)}
                className={isSelected ? 'knowledge-row selected' : 'knowledge-row'}
              >
                <span className="knowledge-row-text">
                  <span className="knowledge-row-name">{root.name || root.path}</span>
                  <span className="knowledge-row-hint">{root.opening ? cut(root.opening, 90) : root.path}</span>
                </span>
              </Link>
            )
          })}
        </div>
      ))}
    </section>
  )
}

// SharedFacts is the facts of a shared page, numbered as the owner numbers
// them, each with where the owner's agent read it and, where the owner chose,
// the words. Nothing on a row changes anything.
function SharedFacts({ facts }: { facts: SharedFact[] }) {
  const { t } = useTranslation()
  if (facts.length === 0) return <SettingsEmpty>{t('knowledge.shared.noFacts')}</SettingsEmpty>
  return (
    <>
      {facts.map((fact) => (
        <SettingsRow
          key={fact.number}
          title={`#${fact.number} ${fact.text}`}
          badge={
            <>
              {fact.factKind !== 'fact' ? (
                <Tag value={t(`knowledge.factKind.${fact.factKind}` as 'knowledge.factKind.preference')} />
              ) : null}
              {fact.isInferred ? <Tag value={t('knowledge.inferred')} tone="warn" /> : null}
            </>
          }
          subtitle={
            fact.evidenceSource || fact.evidenceQuote ? (
              <>
                {fact.evidenceSource ? <span>{fact.evidenceSource}</span> : null}
                {fact.evidenceQuote ? <blockquote className="knowledge-quote">{fact.evidenceQuote}</blockquote> : null}
              </>
            ) : null
          }
        />
      ))}
    </>
  )
}

// SharedPageBody is a shared page as the person reads it, and as the owner
// sees it before they confirm a share: the opening, the overview, the facts,
// the links to other shared pages and the pages under it. The same drawing
// for both, so that the preview is what is read.
export function SharedPageBody({ page, ownerUsername }: { page: SharedPage; ownerUsername?: string }) {
  const { t } = useTranslation()
  const isLinked = Boolean(ownerUsername)
  const open = (path: string) =>
    isLinked ? <Link to={sharedAddress(path, ownerUsername ?? '')}>{path}</Link> : <code>{path}</code>
  return (
    <>
      <SettingsSection card>
        {page.aliases.length > 0 ? (
          <p className="muted">{t('knowledge.alsoCalled', { names: page.aliases.join(', ') })}</p>
        ) : null}
        {page.opening ? (
          <div className="knowledge-summary">
            <Markdown text={page.opening} />
          </div>
        ) : (
          <SettingsEmpty>{t('knowledge.noSummary')}</SettingsEmpty>
        )}
      </SettingsSection>
      {page.overview ? (
        <SettingsSection
          card
          title={t('knowledge.overview')}
          description={t('knowledge.overviewHint', {
            when: page.overviewWrittenAt ? new Date(page.overviewWrittenAt).toLocaleDateString() : '',
          })}
        >
          <div className="knowledge-overview">
            <Markdown text={page.overview} />
          </div>
        </SettingsSection>
      ) : null}
      <SettingsSection card title={t('knowledge.facts')} description={t('knowledge.shared.factsHint')}>
        <SharedFacts facts={page.facts} />
      </SettingsSection>
      {page.edges.length > 0 ? (
        <SettingsSection card title={t('knowledge.shared.links')} description={t('knowledge.shared.linksHint')}>
          {page.edges.map((edge) => {
            const isFrom = edge.fromPath === page.path
            const far = isFrom ? edge.toPath : edge.fromPath
            return (
              <SettingsRow
                key={`${edge.fromPath}>${edge.relation}>${edge.toPath}`}
                title={
                  <>
                    {isFrom ? '→' : '←'} {edge.relation.replace(/_/g, ' ')} {open(far)}
                  </>
                }
                badge={edge.isProposed ? <Tag value={t('memoryLinks.perhaps')} tone="warn" /> : null}
                subtitle={edge.note || null}
              />
            )
          })}
        </SettingsSection>
      ) : null}
      {page.children.length > 0 ? (
        <SettingsSection card title={t('knowledge.shared.under')}>
          {page.children.map((child) => (
            <SettingsRow
              key={child.path}
              title={
                isLinked ? (
                  <Link to={sharedAddress(child.path, ownerUsername ?? '')}>{child.name || child.path}</Link>
                ) : (
                  child.name || child.path
                )
              }
              subtitle={child.opening ? cut(child.opening, 120) : child.path}
            />
          ))}
        </SettingsSection>
      ) : null}
    </>
  )
}

// SharedKnowledge is the Knowledge page when a shared page is open: the list
// of what is shared on the left, in the same frame as the person's own pages,
// and the page, read only, with its owner on the right. Reached with ?owner=
// on a Knowledge address, which is how the agent's links to a shared page and
// the list's rows get here.
export function SharedKnowledge({ owner }: { owner: string }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const path = (useParams()['*'] || '').replace(/^\/+|\/+$/g, '')
  const roots = useQuery(() => graphql<{ ListSharedAgentPages: SharedRoot[] }>(LIST_SHARED), [], { refresh: false })
  const ownerUserId =
    (roots.data?.ListSharedAgentPages ?? []).find((root) => root.ownerUsername.toLowerCase() === owner.toLowerCase())
      ?.ownerUserId ?? ''
  const page = useQuery(
    async () => {
      const key = `${ownerUserId}|${path}`
      if (!ownerUserId || !path) return { key, found: null }
      try {
        return {
          key,
          found: (await graphql<{ ReadSharedAgentPage: SharedPage }>(READ_SHARED, { ownerUserId, path }))
            .ReadSharedAgentPage,
        }
      } catch (caught) {
        // Not shared, or not shared any more, answers as not found: that is
        // a page to say so about, not a failure.
        if (isNotFound(caught)) return { key, found: null }
        throw caught
      }
    },
    [ownerUserId, path],
    { refresh: false },
  )
  const answered = page.data?.key === `${ownerUserId}|${path}`
  const found = answered ? (page.data?.found ?? null) : null
  const isKnown = roots.data !== null
  useBreadcrumbDetail(found ? found.name || found.path : null)

  const nothing = isKnown && (ownerUserId === '' || (answered && !found && !page.loading))
  return (
    <div className="knowledge-frame">
      <div className="knowledge-split reading">
        <div className="knowledge-list">
          <div className="knowledge-trail list-toolbar">
            <Tooltip label={t('knowledge.shared.back')}>
              <button
                type="button"
                className="icon-action"
                aria-label={t('knowledge.shared.back')}
                onClick={() => navigate('/knowledge')}
              >
                <ChevronLeftIcon size={16} />
              </button>
            </Tooltip>
            <span className="knowledge-trail-name">{t('knowledge.shared.title')}</span>
          </div>
          <div className="knowledge-list-rows">
            <SharedWithYou selectedOwner={owner} selectedPath={path} />
          </div>
        </div>
        <div className="knowledge-pane knowledge-page">
          {(page.loading || roots.loading) && !found ? <Loading /> : null}
          {page.error ? <ErrorMessage error={page.error} /> : null}
          {nothing ? (
            <div className="card">
              <h3>{path}</h3>
              <SettingsEmpty>{t('knowledge.shared.noPage')}</SettingsEmpty>
            </div>
          ) : null}
          {found ? (
            <>
              <header className="knowledge-page-head">
                <h3>{found.name || found.path}</h3>
                <div className="knowledge-page-head-meta">
                  <div>
                    <code className="tag knowledge-path">{found.path}</code>{' '}
                    <Tag value={t(`knowledge.kind.${found.nodeKind}` as 'knowledge.kind.person')} />
                    <Tag value={t('knowledge.shared.readOnly')} />
                    {found.isQuoteShared ? <Tag value={t('knowledge.shared.quotesShown')} /> : null}
                  </div>
                  <div className="knowledge-shared-person" data-testid="owner-badge">
                    <Avatar
                      name={found.ownerName || found.ownerUsername}
                      avatarUrl={found.ownerAvatarUrl}
                      colorIndex={found.ownerAvatarColorIndex}
                      size={20}
                    />
                    <span>{t('knowledge.shared.by', { name: found.ownerName || found.ownerUsername })}</span>
                  </div>
                </div>
              </header>
              <SharedPageBody page={found} ownerUsername={found.ownerUsername} />
            </>
          ) : null}
        </div>
      </div>
    </div>
  )
}

// --- sharing the person's own pages -----------------------------------

type Share = {
  id: string
  path: string
  pageName: string
  granteeUserId: string | null
  granteeUsername: string
  granteeName: string
  granteeCircleId: string | null
  granteeCircleName: string
  isQuoteShared: boolean
}

type Person = { userId: string; username: string; name: string }
type CircleChoice = { id: string; name: string }

function shareName(share: Share, everyoneIn: (name: string) => string): string {
  if (share.granteeCircleId) return everyoneIn(share.granteeCircleName)
  return share.granteeName || share.granteeUsername
}

// isCovering says a share of a page reaches the one at path: the page itself
// or one above it, at a slash boundary ("people/a" reaches people/a/x and not
// people/ab).
export function isCovering(shareRoot: string, path: string): boolean {
  return path === shareRoot || path.startsWith(`${shareRoot}/`)
}

// PageShares lists who a page is shared with, the shares of pages above it
// included, since those reach it too, with a way to stop each. Nothing is
// drawn when the page is shared with nobody, so a page carries no heading
// about sharing until it has some. The key changes when the shares may have,
// which reads them again.
export function PageShares({ path, refreshKey }: { path: string; refreshKey: number }) {
  const { t } = useTranslation()
  const toast = useToast()
  const shares = useQuery(() => graphql<{ ListAgentPageShares: Share[] }>(LIST_SHARES), [refreshKey], {
    refresh: false,
  })
  const [busy, setBusy] = useState(false)
  const covering = (shares.data?.ListAgentPageShares ?? []).filter((share) => isCovering(share.path, path))
  if (covering.length === 0) return null
  const everyoneIn = (name: string) => t('knowledge.share.everyoneIn', { name })

  async function stop(share: Share) {
    const name = shareName(share, everyoneIn)
    setBusy(true)
    try {
      await graphql(UNSHARE, { shareId: share.id })
      toast.done(t('knowledge.share.stopped', { name }))
      await shares.reload()
    } catch (caught) {
      toast.failure(caught, t('knowledge.share.failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <SettingsSection card title={t('knowledge.share.shares')} description={t('knowledge.share.sharesHint')}>
      {covering.map((share) => {
        const name = shareName(share, everyoneIn)
        return (
          <SettingsRow
            key={share.id}
            title={name}
            avatar={share.granteeCircleId ? undefined : name}
            badge={share.isQuoteShared ? <Tag value={t('knowledge.shared.quotesShown')} /> : null}
            subtitle={share.path === path ? null : t('knowledge.share.via', { path: share.path })}
            actions={
              <Tooltip label={t('knowledge.share.stop', { name })}>
                <button
                  type="button"
                  className="icon-action danger"
                  aria-label={t('knowledge.share.stop', { name })}
                  disabled={busy}
                  onClick={() => void stop(share)}
                >
                  <CloseIcon size={16} />
                </button>
              </Tooltip>
            }
          />
        )
      })}
    </SettingsSection>
  )
}

type Preview = { PreviewAgentPageShare: { page: SharedPage; reachedPageCount: number; isReachedPageCountCut: boolean } }

// SharePageDialog shares one of the person's pages, and everything under it,
// with a person they share a circle with or a circle they are in. The choices
// are only the people and circles the server would accept. Before they confirm
// they see exactly what the other side will be shown (the server makes the
// preview with the code that makes the share's own view), and whether the
// quotes are in it.
export function SharePageDialog({
  path,
  name,
  onClose,
  onChanged,
}: {
  path: string
  name: string
  onClose: () => void
  onChanged: () => void
}) {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const choices = useQuery(
    async () => {
      const [people, circles] = await Promise.all([
        graphql<{ ListCirclePeople: Person[] }>(LIST_PEOPLE),
        graphql<{ ListCircles: CircleChoice[] }>(LIST_CIRCLES),
      ])
      return { people: people.ListCirclePeople, circles: circles.ListCircles }
    },
    [],
    { refresh: false },
  )
  const [choice, setChoice] = useState('')
  const [isQuoteShared, setIsQuoteShared] = useState(false)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  const people = choices.data?.people ?? []
  const circles = choices.data?.circles ?? []
  const everyoneIn = (circle: string) => t('knowledge.share.everyoneIn', { name: circle })
  const [kind, id] = choice.split(':')
  const grantee = id
    ? { granteeUserId: kind === 'user' ? id : null, granteeCircleId: kind === 'circle' ? id : null }
    : null
  const chosenName =
    kind === 'circle'
      ? everyoneIn(circles.find((each) => each.id === id)?.name ?? '')
      : ((people.find((each) => each.userId === id)?.name || people.find((each) => each.userId === id)?.username) ?? '')

  // What they would see, asked again when the choice or the quotes change.
  const preview = useQuery(
    () =>
      grantee ? graphql<Preview>(PREVIEW, { path, ...grantee, isQuoteShared }) : Promise.resolve<Preview | null>(null),
    [path, choice, isQuoteShared],
    { refresh: false },
  )
  const previewed = preview.data?.PreviewAgentPageShare ?? null

  async function share() {
    if (!grantee) return
    setBusy(true)
    setProblem(null)
    try {
      await graphql(SHARE, { path, ...grantee, isQuoteShared })
      toast.done(t('knowledge.share.said', { name: chosenName }))
      onChanged()
      onClose()
    } catch (caught) {
      setProblem(messageOf(caught))
      toast.failure(caught, t('knowledge.share.failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <FormDialog
      title={t('knowledge.share.title', { name })}
      submitLabel={t('knowledge.share.submit')}
      busy={busy}
      error={problem}
      canSubmit={Boolean(grantee) && previewed !== null}
      wide
      onClose={onClose}
      onSubmit={() => void share()}
    >
      <p className="muted">{t('knowledge.share.intro')}</p>
      {choices.data && people.length === 0 && circles.length === 0 ? (
        <p className="muted">{t('knowledge.share.noPeople')}</p>
      ) : (
        <>
          <label>
            {t('knowledge.share.with')}
            <select value={choice} onChange={(event) => setChoice(event.target.value)}>
              <option value="">{t('knowledge.share.choose')}</option>
              {people.length > 0 && (
                <optgroup label={t('knowledge.share.people')}>
                  {people.map((person) => (
                    <option key={person.userId} value={`user:${person.userId}`}>
                      {person.name || person.username}
                    </option>
                  ))}
                </optgroup>
              )}
              {circles.length > 0 && (
                <optgroup label={t('knowledge.share.circles')}>
                  {circles.map((circle) => (
                    <option key={circle.id} value={`circle:${circle.id}`}>
                      {everyoneIn(circle.name)}
                    </option>
                  ))}
                </optgroup>
              )}
            </select>
          </label>
          <label className="checkbox">
            <input
              type="checkbox"
              checked={isQuoteShared}
              onChange={(event) => setIsQuoteShared(event.target.checked)}
            />
            {t('knowledge.share.quotes')}
          </label>
          <p className="muted">{t('knowledge.share.quotesHint')}</p>
        </>
      )}
      {grantee ? (
        <div className="share-preview" aria-label={t('knowledge.share.preview', { name: chosenName })}>
          <h4>{t('knowledge.share.preview', { name: chosenName })}</h4>
          {preview.loading && !previewed ? <p className="muted">{t('knowledge.share.previewing')}</p> : null}
          {preview.error ? <ErrorMessage error={preview.error} /> : null}
          {previewed ? (
            <>
              <p className="muted">
                {previewed.isReachedPageCountCut
                  ? plural(previewed.reachedPageCount, {
                      one: 'knowledge.share.reachesAtLeastOne',
                      other: 'knowledge.share.reachesAtLeastOther',
                    })
                  : plural(previewed.reachedPageCount, {
                      one: 'knowledge.share.reachesOne',
                      other: 'knowledge.share.reachesOther',
                    })}
              </p>
              <SharedPageBody page={previewed.page} />
            </>
          ) : null}
        </div>
      ) : null}
    </FormDialog>
  )
}

// useRefreshKey is a number that goes up when asked, for a panel that reads
// again after a dialog changed what it shows.
export function useRefreshKey(): [number, () => void] {
  const [key, setKey] = useState(0)
  return [key, () => setKey((before) => before + 1)]
}
