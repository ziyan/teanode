// The map is loaded from MapLibre's build for a page whose content security
// policy allows no blob: workers (maplibre-gl-csp.js, with its worker served
// by the dashboard itself). That file ships without a declaration of its own;
// it exports exactly what the main build does.
declare module 'maplibre-gl/dist/maplibre-gl-csp.js' {
  export * from 'maplibre-gl'
}
