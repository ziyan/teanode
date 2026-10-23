// The agent's actions inside a page of the person's browser: a snapshot of it
// as a tree with a [ref=N] on everything that can be acted on, and click,
// type, select, press, scroll, wait, fetch and storage (evaluate runs
// through the debugger, in background.js: a content script may not compile
// text into code). background.js
// runs inPage in the tab with chrome.scripting.executeScript, which sends the
// function's source and runs it in the page's isolated world.
//
// inPage runs inside the page. It is self-contained: nothing from this
// file is in scope there.
export async function inPage(action, args, ownHostname, ownPagesRefusal, panel, panelWords) {
  // The check that was made on the tab's address a moment ago, made again
  // where the script runs: a page can go to TeaNode in between.
  if (/^(chrome|moz)-extension:$/.test(location.protocol) || (ownHostname && location.hostname.replace(/\.$/, '').toLowerCase() === ownHostname)) {
    return { ok: false, error: ownPagesRefusal }
  }
  // Whether an element is under the person's TeaNode panel, which is where
  // the agent does not act.
  const isUnderPanel = (node) => {
    if (!panel || !panel.isOpen) return false
    const box = node.getBoundingClientRect()
    return box.left <= panel.right && box.right >= panel.left && box.top <= panel.bottom && box.bottom >= panel.top
  }
  const refs = (window.__teanodeRefs = window.__teanodeRefs || new Map())
  // How many of a select's options a snapshot lists.
  const OPTIONS_SHOWN = 25
  let next = window.__teanodeNextRef || 1
  const interactive = new Set(['a', 'button', 'input', 'select', 'textarea', 'summary', 'option', 'label'])
  // isFilledDocument: the tab shows a document the agent filled a password or
  // a code into (background.js sets it, after whatever came with the action).
  // The value of no input or textarea is then read into a name or a snapshot,
  // not only the filled ones: a field the filled text was copied into is not
  // known to be a secret.
  const isFilledDocument = args.isFilledDocument === true
  const isTextField = (node) => node instanceof HTMLInputElement || node instanceof HTMLTextAreaElement
  // The inputs that take typed text: not a checkbox, a button or a file.
  const UNTYPED_INPUTS = ['checkbox', 'radio', 'button', 'submit', 'reset', 'image', 'file', 'hidden', 'range', 'color']
  const takesTypedText = (node) =>
    node instanceof HTMLTextAreaElement ||
    node.isContentEditable ||
    (node instanceof HTMLInputElement && !UNTYPED_INPUTS.includes((node.type || 'text').toLowerCase()))
  // A label stands for the field it labels: typing into or choosing in a
  // label acts on its field. A click stays on the label, as the person's
  // would.
  const fieldOf = (node) => (node instanceof HTMLLabelElement && node.control ? node.control : node)
  // Whether an element is drawn: a selector that also matches a hidden
  // element (a closed menu's button) means the one the person can see.
  const isDrawn = (node) => {
    const style = window.getComputedStyle(node)
    if (style.visibility === 'hidden' || style.display === 'none' || node.hidden) return false
    const box = node.getBoundingClientRect()
    return box.width > 0 || box.height > 0
  }
  const find = () => {
    // A ref comes back through a tool call, where it may be a string.
    if (args.ref) {
      return refs.get(Number(args.ref)) || refs.get(args.ref) || null
    }
    if (args.selector && !isFilledDocument) {
      const matches = Array.from(document.querySelectorAll(args.selector))
      return matches.find(isDrawn) || matches[0] || null
    }
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
  // The words of a field's labels, without the words of the field itself: a
  // label around a select holds every option's text too.
  const labelWordsOf = (node) => {
    if (!node.labels || node.labels.length === 0) return ''
    const words = []
    const collect = (part) => {
      if (part === node) return
      if (part.nodeType === Node.TEXT_NODE) words.push(part.textContent)
      else if (part.nodeType === Node.ELEMENT_NODE && !part.isContentEditable && !['select', 'textarea', 'input', 'output', 'script', 'style'].includes(part.tagName.toLowerCase())) for (const child of part.childNodes) collect(child)
    }
    for (const label of node.labels) collect(label)
    return words.join(' ').trim().replace(/\s+/g, ' ').slice(0, 120)
  }
  const nameOf = (node) => {
    const label = node.getAttribute && (node.getAttribute('aria-label') || labelWordsOf(node) || node.getAttribute('title') || node.getAttribute('placeholder') || node.getAttribute('name'))
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
      // A page that draws itself after it loads (most of them) is read
      // once it has: loaded, and its document quiet for a moment. Without
      // this a snapshot straight after navigate or click read a page with
      // half its content, and the agent had to wait and ask again.
      const startedAt = Date.now()
      while (document.readyState !== 'complete' && Date.now() - startedAt < 5000) await new Promise((resolve) => setTimeout(resolve, 100))
      await new Promise((resolve) => {
        let quiet = null
        let longest = null
        let observer = null
        const finish = () => {
          if (observer) observer.disconnect()
          clearTimeout(quiet)
          clearTimeout(longest)
          resolve()
        }
        observer = new MutationObserver(() => {
          clearTimeout(quiet)
          quiet = setTimeout(finish, 200)
        })
        observer.observe(document.documentElement, { childList: true, subtree: true, characterData: true })
        quiet = setTimeout(finish, 200)
        longest = setTimeout(finish, 1500)
      })
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
        // A label of a field is not a thing to act on of its own: the field
        // has the ref, named by the label, and the label's words are read
        // as it walks on. A label whose field is not drawn (a checkbox the
        // page draws itself) is what the person clicks, so it keeps one.
        // A checkbox or a radio is often moved off the page or clipped to a
        // pixel, with its label drawn as the box: that label keeps its ref.
        const isFieldLabel = tag === 'label' && node.control && (takesTypedText(node.control) || node.control instanceof HTMLSelectElement) && isDrawn(node.control)
        if (!isFieldLabel && (interactive.has(tag) || (node.getAttribute('role') && /button|link|checkbox|radio|textbox|combobox|tab|menuitem/.test(node.getAttribute('role'))))) {
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
            // A long list (every country's calling code) is cut short: select
            // takes an option's text or value whether it was shown or not.
            const shown = Array.from(node.options).filter((option, index) => index < OPTIONS_SHOWN || option.selected)
            for (const option of shown) lines.push('  '.repeat(depth + 1) + '- option ' + JSON.stringify(option.text) + (option.selected ? ' (selected)' : ''))
            if (node.options.length > shown.length) lines.push('  '.repeat(depth + 1) + '- and ' + (node.options.length - shown.length) + ' more options')
            total += 30 * shown.length
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
      if (isUnderPanel(node)) return { ok: false, error: panelWords }
      node.click()
      await new Promise((resolve) => setTimeout(resolve, 300))
      return { ok: true, data: { clicked: nameOf(node), now: { url: location.href, title: document.title } } }
    }
    case 'hover': {
      const node = find()
      if (!node) return { ok: false, error: 'nothing matches' }
      if (isUnderPanel(node)) return { ok: false, error: panelWords }
      node.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))
      return { ok: true, data: { hovered: nameOf(node) } }
    }
    case 'type': {
      const found = find()
      const node = found && fieldOf(found)
      if (!node) return { ok: false, error: 'nothing matches; take a snapshot first' }
      if (isUnderPanel(node)) return { ok: false, error: panelWords }
      if (!takesTypedText(node)) return { ok: false, error: 'that is not a field to type into; take a snapshot and use the ref of the field itself' }
      node.focus()
      if (args.clear_first && 'value' in node) node.value = ''
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
      const found = find()
      const node = found && fieldOf(found)
      if (!node || node.tagName.toLowerCase() !== 'select') return { ok: false, error: 'not a select' }
      if (isUnderPanel(node)) return { ok: false, error: panelWords }
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
    case 'fetch': {
      // With the tab's own session: the primitive for "download my
      // statement". Same origin only, so a page cannot be used to reach
      // another site the person is signed into.
      try {
        const target = new URL(args.url || '', location.href)
        if (target.origin !== location.origin) return { ok: false, error: 'fetch is limited to the attached page’s own site' }
        if (ownHostname && target.hostname.replace(/\.$/, '').toLowerCase() === ownHostname) return { ok: false, error: ownPagesRefusal }
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
