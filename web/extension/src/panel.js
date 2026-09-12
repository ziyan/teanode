// The agent's panel on the page: the dashboard's own drawer, framed from
// the server, with a bar above it to attach this tab and to close it. Run
// once by the extension's button; run again, it toggles. On the
// dashboard's own pages the dashboard's drawer opens instead of a copy.
;(() => {
  const ID = 'teanode-agent-panel'
  // docs/images/logo.svg, inline. Regenerate with the same file if the
  // logo ever changes.
  const LOGO =
    '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512" width="18" height="18" aria-hidden="true"><g transform="translate(0,-610.51967)"> <path fill="#729d39" d="M 511.99979,910.51965 A 212.00003,212.00003 0 0 1 299.99976,1122.5197 212.00003,212.00003 0 0 1 87.999725,910.51965 212.00003,212.00003 0 0 1 299.99976,698.51962 212.00003,212.00003 0 0 1 511.99979,910.51965 Z m -88.00003,-88 A 212.00002,212.00002 0 0 1 211.99974,1034.5197 212.00002,212.00002 0 0 1 -2.746582e-4,822.51965 212.00002,212.00002 0 0 1 211.99974,610.51964 212.00002,212.00002 0 0 1 423.99976,822.51965 Z m -212.00002,-212 h 300 v 300 h -300 z M -2.6999999e-4,822.51971 H 299.99973 V 1122.5197 H -2.6999999e-4 Z"/> <g transform="matrix(0.41621483,0,0,0.41621483,53.083626,601.03968)" fill="#c6e377"> <path d="m 266.28598,928.39916 c -54.33136,-0.11245 -71.16015,20.05196 -71.16015,85.26364 0,59.6851 14.26544,80.9392 57.04883,85.0039 10.88826,1.0344 27.13339,0.3406 38.08789,-1.6289 27.3902,-4.9243 40.69926,-21.4817 44.91406,-55.875 1.4208,-11.5941 1.4208,-43.4058 0,-54.99997 -5.2175,-42.57541 -23.21692,-57.66917 -68.89063,-57.76367 z m -2.41015,28.35547 c 1.36238,-0.0124 2.80164,0.0131 4.32226,0.0742 8.52145,0.34226 10.26823,0.74158 15.07813,3.45898 11.3088,6.38907 15.28145,20.10818 15.31445,52.87499 0.0344,34.3246 -4.20614,48.219 -16.71094,54.7598 -5.3109,2.778 -22.60437,3.503 -29.66992,1.2441 -5.34263,-1.708 -11.52119,-7.316 -13.68164,-12.4179 -4.98456,-11.7711 -7.18031,-35.2835 -5.40039,-57.83599 1.69868,-21.52317 3.9909,-29.07684 10.83789,-35.72656 4.54271,-4.41182 10.3735,-6.34477 19.91016,-6.43164 z"/> <path d="m 414.96176,929.69213 c -7.61692,0.0329 -16.05133,0.18678 -25.25585,0.46289 l -24.41993,0.73242 v 82.88676 82.8886 l 6.25,0.1348 c 3.4375,0.075 11.65,0.4714 18.25,0.8809 21.5892,1.3395 58.006,0.5261 66.5,-1.4844 33.60996,-7.9552 45.11996,-31.2864 43.67386,-88.5313 -0.6494,-25.70653 -2.6461,-37.05968 -8.7403,-49.71286 -6.52688,-13.55165 -15.35606,-20.78226 -30.93746,-25.33398 -6.96383,-2.03432 -22.46955,-3.02257 -45.32032,-2.92383 z m -13.67578,26.78711 19.25,0.34375 c 18.3207,0.32852 19.52113,0.47339 24.85743,2.97265 13.5389,6.341 17.54886,20.78281 16.60546,59.79496 -0.5648,23.3562 -1.83279,31.188 -6.27929,38.7754 -3.3237,5.6714 -7.8043,8.8969 -14.9336,10.7539 -4.1976,1.0933 -10.80833,1.5429 -22.71093,1.5429 h -16.78907 v -57.0918 z"/> <path d="m 555.01701,1096.6252 c -10.1976,-1.7154 -19.9878,-8.9552 -24.0792,-17.8063 -2.1274,-4.6026 -2.1523,-5.3498 -2.1523,-64.6564 v -59.99999 l 2.6858,-5.09004 c 3.27,-6.1972 8.4009,-10.85313 15.7686,-14.30885 l 5.5456,-2.60112 h 41.5 c 22.825,0 42.7394,0.34656 44.2543,0.77013 l 2.7543,0.77013 -0.5506,8.72987 c -0.3029,4.80143 -0.7824,10.64238 -1.0656,12.97988 l -0.5149,4.25 h -34.0045 c -21.41,0 -34.7171,0.38135 -35.9284,1.02963 -4.0789,2.18298 -4.9446,6.0207 -4.9446,21.9207 v 15.04967 h 33 33 v 12.99999 13 h -33 -33 v 18.5497 c 0,19.6145 0.6861,23.1416 4.9446,25.4207 1.2113,0.6483 14.5184,1.0296 35.9284,1.0296 h 34.0045 l 0.5149,4.25 c 0.2832,2.3375 0.7607,8.1518 1.061,12.9206 l 0.5461,8.6707 -2.7498,0.5772 c -3.5011,0.735 -55.5181,2.5894 -68.7497,2.451 -5.5,-0.058 -12.1458,-0.4657 -14.7685,-0.9068 z"/> <path d="m 26.506287,1018.4125 c 0.22974,-64.38917 0.52515,-78.72858 1.66775,-80.95181 2.42972,-4.72768 6.41073,-5.7982 21.56223,-5.7982 15.96665,0 17.86959,0.65791 22.29419,7.70776 4.81465,7.67136 48.786363,91.80535 53.723243,102.79225 3.45529,7.6897 4.99217,10.0781 6.6522,10.3379 1.85781,0.2907 2.05247,0.012 1.3962,-2 -0.41943,-1.2859 -0.97932,-28.5504 -1.2442,-60.58789 l -0.48161,-58.25001 h 17.12803 17.12803 l -0.27343,78.9777 -0.27344,78.9776 -2.59252,2.5922 c -3.36609,3.3656 -8.14183,4.4007 -20.40748,4.4231 -11.90232,0.022 -16.23604,-1.0162 -19.68259,-4.7142 -3.56571,-3.8259 -52.380073,-96.60288 -56.958803,-108.25639 -2.05293,-5.22501 -4.32345,-9.62401 -5.04561,-9.77558 -1.07542,-0.2257 -1.25648,10.85688 -1.00065,61.24997 l 0.31236,61.5256 h -17.09155 -17.09154 z"/> </g> <g transform="matrix(0.41621483,0,0,0.41621483,47.470149,798.84172)" fill="#fbfad3"> <path d="m 222.44557,392.33942 c -10.19756,-1.7154 -19.98784,-8.9552 -24.07916,-17.80631 -2.12747,-4.60253 -2.15234,-5.34972 -2.15234,-64.65634 v -60 l 2.68578,-5.09004 c 3.27,-6.1972 8.40088,-10.85313 15.76859,-14.30885 l 5.54563,-2.60112 h 41.5 c 22.825,0 42.73943,0.34656 44.25429,0.77013 l 2.75428,0.77013 -0.55062,8.72987 c -0.30284,4.80143 -0.78235,10.64238 -1.06556,12.97988 l -0.51494,4.25 h -34.00446 c -21.40998,0 -34.71705,0.38135 -35.92838,1.02963 -4.07894,2.18298 -4.94461,6.0207 -4.94461,21.9207 v 15.04967 h 33 33 v 13 13 h -33 -33 v 18.54966 c 0,19.61453 0.68605,23.14159 4.94461,25.4207 1.21133,0.64828 14.5184,1.02964 35.92838,1.02964 h 34.00446 l 0.51494,4.25 c 0.28321,2.3375 0.76067,8.15177 1.061,12.9206 l 0.54607,8.67065 -2.74973,0.5772 c -3.50114,0.735 -55.5181,2.5894 -68.74973,2.451 -5.5,-0.058 -12.14582,-0.4657 -14.7685,-0.9068 z"/> <path d="m 89.714067,323.87677 v -68.5 h -25 -25 v -13.84166 -13.84165 l 33.75,-0.59251 c 18.5625,-0.32587 48.937503,-0.32666 67.500003,-0.002 l 33.75,0.59072 v 13.84339 13.8434 h -24.5 -24.5 v 68.5 68.49996 h -18 -18.000003 z"/> <path transform="translate(-2.7e-4,610.51967)" d="m 402.33594,-384.03125 c -2.13527,0.004 -4.49855,0.0288 -7.1211,0.0723 -20.27203,0.33625 -24.01112,1.16439 -26.5332,5.87695 -0.6008,1.12259 -12.019,37.3532 -25.37305,80.51172 l -24.27929,78.4707 18.50976,-0.27148 18.50977,-0.27149 4.84961,-17.5 c 2.66707,-9.625 5.31056,-19.19486 5.875,-21.26562 l 1.02539,-3.76563 29.09375,0.26563 29.09375,0.26562 4.86719,17.5 c 2.67665,9.625 5.33285,19.1875 5.90234,21.25 l 1.03516,3.75 h 18.08593 18.08789 l -0.60351,-2.75 c -2.11922,-9.6545 -47.78935,-154.96756 -49.37695,-157.10745 -2.95025,-3.97654 -6.70156,-5.05652 -21.64844,-5.03125 z m -1.98438,26.78906 1.72266,8.01563 c 0.94745,4.40902 4.85035,19.17003 8.67187,32.80078 3.82152,13.63074 6.95299,25.3457 6.95899,26.0332 0.008,0.96064 -4.85152,1.25 -20.99024,1.25 -11.55,0 -21,-0.31405 -21,-0.69726 0,-0.38321 3.13431,-11.97071 6.96485,-25.75 3.83055,-13.7793 7.80935,-28.65275 8.84375,-33.05274 1.87885,-7.99199 1.88528,-8.00152 5.35547,-8.30078 z"/> </g> </g></svg>'
  const existing = document.getElementById(ID)
  if (existing) {
    existing.hidden = !existing.hidden
    return
  }
  chrome.runtime.sendMessage({ type: 'panel:token' }, (answer) => {
    if (!answer || !answer.server || !answer.token) {
      chrome.runtime.sendMessage({ type: 'panel:options' })
      return
    }
    const origin = new URL(answer.server).origin
    if (window.location.origin === origin) {
      window.dispatchEvent(new CustomEvent('teanode:agent', { detail: 'toggle' }))
      return
    }
    const host = document.createElement('div')
    host.id = ID
    // Placed by script, because the person can move and size it. Left and
    // top rather than right and bottom: dragging sets those.
    host.style.cssText = 'position:fixed;z-index:2147483647;left:0;top:0;width:440px;height:760px;'
    const shadow = host.attachShadow({ mode: 'closed' })
    const style = document.createElement('style')
    style.textContent = `
      :host { color-scheme: light dark; }
      .panel { position: relative; --background: #ffffff; --border: #e7e7e4; --text: #18181b; --muted: #6f6f76; --accent: #1f1f22; --accent-text: #ffffff; --leaf: #729d39; --hover: #efefec;
        display: flex; flex-direction: column; width: 100%; height: 100%; border: 1px solid var(--border); border-radius: 14px; background: var(--background); color: var(--text);
        box-shadow: 0 12px 40px rgba(0, 0, 0, 0.22); overflow: hidden; font: 13px/1.4 -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; }
      @media (prefers-color-scheme: dark) { .panel:not([data-theme='light']) { --background: #141416; --border: #2a2a2e; --text: #f4f4f5; --muted: #a0a0a8; --accent: #f4f4f5; --accent-text: #18181b; --leaf: #9fca63; --hover: #17171a; } }
      .panel[data-theme='dark'] { --background: #141416; --border: #2a2a2e; --text: #f4f4f5; --muted: #a0a0a8; --accent: #f4f4f5; --accent-text: #18181b; --leaf: #9fca63; --hover: #17171a; }
      .bar { display: flex; align-items: center; gap: 8px; padding: 6px 8px 6px 12px; border-bottom: 1px solid var(--border); cursor: grab; user-select: none; touch-action: none; }
      .bar.dragging { cursor: grabbing; }
      .grip { position: absolute; right: 0; bottom: 0; width: 18px; height: 18px; cursor: nwse-resize; touch-action: none; }
      .grip::after { content: ''; position: absolute; right: 4px; bottom: 4px; width: 7px; height: 7px; border-right: 2px solid var(--muted); border-bottom: 2px solid var(--muted); opacity: 0.7; }
      .mark { width: 18px; height: 18px; display: block; flex: 0 0 auto; }
      .mark svg { display: block; width: 18px; height: 18px; }
      .name { font-weight: 600; flex: 1; }
      button { font: inherit; font-weight: 550; padding: 4px 10px; border-radius: 8px; border: 1px solid var(--border); background: var(--background); color: var(--text); cursor: pointer; }
      button:hover { background: var(--hover); }
      button.primary { background: var(--accent); color: var(--accent-text); border-color: var(--accent); }
      iframe { flex: 1; border: 0; width: 100%; background: var(--background); }
    `
    const panel = document.createElement('div')
    panel.className = 'panel'
    const bar = document.createElement('div')
    bar.className = 'bar'
    const mark = document.createElement('span')
    mark.className = 'mark'
    // The logo itself, drawn into the panel rather than fetched: a page
    // with a strict policy blocks an image from the extension, and this
    // is a shape, not a request.
    mark.innerHTML = LOGO
    const name = document.createElement('span')
    name.className = 'name'
    name.textContent = 'TeaNode'
    const attach = document.createElement('button')
    attach.style.cursor = 'pointer'
    bar.append(mark, name, attach)
    const frame = document.createElement('iframe')
    // The token goes in the frame's address, in the fragment: the server
    // never sees it, and neither does the page around the frame, which
    // cannot read a cross-origin frame's address and cannot see into a
    // closed shadow root. A message would not do — the page around the
    // frame is its parent as much as this script is.
    frame.src = origin + '/drawer#token=' + encodeURIComponent(answer.token)
    frame.title = 'Your agent'
    const grip = document.createElement('div')
    grip.className = 'grip'
    grip.title = 'Drag to resize'
    panel.append(bar, frame, grip)
    shadow.append(style, panel)
    document.documentElement.append(host)

    // Where the panel sits and how big it is, remembered per site. The
    // place is kept as a fraction of the window so that a panel put in a
    // corner stays in that corner on a window of another size, while the
    // size is kept in pixels, which is what the person actually chose.
    const LEAST_WIDE = 320
    const LEAST_TALL = 360
    const GEOMETRY = 'panel:geometry:' + window.location.origin
    const clamp = (value, least, most) => Math.max(least, Math.min(most, value))

    let geometry = { across: 1, down: 1, width: 440, height: 760 }
    let writing = 0
    const remember = () => {
      clearTimeout(writing)
      writing = setTimeout(() => {
        try {
          chrome.storage.local.set({ [GEOMETRY]: geometry })
        } catch {
          // A panel that cannot remember where it was is still a panel.
        }
      }, 400)
    }
    // Put the panel where the numbers say, never off the screen: a window
    // made smaller since last time must not leave it out of reach.
    const place = () => {
      const width = clamp(geometry.width, LEAST_WIDE, Math.max(LEAST_WIDE, window.innerWidth - 16))
      const height = clamp(geometry.height, LEAST_TALL, Math.max(LEAST_TALL, window.innerHeight - 16))
      const left = clamp(geometry.across * (window.innerWidth - width), 0, Math.max(0, window.innerWidth - width))
      const top = clamp(geometry.down * (window.innerHeight - height), 0, Math.max(0, window.innerHeight - height))
      host.style.width = width + 'px'
      host.style.height = height + 'px'
      host.style.left = Math.round(left) + 'px'
      host.style.top = Math.round(top) + 'px'
    }
    // Read back where it is, as fractions, after the person let go.
    const measure = () => {
      const width = host.offsetWidth
      const height = host.offsetHeight
      const acrossRoom = Math.max(1, window.innerWidth - width)
      const downRoom = Math.max(1, window.innerHeight - height)
      geometry = {
        across: clamp(host.offsetLeft / acrossRoom, 0, 1),
        down: clamp(host.offsetTop / downRoom, 0, 1),
        width,
        height,
      }
    }
    try {
      chrome.storage.local.get([GEOMETRY], (stored) => {
        const saved = stored && stored[GEOMETRY]
        if (saved && typeof saved.width === 'number') geometry = saved
        place()
      })
    } catch {
      place()
    }
    place()

    // Dragging by the bar, and sizing by the corner. Both take the
    // pointer for the whole gesture, so it keeps up even when the pointer
    // crosses the frame, which would otherwise swallow the movement.
    let moving = null
    bar.addEventListener('pointerdown', (event) => {
      if (event.target !== bar && event.target !== name && event.target !== mark) return
      moving = { x: event.clientX - host.offsetLeft, y: event.clientY - host.offsetTop }
      bar.classList.add('dragging')
      bar.setPointerCapture(event.pointerId)
      event.preventDefault()
    })
    bar.addEventListener('pointermove', (event) => {
      if (!moving) return
      host.style.left = clamp(event.clientX - moving.x, 0, window.innerWidth - host.offsetWidth) + 'px'
      host.style.top = clamp(event.clientY - moving.y, 0, window.innerHeight - host.offsetHeight) + 'px'
    })
    const letGo = () => {
      if (!moving) return
      moving = null
      bar.classList.remove('dragging')
      measure()
      remember()
    }
    bar.addEventListener('pointerup', letGo)
    bar.addEventListener('pointercancel', letGo)

    let sizing = null
    grip.addEventListener('pointerdown', (event) => {
      sizing = { x: event.clientX, y: event.clientY, width: host.offsetWidth, height: host.offsetHeight }
      grip.setPointerCapture(event.pointerId)
      event.preventDefault()
    })
    grip.addEventListener('pointermove', (event) => {
      if (!sizing) return
      const width = clamp(sizing.width + (event.clientX - sizing.x), LEAST_WIDE, window.innerWidth - host.offsetLeft)
      const height = clamp(sizing.height + (event.clientY - sizing.y), LEAST_TALL, window.innerHeight - host.offsetTop)
      host.style.width = width + 'px'
      host.style.height = height + 'px'
    })
    const stopSizing = () => {
      if (!sizing) return
      sizing = null
      measure()
      remember()
    }
    grip.addEventListener('pointerup', stopSizing)
    grip.addEventListener('pointercancel', stopSizing)

    window.addEventListener('resize', place)
    // A site whose own policy refuses the frame shows nothing and says
    // nothing; after a while the bar says it instead.
    let signedIn = false
    const notice = document.createElement('span')
    notice.className = 'name'
    setTimeout(() => {
      if (signedIn) return
      notice.textContent = 'This site does not allow the panel here; open the dashboard instead.'
      frame.replaceWith(notice)
      attach.hidden = true
    }, 8000)

    let attached = !!answer.attached
    const draw = () => {
      attach.textContent = attached ? 'Detach this tab' : 'Attach this tab'
      attach.className = attached ? '' : 'primary'
      attach.title = attached ? 'The agent can no longer act in this tab' : 'Let the agent read and act in this tab, with your session, while you watch'
    }
    draw()
    attach.addEventListener('click', () => {
      chrome.runtime.sendMessage({ type: attached ? 'panel:detach' : 'panel:attach' }, (state) => {
        attached = !!(state && state.attached)
        draw()
      })
    })
    chrome.runtime.onMessage.addListener((message) => {
      if (message && message.type === 'state') {
        attached = !!message.attached
        draw()
      }
    })
    // What the frame says, and only the frame: whom it signed in as —
    // which must be the person, or the page around it has navigated the
    // frame to somebody else's — the theme it wears, and its close mark.
    window.addEventListener('message', (event) => {
      if (event.source !== frame.contentWindow || event.origin !== origin) return
      const message = event.data
      if (!message) return
      if (message.teanode === 'signedIn') {
        if (answer.username && message.username !== answer.username) {
          notice.textContent = 'This page interfered with the panel. Close it and open the dashboard instead.'
          frame.replaceWith(notice)
          attach.hidden = true
          return
        }
        signedIn = true
      }
      if (message.teanode === 'theme') panel.dataset.theme = message.theme === 'dark' ? 'dark' : 'light'
      if (message.teanode === 'close') host.hidden = true
    })
  })
})()
