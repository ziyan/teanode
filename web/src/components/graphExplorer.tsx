import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react'

import { ConfirmDialog } from './dialog'
import { Tooltip } from './tooltip'
import { ArrowLeftIcon, ExternalIcon, UnlinkIcon } from './icons'
import { ErrorMessage, Loading } from './common'
import { SettingsEmpty } from './settingsList'
import { graphql } from '../api'
import { useQuery } from './useQuery'
import { useToast } from './toast'
import { useTranslation } from '../i18n/i18n'
import { useSession } from '../session'

// The pages around one page, drawn rather than listed.
//
// A list of links answers "what is this linked to". It does not answer
// "and what is that linked to", which is the question somebody reading a
// graph actually has, and which used to mean opening a page to find out
// and then coming back. Clicking a page here re-centres the drawing on
// it and fetches its own links; opening one is a separate, deliberate
// act, so a walk through the neighbourhood never loses the page being
// read.

const NEIGHBOURS = `query ($path: String!) {
  AgentGraphNeighbours(path: $path) {
    node { id path kind name }
    parent { id path kind name }
    neighbours { node { id path kind name } relation outward note status }
    children
  }
}`

const UNLINK = `mutation ($path: String!, $to: String!, $relation: String!) {
  UnlinkAgentNodes(path: $path, to: $to, relation: $relation)
}`

type GraphNode = { id: string; path: string; kind: string; name: string }

type Neighbour = { node: GraphNode; relation: string; outward: boolean; note: string; status: string }

type Neighbourhood = {
  node: GraphNode
  parent: GraphNode | null
  neighbours: Neighbour[]
  children: number
}

// How many pages the drawing holds at once. Past about forty the labels
// are closer together than they are tall; the oldest pages walked through
// drop off the edge rather than the drawing turning into a smudge.
const MAX_NODES = 40

// The arc each side of the drawing gets, in radians -- a little under a
// quarter turn above the horizontal and the same below. The top and the
// bottom are left to the page this one is filed under and to the pages
// filed under it, so those two always read as up and down.
const ARC = 1.0

const LABEL_LIMIT = 18

// Roughly how wide a character of a label is at the size it is drawn,
// used only to keep a label from running out of the box. A Han character
// or a kana is about twice a Latin one. Measuring the text would mean
// drawing it first and reading it back, and being a few pixels out here
// costs nothing that clamping does not fix.
const LATIN_CHARACTER = 6.6
const WIDE_CHARACTER = 12

// What a node is in the drawing. The centre is the page the explorer is
// showing; a seen page is one walked through earlier, kept around so the
// graph grows as it is explored.
type Role = 'centre' | 'parent' | 'children' | 'link' | 'seen'

type Placed = {
  key: string
  role: Role
  // The page itself, kept so that walking onto it files the name the
  // server gave it rather than the shortened one on the drawing. Null for
  // the node that stands for the pages filed under the centre: it is a
  // count, not a page, so there is nothing to open or to walk onto.
  node: GraphNode | null
  path: string
  label: string
  title: string
  x: number
  y: number
  radius: number
  // The words on the line back to the centre and which end the arrow is
  // on. No relation means no line: a page walked through earlier is not
  // claimed to be linked to anything.
  relation: string
  // The same relations as the graph stores them, which is what an unlink
  // has to name. The line above says them in the reader's language, and
  // that is not a thing the server has a word for.
  relations: string[]
  direction: 'out' | 'in' | 'none'
  // Every relation to this page is one the night guessed from a walk, so
  // the line is drawn dashed and the word beside it says so. A page
  // joined by one stated relation and one guessed one is drawn solid: the
  // pages are joined, and only one of the reasons is a guess.
  proposed: boolean
}

// A link somebody has just walked along, kept so it can be taken back
// again. The pair of paths rather than the drawing's own nodes, because
// by the time the button is pressed the drawing has moved on to the far
// end of it.
type Walked = { from: string; fromName: string; to: string; relations: string[]; proposed: boolean }

// messageOf is what went wrong, in words a person can act on.
function messageOf(caught: unknown): string {
  return caught instanceof Error ? caught.message : String(caught)
}

// lastSegment is the name a path carries when the page has none: the leaf
// of "work/portal" rather than the whole of it, because the whole of it is
// in the tooltip and on the page itself.
function lastSegment(path: string): string {
  const parts = path.split('/')
  return parts[parts.length - 1] || path
}

// nameOf is what a node is called on the drawing: the person's own name
// on their page, since the graph calls it "Who they are", and otherwise
// the page's name or the end of its path.
function nameOf(node: { path: string; name: string }, me: string): string {
  if (node.path === 'self' && me) return me
  return node.name || lastSegment(node.path)
}

function shorten(text: string): string {
  return text.length > LABEL_LIMIT ? text.slice(0, LABEL_LIMIT - 1).trimEnd() + '…' : text
}

function labelWidth(label: string): number {
  let width = 0
  for (const character of label) {
    width += (character.codePointAt(0) ?? 0) > 0x2e80 ? WIDE_CHARACTER : LATIN_CHARACTER
  }
  return width
}

// usePhone is the same 600px the stylesheet uses for the drawing's height.
// Both have to agree: the box is laid out in the pixels it is drawn in, so
// that a label is twelve real pixels rather than twelve scaled ones, and a
// viewBox taller than the element would scale them down again.
function usePhone(): boolean {
  const [phone, setPhone] = useState(() => window.matchMedia('(max-width: 600px)').matches)
  useEffect(() => {
    const query = window.matchMedia('(max-width: 600px)')
    const onChange = (event: MediaQueryListEvent) => setPhone(event.matches)
    query.addEventListener('change', onChange)
    return () => query.removeEventListener('change', onChange)
  }, [])
  return phone
}

// useBoxWidth is how wide the drawing actually is. The panel is not the
// window -- the page column is a third of a laptop with the folders beside
// it -- so the width is measured on the element rather than assumed.
function useBoxWidth(element: React.RefObject<HTMLDivElement | null>): number | null {
  const [width, setWidth] = useState<number | null>(null)
  useEffect(() => {
    const box = element.current
    if (!box) {
      return
    }
    const observer = new ResizeObserver((entries) => {
      const measured = entries[0]?.contentRect.width
      if (measured) {
        setWidth(Math.round(measured))
      }
    })
    observer.observe(box)
    return () => observer.disconnect()
  }, [element])
  return width
}

export function GraphExplorer({
  path,
  onOpen,
  version = 0,
}: {
  path: string
  onOpen: (path: string) => void
  // Changed by whoever made a link to this page, to say the
  // neighbourhood is not what it was. A number rather than a key, so the
  // drawing fetches again instead of starting over: somebody three pages
  // into a walk should not be put back at the beginning of it.
  version?: number
}) {
  const me = useSession().name || ''
  const { t } = useTranslation()
  const toast = useToast()
  const box = useRef<HTMLDivElement | null>(null)
  const measured = useBoxWidth(box)
  const phone = usePhone()

  // A marker is referenced by id, and two explorers on one screen must not
  // share one. useId's own value carries punctuation that is not valid in
  // the url() a marker is referenced with, so only its letters and digits
  // are kept.
  const markerId = 'graph-explorer-arrow-' + useId().replace(/[^a-zA-Z0-9]/g, '')

  const [centre, setCentre] = useState(path)
  // The pages walked through on the way here, oldest first. They stay on
  // the drawing without a line to anything: whether they are linked to
  // what is now in the middle is exactly what is not known, and drawing a
  // line would say they are.
  const [seen, setSeen] = useState<GraphNode[]>([])
  const [homeName, setHomeName] = useState(() => lastSegment(path))
  const [walked, setWalked] = useState<Walked | null>(null)
  const [unlinking, setUnlinking] = useState<Walked | null>(null)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState('')

  // Another page in the column is another neighbourhood: the walk that led
  // here was about the page that has just been left.
  useEffect(() => {
    setCentre(path)
    setSeen([])
    setWalked(null)
    setHomeName(lastSegment(path))
  }, [path])

  const around = useQuery(
    () => graphql<{ AgentGraphNeighbours: Neighbourhood | null }>(NEIGHBOURS, { path: centre }),
    [centre, version],
    { refresh: false },
  )
  const neighbourhood = around.data?.AgentGraphNeighbours ?? null

  // The chip back says the page's name rather than its path, which means
  // waiting until the page has been fetched once to learn it.
  useEffect(() => {
    if (neighbourhood && neighbourhood.node.path === path) {
      setHomeName(nameOf(neighbourhood.node, me))
    }
  }, [neighbourhood, path, me])

  const recentre = useCallback(
    (step: Placed) => {
      const next = step.node
      if (!next || next.path === centre) {
        return
      }
      const here = neighbourhood?.node
      setSeen((before) => {
        const kept = before.filter((node) => node.path !== next.path && node.path !== here?.path)
        return (here ? [...kept, here] : kept).slice(-MAX_NODES)
      })
      // Only a stated link can be taken back. Where a page is filed is
      // changed by moving it rather than by unlinking, and a page walked
      // through earlier has no line to the middle at all.
      const takeable = step.role === 'link' ? step.relations.filter((relation) => relation !== 'part_of') : []
      setWalked(
        here && takeable.length > 0
          ? { from: here.path, fromName: nameOf(here, me), to: next.path, relations: takeable, proposed: step.proposed }
          : null,
      )
      setCentre(next.path)
    },
    [centre, me, neighbourhood],
  )

  // relationWords is the relation in the reader's language. The graph takes
  // whatever relation a source states, so one the catalogs have no word for
  // is shown as it is stored rather than as a gap on the line.
  const relationWords = useCallback(
    (relation: string): string => {
      const words: string | undefined = t(`knowledge.relation.${relation}` as 'knowledge.relation.works_on')
      return words || relation.replace(/_/g, ' ')
    },
    [t],
  )

  // proposedWords marks a link the agent guessed rather than one somebody
  // stated. The word goes beside the relation rather than replacing it:
  // what the guess is still matters, and only how much to believe it
  // changes.
  const proposedWords = useCallback(
    (relation: string, proposed: boolean): string =>
      proposed ? t('knowledge.proposedRelation', { relation }) : relation,
    [t],
  )

  // Two pages joined by two relations are one line on the drawing, so
  // taking that line back is a call per relation: leaving one of them
  // behind would redraw the line the press was meant to remove.
  const unlink = async (link: Walked) => {
    setBusy(true)
    setProblem('')
    try {
      for (const relation of link.relations) {
        await graphql(UNLINK, { path: link.from, to: link.to, relation })
      }
      toast.done(t('knowledge.unlinked'))
      setUnlinking(null)
      setWalked(null)
      await around.reload()
    } catch (caught) {
      setProblem(messageOf(caught))
      toast.failed(messageOf(caught))
    } finally {
      setBusy(false)
    }
  }

  const width = measured ?? (phone ? 360 : 720)
  const height = phone ? 300 : 360

  const nodes = useMemo<Placed[]>(() => {
    if (!neighbourhood) {
      return []
    }

    const middleX = width / 2
    const middleY = height / 2
    // Two rings. The first holds as many pages as fit side by side with
    // their labels apart; the rest, and the pages walked through earlier,
    // go out to the second.
    const rings = [
      { x: width * 0.33, y: height * 0.29 },
      { x: width * 0.44, y: height * 0.43 },
    ]
    const innerRing = phone ? 6 : 10
    const radius = phone ? 16 : 20

    const here = neighbourhood.node
    const parent = neighbourhood.parent

    // One entry per page rather than per edge: two relations to the same
    // page are two words on one line, because two lines between the same
    // two circles are drawn on top of each other.
    const links = new Map<
      string,
      { node: GraphNode; relations: string[]; outward: boolean[]; proposed: boolean[]; note: string }
    >()
    for (const neighbour of neighbourhood.neighbours) {
      // Pages filed under this one come back in the same list with no
      // relation. They are one node of their own below: a folder of forty
      // pages drawn as forty circles is not a drawing anybody can read,
      // and the folder's own list is a click away.
      if (!neighbour.relation || neighbour.node.id === here.id) {
        continue
      }
      // The edge to the page this one is filed under, where the graph
      // states it as an edge as well. It is already drawn above.
      if (parent && neighbour.node.id === parent.id) {
        continue
      }
      const already = links.get(neighbour.node.id)
      if (!already) {
        links.set(neighbour.node.id, {
          node: neighbour.node,
          relations: [neighbour.relation],
          outward: [neighbour.outward],
          proposed: [neighbour.status === 'proposed'],
          note: neighbour.note,
        })
        continue
      }
      if (!already.relations.includes(neighbour.relation)) {
        already.relations.push(neighbour.relation)
      }
      already.outward.push(neighbour.outward)
      already.proposed.push(neighbour.status === 'proposed')
      already.note = already.note || neighbour.note
    }

    const fixed = 1 + (parent ? 1 : 0) + (neighbourhood.children > 0 ? 1 : 0)
    const linked = [...links.values()].slice(0, Math.max(0, MAX_NODES - fixed))
    // Whatever room is left goes to the walk, most recent first to be
    // dropped last: the page before this one is the one worth keeping.
    const room = Math.max(0, MAX_NODES - fixed - linked.length)
    const earlier = seen
      .filter(
        (node) =>
          node.path !== here.path && node.path !== parent?.path && !linked.some((link) => link.node.path === node.path),
      )
      .slice(-room)

    const orbit = earlier.length + linked.length
    // Sides alternate so that two links are left and right rather than
    // stacked, and the first ring fills before the second.
    const at = (index: number) => {
      const ring = index < innerRing ? 0 : 1
      const within = ring === 0 ? index : index - innerRing
      const count = ring === 0 ? Math.min(orbit, innerRing) : orbit - innerRing
      const side = within % 2 === 0 ? 1 : -1
      const onSide = Math.floor(within / 2)
      const sideCount = side === 1 ? Math.ceil(count / 2) : Math.floor(count / 2)
      const fraction = sideCount <= 1 ? 0.5 : onSide / (sideCount - 1)
      const angle = -ARC + fraction * ARC * 2
      return {
        x: middleX + side * rings[ring].x * Math.cos(angle),
        y: middleY + rings[ring].y * Math.sin(angle),
      }
    }

    const placed: Placed[] = [
      {
        key: 'centre',
        role: 'centre',
        node: here,
        path: here.path,
        label: shorten(nameOf(here, me)),
        title: here.path,
        x: middleX,
        y: middleY,
        radius: radius + 6,
        relation: '',
        relations: [],
        direction: 'none',
        proposed: false,
      },
    ]

    if (parent) {
      placed.push({
        key: 'parent',
        role: 'parent',
        node: parent,
        path: parent.path,
        label: shorten(nameOf(parent, me)),
        title: parent.path,
        x: middleX,
        y: radius + 10,
        radius,
        relation: relationWords('part_of'),
        relations: ['part_of'],
        direction: 'out',
        proposed: false,
      })
    }

    if (neighbourhood.children > 0) {
      placed.push({
        key: 'children',
        role: 'children',
        node: null,
        path: '',
        label: t('knowledge.moreUnder', { count: neighbourhood.children }),
        title: here.path,
        x: middleX,
        // Its label hangs below it, and the box has no room under that:
        // the baseline has to sit far enough up that a descender is drawn
        // rather than cut off by the edge.
        y: height - radius - 24,
        radius,
        relation: relationWords('part_of'),
        relations: ['part_of'],
        direction: 'in',
        proposed: false,
      })
    }

    linked.forEach((link, index) => {
      const outward = link.outward.every(Boolean) ? 'out' : link.outward.some(Boolean) ? 'none' : 'in'
      const proposed = link.proposed.every(Boolean)
      placed.push({
        key: link.node.id,
        role: 'link',
        node: link.node,
        path: link.node.path,
        label: shorten(nameOf(link.node, me)),
        title: link.note ? `${link.node.path} — ${link.note}` : link.node.path,
        ...at(index),
        radius,
        relation: proposedWords(link.relations.map(relationWords).join(', '), proposed),
        relations: link.relations,
        direction: outward,
        proposed,
      })
    })

    earlier.forEach((node, index) => {
      placed.push({
        key: node.id,
        role: 'seen',
        node,
        path: node.path,
        label: shorten(nameOf(node, me)),
        title: node.path,
        ...at(linked.length + index),
        radius: radius - 6,
        relation: '',
        relations: [],
        direction: 'none',
        proposed: false,
      })
    })

    return placed
  }, [height, me, neighbourhood, phone, proposedWords, relationWords, seen, t, width])

  const middleX = width / 2
  const middleY = height / 2
  const moved = centre !== path
  const bare = nodes.every((node) => node.role === 'centre' || node.role === 'parent')

  // A label is drawn from its middle, so one on a node near the edge would
  // hang out of the box. Sliding it back in leaves it a little off its
  // circle, which is easier to read than a name cut in half.
  const labelAt = (node: Placed) => {
    const half = labelWidth(node.label) / 2 + 4
    return Math.min(Math.max(node.x, half), width - half)
  }

  // The line stops at each circle rather than under it, so the arrow is on
  // the edge of the page it points at.
  const line = (node: Placed) => {
    const span = Math.hypot(node.x - middleX, node.y - middleY) || 1
    const stepX = (node.x - middleX) / span
    const stepY = (node.y - middleY) / span
    const from = nodes[0].radius + 2
    const to = span - node.radius - (node.direction === 'out' ? 8 : 2)
    return {
      x1: middleX + stepX * from,
      y1: middleY + stepY * from,
      x2: middleX + stepX * to,
      y2: middleY + stepY * to,
      // Where the words go: the middle of what is drawn, which is not the
      // middle of the pair when one circle is larger than the other.
      midX: middleX + stepX * ((from + to) / 2),
      midY: middleY + stepY * ((from + to) / 2),
    }
  }

  return (
    <div className="graph-explorer" ref={box}>
      <ErrorMessage error={around.error} />
      {!neighbourhood && around.loading ? <Loading /> : null}
      {!neighbourhood && !around.loading && !around.error ? (
        <SettingsEmpty>{t('knowledge.noLinks')}</SettingsEmpty>
      ) : null}
      {neighbourhood && bare ? <SettingsEmpty>{t('knowledge.noLinks')}</SettingsEmpty> : null}
      {neighbourhood && !bare ? (
        <svg className="graph-explorer-canvas" viewBox={`0 0 ${width} ${height}`}>
          <defs>
            <marker
              id={markerId}
              className="graph-explorer-arrow"
              viewBox="0 0 8 8"
              refX="7"
              refY="4"
              markerWidth="7"
              markerHeight="7"
              orient="auto-start-reverse"
            >
              <path d="M 0 0 L 8 4 L 0 8 z" />
            </marker>
          </defs>
          {nodes.slice(1).map((node) => {
            if (!node.relation) {
              return null
            }
            const edge = line(node)
            return (
              <g key={`edge-${node.key}`}>
                <line
                  className={node.proposed ? 'graph-explorer-edge proposed' : 'graph-explorer-edge'}
                  x1={edge.x1}
                  y1={edge.y1}
                  x2={edge.x2}
                  y2={edge.y2}
                  markerEnd={node.direction === 'out' ? `url(#${markerId})` : undefined}
                  markerStart={node.direction === 'in' ? `url(#${markerId})` : undefined}
                />
                <text className="graph-explorer-relation" x={edge.midX} y={edge.midY - 4} textAnchor="middle">
                  {node.relation}
                </text>
              </g>
            )
          })}
          {nodes.map((node) => {
            const page = node.node
            const open = page ? () => onOpen(page.path) : undefined
            const walk = page && page.path !== centre ? () => recentre(node) : undefined
            return (
              <g
                key={node.key}
                className={`graph-explorer-node ${node.role}`}
                role={walk || open ? 'button' : undefined}
                tabIndex={walk || open ? 0 : undefined}
                aria-label={node.path || node.label}
                onClick={walk}
                onDoubleClick={open}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault()
                    ;(walk ?? open)?.()
                  }
                }}
              >
                <title>{node.title}</title>
                <circle cx={node.x} cy={node.y} r={node.radius} />
                <text x={labelAt(node)} y={node.y + node.radius + 14} textAnchor="middle">
                  {node.label}
                </text>
              </g>
            )
          })}
        </svg>
      ) : null}
      {moved ? (
        <div className="graph-explorer-foot">
          {/* Icons with their words on hover: three sentences of buttons
              under a drawing on a phone were most of the screen, and the
              page names inside them wrapped onto three lines each. */}
          <Tooltip label={t('knowledge.backTo', { name: homeName })}>
            <button
              type="button"
              className="icon-button"
              aria-label={t('knowledge.backTo', { name: homeName })}
              onClick={() => {
                setCentre(path)
                setWalked(null)
              }}
            >
              <ArrowLeftIcon />
            </button>
          </Tooltip>
          <span className="muted graph-explorer-here">{centre}</span>
          <Tooltip label={t('knowledge.openPage')}>
            <button
              type="button"
              className="icon-button"
              aria-label={t('knowledge.openPage')}
              onClick={() => onOpen(centre)}
            >
              <ExternalIcon />
            </button>
          </Tooltip>
          {/* Offered only for the link just walked along, because that is
              the only pair of pages on screen whose join the reader has
              seen stated. */}
          {walked && walked.to === centre ? (
            <Tooltip label={t('knowledge.unlinkFrom', { name: walked.fromName })}>
              <button
                type="button"
                className="icon-button danger"
                aria-label={t('knowledge.unlinkFrom', { name: walked.fromName })}
                disabled={busy}
                onClick={() => {
                  setProblem('')
                  setUnlinking(walked)
                }}
              >
                <UnlinkIcon />
              </button>
            </Tooltip>
          ) : null}
        </div>
      ) : null}
      {unlinking ? (
        <ConfirmDialog
          title={t('knowledge.unlink')}
          body={t('knowledge.unlinkBody', {
            from: unlinking.from,
            to: unlinking.to,
            relation: proposedWords(unlinking.relations.map(relationWords).join(', '), unlinking.proposed),
          })}
          confirmLabel={t('knowledge.unlink')}
          busy={busy}
          error={problem}
          onClose={() => setUnlinking(null)}
          onConfirm={() => void unlink(unlinking)}
        />
      ) : null}
    </div>
  )
}
