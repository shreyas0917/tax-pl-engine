# Tax P&L Engine frontend

React 19 + TypeScript (strict) + Vite, Tailwind CSS v4, TanStack Query/Table/Virtual, Radix primitives, Motion, Recharts, React Hook Form + Zod, openapi-typescript + openapi-fetch, Vitest + Testing Library, Playwright.

## Scripts

| Script | What it does |
|---|---|
| `npm run dev` | Vite dev server on port 5173 (the API's allowed CORS origin) |
| `npm run build` | Type-check and build to `dist/` |
| `npm run preview` | Serve the production build |
| `npm run typecheck` | `tsc -b --noEmit` |
| `npm run lint` | ESLint (typescript-eslint, react-hooks, react-refresh) |
| `npm test` | Vitest unit tests |
| `npm run test:e2e` | Playwright flow against a running API (`npm run e2e:install` once for Chromium) |
| `npm run gen:api` | Regenerate `src/api/schema.d.ts` from `../backend/api/openapi.yaml`. API types are never hand-written. |

Scripts call package binaries through `node node_modules/...` rather than the `.bin` shims because the project folder name contains `&`, which breaks npm's `.cmd` shims on Windows.

## Configuration

`VITE_API_URL` is the API base URL without `/api/v1` (default `http://localhost:8080`). Copy `.env.example` to `.env.local` to point at another instance, e.g. the dev stack on `http://localhost:8085`.

## Layout

```
src/api         generated schema, typed client with bearer + refresh-once-and-retry, SSE job progress, query hooks
src/components  design-system primitives (restyled Radix) and the job stepper
src/features    landing, auth, dashboard, upload, review, gains, explain, trades/holdings, reports, settings
src/lib         INR/date formatting, motion presets, theme, year context
src/styles      tokens (light and dark CSS variables) and base styles
e2e             Playwright flow and the screenshots it captures
```

## Design

A ruled ledger: one sans (IBM Plex Sans) with tabular figures, hairlines instead of shadows, one ink-blue accent, gains in green and losses in red always with an explicit sign, and a double rule under every total. Light and dark themes are CSS variables in `src/styles/tokens.css`; the theme preference (system, light, dark) is persisted. Motion presets live in `src/lib/motion.ts` and the app is wrapped in `MotionConfig reducedMotion="user"`.

Screenshots from the e2e run: `e2e/screenshots/dashboard-light.png`, `dashboard-dark.png`, `gains.png`, `explain-drawer.png`.
