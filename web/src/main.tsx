import { ErrorBoundary } from '@sentry/react';
import { createRoot } from 'react-dom/client';
import { initSentry } from './lib/sentry';
import './index.css';
import App from './app.tsx';
import { ThemeProvider } from './components/theme-provider';

const sentryActive = initSentry();

function AppRoot() {
  return (
    <ThemeProvider defaultTheme="dark" storageKey="vite-ui-theme">
      <App />
    </ThemeProvider>
  );
}

function ErrorFallback() {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-2 p-8 text-center text-muted-foreground text-sm">
      <p className="font-semibold text-base text-foreground">
        Something went wrong
      </p>
      <p>
        Please refresh the page. If the issue continues, contact an
        administrator so we can take a look.
      </p>
    </div>
  );
}

const rootContent = sentryActive ? (
  <ErrorBoundary fallback={<ErrorFallback />} showDialog={false}>
    <AppRoot />
  </ErrorBoundary>
) : (
  <AppRoot />
);

createRoot(document.getElementById('root')!).render(rootContent);
