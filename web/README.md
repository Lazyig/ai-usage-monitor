# ai-usage-monitor dashboard (web/)

React + Vite + TypeScript SPA for the admin dashboard. Consumes the HTTP API
described in `../docs/API.md`. The Go server embeds the built `dist/` output
(`web/embed.go`) — do not remove `dist/.gitkeep` or `embed.go`.

## Requirements

Node 22+, npm.

## Develop

```sh
npm ci
npm run dev
```

Vite's dev server proxies `/api`, `/downloads` and `/health` to
`http://localhost:8080` (see `vite.config.ts`), so run the Go server (`cmd/server`)
alongside it for a real backend, or use mock mode instead:

```sh
VITE_MOCK=1 npm run dev
```

In mock mode `src/api/client.ts` serves deterministic fake data shaped exactly
like `docs/API.md` (see `src/api/mock.ts`) — useful since the server is written
in parallel and may not be running yet. Log in with any non-empty token except
`invalid`/`bad` (those simulate a 401 to exercise the login error state).

## Build

```sh
npm run build        # production build -> dist/
npm run build:mock   # same, but with VITE_MOCK=1 baked in (sanity-check only,
                      # do not ship this artifact)
npm run preview      # serve the last build locally
```

`npm run build` must produce zero TypeScript errors (`tsc -b` runs first) and
should stay well under ~600 KB gzipped total.

## Structure

- `src/api/types.ts` — every API response/request shape, mirroring `docs/API.md`.
- `src/api/client.ts` — the single fetch wrapper (auth header injection, 401
  handling, mock-mode dispatch).
- `src/api/mock.ts` — deterministic mock backend used only when `VITE_MOCK=1`.
- `src/state/` — `AuthContext` (token/login/logout) and `FiltersContext`
  (global date range + auto-refresh, shared by every page via the top bar).
- `src/pages/` — one file per route (Overview, Agents, Models, Projects,
  DeviceUsage + Devices — the 사용량/관리 sub-tabs of `/devices` —, Downloads,
  Events, Pricing) plus Login.
- `src/components/` — shared UI: `TopBar`, `DataTable` (generic sortable
  table), `StateContainer` (loading/error/empty), `StatTile`, `Badge`, charts.
- `src/lib/` — formatting (`format.ts`), date-range presets (`dateRange.ts`),
  chart data shaping (`chartData.ts`), the fixed categorical color assignment
  (`colors.ts`), and the source × device matrix (`matrix.ts`) behind the two
  composition charts — the API has no 2-D grouping, so it fans out one
  `/breakdown?group_by=device&source=…` per source and pivots.

## Notes

- Auth: the login screen verifies the admin token against
  `GET /api/v1/devices` and stores it in `localStorage`. Any `401` from any
  request drops back to `/login`.
- Timezone is fixed to `Asia/Seoul` for date-range boundaries (shown in the
  top bar); all displayed timestamps are still formatted in that zone.
- Dark/light mode follows `prefers-color-scheme` — no manual toggle.
