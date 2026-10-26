import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MotionConfig } from 'motion/react'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router'
import { isApiError } from '@/api/client'
import { TooltipProvider } from '@/components/ui/tooltip'
import { YearProvider } from '@/lib/year'
import { router } from '@/routes/router'
import '@/styles/index.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: (count, error) => {
        if (isApiError(error) && error.status >= 400 && error.status < 500) return false
        return count < 2
      },
      refetchOnWindowFocus: false,
    },
  },
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <MotionConfig reducedMotion="user">
        <TooltipProvider>
          <YearProvider>
            <RouterProvider router={router} />
          </YearProvider>
        </TooltipProvider>
      </MotionConfig>
    </QueryClientProvider>
  </StrictMode>,
)
