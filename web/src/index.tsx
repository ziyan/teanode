import React from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'

import './style.css'
import { App } from './app'
import { initializeTheme } from './components/theme'
import { TranslationProvider } from './i18n/i18n'

// The page already did this inline, before the bundle was fetched. Doing it
// again keeps the two from drifting if the storage key ever changes.
initializeTheme()

const container = document.getElementById('teanode')
if (!container) {
  throw new Error('the page is missing its root element')
}

createRoot(container).render(
  <React.StrictMode>
    <BrowserRouter>
      <TranslationProvider>
        <App />
      </TranslationProvider>
    </BrowserRouter>
  </React.StrictMode>,
)
