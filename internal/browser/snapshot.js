// The page as a model reads it: a tree of the elements that matter, with a
// [ref=N] on everything it could act on, so "click the Track button" is a
// reference and not a guess at a selector. Text mode is the page's words
// alone. Refs are kept on the elements themselves, so a later click finds
// the same node.
(function (mode, maximum) {
  const interactive = new Set(['a', 'button', 'input', 'select', 'textarea', 'summary', 'option', 'label']);
  const roles = new Set(['button', 'link', 'checkbox', 'radio', 'textbox', 'combobox', 'listbox', 'menuitem', 'tab', 'switch', 'option', 'searchbox', 'slider', 'spinbutton']);
  const skip = new Set(['script', 'style', 'noscript', 'svg', 'path', 'template', 'head', 'meta', 'link']);
  window.__teanodeRefs = window.__teanodeRefs || new Map();
  let next = window.__teanodeNextRef || 1;
  function visible(element) {
    if (!(element instanceof Element)) return true;
    const style = window.getComputedStyle(element);
    if (style.display === 'none' || style.visibility === 'hidden' || element.hidden) return false;
    const rect = element.getBoundingClientRect();
    return !(rect.width === 0 && rect.height === 0 && element.childElementCount === 0);
  }
  function nameOf(element) {
    const label = element.getAttribute && (element.getAttribute('aria-label') || element.getAttribute('title') || element.getAttribute('placeholder') || element.getAttribute('alt') || element.getAttribute('name'));
    if (label) return label.trim();
    if (element.id) {
      const forLabel = document.querySelector('label[for="' + CSS.escape(element.id) + '"]');
      if (forLabel) return forLabel.innerText.trim();
    }
    const text = (element.innerText || element.value || element.textContent || '').trim().replace(/\s+/g, ' ');
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
    if (interactive.has(tag)) return true;
    const role = element.getAttribute('role');
    if (role && roles.has(role)) return true;
    if (element.hasAttribute('onclick') || element.getAttribute('tabindex') === '0' || element.isContentEditable) return true;
    return false;
  }
  if (mode === 'text') {
    const text = (document.body ? document.body.innerText : '') || '';
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
      if ((tag === 'input' || tag === 'textarea') && node.value && node.type !== 'password') extra = ' value=' + JSON.stringify(node.value.slice(0, 80));
      if (tag === 'input' && (node.type === 'checkbox' || node.type === 'radio')) extra = node.checked ? ' checked' : '';
      if (tag === 'select') extra = ' value=' + JSON.stringify(node.value);
      lines.push('  '.repeat(depth) + '[ref=' + ref + '] ' + description + (name ? ' "' + name + '"' : '') + extra);
      total += 40 + (name ? name.length : 0);
      if (tag === 'select') {
        for (const option of node.options) { lines.push('  '.repeat(depth + 1) + '- option ' + JSON.stringify(option.text) + (option.selected ? ' (selected)' : '')); }
        return;
      }
      if (tag === 'a' || tag === 'button' || tag === 'label' || tag === 'option') return;
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
