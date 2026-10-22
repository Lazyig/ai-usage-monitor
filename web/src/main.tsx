import { FluentProvider } from '@fluentui/react-components'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import { useMediaQuery } from './hooks/useMediaQuery'
import { DARK_QUERY, darkTheme, lightTheme } from './theme'

function Root() {
  const dark = useMediaQuery(DARK_QUERY)
  return (
    // Fluent copies the provider's className onto portal mount nodes (dialogs,
    // tooltips), so only the theme class goes there; layout lives on an inner div.
    <FluentProvider theme={dark ? darkTheme : lightTheme} className={dark ? 'theme-dark' : 'theme-light'}>
      <div className="min-h-svh bg-app">
        <App />
      </div>
    </FluentProvider>
  )
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Root />
  </StrictMode>,
)
