// Whether the person has just done something on the page: pressed a key,
// clicked or tapped. The browser keeps this for a few seconds after each such
// gesture and a script cannot make one. A page's script can focus a field,
// submit a form or click a button without anybody touching anything, so
// before the extension shows a suggestion or remembers a sign-in as the
// person's, the browser is asked whether a gesture has just happened. A
// browser that cannot say counts as no.
export function isUserActive(view: Window = window): boolean {
  return view.navigator.userActivation?.isActive === true
}
