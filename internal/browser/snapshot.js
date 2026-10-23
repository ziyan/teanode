// The page as a model reads it: a tree of the elements that matter, with a
// [ref=N] on everything it could act on, so "click the Track button" is a
// reference and not a guess at a selector. Text mode is the page's words
// alone. Refs are kept on the elements themselves, so a later click finds
// the same node.
(function (mode, maximum, isLocked) {
  const interactive = new Set(['a', 'button', 'input', 'select', 'textarea', 'summary', 'option', 'label']);
  const roles = new Set(['button', 'link', 'checkbox', 'radio', 'textbox', 'combobox', 'listbox', 'menuitem', 'tab', 'switch', 'option', 'searchbox', 'slider', 'spinbutton']);
  const skip = new Set(['script', 'style', 'noscript', 'svg', 'path', 'template', 'head', 'meta', 'link']);
  // How many of a select's options a snapshot lists.
  const optionsShown = 25;
  window.__teanodeRefs = window.__teanodeRefs || new Map();
  let next = window.__teanodeNextRef || 1;
  function visible(element) {
    if (!(element instanceof Element)) return true;
    const style = window.getComputedStyle(element);
    if (style.display === 'none' || style.visibility === 'hidden' || element.hidden) return false;
    const rect = element.getBoundingClientRect();
    return !(rect.width === 0 && rect.height === 0 && element.childElementCount === 0);
  }
  // While the page is locked (a password or code was filled into it), no
  // input, textarea or editable region shows what it holds, whoever or
  // whatever put it there: text that was copied from a filled field and pasted
  // into another one is a way to read the secret back that comparing a value
  // with the secret cannot catch. The page's own flag for a filled document
  // counts too, so a page that was filled stays hidden if the server's lock
  // is not known.
  const hideValues = !!isLocked || !!window.__teanodeFilled;
  // Whether an element holds text somebody typed or pasted. A button's value
  // is its label and a checkbox's is the page's own, so those are not.
  function holdsTypedText(element) {
    if (element instanceof HTMLTextAreaElement || element.isContentEditable) return true;
    if (!(element instanceof HTMLInputElement)) return false;
    return !['button', 'submit', 'reset', 'image', 'checkbox', 'radio'].includes((element.type || 'text').toLowerCase());
  }
  // A field that holds a secret: a password, or a code the page asked for as
  // one, or any input the agent filled a password or a code into, whatever its
  // type is by now (a show-password button turns a password field into a text
  // one). Its value is never read into a snapshot, whoever put it there, so a
  // password filled in for the person is not sent to the model afterwards.
  // An input that cannot be told is taken to be one.
  // A text input a page draws as dots with -webkit-text-security (how a card
  // number or a code is masked without a password input) holds a secret too,
  // whatever its type and name say. A style that cannot be read is taken to be
  // masked.
  function isMasked(element) {
    try {
      const style = window.getComputedStyle(element);
      const mask = style.getPropertyValue('-webkit-text-security') || style.webkitTextSecurity || '';
      return mask !== '' && mask !== 'none';
    } catch (error) {
      return true;
    }
  }
  function isSecretField(element) {
    if (!(element instanceof HTMLInputElement)) return false;
    try {
      if (window.__teanodeFilled && window.__teanodeFilled.has(element)) return true;
    } catch (error) {
      return true;
    }
    if (isMasked(element)) return true;
    return (element.type || '').toLowerCase() === 'password' || /(^|\s)(current-password|new-password|one-time-code)(\s|$)/i.test(element.getAttribute('autocomplete') || '');
  }
  // The words of a field's labels, without the words of the field itself: a
  // label around a select holds every option's text too.
  function labelWordsOf(element) {
    if (!element.labels || element.labels.length === 0) return '';
    const words = [];
    function collect(part) {
      if (part === element) return;
      if (part.nodeType === Node.TEXT_NODE) { words.push(part.textContent); return; }
      if (part.nodeType !== Node.ELEMENT_NODE || ['select', 'textarea', 'input', 'script', 'style'].includes(part.tagName.toLowerCase())) return;
      for (const child of part.childNodes) collect(child);
    }
    for (const label of element.labels) collect(label);
    return words.join(' ').trim().replace(/\s+/g, ' ').slice(0, 120);
  }
  function nameOf(element) {
    const label = element.getAttribute && (element.getAttribute('aria-label') || labelWordsOf(element) || element.getAttribute('title') || element.getAttribute('placeholder') || element.getAttribute('alt') || element.getAttribute('name'));
    if (label) return label.trim();
    if (hideValues && holdsTypedText(element)) return '';
    const text = (element.innerText || (isSecretField(element) ? '' : element.value) || element.textContent || '').trim().replace(/\s+/g, ' ');
    return text.slice(0, 120);
  }
  function refFor(element) {
    for (const [ref, candidate] of window.__teanodeRefs) if (candidate === element) return ref;
    const ref = next++;
    window.__teanodeRefs.set(ref, element);
    return ref;
  }
  function isInteractive(element) {
    const tag = element.tagName.toLowerCase();
    // A label of a field is not a thing to act on of its own: the field has
    // the ref, named by the label, and the label's words are read as the walk
    // goes on. A label whose field is not drawn (a checkbox the page draws
    // itself) is what the person clicks, so it keeps one.
    if (tag === 'label' && element.control && visible(element.control)) return false;
    if (interactive.has(tag)) return true;
    const role = element.getAttribute('role');
    if (role && roles.has(role)) return true;
    if (element.hasAttribute('onclick') || element.getAttribute('tabindex') === '0' || element.isContentEditable) return true;
    return false;
  }
  if (mode === 'text') {
    let text = (document.body ? document.body.innerText : '') || '';
    if (hideValues) {
      // Without the editable regions: their text is what somebody typed or pasted.
      const parts = [];
      (function collect(node) {
        if (node.nodeType === Node.TEXT_NODE) {
          const piece = node.textContent.trim();
          if (piece) parts.push(piece);
          return;
        }
        if (node.nodeType !== Node.ELEMENT_NODE) return;
        if (skip.has(node.tagName.toLowerCase()) || !visible(node)) return;
        if (node instanceof HTMLTextAreaElement || node.isContentEditable) return;
        for (const child of node.childNodes) collect(child);
      })(document.body || document.documentElement);
      text = parts.join('\n');
    }
    return { title: document.title, url: location.href, text: text.slice(0, maximum), truncated: text.length > maximum };
  }
  const lines = [];
  let total = 0;
  function walk(node, depth) {
    if (total > maximum) return;
    if (node.nodeType === Node.TEXT_NODE) {
      const text = node.textContent.trim().replace(/\s+/g, ' ');
      if (text) { lines.push('  '.repeat(depth) + text.slice(0, 200)); total += text.length; }
      return;
    }
    if (node.nodeType !== Node.ELEMENT_NODE) return;
    const tag = node.tagName.toLowerCase();
    if (skip.has(tag) || !visible(node)) return;
    if (isInteractive(node)) {
      const ref = refFor(node);
      let description = tag;
      const role = node.getAttribute('role');
      if (role) description = role;
      if (tag === 'input') description = 'input(' + (node.type || 'text') + ')';
      const name = nameOf(node);
      let extra = '';
      if (tag === 'a' && node.href) extra = ' href=' + node.getAttribute('href');
      if ((tag === 'input' || tag === 'textarea') && node.value) {
        // A value too long to show whole is not shown at all: its first
        // characters would be a part of whatever it is.
        if (isSecretField(node)) extra = ' (filled, value not shown)';
        else if (hideValues && holdsTypedText(node)) extra = ' (has text, not shown while the page is locked)';
        else if (node.value.length > 80) extra = ' (long value, not shown)';
        else extra = ' value=' + JSON.stringify(node.value);
      }
      if (tag === 'input' && (node.type === 'checkbox' || node.type === 'radio')) extra = node.checked ? ' checked' : '';
      if (tag === 'select') extra = ' value=' + JSON.stringify(node.value);
      lines.push('  '.repeat(depth) + '[ref=' + ref + '] ' + description + (name ? ' "' + name + '"' : '') + extra);
      total += 40 + (name ? name.length : 0);
      if (hideValues && node.isContentEditable) {
        lines[lines.length - 1] += ' (text not shown while the page is locked)';
        return;
      }
      if (tag === 'select') {
        // A long list (every country's calling code) is cut short: select
        // takes an option's text or value whether it was shown or not.
        const shown = Array.from(node.options).filter((option, index) => index < optionsShown || option.selected);
        for (const option of shown) { lines.push('  '.repeat(depth + 1) + '- option ' + JSON.stringify(option.text) + (option.selected ? ' (selected)' : '')); }
        if (node.options.length > shown.length) lines.push('  '.repeat(depth + 1) + '- and ' + (node.options.length - shown.length) + ' more options');
        total += 30 * shown.length;
        return;
      }
      if (tag === 'a' || tag === 'button' || tag === 'label' || tag === 'option') return;
      if (tag === 'textarea' && hideValues) return;
    } else if (/^h[1-6]$/.test(tag)) {
      lines.push('  '.repeat(depth) + tag + ': ' + (node.innerText || '').trim().replace(/\s+/g, ' ').slice(0, 200));
      total += 20;
      return;
    } else if (tag === 'img' && node.alt) {
      lines.push('  '.repeat(depth) + 'image "' + node.alt.slice(0, 80) + '"');
      return;
    }
    for (const child of node.childNodes) walk(child, depth + (['div', 'span', 'section', 'main', 'article', 'body', 'html', 'p'].includes(tag) ? 0 : 1));
  }
  walk(document.documentElement, 0);
  window.__teanodeNextRef = next;
  return { title: document.title, url: location.href, text: lines.join('\n'), truncated: total > maximum };
})
