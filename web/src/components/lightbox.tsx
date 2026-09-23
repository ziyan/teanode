import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'

import { CloseIcon, ExternalIcon, MinusIcon, PlusIcon } from './icons'
import { Tooltip } from './tooltip'
import { useTranslation } from '../i18n/i18n'

// A picture in the dashboard used to be a link to its own bytes: clicking
// it left the page for a tab showing the raw file, and on a phone that is
// the browser's own viewer, which is a different program with a different
// way back. The picture is usually a screenshot, and a screenshot is read
// by getting close to one corner of it -- so what was wanted was a look,
// not a navigation.
//
// The lightbox is that look. It opens over the page, says what the picture
// is, zooms and pans, and closes back to the exact spot it was opened from.
// The address is still on the anchor underneath, so a middle click or a
// modified click opens the raw file the way it always did, and the frame
// carries a link to it for anybody who wants the bytes.

// Fit is one, and there is nothing below it: a picture smaller than the
// window it is in has nowhere useful to go. Eight is far enough to read
// the smallest text a screenshot is likely to carry.
const SMALLEST_SCALE = 1
const LARGEST_SCALE = 8

// What a tap on the picture zooms to, and what one button press multiplies
// by. A step of a quarter takes fit to full in four presses, which is few
// enough to be worth pressing and many enough to stop where you meant to.
const TAP_SCALE = 2.5
const STEP = 1.25

// How far a pointer may wander and still count as a tap rather than a drag,
// and how long a second tap has to land in to be a double tap. Both are the
// usual numbers; a finger is less precise than a mouse, hence six pixels
// rather than one.
const TAP_SLACK = 6
const TAP_WINDOW = 320

// View is where the picture sits: a multiplier over the size it is drawn at
// when it fits, and an offset from the middle of the stage in the pixels
// the stage is measured in.
interface View {
  scale: number
  x: number
  y: number
}

const FIT: View = { scale: 1, x: 0, y: 0 }

function scaleWithin(scale: number): number {
  return Math.min(LARGEST_SCALE, Math.max(SMALLEST_SCALE, scale))
}

// viewWithin keeps the picture in front of the reader. Where an axis has
// more picture than stage the offset may run to half the difference, which
// puts that edge of the picture on that edge of the stage and no further;
// where the picture is the smaller of the two it stays centered, because a
// picture that fits has no part hidden to drag into view.
function viewWithin(view: View, stage: HTMLElement | null, picture: HTMLImageElement | null): View {
  const scale = scaleWithin(view.scale)
  if (!stage || !picture) return { scale, x: 0, y: 0 }
  const spareWidth = Math.max(0, (picture.offsetWidth * scale - stage.clientWidth) / 2)
  const spareHeight = Math.max(0, (picture.offsetHeight * scale - stage.clientHeight) / 2)
  return {
    scale,
    x: Math.min(spareWidth, Math.max(-spareWidth, view.x)),
    y: Math.min(spareHeight, Math.max(-spareHeight, view.y)),
  }
}

// zoomAbout scales around a point rather than around the middle, so that
// what is under the pointer or between the fingers stays under them. The
// point is in stage pixels measured from the middle of the stage, which is
// what the transform's own origin is.
function zoomAbout(previous: View, wanted: number, pointX: number, pointY: number): View {
  const scale = scaleWithin(wanted)
  const ratio = scale / previous.scale
  return {
    scale,
    x: pointX - (pointX - previous.x) * ratio,
    y: pointY - (pointY - previous.y) * ratio,
  }
}

// middleOf is where a gesture is: the one pointer, or the point between two
// of them, in the window's own coordinates.
function middleOf(points: { x: number; y: number }[]): { x: number; y: number } {
  let x = 0
  let y = 0
  for (const point of points) {
    x += point.x
    y += point.y
  }
  return { x: x / points.length, y: y / points.length }
}

// spreadOf is how far apart two fingers are, and zero for anything else:
// one finger cannot pinch, and the ratio of a pinch is this over what it
// was when the second finger landed.
function spreadOf(points: { x: number; y: number }[]): number {
  if (points.length < 2) return 0
  return Math.hypot(points[0].x - points[1].x, points[0].y - points[1].y)
}

// A gesture is everything one or two pointers are doing, remembered from
// where they started: the view they started from, where their middle was,
// and how far apart they were. It is recorded again whenever a finger lands
// or lifts, so that a second finger joining a drag continues from where the
// drag had got to rather than snapping back.
interface Gesture {
  view: View
  middle: { x: number; y: number }
  spread: number
  moved: boolean
}

// Lightbox is the picture over the page. It is a dialog like the rest of
// them -- the same scrim, dismissed the same three ways -- with a stage in
// place of a form.
export function Lightbox({
  source,
  name,
  where,
  onClose,
}: {
  source: string
  name: string
  // The thread or channel the picture was posted in, where the site that
  // opened it knows one. A picture a person handed the agent directly does
  // not have one, and then the frame says only what it is.
  where?: string
  onClose: () => void
}) {
  const { t } = useTranslation()
  const stage = useRef<HTMLDivElement>(null)
  const picture = useRef<HTMLImageElement>(null)
  const closeButton = useRef<HTMLButtonElement>(null)
  const pointers = useRef(new Map<number, { x: number; y: number }>())
  const gesture = useRef<Gesture | null>(null)
  const lastTap = useRef(0)
  const [view, setView] = useState<View>(FIT)
  const [dragging, setDragging] = useState(false)

  // Where the picture is now, for the gesture that is about to start. A
  // gesture begins in a pointer event and the view it continues from was
  // set by the move events just before it, which the render has not
  // necessarily caught up with.
  const viewNow = useRef(view)
  viewNow.current = view

  // Where a point in the window falls on the stage, measured from the
  // middle of it, which is where the picture's own transform is anchored.
  const onStage = useCallback((x: number, y: number) => {
    const box = stage.current?.getBoundingClientRect()
    if (!box) return { x: 0, y: 0 }
    return { x: x - box.left - box.width / 2, y: y - box.top - box.height / 2 }
  }, [])

  const zoomBy = useCallback((factor: number) => {
    setView((previous) =>
      viewWithin(zoomAbout(previous, previous.scale * factor, 0, 0), stage.current, picture.current),
    )
  }, [])

  // Escape closes, and the three keys every viewer has: more, less, and back
  // to the whole picture. Escape is checked against defaultPrevented the way
  // the other dialogs check it, so a control inside that took the key first
  // keeps it.
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.defaultPrevented) return
      if (event.key === 'Escape') {
        onClose()
        return
      }
      if (event.key === '+' || event.key === '=') {
        event.preventDefault()
        zoomBy(STEP)
      } else if (event.key === '-' || event.key === '_') {
        event.preventDefault()
        zoomBy(1 / STEP)
      } else if (event.key === '0') {
        event.preventDefault()
        setView(FIT)
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [onClose, zoomBy])

  // The way out is where the keyboard lands, so that Tab starts inside the
  // lightbox and Enter on arrival closes it.
  useEffect(() => {
    closeButton.current?.focus()
  }, [])

  // The wheel is listened for here rather than through React, which attaches
  // its own wheel listener passively: a passive listener may not refuse the
  // scroll, and a wheel that zooms the picture and scrolls the page behind
  // it at the same time is neither.
  useEffect(() => {
    const element = stage.current
    if (!element) return
    function onWheel(event: WheelEvent) {
      event.preventDefault()
      // Wheels report in pixels, lines or pages depending on the device, so
      // they are brought to pixels before being read as an amount.
      const lines = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? 400 : 1
      const point = onStage(event.clientX, event.clientY)
      setView((previous) =>
        viewWithin(
          zoomAbout(previous, previous.scale * Math.exp((-event.deltaY * lines) / 400), point.x, point.y),
          element,
          picture.current,
        ),
      )
    }
    element.addEventListener('wheel', onWheel, { passive: false })
    return () => element.removeEventListener('wheel', onWheel)
  }, [onStage])

  // A gesture is recorded from wherever the pointers are now. Called when
  // one lands and again when one lifts, so that the count changing is a new
  // gesture continuing from the view the last one reached.
  function beginGesture(moved = false) {
    const points = [...pointers.current.values()]
    if (points.length === 0) {
      gesture.current = null
      return
    }
    gesture.current = { view: viewNow.current, middle: middleOf(points), spread: spreadOf(points), moved }
  }

  function onPointerDown(event: React.PointerEvent) {
    // A third finger has nothing to say that two are not already saying.
    if (pointers.current.size >= 2) return
    event.currentTarget.setPointerCapture(event.pointerId)
    pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY })
    beginGesture()
    setDragging(true)
  }

  function onPointerMove(event: React.PointerEvent) {
    if (!pointers.current.has(event.pointerId)) return
    pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY })
    const started = gesture.current
    if (!started) return
    const points = [...pointers.current.values()]
    const middle = middleOf(points)
    const spread = spreadOf(points)
    if (Math.hypot(middle.x - started.middle.x, middle.y - started.middle.y) > TAP_SLACK) started.moved = true

    // Two fingers spreading scale the picture about the point they started
    // between; one finger, or two held at the same distance, only move it.
    // Both happen at once because a pinch drifts as well as opens.
    const anchor = onStage(started.middle.x, started.middle.y)
    const zoomed =
      started.spread > 0 && spread > 0
        ? zoomAbout(started.view, started.view.scale * (spread / started.spread), anchor.x, anchor.y)
        : started.view
    setView(
      viewWithin(
        {
          scale: zoomed.scale,
          x: zoomed.x + (middle.x - started.middle.x),
          y: zoomed.y + (middle.y - started.middle.y),
        },
        stage.current,
        picture.current,
      ),
    )
  }

  function onPointerUp(event: React.PointerEvent) {
    if (!pointers.current.has(event.pointerId)) return
    pointers.current.delete(event.pointerId)
    const started = gesture.current
    const wasTap = pointers.current.size === 0 && started !== null && !started.moved
    if (pointers.current.size === 0) {
      gesture.current = null
      setDragging(false)
    } else {
      // A finger lifting out of a pinch leaves a drag, from here.
      beginGesture(true)
    }
    if (!wasTap) return

    // A tap on the ground around the picture closes, the way a click on a
    // dialog's scrim does. A tap on the picture itself is a zoom, but only
    // the second of two: a single one is how somebody steadies a phone.
    if (event.target === stage.current) {
      onClose()
      return
    }
    const now = Date.now()
    if (now - lastTap.current < TAP_WINDOW) {
      lastTap.current = 0
      const point = onStage(event.clientX, event.clientY)
      setView((previous) =>
        previous.scale > 1.05
          ? FIT
          : viewWithin(zoomAbout(previous, TAP_SCALE, point.x, point.y), stage.current, picture.current),
      )
    } else {
      lastTap.current = now
    }
  }

  const said = where && where.trim() !== '' ? t('lightbox.from', { where }) : null

  return createPortal(
    // The scrim is the dialogs' own, so that Escape landing here is not also
    // read as Escape landing on the page behind -- the drawer watches for a
    // .dialog-scrim before it treats the key as its own.
    <div
      className="dialog-scrim lightbox-scrim"
      onMouseDown={(event) => event.target === event.currentTarget && onClose()}
    >
      <div className="lightbox" role="dialog" aria-modal="true" aria-label={name}>
        <div className="lightbox-head">
          <div className="lightbox-name">
            <strong>{name}</strong>
            {said ? <span className="muted">{said}</span> : null}
          </div>
          <Tooltip label={t('lightbox.openRaw')}>
            <a
              className="icon-button"
              href={source}
              target="_blank"
              rel="noreferrer"
              aria-label={t('lightbox.openRaw')}
            >
              <ExternalIcon size={16} />
            </a>
          </Tooltip>
          <Tooltip label={t('common.close')}>
            <button
              ref={closeButton}
              type="button"
              className="icon-button"
              onClick={onClose}
              aria-label={t('common.close')}
            >
              <CloseIcon size={18} />
            </button>
          </Tooltip>
        </div>
        <div
          ref={stage}
          className={dragging ? 'lightbox-stage dragging' : 'lightbox-stage'}
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
          onPointerCancel={onPointerUp}
        >
          {/* Loaded at once rather than lazily, like the thumbnail that
              opened it: width and height are auto under a maximum, so a lazy
              picture has no box to come into view and is never fetched.
              Dragging is the browser's own and would fight the pan, so it is
              turned off here and the pointer does the moving. */}
          <img
            ref={picture}
            className="lightbox-picture"
            src={source}
            alt={name}
            draggable={false}
            // A transform that changes with every frame of a gesture is a
            // value, not a rule, which is why it is written here and the
            // rest of the picture's styling is not.
            style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }}
          />
        </div>
        <div className="lightbox-foot">
          <Tooltip label={t('lightbox.zoomOut')}>
            <button
              type="button"
              className="icon-button"
              onClick={() => zoomBy(1 / STEP)}
              disabled={view.scale <= SMALLEST_SCALE}
              aria-label={t('lightbox.zoomOut')}
            >
              <MinusIcon size={16} />
            </button>
          </Tooltip>
          <span className="lightbox-scale">{t('lightbox.scale', { percent: Math.round(view.scale * 100) })}</span>
          <Tooltip label={t('lightbox.zoomIn')}>
            <button
              type="button"
              className="icon-button"
              onClick={() => zoomBy(STEP)}
              disabled={view.scale >= LARGEST_SCALE}
              aria-label={t('lightbox.zoomIn')}
            >
              <PlusIcon size={16} />
            </button>
          </Tooltip>
          <button
            type="button"
            className="lightbox-fit"
            onClick={() => setView(FIT)}
            disabled={view.scale <= SMALLEST_SCALE}
          >
            {t('lightbox.fit')}
          </button>
          <span className="muted lightbox-hint">{t('lightbox.hint')}</span>
        </div>
      </div>
    </div>,
    document.body,
  )
}

// ZoomablePicture is the picture where it is shown and the lightbox it
// opens into, together, because everywhere one is wanted the other is too.
//
// It stays an anchor to the file rather than becoming a button. A picture
// that is a link can be opened in a tab of its own with the middle button or
// a held modifier, and somebody who has been doing that for as long as this
// dashboard has had pictures should not find it stopped working. A plain
// click is the lightbox.
export function ZoomablePicture({
  source,
  name,
  where,
  imageClassName,
  openTitle,
}: {
  source: string
  name: string
  where?: string
  // The class the picture is drawn small with, which differs by where it is
  // shown: a fact's evidence is wider than a file under a turn in the
  // drawer.
  imageClassName: string
  openTitle: string
}) {
  const opener = useRef<HTMLAnchorElement>(null)
  const [open, setOpen] = useState(false)
  return (
    <>
      <Tooltip label={openTitle}>
        <a
          ref={opener}
          className="picture-opener"
          href={source}
          target="_blank"
          rel="noreferrer"
          onClick={(event) => {
            // A middle click never reaches here, and a modified one is a
            // deliberate ask for a tab of its own: both are left to the
            // browser and the address on the anchor.
            if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
            event.preventDefault()
            setOpen(true)
          }}
        >
          {/* Loaded at once rather than lazily. A lazy picture is only fetched
            when its box comes into view, and this box has no size until the
            picture is in it: width and height are auto under a maximum, so
            before the bytes arrive the element is three pixels square, never
            intersects anything, and the picture is never asked for. That
            shipped twice, and left a blank where every screenshot should
            be. */}
          <img className={imageClassName} src={source} alt={name} />
        </a>
      </Tooltip>
      {open ? (
        <Lightbox
          source={source}
          name={name}
          where={where}
          onClose={() => {
            setOpen(false)
            // Back to the picture that was clicked, which is where the
            // reader was before the lightbox took the screen.
            opener.current?.focus()
          }}
        />
      ) : null}
    </>
  )
}
