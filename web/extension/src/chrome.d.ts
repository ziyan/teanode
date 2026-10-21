// The extension's code uses the browser's chrome.* APIs. The repository does
// not depend on @types/chrome, and the extension's older scripts are plain
// JavaScript, so the APIs the new TypeScript calls are left untyped here
// rather than half described. Every callback parameter that is written out
// carries its own type.
declare const chrome: any
