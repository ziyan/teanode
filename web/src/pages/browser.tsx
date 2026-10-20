import { useCallback, useEffect, useRef, useState } from 'react'

import { graphql } from '../api'
import { Loading } from '../components/common'
import { CloseIcon, KeyboardIcon } from '../components/icons'
import { SettingsEmpty } from '../components/settingsList'
import { useToast } from '../components/toast'
import { Tooltip } from '../components/tooltip'
import { useTranslation } from '../i18n/i18n'
import { buttonOf, clickCountOf, keyMessage, modifiersOf, pointToTab, type ClickRecord } from './browserInput'

// The Browser page: the person's browser tab live, with Take over and Hand
// back. The picture and the input travel over a websocket of their own,
// /api/v1/agent/tab/view, whose messages are in
// docs/planning/tab-take-over-execplan.md under Interfaces and Dependencies.
// Watching is passive: only Take over stops the agent, and input is sent
// only while the server says the person has control.

// READ_AGENT_TAB says whether the person's browser is connected through the
// extension. The socket is opened only once it is.
const READ_AGENT_TAB = `query { ReadAgentTab { connected attached title url } }`

// READ_REQUESTS reads what waits for the person, for the agent's newest
// request to take the tab over: on a phone the card that opened this page
// is gone once it has, and the request is what they came to do.
const READ_REQUESTS = `query { ReadAgentWaiting(first: 20) { interactions { interactionKind interactionText } } }`

// How often the request is read again while the page is shown.
const REQUESTS_EVERY_MS = 10_000

// HAND_BACK gives the tab back, with the person's note, and answers the
// agent's card asking for it: the API's to do, so not a socket message.
const HAND_BACK = `mutation ($note: String) { HandBackAgentTab(note: $note) }`

// A socket that is open is told it is alive this often, as the extension's is.
const PING_EVERY_MS = 20_000

// ViewerTabID is a tab's number from the extension, or "server" for the
// server's own browser.
type ViewerTabID = number | string

interface ViewerTab {
  tab: ViewerTabID
  title: string
  url: string
  isOwn: boolean
  isCurrent: boolean
}

interface ViewerState {
  isConnected: boolean
  canCast: boolean
  tabs: ViewerTab[]
  watching?: ViewerTabID | null
  controller: 'agent' | 'person'
}

interface ViewerFrame {
  tab: ViewerTabID
  data: string
  // The page's size in CSS pixels, which the picture is scaled from.
  width: number
  height: number
}

type ViewerMessage =
  | ({ type: 'state' } & ViewerState)
  | ({ type: 'frame' } & ViewerFrame)
  | { type: 'refused'; reason?: string }
  | { type: 'pong' }

interface ViewerSocket {
  // Whether the server has accepted this connection and said what there is.
  isLinked: boolean
  send: (message: object) => void
}

// useViewerSocket keeps the viewer socket open while isEnabled. A socket
// that drops comes back a little later each time, one second to thirty with
// some jitter, as the GraphQL socket does (subscribe in api.ts), and at once
// when the network or the tab returns. A refusal is final: the server said
// why, and asking again would only be refused again.
function useViewerSocket(
  isEnabled: boolean,
  handlers: { onOpen: () => void; onMessage: (message: ViewerMessage) => void },
): ViewerSocket {
  const [isLinked, setLinked] = useState(false)
  const socketRef = useRef<WebSocket | null>(null)
  const handlersRef = useRef(handlers)
  handlersRef.current = handlers
  const send = useCallback((message: object) => {
    const socket = socketRef.current
    if (socket && socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify(message))
  }, [])
  useEffect(() => {
    if (!isEnabled) return
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const address = `${protocol}//${window.location.host}/api/v1/agent/tab/view`
    let isStopped = false
    let isRefused = false
    let attempts = 0
    let retryTimer: number | undefined
    let pingTimer: number | undefined
    const connect = () => {
      retryTimer = undefined
      const current = new WebSocket(address)
      socketRef.current = current
      current.onopen = () => {
        handlersRef.current.onOpen()
        // The session's cookie came with the upgrade, from a page of this
        // server, which is how the server knows who this is.
        current.send(JSON.stringify({ type: 'hello' }))
        pingTimer = window.setInterval(() => current.send(JSON.stringify({ type: 'ping' })), PING_EVERY_MS)
      }
      current.onmessage = (event) => {
        let message: ViewerMessage
        try {
          message = JSON.parse(String(event.data))
        } catch {
          return
        }
        if (message.type === 'state') {
          attempts = 0
          setLinked(true)
        }
        if (message.type === 'refused') isRefused = true
        handlersRef.current.onMessage(message)
      }
      current.onerror = () => {
        // The close that follows says what to do.
      }
      current.onclose = () => {
        window.clearInterval(pingTimer)
        if (socketRef.current !== current) return
        socketRef.current = null
        setLinked(false)
        if (isStopped || isRefused) return
        attempts += 1
        const delay = Math.min(30000, 1000 * 2 ** Math.min(attempts - 1, 5)) + Math.random() * 500
        retryTimer = window.setTimeout(connect, delay)
      }
    }
    // Back on the network, or back to the tab: no reason to keep waiting.
    const wake = () => {
      if (isStopped || isRefused || socketRef.current || retryTimer === undefined) return
      if (document.visibilityState === 'hidden') return
      window.clearTimeout(retryTimer)
      connect()
    }
    window.addEventListener('online', wake)
    document.addEventListener('visibilitychange', wake)
    connect()
    return () => {
      isStopped = true
      window.removeEventListener('online', wake)
      document.removeEventListener('visibilitychange', wake)
      window.clearTimeout(retryTimer)
      window.clearInterval(pingTimer)
      const current = socketRef.current
      socketRef.current = null
      current?.close()
      setLinked(false)
    }
  }, [isEnabled])
  return { isLinked, send }
}

// siteOf is the host a tab is on, or the address as it came when it is not
// one that can be read.
function siteOf(url: string): string {
  try {
    return new URL(url).host
  } catch {
    return url
  }
}

export function BrowserPage() {
  const { t } = useTranslation()
  const toast = useToast()
  // What the server says of the person's browser; null until it has.
  const [isBrowserConnected, setBrowserConnected] = useState<boolean | null>(null)
  const [viewerState, setViewerState] = useState<ViewerState | null>(null)
  const [frame, setFrame] = useState<ViewerFrame | null>(null)
  const [isHandingBack, setHandingBack] = useState(false)
  // The agent's newest request to take the tab over, or none.
  const [request, setRequest] = useState('')
  const [handBackNote, setHandBackNote] = useState('')
  // The tab whose frames are drawn: what the server last said is watched, or
  // what was just chosen, so a frame of the tab left behind is dropped.
  const watchedTab = useRef<ViewerTabID | null>(null)
  // Whether the current connection has chosen a tab on its own yet, so that a
  // server that never confirms one is asked once and not on every state.
  const hasAutoWatched = useRef(false)
  const pictureRef = useRef<HTMLImageElement>(null)
  const keyboardRef = useRef<HTMLTextAreaElement>(null)
  // The press in progress: its button and click count, kept until it is
  // released, and the one before it for counting a double click.
  const heldPress = useRef<{ button: 'left' | 'middle' | 'right'; clickCount: number } | null>(null)
  const lastPress = useRef<ClickRecord | null>(null)
  // The latest pointer move not yet sent: one is sent per animation frame.
  const pendingMove = useRef<Record<string, unknown> | null>(null)
  const moveFrame = useRef<number | null>(null)

  useEffect(() => {
    let isStopped = false
    const read = async (isFirst: boolean) => {
      try {
        const response = await graphql<{ ReadAgentTab: { connected: boolean } }>(READ_AGENT_TAB)
        if (!isStopped) setBrowserConnected(response.ReadAgentTab.connected)
      } catch (caught) {
        if (!isStopped && isFirst) toast.failure(caught, t('browser.readFailed'))
      }
    }
    void read(true)
    // A browser connected in another window is noticed when the person comes
    // back to this one, rather than after a wait.
    const onVisible = () => {
      if (document.visibilityState === 'visible') void read(false)
    }
    window.addEventListener('focus', onVisible)
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      isStopped = true
      window.removeEventListener('focus', onVisible)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [toast, t])

  useEffect(() => {
    if (!isBrowserConnected) return
    let isStopped = false
    const read = async () => {
      if (document.visibilityState === 'hidden') return
      try {
        const response = await graphql<{
          ReadAgentWaiting: { interactions: { interactionKind: string; interactionText: string }[] }
        }>(READ_REQUESTS)
        const asked = response.ReadAgentWaiting.interactions.find((interaction) => interaction.interactionKind === 'take_over')
        if (!isStopped) setRequest(asked?.interactionText ?? '')
      } catch {
        // The picture is what matters here; a request not read is shown
        // on the next look.
      }
    }
    void read()
    const every = window.setInterval(() => void read(), REQUESTS_EVERY_MS)
    return () => {
      isStopped = true
      window.clearInterval(every)
    }
  }, [isBrowserConnected])

  const { isLinked, send } = useViewerSocket(isBrowserConnected === true, {
    onOpen: () => {
      hasAutoWatched.current = false
    },
    onMessage: (message) => {
      switch (message.type) {
        case 'state': {
          const next: ViewerState = message
          watchedTab.current = next.watching ?? null
          setViewerState(next)
          // Nothing is watched yet: look at the tab the agent acts in.
          if (next.watching == null && next.tabs.length > 0 && !hasAutoWatched.current) {
            hasAutoWatched.current = true
            const chosen = next.tabs.find((tab) => tab.isCurrent) ?? next.tabs[0]
            watchedTab.current = chosen.tab
            send({ type: 'watch', tab: chosen.tab })
          }
          break
        }
        case 'frame': {
          const next: ViewerFrame = message
          if (next.tab === watchedTab.current) setFrame(next)
          break
        }
        case 'refused':
          toast.failure(null, message.reason || t('browser.refused'))
          break
        default:
          break
      }
    },
  })

  const isControlling = viewerState?.controller === 'person'
  // What was being asked of the person does not outlast their control.
  useEffect(() => {
    if (!isControlling) {
      setHandingBack(false)
      setHandBackNote('')
    }
  }, [isControlling])
  useEffect(
    () => () => {
      if (moveFrame.current !== null) window.cancelAnimationFrame(moveFrame.current)
    },
    [],
  )

  const drawnFrame = frame && viewerState && frame.tab === viewerState.watching ? frame : null
  const frameRef = useRef(drawnFrame)
  frameRef.current = drawnFrame
  const isPictureShown = drawnFrame !== null

  // pointOf is where an event is in the tab, from the picture as it is drawn
  // now and the page's size from the latest frame.
  const pointOf = useCallback((clientX: number, clientY: number) => {
    const picture = pictureRef.current
    const current = frameRef.current
    if (!picture || !current) return null
    return pointToTab(clientX, clientY, picture.getBoundingClientRect(), current.width, current.height)
  }, [])

  // The wheel is listened for outside React, which registers it as passive,
  // so that the dashboard does not scroll under a wheel meant for the tab.
  useEffect(() => {
    const picture = pictureRef.current
    if (!picture || !isControlling || !isPictureShown) return
    const onWheel = (event: WheelEvent) => {
      const point = pointOf(event.clientX, event.clientY)
      if (!point) return
      event.preventDefault()
      // A line or a page of scrolling, as Firefox reports it, in pixels.
      const unit = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? (frameRef.current?.height ?? 1) : 1
      send({ type: 'wheel', x: point.x, y: point.y, deltaX: event.deltaX * unit, deltaY: event.deltaY * unit })
    }
    picture.addEventListener('wheel', onWheel, { passive: false })
    return () => picture.removeEventListener('wheel', onWheel)
  }, [isControlling, isPictureShown, pointOf, send])

  const chooseTab = (tab: ViewerTab) => {
    if (tab.tab === viewerState?.watching) return
    watchedTab.current = tab.tab
    setFrame(null)
    send({ type: 'watch', tab: tab.tab })
  }

  const handBack = async () => {
    try {
      await graphql(HAND_BACK, { note: handBackNote.trim() })
      setHandingBack(false)
      setHandBackNote('')
      setRequest('')
    } catch (caught) {
      toast.failure(caught, t('browser.handBackFailed'))
    }
  }

  const onPointerDown = (event: React.PointerEvent<HTMLImageElement>) => {
    if (!isControlling) return
    const point = pointOf(event.clientX, event.clientY)
    if (!point) return
    event.preventDefault()
    // A pointer down that is not left to the browser does not focus the
    // picture, and the keys that follow need it focused.
    event.currentTarget.focus()
    event.currentTarget.setPointerCapture?.(event.pointerId)
    const button = buttonOf(event.button)
    const clickCount = clickCountOf(lastPress.current, event.timeStamp, point.x, point.y, button)
    lastPress.current = { timeMS: event.timeStamp, x: point.x, y: point.y, button, clickCount }
    heldPress.current = { button, clickCount }
    pendingMove.current = null
    send({
      type: 'pointer',
      event: 'down',
      x: point.x,
      y: point.y,
      button,
      clickCount,
      modifiers: modifiersOf(event),
    })
  }

  const onPointerMove = (event: React.PointerEvent<HTMLImageElement>) => {
    if (!isControlling) return
    const point = pointOf(event.clientX, event.clientY)
    if (!point) return
    pendingMove.current = {
      type: 'pointer',
      event: 'move',
      x: point.x,
      y: point.y,
      button: heldPress.current?.button ?? 'none',
      clickCount: 0,
      modifiers: modifiersOf(event),
    }
    if (moveFrame.current !== null) return
    moveFrame.current = window.requestAnimationFrame(() => {
      moveFrame.current = null
      const move = pendingMove.current
      pendingMove.current = null
      if (move) send(move)
    })
  }

  const onPointerUp = (event: React.PointerEvent<HTMLImageElement>) => {
    const held = heldPress.current
    if (!isControlling || !held) return
    const point = pointOf(event.clientX, event.clientY)
    if (!point) return
    event.preventDefault()
    heldPress.current = null
    pendingMove.current = null
    send({
      type: 'pointer',
      event: 'up',
      x: point.x,
      y: point.y,
      button: held.button,
      clickCount: held.clickCount,
      modifiers: modifiersOf(event),
    })
  }

  const onKey = (event: React.KeyboardEvent, phase: 'down' | 'up') => {
    if (!isControlling) return
    // A paste is left to the browser, which then says what was pasted.
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'v') return
    event.preventDefault()
    send(keyMessage(event, phase))
  }

  const onPaste = (event: React.ClipboardEvent) => {
    if (!isControlling) return
    event.preventDefault()
    const text = event.clipboardData.getData('text')
    if (text) send({ type: 'text', text })
  }

  // What the phone's keyboard types into the hidden field goes to the tab as
  // text, and the field is emptied so that the next key is the only thing in
  // it. A word still being composed is sent when it is finished.
  const sendKeyboardText = (field: HTMLTextAreaElement) => {
    if (field.value) send({ type: 'text', text: field.value })
    field.value = ''
  }
  const onKeyboardKey = (event: React.KeyboardEvent<HTMLTextAreaElement>, phase: 'down' | 'up') => {
    if (!isControlling || event.nativeEvent.isComposing) return
    if (event.key !== 'Enter' && event.key !== 'Backspace') return
    event.preventDefault()
    send(keyMessage(event, phase))
  }

  if (isBrowserConnected === null) return <Loading />
  if (!isBrowserConnected || (viewerState && !viewerState.isConnected)) {
    return <SettingsEmpty>{t('browser.notConnected')}</SettingsEmpty>
  }

  const tabs = viewerState ? [...viewerState.tabs].sort((left, right) => Number(right.isOwn) - Number(left.isOwn)) : []

  return (
    <div className="browser-page">
      {isLinked ? null : <p className="muted browser-connecting">{t('browser.connecting')}</p>}
      {viewerState && tabs.length === 0 ? <SettingsEmpty>{t('browser.noTabs')}</SettingsEmpty> : null}
      {tabs.length > 0 ? (
        <div className="browser-tabs" role="group" aria-label={t('browser.tabsLabel')}>
          {tabs.map((tab) => (
            <button
              key={String(tab.tab)}
              type="button"
              className={tab.tab === viewerState?.watching ? 'browser-tab active' : 'browser-tab'}
              aria-current={tab.tab === viewerState?.watching ? 'true' : undefined}
              title={tab.url}
              onClick={() => chooseTab(tab)}
            >
              <strong>{tab.title || siteOf(tab.url)}</strong>
              <span className="muted">{siteOf(tab.url)}</span>
              {tab.isOwn || tab.isCurrent ? (
                <span className="browser-tab-tags">
                  {tab.isOwn ? <span className="tag">{t('browser.tabOwn')}</span> : null}
                  {tab.isCurrent ? <span className="tag good">{t('browser.tabCurrent')}</span> : null}
                </span>
              ) : null}
            </button>
          ))}
        </div>
      ) : null}
      {viewerState && tabs.length > 0 ? (
        <section className="card browser-stage">
          {request ? (
            <p className="browser-request">
              <strong>{t('browser.agentAsks')}</strong> {request}
            </p>
          ) : null}
          <div className="browser-control">
            <p>{isControlling ? t('browser.youControl') : t('browser.agentControls')}</p>
            {isControlling ? (
              <Tooltip label={t('browser.keyboard')}>
                <button
                  type="button"
                  className="icon-button browser-keyboard"
                  aria-label={t('browser.keyboard')}
                  onClick={() => keyboardRef.current?.focus()}
                >
                  <KeyboardIcon size={18} />
                </button>
              </Tooltip>
            ) : null}
            {isControlling && isHandingBack ? (
              <form
                className="browser-hand-back"
                onSubmit={(event) => {
                  event.preventDefault()
                  void handBack()
                }}
              >
                <input
                  type="text"
                  value={handBackNote}
                  placeholder={t('browser.handBackNote')}
                  aria-label={t('browser.handBackNote')}
                  onChange={(event) => setHandBackNote(event.target.value)}
                />
                <button type="submit">{t('browser.handBack')}</button>
                <Tooltip label={t('browser.keepControl')}>
                  <button
                    type="button"
                    className="icon-button"
                    aria-label={t('browser.keepControl')}
                    onClick={() => setHandingBack(false)}
                  >
                    <CloseIcon size={16} />
                  </button>
                </Tooltip>
              </form>
            ) : isControlling ? (
              <button type="button" onClick={() => setHandingBack(true)}>
                {t('browser.handBack')}
              </button>
            ) : viewerState.canCast ? (
              <button type="button" disabled={!isLinked} onClick={() => send({ type: 'take_over' })}>
                {t('browser.takeOver')}
              </button>
            ) : null}
          </div>
          {!viewerState.canCast ? (
            <SettingsEmpty>{t('browser.updateExtension')}</SettingsEmpty>
          ) : drawnFrame ? (
            <img
              ref={pictureRef}
              className={isControlling ? 'browser-picture is-controlling' : 'browser-picture'}
              src={`data:image/jpeg;base64,${drawnFrame.data}`}
              alt={t('browser.pictureLabel')}
              tabIndex={0}
              draggable={false}
              onPointerDown={onPointerDown}
              onPointerMove={onPointerMove}
              onPointerUp={onPointerUp}
              onPointerCancel={onPointerUp}
              onContextMenu={(event) => {
                if (isControlling) event.preventDefault()
              }}
              onKeyDown={(event) => onKey(event, 'down')}
              onKeyUp={(event) => onKey(event, 'up')}
              onPaste={onPaste}
            />
          ) : viewerState.watching != null ? (
            <Loading />
          ) : null}
          {isControlling ? (
            <textarea
              ref={keyboardRef}
              className="visually-hidden"
              tabIndex={-1}
              aria-label={t('browser.keyboard')}
              autoCapitalize="off"
              autoCorrect="off"
              spellCheck={false}
              onInput={(event) => {
                if (!(event.nativeEvent as InputEvent).isComposing) sendKeyboardText(event.currentTarget)
              }}
              onCompositionEnd={(event) => sendKeyboardText(event.currentTarget)}
              onKeyDown={(event) => onKeyboardKey(event, 'down')}
              onKeyUp={(event) => onKeyboardKey(event, 'up')}
            />
          ) : null}
        </section>
      ) : null}
    </div>
  )
}
