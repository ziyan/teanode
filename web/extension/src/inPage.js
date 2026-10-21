// The agent's actions inside a page of the person's browser: a snapshot of it
// as a tree with a [ref=N] on everything that can be acted on, and click,
// type, select, press, scroll, wait, evaluate, fetch and storage. background.js
// runs inPage in the tab with chrome.scripting.executeScript, which sends the
// function's source and runs it in the page's isolated world.
//
// inPage runs inside the page. It is self-contained: nothing from this
// file is in scope there.
export async function inPage(action, args) {
  const refs = (window.__teanodeRefs = window.__teanodeRefs || new Map())
  let next = window.__teanodeNextRef || 1
  const interactive = new Set(['a', 'button', 'input', 'select', 'textarea', 'summary', 'option', 'label'])
  // isFilledDocument: the tab shows a document the agent filled a password or
  // a code into (background.js sets it, after whatever came with the action).
  // The value of no input or textarea is then read into a name or a snapshot,
  // not only the filled ones: a field the filled text was copied into is not
  // known to be a secret.
  const isFilledDocument = args.isFilledDocument === true
  const isTextField = (node) => node instanceof HTMLInputElement || node instanceof HTMLTextAreaElement
  const find = () => {
    // A ref comes back through a tool call, where it may be a string.
    if (args.ref) return refs.get(Number(args.ref)) || refs.get(args.ref) || null
    if (args.selector && !isFilledDocument) return document.querySelector(args.selector)
    return null
  }
  // Which fields hold a secret. It decides what a snapshot says, not what
  // may be typed: a password the person has already filled in is not read
  // back into the conversation, where it would be kept and sent onward.
  // Typing into one is their agent's business and goes ahead.
  // A text input a page draws as dots with -webkit-text-security (how a card
  // number or a code is masked without a password input) holds a secret too,
  // whatever its type and name say. A style that cannot be read is taken to
  // be masked.
  const isMasked = (node) => {
    try {
      const style = window.getComputedStyle(node)
      const mask = style.getPropertyValue('-webkit-text-security') || style.webkitTextSecurity || ''
      return mask !== '' && mask !== 'none'
    } catch {
      return true
    }
  }
  const sensitive = (node) => {
    const type = (node.type || '').toLowerCase()
    const hint = ((node.autocomplete || '') + ' ' + (node.name || '') + ' ' + (node.id || '') + ' ' + (node.getAttribute('aria-label') || '')).toLowerCase()
    return type === 'password' || /cc-|card|cvc|cvv|iban|account-?number|routing|ssn|pass/.test(hint) || isMasked(node)
  }
  // A field a password, or a code the page asked for as one, is typed into:
  // its value is never read into a name or a snapshot, whoever put it there.
  // An input the agent filled a password or a code into is one whatever its
  // type is by now (a show-password button makes it a text input), and an
  // input that cannot be told is taken to be one.
  const isSecretField = (node) => {
    if (!(node instanceof HTMLInputElement)) return false
    try {
      if (window.__teanodeFilled && window.__teanodeFilled.has(node)) return true
    } catch {
      return true
    }
    return (node.type || '').toLowerCase() === 'password' || /(^|\s)(current-password|new-password|one-time-code)(\s|$)/i.test(node.getAttribute('autocomplete') || '') || isMasked(node)
  }
  const nameOf = (node) => {
    const label = node.getAttribute && (node.getAttribute('aria-label') || node.getAttribute('title') || node.getAttribute('placeholder') || node.getAttribute('name'))
    if (label) return label.trim()
    if (isFilledDocument && isTextField(node)) return ''
    const value = isSecretField(node) || sensitive(node) ? '' : node.value
    return ((node.innerText || value || node.textContent || '').trim().replace(/\s+/g, ' ')).slice(0, 120)
  }
  const refFor = (node) => {
    for (const [ref, candidate] of refs) if (candidate === node) return ref
    const ref = next++
    refs.set(ref, node)
    window.__teanodeNextRef = next
    return ref
  }
  switch (action) {
    case 'snapshot': {
      const maximum = args.max_characters || 20000
      if (args.mode === 'text') {
        const text = document.body ? document.body.innerText : ''
        return { ok: true, data: { title: document.title, url: location.href, text: text.slice(0, maximum), truncated: text.length > maximum } }
      }
      const lines = []
      let total = 0
      const walk = (node, depth) => {
        if (total > maximum) return
        if (node.nodeType === Node.TEXT_NODE) {
          const text = node.textContent.trim().replace(/\s+/g, ' ')
          if (text) {
            lines.push('  '.repeat(depth) + text.slice(0, 200))
            total += text.length
          }
          return
        }
        if (node.nodeType !== Node.ELEMENT_NODE) return
        const tag = node.tagName.toLowerCase()
        if (['script', 'style', 'noscript', 'svg', 'template', 'head'].includes(tag)) return
        const style = window.getComputedStyle(node)
        if (style.display === 'none' || style.visibility === 'hidden') return
        if (interactive.has(tag) || (node.getAttribute('role') && /button|link|checkbox|radio|textbox|combobox|tab|menuitem/.test(node.getAttribute('role')))) {
          const ref = refFor(node)
          let description = node.getAttribute('role') || tag
          if (tag === 'input') description = 'input(' + (node.type || 'text') + ')'
          let extra = ''
          if (tag === 'a' && node.getAttribute('href')) extra = ' href=' + node.getAttribute('href')
          // A value too long to show whole is not shown at all: its first
          // characters would be a part of whatever it is.
          if ((tag === 'input' || tag === 'textarea') && node.value && !sensitive(node)) extra = node.value.length > 80 ? ' (long value, not shown)' : ' value=' + JSON.stringify(node.value)
          if (isFilledDocument && isTextField(node) && node.value) extra = ' (value not shown)'
          else if (isSecretField(node) && node.value) extra = ' (filled, value not shown)'
          else if (sensitive(node)) extra = ' (holds a secret)'
          lines.push('  '.repeat(depth) + '[ref=' + ref + '] ' + description + ' "' + nameOf(node) + '"' + extra)
          total += 40
          if (tag === 'select') {
            for (const option of node.options) lines.push('  '.repeat(depth + 1) + '- option ' + JSON.stringify(option.text) + (option.selected ? ' (selected)' : ''))
          }
          if (['a', 'button', 'label', 'option', 'select'].includes(tag)) return
        } else if (/^h[1-6]$/.test(tag)) {
          lines.push('  '.repeat(depth) + tag + ': ' + (node.innerText || '').trim().slice(0, 200))
          return
        }
        for (const child of node.childNodes) walk(child, depth + (['div', 'span', 'section', 'main', 'article', 'body', 'html', 'p'].includes(tag) ? 0 : 1))
      }
      walk(document.documentElement, 0)
      return { ok: true, data: { title: document.title, url: location.href, text: lines.join('\n'), truncated: total > maximum } }
    }
    case 'click': {
      const node = find()
      if (!node) return { ok: false, error: 'nothing matches; take a snapshot first' }
      node.scrollIntoView({ block: 'center' })
      node.click()
      await new Promise((resolve) => setTimeout(resolve, 300))
      return { ok: true, data: { clicked: nameOf(node), now: { url: location.href, title: document.title } } }
    }
    case 'hover': {
      const node = find()
      if (!node) return { ok: false, error: 'nothing matches' }
      node.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))
      return { ok: true, data: { hovered: nameOf(node) } }
    }
    case 'type': {
      const node = find()
      if (!node) return { ok: false, error: 'nothing matches; take a snapshot first' }
      node.focus()
      if (args.clear_first) node.value = ''
      if ('value' in node) {
        node.value = (node.value || '') + (args.text || '')
        node.dispatchEvent(new Event('input', { bubbles: true }))
        node.dispatchEvent(new Event('change', { bubbles: true }))
      } else if (node.isContentEditable) {
        document.execCommand('insertText', false, args.text || '')
      }
      if (args.submit && node.form) {
        node.form.requestSubmit ? node.form.requestSubmit() : node.form.submit()
      }
      return { ok: true, data: { typed: (args.text || '').length } }
    }
    case 'select': {
      const node = find()
      if (!node || node.tagName.toLowerCase() !== 'select') return { ok: false, error: 'not a select' }
      let picked = null
      for (const option of node.options) if (option.value === args.value || option.text.trim() === args.value) picked = option
      if (!picked && typeof args.index === 'number') picked = node.options[args.index]
      if (!picked) return { ok: false, error: 'no such option' }
      node.value = picked.value
      node.dispatchEvent(new Event('change', { bubbles: true }))
      return { ok: true, data: { selected: picked.text } }
    }
    case 'press': {
      const active = document.activeElement || document.body
      for (const kind of ['keydown', 'keypress', 'keyup']) active.dispatchEvent(new KeyboardEvent(kind, { key: args.key, bubbles: true }))
      if (args.key === 'Enter' && active.form) active.form.requestSubmit ? active.form.requestSubmit() : active.form.submit()
      return { ok: true, data: { pressed: args.key } }
    }
    case 'scroll': {
      const node = find() || window
      const height = (node === window ? window.innerHeight : node.clientHeight) * (args.amount || 1) * (args.direction === 'up' ? -1 : 1)
      node.scrollBy({ top: height })
      return { ok: true, data: { scrolled: args.direction || 'down' } }
    }
    case 'wait': {
      const timeout = args.timeout_ms || 30000
      const started = Date.now()
      while (Date.now() - started < timeout) {
        if (args.for === 'selector' && !isFilledDocument && document.querySelector(args.selector)) return { ok: true, data: { ended_by: 'selector' } }
        if ((args.for === 'navigation' || !args.for) && document.readyState === 'complete') return { ok: true, data: { ended_by: 'load' } }
        if (args.for === 'timeout') {
        // Waiting out the whole time is the point of asking for it.
        await new Promise((resolve) => setTimeout(resolve, Math.min(250, timeout)))
        continue
      }
        await new Promise((resolve) => setTimeout(resolve, 250))
      }
      return { ok: true, data: { ended_by: 'timeout' } }
    }
    case 'evaluate': {
      try {
        const value = await Promise.resolve(new Function('return (' + args.expression + ')')())
        return { ok: true, data: JSON.parse(JSON.stringify(value === undefined ? null : value)) }
      } catch (error) {
        return { ok: false, error: String(error) }
      }
    }
    case 'fetch': {
      // With the tab's own session: the primitive for "download my
      // statement". Same origin only, so a page cannot be used to reach
      // another site the person is signed into.
      try {
        const target = new URL(args.url || '', location.href)
        if (target.origin !== location.origin) return { ok: false, error: 'fetch is limited to the attached page’s own site' }
        // Following a redirect would take the person's cookies off this
      // site: any open redirect on it would otherwise read any site they
      // are signed into.
      const response = await fetch(target.toString(), { credentials: 'include', redirect: 'manual' })
      if (response.type === 'opaqueredirect' || (response.status >= 300 && response.status < 400)) {
        return { ok: false, error: 'that address redirects off this site, which is not followed with the page\'s session' }
      }
        const type = response.headers.get('content-type') || ''
        if (/^text\/|json|xml/.test(type)) {
          const text = await response.text()
          return { ok: true, data: { status: response.status, content_type: type, text: text.slice(0, args.max_characters || 20000) } }
        }
        const bytes = await response.arrayBuffer()
        if (bytes.byteLength > 5 * 1024 * 1024) return { ok: false, error: 'the file is over five megabytes' }
        let binary = ''
        for (const byte of new Uint8Array(bytes)) binary += String.fromCharCode(byte)
        return { ok: true, data: { status: response.status, content_type: type, base64: btoa(binary), bytes: bytes.byteLength } }
      } catch (error) {
        return { ok: false, error: String(error) }
      }
    }
    case 'storage': {
      // Only the attached site's own storage, and never another's.
      const entries = {}
      for (let index = 0; index < localStorage.length; index++) {
        const key = localStorage.key(index)
        entries[key] = (localStorage.getItem(key) || '').slice(0, 500)
      }
      return { ok: true, data: { origin: location.origin, localStorage: entries } }
    }
  }
  return { ok: false, error: 'unknown action ' + action }
}
