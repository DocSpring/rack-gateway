import { createRoot } from 'react-dom/client'
import './index.css'
import App from './app.tsx'
import { ThemeProvider } from './components/theme-provider'

createRoot(document.getElementById('root')!).render(
  <ThemeProvider defaultTheme="dark" storageKey="vite-ui-theme">
    <App />
  </ThemeProvider>
)
