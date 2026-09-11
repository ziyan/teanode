// The agent's panel on the page: the dashboard's own drawer, framed from
// the server, with a bar above it to attach this tab and to close it. Run
// once by the extension's button; run again, it toggles. On the
// dashboard's own pages the dashboard's drawer opens instead of a copy.
;(() => {
  const ID = 'teanode-agent-panel'
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
    host.style.cssText = 'position:fixed;right:20px;bottom:20px;z-index:2147483647;width:min(440px,calc(100vw - 40px));height:min(760px,calc(100vh - 40px));'
    const shadow = host.attachShadow({ mode: 'closed' })
    const style = document.createElement('style')
    style.textContent = `
      :host { color-scheme: light dark; }
      .panel { --background: #ffffff; --border: #e7e7e4; --text: #18181b; --muted: #6f6f76; --accent: #1f1f22; --accent-text: #ffffff; --leaf: #729d39; --hover: #efefec;
        display: flex; flex-direction: column; width: 100%; height: 100%; border: 1px solid var(--border); border-radius: 14px; background: var(--background); color: var(--text);
        box-shadow: 0 12px 40px rgba(0, 0, 0, 0.22); overflow: hidden; font: 13px/1.4 -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; }
      @media (prefers-color-scheme: dark) { .panel:not([data-theme='light']) { --background: #141416; --border: #2a2a2e; --text: #f4f4f5; --muted: #a0a0a8; --accent: #f4f4f5; --accent-text: #18181b; --leaf: #9fca63; --hover: #17171a; } }
      .panel[data-theme='dark'] { --background: #141416; --border: #2a2a2e; --text: #f4f4f5; --muted: #a0a0a8; --accent: #f4f4f5; --accent-text: #18181b; --leaf: #9fca63; --hover: #17171a; }
      .bar { display: flex; align-items: center; gap: 8px; padding: 6px 8px 6px 12px; border-bottom: 1px solid var(--border); }
      .mark { width: 18px; height: 18px; border-radius: 5px; background: var(--leaf); color: #fff; font-size: 8px; font-weight: 700; display: grid; place-items: center; }
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
    mark.textContent = 'TN'
    const name = document.createElement('span')
    name.className = 'name'
    name.textContent = 'TeaNode'
    const attach = document.createElement('button')
    bar.append(mark, name, attach)
    const frame = document.createElement('iframe')
    // The token goes in the frame's address, in the fragment: the server
    // never sees it, and neither does the page around the frame, which
    // cannot read a cross-origin frame's address and cannot see into a
    // closed shadow root. A message would not do — the page around the
    // frame is its parent as much as this script is.
    frame.src = origin + '/drawer#token=' + encodeURIComponent(answer.token)
    frame.title = 'Your agent'
    panel.append(bar, frame)
    shadow.append(style, panel)
    document.documentElement.append(host)
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
