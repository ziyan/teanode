import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'

import { Key, useTranslation } from '../i18n/i18n'
import { matchSettingsSurface } from '../pages/settings/nav'
import { ChevronRightIcon } from './icons'

// The section a path belongs to. Derived from the route rather than declared
// by each page, so a new page cannot forget to say where it is — the only
// thing a page supplies is the name of the thing it is showing, which is the
// one part the route cannot know.
//
// Longest prefix first, so /settings/password does not match a shorter entry.
// The trail for a path. Matched longest prefix first, so /settings/password
// wins over /settings, and derived from the route rather than declared by each
// page: a new page cannot forget to say where it is. The only thing a page
// supplies is the name of the thing it is showing, which is the one part the
// route cannot know.
//
// A crumb with no `to` is a grouping rather than a place. Every one of them
// has somewhere to go today, but the shape allows for one that does not.
type Crumb = { label: Key; to?: string }

const TRAILS: { prefix: string; trail: Crumb[] }[] = [
  { prefix: '/domains', trail: [{ label: 'nav.domains', to: '/domains' }] },
  // Every /settings/* page gets its own crumb from SETTINGS_SURFACES below,
  // so there is one entry here rather than one per page.
  { prefix: '/settings', trail: [{ label: 'nav.settings', to: '/settings' }] },
  { prefix: '/mail', trail: [{ label: 'nav.mail', to: '/mail' }] },
  { prefix: '/queue', trail: [{ label: 'nav.queue', to: '/queue' }] },
  { prefix: '/reports', trail: [{ label: 'nav.reports', to: '/reports' }] },
]

// A page belonging to one domain — /domains/<id>/settings — sits two levels
// down: the domain, then the page. Without this the trail ended at the domain
// and read exactly like the domain's own overview, so the settings page looked
// like the page it was reached from and offered no way back to it.
//
// The domain's name is not in the route, so it arrives the same way it always
// does: the page supplies it as the detail. What changes here is that the
// detail becomes a link rather than the end of the trail.
const DOMAIN_PAGES: { suffix: string; label: Key }[] = [
  { suffix: '/settings', label: 'domainOverview.settings' },
  { suffix: '/templates', label: 'templates.title' },
]

// A page belonging to one thing of a domain — a template, a layout — is
// three levels down: the domain, the list it is in, then the thing itself.
// The thing's name is the page's second detail.
const DOMAIN_ITEM_PAGES: { prefix: string; label: Key; list: string }[] = [
  { prefix: '/templates/', label: 'templates.title', list: '/templates' },
  { prefix: '/layouts/', label: 'templates.title', list: '/templates' },
]

// A page under a section that is a page of its own rather than a thing in
// the section: writing a message is under Mail, and is not a message.
const SECTION_PAGES: { path: string; label: Key }[] = [{ path: '/mail/compose', label: 'nav.compose' }]

const SetDetailContext = createContext<((details: string[]) => void) | null>(null)
const DetailContext = createContext<string[]>([])

export function BreadcrumbProvider({ children }: { children: React.ReactNode }) {
  const [detail, setDetail] = useState<string[]>([])
  return (
    <SetDetailContext.Provider value={setDetail}>
      <DetailContext.Provider value={detail}>
        <DocumentTitle />
        {children}
      </DetailContext.Provider>
    </SetDetailContext.Provider>
  )
}

// useBreadcrumbDetail names the thing the page is showing — a domain, a
// subject line — so the trail reads "Domains / example.com" rather than
// "Domains / 01m0b8...".
//
// Passing null, which is what a page does while it is still loading, leaves
// the trail at the section on its own rather than flashing a placeholder.
//
// A second detail names the thing inside the first: a template within its
// domain. It is used only by the pages whose route has a place for it.
export function useBreadcrumbDetail(detail: string | null | undefined, item?: string | null) {
  const setDetail = useContext(SetDetailContext)
  useEffect(() => {
    setDetail?.(detail ? (item ? [detail, item] : [detail]) : [])
    return () => setDetail?.([])
  }, [setDetail, detail, item])
}

// useTrail is where the breadcrumb and the document title agree. Two places
// computing "where am I" separately is two places to drift.
function useTrail(): { label: string; to?: string }[] {
  const { t } = useTranslation()
  const location = useLocation()
  const details = useContext(DetailContext)
  const detail = details[0] ?? null
  const item = details[1] ?? null

  return useMemo(() => {
    const matched = TRAILS.find((candidate) => location.pathname.startsWith(candidate.prefix))
    const crumbs = (matched?.trail ?? []).map((crumb) => ({ label: t(crumb.label), to: crumb.to }))

    // A settings page names itself from the same list the hub renders, rather
    // than each page remembering to. They did not remember: of the six only
    // one did, and the rest read as a bare "Settings".
    const surface = matchSettingsSurface(location.pathname)
    if (surface) {
      crumbs.push({ label: t(surface.label), to: surface.path })
    }
    const sectionPage = SECTION_PAGES.find((candidate) => candidate.path === location.pathname)
    if (sectionPage) {
      crumbs.push({ label: t(sectionPage.label), to: undefined })
    }

    if (detail) {
      const owner = /^\/domains\/([^/]+)(\/[^?#]*)?$/.exec(location.pathname)
      const rest = owner?.[2] ?? ''
      const page = owner && DOMAIN_PAGES.find((candidate) => candidate.suffix === rest)
      if (owner && page) {
        return [...crumbs, { label: detail, to: `/domains/${owner[1]}` }, { label: t(page.label) }]
      }
      const itemPage = owner && DOMAIN_ITEM_PAGES.find((candidate) => rest.startsWith(candidate.prefix))
      if (owner && itemPage) {
        return [
          ...crumbs,
          { label: detail, to: `/domains/${owner[1]}` },
          { label: t(itemPage.label), to: `/domains/${owner[1]}${itemPage.list}` },
          { label: item ?? '…' },
        ]
      }
      return [...crumbs, { label: detail }]
    }
    return crumbs
  }, [location.pathname, detail, item, t])
}

// DocumentTitle keeps the tab label in step with the breadcrumb, most specific
// first: a row of tabs is read left to right and truncated from the right, so
// the part that tells them apart has to come before the part they share.
function DocumentTitle() {
  const { t } = useTranslation()
  const trail = useTrail()

  useEffect(() => {
    // Reversed: a row of tabs is read left to right and truncated from the
    // right, so the part that tells them apart has to come before the part
    // they share.
    const parts = trail.map((crumb) => crumb.label).reverse()
    document.title = [...parts, t('app.name')].join(' · ')
  }, [trail, t])

  return null
}

// PageHeading is the name of what you are looking at, at the top of it.
//
// The same trail the breadcrumb reads, ending at the same place — so there is
// one answer to "where am I" and two ways of showing it: the crumbs on the bar
// say how you got here, and this says what it is. A page with nothing above
// the content reads as a fragment of an application rather than a page of one.
export function PageHeading() {
  const trail = useTrail()
  const last = trail[trail.length - 1]

  if (!last) {
    return null
  }

  return <h1 className="page-heading">{last.label}</h1>
}

export function Breadcrumb() {
  const full = useTrail()

  // Ancestors only, and every one of them a link. The page's own name is the
  // heading right below, in 30px; saying it again immediately above is the
  // same word twice, and on a top-level page it was the only crumb — a trail
  // that went nowhere. What is left is the way back up, which is the part a
  // trail is for.
  const trail = full.slice(0, -1)
  if (trail.length === 0) {
    return null
  }

  return (
    <nav className="breadcrumb" aria-label="breadcrumb">
      {trail.map((crumb, index) => (
        <span className="crumb" key={index}>
          {index > 0 && (
            <span className="separator" aria-hidden="true">
              <ChevronRightIcon size={16} />
            </span>
          )}
          {/* A grouping with no page behind it is not a link. Every crumb has
              somewhere to go today, but the shape allows for one that does
              not. */}
          {crumb.to ? <Link to={crumb.to}>{crumb.label}</Link> : <span>{crumb.label}</span>}
        </span>
      ))}
    </nav>
  )
}
