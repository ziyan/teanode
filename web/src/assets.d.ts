// The stylesheet is imported for its side effect and handed to webpack's
// loader, which turns it into a file the page links. TypeScript 6 asks what
// the import means before the bundler gets there, so it is told: nothing,
// and that is the point.
declare module '*.css'
