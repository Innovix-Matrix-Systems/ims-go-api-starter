import { HeadContent, Scripts, createRootRoute, Outlet } from '@tanstack/react-router'
import { QueryClientProvider } from '@tanstack/react-query'
import { ToastProvider } from '../components/Toasts'
import { queryClient } from '../query'
import '../styles.css'

export const Route = createRootRoute({
  component: RootDocument,
})

function RootDocument() {
  return (
    <html lang="en">
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <meta name="color-scheme" content="dark" />
        <meta name="robots" content="noindex, nofollow" />
        <title>Dev Dashboard — IMS</title>
        {/* TanStack Start's dev HTML bypasses Vite's index.html transform; load the HMR client explicitly. */}
        {import.meta.env.DEV ? (
          <script type="module" src={`${import.meta.env.BASE_URL}@vite/client`} />
        ) : null}
        <HeadContent />
      </head>
      <body>
        <QueryClientProvider client={queryClient}>
          <ToastProvider>
            <Outlet />
          </ToastProvider>
        </QueryClientProvider>
        <Scripts />
      </body>
    </html>
  )
}
