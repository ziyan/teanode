import { useCallback, useEffect, useLayoutEffect, useMemo, useReducer, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { Tag } from '../components/common'
import { SettingsEmpty } from '../components/settingsList'
import { askAgentAbout, graphql } from '../api'
import { useQuery } from '../components/useQuery'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { useSession } from '../session'
import { useToast } from '../components/toast'
import { useTranslation } from '../i18n/i18n'
import { FormDialog } from '../components/dialog'
import { MinusIcon, PlusIcon, SparkIcon } from '../components/icons'

// The whole graph, walked rather than read.
//
// The Connections card on a page answers "what is this linked to" for one
// page at a time, and that is the right size for a card. The question after
// it -- what the shape of the whole thing is, which pages sit between two
// others, what has drifted off on its own -- is not a list at all, and this
// is the page for it.
//
// Everything is drawn inside one <g> that carries the pan and the zoom, and
// the simulation writes positions straight onto the elements rather than
// through React. Three hundred pages re-rendered sixty times a second is the
// whole frame budget spent on bookkeeping; React is told only when the set of
// pages changes, which is when somebody expands one.

// The facts of one page, for the dialog: the liveliest few, in the
// order the page numbers them.
const PAGE_FACTS = `query ($path: String!) {
  AgentGraphPage(path: $path) { facts { id number text } }
}`

const NEIGHBOURS = `query ($path: String!) {
  AgentGraphNeighbours(path: $path) {
    node { id path kind name summary }
    parent { id path kind name }
    neighbours { node { id path kind name summary } relation outward note weight status }
    children
    links
  }
}`

const ROOTS = `query ($path: String!, $first: Int) {
  AgentGraphChildren(path: $path, first: $first) {
    rows { node { id path kind name summary } }
  }
}`

const SEARCH = `query ($query: String!, $first: Int) {
  SearchAgentGraph(query: $query, first: $first) {
    nodes { id path kind name }
  }
}`

type GraphNode = { id: string; path: string; kind: string; name: string; summary?: string }

type Neighbour = {
  node: GraphNode
  relation: string
  outward: boolean
  note: string
  weight: number
  status: string
}

type Neighbourhood = {
  node: GraphNode
  parent: GraphNode | null
  neighbours: Neighbour[]
  children: number
  links: number
}

// A page on the drawing.
//
// Mutable on purpose, and held in a ref rather than in state: the simulation
// moves every one of these sixty times a second, and copying the array to
// tell React about it would be the frame.
type Drawn = {
  path: string
  kind: string
  name: string
  summary: string
  x: number
  y: number
  velocityX: number
  velocityY: number
  // How many edges touch it, which is what its size says. A fact count
  // would be better -- a page of forty facts is a page somebody uses --
  // but the neighbourhood query does not carry one.
  degree: number
  // How many pages are filed under it, as of the last time it was
  // expanded. -1 until that has been asked.
  children: number
  // How many of its links were not drawn. A page linked to hundreds of
  // others is given the strongest of them, and without this the drawing
  // showed part of its neighbourhood and nothing said so: the badge that
  // means "there is more here" goes out when a page is expanded.
  linksLeftOut: number
  expanded: boolean
  // Held under a finger. The simulation reads its position instead of
  // writing it, so a page being dragged does not fight the springs.
  held: boolean
}

// A line between two pages: either something somebody stated, or where a
// page is filed.
type Joined = {
  key: string
  from: string
  to: string
  relation: string
  note: string
  weight: number
  containment: boolean
  // A link the night guessed from a walk rather than one anybody stated.
  // Drawn dashed, and said as a guess wherever it is named: nothing
  // promotes it but the person making the same link themselves.
  proposed: boolean
}

// edgeClasses is how a line is drawn: where a page is filed, a link
// somebody stated, or one the night proposed and nobody has confirmed.
function edgeClasses(edge: Joined): string {
  if (edge.containment) {
    return 'graph-explore-edge under'
  }
  return edge.proposed ? 'graph-explore-edge proposed' : 'graph-explore-edge'
}

// The kinds, in the order the legend reads them: the person, then who and
// what they know, then the structure the rest is filed into. Anything the
// server grows later falls through to the "other" colour rather than
// vanishing.
const KINDS = ['self', 'person', 'organization', 'project', 'topic', 'place', 'thing', 'period', 'folder']

// The simulation. Velocity Verlet with three forces: every page pushes every
// other away, every line pulls its two ends together, and the middle pulls on
// everything so a graph that has come apart does not drift off the canvas.
//
// The numbers are in the drawing's own units, where a page is about ten
// across, and were chosen by watching a real graph settle: springs any
// stiffer and the whole thing rings, repulsion any weaker and a folder's
// children sit on top of each other.
// Gravity and repulsion together decide how big the cloud is: three hundred
// pages settle into a disc about a thousand across, which is a page apart at
// the zoom a name is readable at. Stronger gravity packs them until the
// labels overlap, weaker and the drawing walks off the canvas.
const REPULSION = 4200
const SPRING = 0.05
const LINK_REST = 130
const CHILD_REST = 78
const GRAVITY = 0.008
const DAMPING = 0.84

// Past this the push is too small to move anything and is skipped, which is
// what keeps the pair loop affordable on a large graph.
const REPULSION_RANGE = 520

// alpha is how much of the computed movement is actually applied. It decays
// every frame, so the drawing settles instead of shivering for ever; adding
// pages sets it back up a little, which unfolds them without throwing the
// pages already placed across the canvas.
const ALPHA_DECAY = 0.975
const ALPHA_REST = 0.01
// How long two presses may be apart and still be one gesture.
const DOUBLE_PRESS_MILLISECONDS = 500
const ALPHA_ADDED = 0.45

const MIN_ZOOM = 0.15
const MAX_ZOOM = 3
const ZOOM_STEP = 1.25

// The label size in screen pixels, and the zooms at which names and
// relations are worth drawing. Below the first, the names are closer
// together than they are tall and read as a smudge; the relation on a line
// only fits once the line is long enough on screen to hold it.
const LABEL_SIZE = 12
const NAME_ZOOM = 0.5
const RELATION_ZOOM = 1.15

const LABEL_LIMIT = 22

// The angle between one new page and the next, so that a handful arriving at
// once are spread around their source rather than stacked on one spoke.
const GOLDEN_ANGLE = 2.399963

// How many pages the drawing holds. A graph this size is already past what
// anybody reads at once, and the honest failure is to stop adding rather
// than to grind.
const MAX_NODES = 400

// messageOf is what went wrong, in words a person can act on.
function messageOf(caught: unknown): string {
  return caught instanceof Error ? caught.message : String(caught)
}

function lastSegment(path: string): string {
  const parts = path.split('/')
  return parts[parts.length - 1] || path
}

// nameOf is what a page is called on the drawing: the person's own name on
// their page, since the graph calls it "Who they are", and otherwise the
// page's name or the end of its path.
function nameOf(node: { path: string; name: string }, me: string): string {
  if (node.path === 'self' && me) return me
  return node.name || lastSegment(node.path)
}

function shorten(text: string): string {
  return text.length > LABEL_LIMIT ? text.slice(0, LABEL_LIMIT - 1).trimEnd() + '…' : text
}

function clamp(value: number, least: number, most: number): number {
  return Math.min(most, Math.max(least, value))
}

// radiusOf sizes a page by how much meets at it. The square root rather than
// the count: a page with sixteen links is not sixteen times the page with
// one, and drawn that way it would be the only thing on the canvas.
function radiusOf(node: Drawn): number {
  return 9 + Math.min(11, Math.sqrt(node.degree) * 3)
}

// settle is one step of the simulation, in place.
function settle(nodes: Drawn[], edges: Joined[], byPath: Map<string, Drawn>, alpha: number) {
  for (let index = 0; index < nodes.length; index++) {
    const node = nodes[index]
    for (let other = index + 1; other < nodes.length; other++) {
      const far = nodes[other]
      let apartX = far.x - node.x
      let apartY = far.y - node.y
      let square = apartX * apartX + apartY * apartY
      if (square < 0.01) {
        // Two pages that arrived at exactly the same point would push each
        // other nowhere for ever. Nudged apart by their index, so the same
        // graph always settles the same way.
        apartX = (index % 5) - 2.5
        apartY = (other % 5) - 2.5
        square = apartX * apartX + apartY * apartY
      }
      if (square > REPULSION_RANGE * REPULSION_RANGE) {
        continue
      }
      const distance = Math.sqrt(square)
      const push = REPULSION / square
      const stepX = (apartX / distance) * push
      const stepY = (apartY / distance) * push
      node.velocityX -= stepX
      node.velocityY -= stepY
      far.velocityX += stepX
      far.velocityY += stepY
    }
  }

  for (const edge of edges) {
    const from = byPath.get(edge.from)
    const to = byPath.get(edge.to)
    if (!from || !to) {
      continue
    }
    const apartX = to.x - from.x
    const apartY = to.y - from.y
    const distance = Math.hypot(apartX, apartY) || 1
    const rest = edge.containment ? CHILD_REST : LINK_REST
    // A link the night only proposed is weaker than one somebody stated, and
    // pulls less hard: the drawing should say which joins are load-bearing.
    const strength = edge.containment ? 1.2 : clamp(edge.weight || 1, 0.4, 2)
    const pull = ((distance - rest) / distance) * SPRING * strength
    from.velocityX += apartX * pull
    from.velocityY += apartY * pull
    to.velocityX -= apartX * pull
    to.velocityY -= apartY * pull
  }

  for (const node of nodes) {
    if (node.held) {
      node.velocityX = 0
      node.velocityY = 0
      continue
    }
    node.velocityX = (node.velocityX - node.x * GRAVITY) * DAMPING
    node.velocityY = (node.velocityY - node.y * GRAVITY) * DAMPING
    node.x += node.velocityX * alpha
    node.y += node.velocityY * alpha
  }
}

export function KnowledgeExplorePage() {
  const { t } = useTranslation()
  const toast = useToast()
  const navigate = useNavigate()
  const me = useSession().name || ''
  const [parameters] = useSearchParams()
  const from = parameters.get('from') ?? ''

  useBreadcrumbDetail(t('knowledge.explore.title'))

  // The drawing itself. Held in refs and redrawn by hand, for the reason at
  // the top of the file; `redraw` is what tells React the set of pages has
  // changed, and nothing else in here is state.
  const nodes = useRef<Drawn[]>([])
  const edges = useRef<Joined[]>([])
  const byPath = useRef(new Map<string, Drawn>())
  const [, redraw] = useReducer((count: number) => count + 1, 0)

  const [selected, setSelected] = useState('')
  const [loading, setLoading] = useState(true)
  const [query, setQuery] = useState('')
  const [found, setFound] = useState<GraphNode[] | null>(null)

  const frame = useRef<HTMLDivElement | null>(null)
  const surface = useRef<SVGSVGElement | null>(null)
  const zoomLayer = useRef<SVGGElement | null>(null)
  const nodeElements = useRef(new Map<string, SVGGElement>())
  const edgeLines = useRef(new Map<string, SVGLineElement>())
  const edgeLabels = useRef(new Map<string, SVGTextElement>())

  const view = useRef({ x: 0, y: 0, k: 1 })
  const size = useRef({ width: 0, height: 0 })
  const alpha = useRef(0)
  const running = useRef(0)
  // Set while the first pages are still finding their places, so the view is
  // framed once around whatever they settled into rather than while they are
  // still flying apart.
  const fitWhenSettled = useRef(true)
  const expanding = useRef(new Set<string>())

  // --- the view ----------------------------------------------------------

  const applyView = useCallback(() => {
    const { x, y, k } = view.current
    zoomLayer.current?.setAttribute('transform', `translate(${x.toFixed(2)},${y.toFixed(2)}) scale(${k.toFixed(4)})`)
    const canvas = surface.current
    if (!canvas) {
      return
    }
    // The labels live inside the scaled group, so their size is divided by
    // the zoom to come out the same on screen at every zoom. One custom
    // property rather than an attribute per label.
    canvas.style.setProperty('--graph-explore-label', `${(LABEL_SIZE / k).toFixed(2)}px`)
    canvas.classList.toggle('names', k >= NAME_ZOOM)
    canvas.classList.toggle('relations', k >= RELATION_ZOOM)
  }, [])

  const paint = useCallback(() => {
    for (const node of nodes.current) {
      nodeElements.current
        .get(node.path)
        ?.setAttribute('transform', `translate(${node.x.toFixed(1)},${node.y.toFixed(1)})`)
    }
    for (const edge of edges.current) {
      const line = edgeLines.current.get(edge.key)
      const from = byPath.current.get(edge.from)
      const to = byPath.current.get(edge.to)
      if (!line || !from || !to) {
        continue
      }
      const apartX = to.x - from.x
      const apartY = to.y - from.y
      const distance = Math.hypot(apartX, apartY) || 1
      // The line stops at each circle rather than under it, so an arrowhead
      // lands on the edge of the page it points at and not inside it.
      const startX = from.x + (apartX / distance) * radiusOf(from)
      const startY = from.y + (apartY / distance) * radiusOf(from)
      const reach = distance - radiusOf(to) - (edge.containment ? 1 : 8)
      const endX = from.x + (apartX / distance) * Math.max(reach, radiusOf(from))
      const endY = from.y + (apartY / distance) * Math.max(reach, radiusOf(from))
      line.setAttribute('x1', startX.toFixed(1))
      line.setAttribute('y1', startY.toFixed(1))
      line.setAttribute('x2', endX.toFixed(1))
      line.setAttribute('y2', endY.toFixed(1))
      const label = edgeLabels.current.get(edge.key)
      if (label) {
        label.setAttribute('x', ((startX + endX) / 2).toFixed(1))
        label.setAttribute('y', ((startY + endY) / 2 - 3).toFixed(1))
      }
    }
  }, [])

  const fit = useCallback(() => {
    const { width, height } = size.current
    if (width === 0 || height === 0) {
      return
    }
    if (nodes.current.length === 0) {
      view.current = { x: width / 2, y: height / 2, k: 1 }
      applyView()
      return
    }
    let leftMost = Infinity
    let rightMost = -Infinity
    let topMost = Infinity
    let bottomMost = -Infinity
    for (const node of nodes.current) {
      const radius = radiusOf(node) + 28
      leftMost = Math.min(leftMost, node.x - radius)
      rightMost = Math.max(rightMost, node.x + radius)
      topMost = Math.min(topMost, node.y - radius)
      bottomMost = Math.max(bottomMost, node.y + radius)
    }
    const spanX = Math.max(rightMost - leftMost, 1)
    const spanY = Math.max(bottomMost - topMost, 1)
    // Never magnified past life size by fitting: a graph of three pages
    // blown up to fill a laptop reads as a mistake.
    const zoom = clamp(Math.min(width / spanX, height / spanY), MIN_ZOOM, 1)
    view.current = {
      k: zoom,
      x: width / 2 - ((leftMost + rightMost) / 2) * zoom,
      y: height / 2 - ((topMost + bottomMost) / 2) * zoom,
    }
    applyView()
  }, [applyView])

  // Fullscreen is the browser's: the frame becomes the screen, the
  // canvas fills it (see the stylesheet), and the drawing is refitted
  // once the size has settled. Escape leaves, as it does everywhere.
  const toggleFullscreen = useCallback(() => {
    const box = frame.current
    if (!box) return
    if (document.fullscreenElement) {
      void document.exitFullscreen()
    } else {
      void box.requestFullscreen()
    }
  }, [])
  useEffect(() => {
    const onChange = () => {
      window.setTimeout(() => {
        const box = frame.current
        if (box) {
          size.current = { width: box.clientWidth, height: box.clientHeight }
        }
        fit()
      }, 150)
    }
    document.addEventListener('fullscreenchange', onChange)
    return () => document.removeEventListener('fullscreenchange', onChange)
  }, [fit])

  const runFrame = useCallback<() => void>(() => {
    settle(nodes.current, edges.current, byPath.current, alpha.current)
    paint()
    alpha.current *= ALPHA_DECAY
    if (alpha.current > ALPHA_REST) {
      running.current = requestAnimationFrame(runFrame)
      return
    }
    running.current = 0
    if (fitWhenSettled.current) {
      fitWhenSettled.current = false
      fit()
    }
  }, [fit, paint])

  // kick starts the simulation again, or keeps it going. Never past what is
  // already in flight: a drag arriving every frame must not restart the
  // layout from the beginning under the finger.
  const kick = useCallback(
    (amount: number) => {
      alpha.current = Math.max(alpha.current, amount)
      if (running.current === 0) {
        running.current = requestAnimationFrame(runFrame)
      }
    },
    [runFrame],
  )

  useEffect(() => {
    return () => {
      if (running.current !== 0) {
        cancelAnimationFrame(running.current)
        running.current = 0
      }
    }
  }, [])

  const zoomAt = useCallback(
    (pointerX: number, pointerY: number, factor: number) => {
      const zoom = clamp(view.current.k * factor, MIN_ZOOM, MAX_ZOOM)
      const scale = zoom / view.current.k
      // The point under the pointer stays under the pointer, which is the
      // only zoom that does not feel like the drawing running away.
      view.current = {
        k: zoom,
        x: pointerX - (pointerX - view.current.x) * scale,
        y: pointerY - (pointerY - view.current.y) * scale,
      }
      applyView()
    },
    [applyView],
  )

  const zoomBy = useCallback(
    (factor: number) => zoomAt(size.current.width / 2, size.current.height / 2, factor),
    [zoomAt],
  )

  const centreOn = useCallback(
    (path: string) => {
      const node = byPath.current.get(path)
      if (!node) {
        return
      }
      view.current = {
        k: view.current.k,
        x: size.current.width / 2 - node.x * view.current.k,
        y: size.current.height / 2 - node.y * view.current.k,
      }
      applyView()
    },
    [applyView],
  )

  // --- the box the drawing lives in --------------------------------------

  // The canvas takes what is left of the window under the page's heading,
  // and no more: a page that scrolls would fight every drag on it. The room
  // available is measured against the scrolling area's own padding rather
  // than guessed from the breakpoints, which are three numbers that would
  // then have to agree with the stylesheet for ever.
  useLayoutEffect(() => {
    const box = frame.current
    if (!box) {
      return
    }
    const measure = () => {
      const content = box.closest('.content')
      let bottom = window.innerHeight
      if (content instanceof HTMLElement) {
        bottom = content.getBoundingClientRect().bottom - Number.parseFloat(getComputedStyle(content).paddingBottom)
      }
      box.style.height = `${Math.max(320, Math.round(bottom - box.getBoundingClientRect().top))}px`
      size.current = { width: box.clientWidth, height: box.clientHeight }
    }
    measure()
    if (view.current.k === 1 && view.current.x === 0 && view.current.y === 0) {
      view.current = { x: size.current.width / 2, y: size.current.height / 2, k: 1 }
      applyView()
    }
    const observer = new ResizeObserver(() => {
      size.current = { width: box.clientWidth, height: box.clientHeight }
    })
    observer.observe(box)
    window.addEventListener('resize', measure)
    return () => {
      observer.disconnect()
      window.removeEventListener('resize', measure)
    }
  }, [applyView])

  // --- what is on the drawing --------------------------------------------

  // ensure is the one way a page joins the drawing. A page already on it
  // keeps its position and takes whatever the new answer knows that the old
  // one did not.
  const ensure = useCallback((node: GraphNode, near: { x: number; y: number } | null): Drawn | null => {
    const already = byPath.current.get(node.path)
    if (already) {
      already.name = node.name || already.name
      already.kind = node.kind || already.kind
      if (node.summary) {
        already.summary = node.summary
      }
      return already
    }
    if (nodes.current.length >= MAX_NODES) {
      return null
    }
    // Around whatever it came from, on a spiral rather than at random: the
    // same graph then settles into the same picture twice running, which
    // is what makes it a drawing somebody can recognize.
    const angle = nodes.current.length * GOLDEN_ANGLE
    const spread = near ? 70 + (nodes.current.length % 5) * 8 : 150
    const made: Drawn = {
      path: node.path,
      kind: node.kind || 'thing',
      name: node.name,
      summary: node.summary ?? '',
      x: (near?.x ?? 0) + Math.cos(angle) * spread,
      y: (near?.y ?? 0) + Math.sin(angle) * spread,
      velocityX: 0,
      velocityY: 0,
      degree: 0,
      children: -1,
      linksLeftOut: 0,
      expanded: false,
      held: false,
    }
    nodes.current.push(made)
    byPath.current.set(made.path, made)
    return made
  }, [])

  const recount = useCallback(() => {
    for (const node of nodes.current) {
      node.degree = 0
    }
    for (const edge of edges.current) {
      const from = byPath.current.get(edge.from)
      const to = byPath.current.get(edge.to)
      if (from) from.degree += 1
      if (to) to.degree += 1
    }
  }, [])

  // join adds a line, once. A page has exactly one parent, so where it is
  // filed is keyed by the child alone: the same containment arrives as the
  // parent of one page and as a child of the other, and drawn twice it is
  // two lines lying on top of each other with arrows at both ends.
  const join = useCallback((edge: Joined) => {
    if (edges.current.some((already) => already.key === edge.key)) {
      return
    }
    if (!edge.containment) {
      const mirror = `${edge.relation}|${edge.to}|${edge.from}`
      if (edges.current.some((already) => already.key === mirror)) {
        return
      }
    }
    edges.current.push(edge)
  }, [])

  const contain = useCallback(
    (parentPath: string, childPath: string) => {
      join({
        key: `under|${childPath}`,
        from: parentPath,
        to: childPath,
        relation: 'part_of',
        note: '',
        weight: 1,
        containment: true,
        proposed: false,
      })
    },
    [join],
  )

  const expand = useCallback(
    async (path: string) => {
      if (!path || expanding.current.has(path)) {
        return
      }
      expanding.current.add(path)
      try {
        const answer = await graphql<{ AgentGraphNeighbours: Neighbourhood | null }>(NEIGHBOURS, { path })
        const around = answer.AgentGraphNeighbours
        if (!around) {
          toast.failed(t('knowledge.noPage'))
          return
        }
        const here = ensure(around.node, byPath.current.get(path) ?? null)
        if (!here) {
          return
        }
        here.expanded = true
        here.children = around.children
        // What came back against what the page has. The server gives the
        // strongest links rather than all of them, so a page can be
        // expanded and still have more behind it.
        here.linksLeftOut = Math.max(0, around.links - around.neighbours.filter((one) => one.relation).length)
        if (around.parent) {
          if (ensure(around.parent, here)) {
            contain(around.parent.path, here.path)
          }
        }
        for (const neighbour of around.neighbours) {
          if (neighbour.node.path === here.path) {
            continue
          }
          const other = ensure(neighbour.node, here)
          if (!other) {
            continue
          }
          if (!neighbour.relation) {
            contain(here.path, other.path)
            continue
          }
          // The graph states containment as an edge as well as in the path.
          // Either way round it is the same line, drawn once.
          if (neighbour.relation === 'part_of') {
            if (neighbour.outward) {
              contain(other.path, here.path)
            } else {
              contain(here.path, other.path)
            }
            continue
          }
          join({
            key: `${neighbour.relation}|${neighbour.outward ? here.path : other.path}|${
              neighbour.outward ? other.path : here.path
            }`,
            from: neighbour.outward ? here.path : other.path,
            to: neighbour.outward ? other.path : here.path,
            relation: neighbour.relation,
            note: neighbour.note,
            weight: neighbour.weight,
            containment: false,
            proposed: neighbour.status === 'proposed',
          })
        }
        recount()
        redraw()
        kick(ALPHA_ADDED)
      } catch (caught) {
        toast.failed(messageOf(caught))
      } finally {
        expanding.current.delete(path)
      }
    },
    [contain, ensure, join, kick, recount, t, toast],
  )

  // collapse folds a page back up: the pages hanging off it that hang off
  // nothing else go, and the page itself stays. What is left is what was
  // there before it was expanded, which is the only collapse that does not
  // take somebody else's walk away with it.
  const collapse = useCallback(
    (path: string) => {
      const node = byPath.current.get(path)
      if (!node) {
        return
      }
      const touching = new Map<string, number>()
      for (const edge of edges.current) {
        touching.set(edge.from, (touching.get(edge.from) ?? 0) + 1)
        touching.set(edge.to, (touching.get(edge.to) ?? 0) + 1)
      }
      const dropped = new Set<string>()
      for (const edge of edges.current) {
        const other = edge.from === path ? edge.to : edge.to === path ? edge.from : ''
        if (!other || other === path) {
          continue
        }
        if ((touching.get(other) ?? 0) <= 1) {
          dropped.add(other)
        }
      }
      if (dropped.size === 0) {
        node.expanded = false
        redraw()
        return
      }
      nodes.current = nodes.current.filter((candidate) => !dropped.has(candidate.path))
      edges.current = edges.current.filter((edge) => !dropped.has(edge.from) && !dropped.has(edge.to))
      for (const gone of dropped) {
        byPath.current.delete(gone)
        nodeElements.current.delete(gone)
      }
      node.expanded = false
      if (dropped.has(selected)) {
        setSelected('')
      }
      recount()
      redraw()
      kick(0.15)
    },
    [kick, recount, selected],
  )

  // The first pages: the one the page was opened from, or the roots, which
  // are the eight folders everything else is filed under.
  useEffect(() => {
    let alive = true
    setLoading(true)
    const start = async () => {
      try {
        if (from) {
          // Expanded and centred, not selected: selecting opens the
          // dialog, and arriving under a dialog is arriving nowhere.
          await expand(from)
          return
        }
        const answer = await graphql<{ AgentGraphChildren: { rows: { node: GraphNode }[] } }>(ROOTS, {
          path: '',
          first: 100,
        })
        if (!alive) {
          return
        }
        for (const row of answer.AgentGraphChildren.rows) {
          ensure(row.node, null)
        }
        recount()
        redraw()
        kick(1)
      } catch (caught) {
        if (alive) {
          toast.failed(messageOf(caught))
        }
      } finally {
        if (alive) {
          setLoading(false)
        }
      }
    }
    void start()
    return () => {
      alive = false
    }
    // Once per page opened from, and never again: everything after the first
    // answer is somebody expanding something.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [from])

  // --- the pointer -------------------------------------------------------

  const pointers = useRef(new Map<number, { x: number; y: number }>())
  const dragging = useRef<{ path: string; moved: number } | null>(null)
  // A press that may turn out to be the first of two: the selection waits
  // a moment, and a second press on the same page in that moment expands
  // it instead. Detected here rather than from the browser's double
  // click, because the canvas takes pointer capture on the first press
  // and the double click then names the canvas, not the page; and a
  // finger has no double click at all.
  const pressed = useRef<{ path: string; timer: number } | null>(null)
  const panning = useRef(false)
  const pinching = useRef<number | null>(null)

  const pathUnder = (target: EventTarget | null): string => {
    if (!(target instanceof Element)) {
      return ''
    }
    return target.closest('[data-path]')?.getAttribute('data-path') ?? ''
  }

  const gap = (): { distance: number; midX: number; midY: number } => {
    const [first, second] = [...pointers.current.values()]
    return {
      distance: Math.hypot(second.x - first.x, second.y - first.y) || 1,
      midX: (first.x + second.x) / 2,
      midY: (first.y + second.y) / 2,
    }
  }

  const onPointerDown = (event: React.PointerEvent<SVGSVGElement>) => {
    surface.current?.setPointerCapture(event.pointerId)
    pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY })
    if (pointers.current.size === 2) {
      // A second finger turns whatever was happening into a pinch, and the
      // page that was under the first finger is put down where it is.
      const held = dragging.current && byPath.current.get(dragging.current.path)
      if (held) {
        held.held = false
      }
      dragging.current = null
      panning.current = false
      pinching.current = gap().distance
      return
    }
    if (pointers.current.size > 2) {
      return
    }
    const path = pathUnder(event.target)
    if (path) {
      const node = byPath.current.get(path)
      if (node) {
        node.held = true
        dragging.current = { path, moved: 0 }
        kick(0.2)
        return
      }
    }
    panning.current = true
  }

  const onPointerMove = (event: React.PointerEvent<SVGSVGElement>) => {
    const before = pointers.current.get(event.pointerId)
    if (!before) {
      return
    }
    pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY })
    if (pointers.current.size >= 2) {
      if (pinching.current !== null) {
        const now = gap()
        const rectangle = surface.current?.getBoundingClientRect()
        zoomAt(now.midX - (rectangle?.left ?? 0), now.midY - (rectangle?.top ?? 0), now.distance / pinching.current)
        pinching.current = now.distance
      }
      return
    }
    const alongX = event.clientX - before.x
    const alongY = event.clientY - before.y
    const held = dragging.current
    if (held) {
      const node = byPath.current.get(held.path)
      if (node) {
        node.x += alongX / view.current.k
        node.y += alongY / view.current.k
        held.moved += Math.abs(alongX) + Math.abs(alongY)
        kick(0.15)
      }
      return
    }
    if (panning.current) {
      view.current = { ...view.current, x: view.current.x + alongX, y: view.current.y + alongY }
      applyView()
    }
  }

  const onPointerUp = (event: React.PointerEvent<SVGSVGElement>) => {
    pointers.current.delete(event.pointerId)
    if (surface.current?.hasPointerCapture(event.pointerId)) {
      surface.current.releasePointerCapture(event.pointerId)
    }
    if (pointers.current.size < 2) {
      pinching.current = null
    }
    const held = dragging.current
    if (held) {
      const node = byPath.current.get(held.path)
      if (node) {
        node.held = false
      }
      // A press that did not move is a press, not a drag. Four pixels of
      // slack, because a finger never lands and lifts on the same pixel.
      if (held.moved < 5) {
        if (event.shiftKey) {
          collapse(held.path)
        } else if (pressed.current && pressed.current.path === held.path) {
          window.clearTimeout(pressed.current.timer)
          pressed.current = null
          void expand(held.path)
        } else {
          if (pressed.current) {
            window.clearTimeout(pressed.current.timer)
          }
          const path = held.path
          pressed.current = {
            path,
            timer: window.setTimeout(() => {
              pressed.current = null
              setSelected(path)
            }, DOUBLE_PRESS_MILLISECONDS),
          }
        }
      }
      dragging.current = null
      kick(0.15)
    }
    panning.current = false
  }

  const onDoubleClick = (event: React.MouseEvent<SVGSVGElement>) => {
    const path = pathUnder(event.target)
    if (path) {
      void expand(path)
    }
  }

  // The wheel is attached by hand because React listens for it passively,
  // and a passive listener cannot stop the page behind the canvas moving.
  useEffect(() => {
    const canvas = surface.current
    if (!canvas) {
      return
    }
    const onWheel = (event: WheelEvent) => {
      event.preventDefault()
      const rectangle = canvas.getBoundingClientRect()
      // Trackpads report pixels and mice report lines; the exponent keeps
      // both from turning one flick into the whole zoom range.
      const steps = event.deltaMode === 1 ? event.deltaY * 16 : event.deltaY
      zoomAt(event.clientX - rectangle.left, event.clientY - rectangle.top, Math.exp(-clamp(steps, -240, 240) * 0.0015))
    }
    canvas.addEventListener('wheel', onWheel, { passive: false })
    return () => canvas.removeEventListener('wheel', onWheel)
  }, [zoomAt])

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const target = event.target
      // Not while somebody is typing a name into the lookup: a minus sign
      // belongs in the box, not in the zoom.
      if (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement) {
        if (event.key === 'Escape') {
          ;(target as HTMLInputElement).blur()
        }
        return
      }
      if (event.key === 'Escape') {
        setSelected('')
      } else if (event.key === '+' || event.key === '=') {
        zoomBy(ZOOM_STEP)
      } else if (event.key === '-' || event.key === '_') {
        zoomBy(1 / ZOOM_STEP)
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [zoomBy])

  // --- the lookup --------------------------------------------------------

  useEffect(() => {
    const wanted = query.trim()
    if (!wanted) {
      setFound(null)
      return
    }
    let alive = true
    const timer = window.setTimeout(() => {
      graphql<{ SearchAgentGraph: { nodes: GraphNode[] } }>(SEARCH, { query: wanted, first: 8 })
        .then((answer) => {
          if (alive) {
            setFound(answer.SearchAgentGraph.nodes)
          }
        })
        .catch((caught: unknown) => {
          if (alive) {
            toast.failed(messageOf(caught))
          }
        })
    }, 250)
    return () => {
      alive = false
      window.clearTimeout(timer)
    }
  }, [query, toast])

  const bringIn = (node: GraphNode) => {
    // Where the reader is looking, in the drawing's own units, so a page
    // fetched by name arrives in front of them rather than at the origin.
    const middle = {
      x: (size.current.width / 2 - view.current.x) / view.current.k,
      y: (size.current.height / 2 - view.current.y) / view.current.k,
    }
    const added = ensure(node, middle)
    if (!added) {
      toast.failed(t('knowledge.explore.full', { count: MAX_NODES }))
      return
    }
    setQuery('')
    setFound(null)
    setSelected(added.path)
    redraw()
    centreOn(added.path)
    void expand(added.path)
  }

  // --- drawing it --------------------------------------------------------

  const chosen = selected ? (byPath.current.get(selected) ?? null) : null
  const kindWord = useCallback(
    (kind: string): string => {
      const words: string | undefined = t(`knowledge.kind.${kind}` as 'knowledge.kind.person')
      return words || kind
    },
    [t],
  )
  const relationWords = useCallback(
    (relation: string): string => {
      const words: string | undefined = t(`knowledge.relation.${relation}` as 'knowledge.relation.works_on')
      return words || relation.replace(/_/g, ' ')
    },
    [t],
  )

  // relationLabel is what a line is called wherever it is named: on the
  // line itself and in its tooltip. A link the night guessed carries the
  // word beside the relation, because a dashed line says a guess only to
  // somebody who already knows the drawing.
  const relationLabel = useCallback(
    (edge: Joined): string =>
      edge.proposed
        ? t('knowledge.proposedRelation', { relation: relationWords(edge.relation) })
        : relationWords(edge.relation),
    [relationWords, t],
  )

  const legend = useMemo(() => KINDS.map((kind) => ({ kind, label: kindWord(kind) })), [kindWord])

  const empty = !loading && nodes.current.length === 0

  return (
    <div className="graph-explore" ref={frame}>
      {/* One picture to a reader who cannot see it, rather than three hundred
          circles none of which can be reached from a keyboard. The same graph
          in words is the Knowledge page, which is the way through it for a
          screen reader and is linked from the panel. */}
      <svg
        className="graph-explore-canvas"
        ref={surface}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
        onDoubleClick={onDoubleClick}
        role="img"
        aria-label={t('knowledge.explore.title')}
      >
        <defs>
          <marker
            id="graph-explore-arrow"
            className="graph-explore-arrow"
            viewBox="0 0 8 8"
            refX="7"
            refY="4"
            markerWidth="6"
            markerHeight="6"
            orient="auto-start-reverse"
          >
            <path d="M 0 0 L 8 4 L 0 8 z" />
          </marker>
        </defs>
        <g ref={zoomLayer}>
          {edges.current.map((edge) => (
            <g key={edge.key} className={edgeClasses(edge)}>
              <line
                ref={(element) => {
                  if (element) {
                    edgeLines.current.set(edge.key, element)
                  }
                  return () => {
                    edgeLines.current.delete(edge.key)
                  }
                }}
                // Weights sit between a half and one and a half -- a proposed
                // link starts at half weight, a stated one at one, and use
                // raises it -- so the width spreads that range rather than
                // drawing them all alike, and a weak link fades.
                strokeWidth={edge.containment ? 1 : 0.8 + 3.2 * clamp((edge.weight || 1) - 0.5, 0, 1)}
                strokeOpacity={edge.containment ? undefined : 0.45 + 0.55 * clamp((edge.weight || 1) - 0.5, 0, 1)}
                markerEnd={edge.containment ? undefined : 'url(#graph-explore-arrow)'}
              >
                <title>
                  {edge.note
                    ? `${relationLabel(edge)} — ${edge.note}`
                    : `${edge.from} ${relationLabel(edge)} ${edge.to}`}
                </title>
              </line>
              {edge.containment ? null : (
                <text
                  className="graph-explore-relation"
                  textAnchor="middle"
                  ref={(element) => {
                    if (element) {
                      edgeLabels.current.set(edge.key, element)
                    }
                    return () => {
                      edgeLabels.current.delete(edge.key)
                    }
                  }}
                >
                  {relationLabel(edge)}
                </text>
              )}
            </g>
          ))}
          {nodes.current.map((node) => {
            const radius = radiusOf(node)
            // A page with pages under it, or one nobody has opened yet, has
            // more behind it than is drawn. The badge is the only thing on
            // the canvas that says so.
            const more = node.children > 0 || !node.expanded || node.linksLeftOut > 0
            const classes = ['graph-explore-node', `graph-explore-kind-${node.kind}`]
            if (node.expanded) classes.push('expanded')
            if (node.path === selected) classes.push('selected')
            return (
              <g
                key={node.path}
                data-path={node.path}
                className={classes.join(' ')}
                ref={(element) => {
                  if (element) {
                    nodeElements.current.set(node.path, element)
                    element.setAttribute('transform', `translate(${node.x.toFixed(1)},${node.y.toFixed(1)})`)
                  }
                  return () => {
                    nodeElements.current.delete(node.path)
                  }
                }}
              >
                <title>
                  {node.linksLeftOut > 0
                    ? `${node.path} · ${t('knowledge.linksLeftOut', { count: node.linksLeftOut })}`
                    : node.path}
                </title>
                <circle r={radius} />
                {more ? (
                  <g className="graph-explore-more">
                    <circle cx={radius * 0.72} cy={-radius * 0.72} r={5} />
                    <path
                      d={`M ${radius * 0.72 - 2.5} ${-radius * 0.72} h 5 M ${radius * 0.72} ${-radius * 0.72 - 2.5} v 5`}
                    />
                  </g>
                ) : null}
                <text className="graph-explore-name" textAnchor="middle" y={radius + 11}>
                  {shorten(nameOf(node, me))}
                </text>
              </g>
            )
          })}
        </g>
      </svg>

      <div className="graph-explore-lookup">
        <input
          type="search"
          value={query}
          placeholder={t('knowledge.explore.find')}
          aria-label={t('knowledge.explore.find')}
          onChange={(event) => setQuery(event.target.value)}
        />
        {found ? (
          <ul className="graph-explore-found">
            {found.length === 0 ? <li className="muted">{t('knowledge.nothingFound')}</li> : null}
            {found.map((node) => (
              <li key={node.id}>
                <button type="button" onClick={() => bringIn(node)}>
                  <span>{nameOf(node, me)}</span>
                  <span className="muted">{node.path}</span>
                </button>
              </li>
            ))}
          </ul>
        ) : null}
      </div>

      <div className="graph-explore-zoom">
        <button
          type="button"
          className="icon-button"
          title={t('knowledge.explore.zoomIn')}
          aria-label={t('knowledge.explore.zoomIn')}
          onClick={() => zoomBy(ZOOM_STEP)}
        >
          +
        </button>
        <button
          type="button"
          className="icon-button"
          title={t('knowledge.explore.zoomOut')}
          aria-label={t('knowledge.explore.zoomOut')}
          onClick={() => zoomBy(1 / ZOOM_STEP)}
        >
          −
        </button>
        {/* Fitting the drawing and filling the screen are two ways of framing
            the same picture, so they are one set, drawn as the dashboard
            draws a set of related buttons everywhere else. */}
        <div className="segmented" role="group">
          <button type="button" onClick={fit}>
            {t('knowledge.explore.fit')}
          </button>
          {document.fullscreenEnabled ? (
            <button type="button" onClick={toggleFullscreen}>
              {t('knowledge.explore.fullscreen')}
            </button>
          ) : null}
        </div>
      </div>

      <div className="graph-explore-legend">
        {legend.map((entry) => (
          <span key={entry.kind} className={`graph-explore-swatch graph-explore-kind-${entry.kind}`}>
            <i />
            {entry.label}
          </span>
        ))}
        <span className="graph-explore-hint muted">{t('knowledge.explore.hint')}</span>
      </div>

      {empty ? (
        <div className="graph-explore-nothing">
          <SettingsEmpty>{t('knowledge.explore.empty')}</SettingsEmpty>
        </div>
      ) : null}

      {chosen ? (
        // A dialog rather than a panel beside the drawing: the drawing
        // stays exactly where it was, under it, and closing it changes
        // nothing.
        <FormDialog
          title={nameOf(chosen, me)}
          submitLabel={t('knowledge.explore.open')}
          closeLabel={t('common.close')}
          wide
          onSubmit={() => navigate(`/settings/knowledge/${chosen.path}`)}
          onClose={() => setSelected('')}
          otherAction={
            // Icons, with the words as their titles: four worded buttons
            // did not fit a phone's width and broke their own words.
            <>
              <button
                type="button"
                className="icon-action graph-explore-dialog-action"
                title={t('knowledge.explore.expand')}
                aria-label={t('knowledge.explore.expand')}
                onClick={() => {
                  void expand(chosen.path)
                  setSelected('')
                }}
              >
                <PlusIcon size={18} />
              </button>
              <button
                type="button"
                className="icon-action graph-explore-dialog-action"
                title={t('knowledge.explore.collapse')}
                aria-label={t('knowledge.explore.collapse')}
                onClick={() => {
                  collapse(chosen.path)
                  setSelected('')
                }}
              >
                <MinusIcon size={18} />
              </button>
              {/* The agent, pointed at this page: the drawer opens with a
                  chip for it, and the person asks it to dig deeper, or to
                  change what the page says and links to. */}
              <button
                type="button"
                className="icon-action graph-explore-dialog-action"
                title={t('knowledge.askAgent')}
                aria-label={t('knowledge.askAgent')}
                onClick={() => {
                  setSelected('')
                  if (!askAgentAbout({ path: chosen.path, name: chosen.name })) navigate('/settings/agent')
                }}
              >
                <SparkIcon size={18} />
              </button>
            </>
          }
        >
          <p className="muted">
            <code className="tag knowledge-path">{chosen.path}</code> <Tag value={kindWord(chosen.kind)} />
          </p>
          {chosen.summary ? (
            <p className="graph-explore-opening">{opening(chosen.summary)}</p>
          ) : (
            <p className="muted">{t('knowledge.noSummary')}</p>
          )}
          <PageFacts path={chosen.path} />
        </FormDialog>
      ) : null}
    </div>
  )
}

// opening is as much of a page as belongs in a panel beside the drawing:
// the first paragraph, and not all of that. The page itself is a button
// away.
function opening(summary: string): string {
  const first = summary.split(/\n\s*\n/)[0].trim()
  return first.length > 260 ? first.slice(0, 259).trimEnd() + '…' : first
}

// PageFacts is the page's facts as the dialog shows them, every one the
// page carries, in a list that scrolls.

function PageFacts({ path }: { path: string }) {
  const page = useQuery(
    () =>
      graphql<{ AgentGraphPage: { facts: { id: string; number: number; text: string }[] } | null }>(PAGE_FACTS, {
        path,
      }),
    [path],
    { refresh: false },
  )
  const facts = page.data?.AgentGraphPage?.facts ?? []
  if (page.loading && !page.data) return null
  if (facts.length === 0) return null
  return (
    <div className="graph-explore-facts">
      <ul className="knowledge-rows">
        {facts.map((fact) => (
          <li key={fact.id} className="graph-explore-fact">
            <span className="muted">#{fact.number}</span> {cutText(fact.text, 220)}
          </li>
        ))}
      </ul>
    </div>
  )
}

function cutText(text: string, length: number): string {
  return text.length > length ? text.slice(0, length - 1).trimEnd() + '\u2026' : text
}
