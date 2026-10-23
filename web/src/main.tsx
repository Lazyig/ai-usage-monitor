import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'

// Light and dark follow the OS setting, like Windows apps do: the tokens switch
// in styles/fluent-tokens.css, so there is no theme provider.
createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <div className="min-h-svh bg-app">
      <App />
    </div>
  </StrictMode>,
)
