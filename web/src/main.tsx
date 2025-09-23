import { createRoot } from 'react-dom/client'
import './index.css'
import App from './app.tsx'
import { ThemeProvider } from './components/theme-provider'

const styleNonceMeta = document.querySelector<HTMLMetaElement>('meta[name="cgw-style-nonce"]')
const styleNonce = styleNonceMeta?.content

if (styleNonce) {
  ;(globalThis as { __webpack_nonce__?: string }).__webpack_nonce__ = styleNonce
}

createRoot(document.getElementById('root')!).render(
  <ThemeProvider defaultTheme="dark" storageKey="vite-ui-theme">
    <App />
  </ThemeProvider>
)
