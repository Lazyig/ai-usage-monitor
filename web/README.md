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
`http://localhost:8080` (see `vite.config.ts`), so run the Go server alongside it
and log in with the same token:

```sh
ADMIN_TOKEN=dev go run ./cmd/server   # from the repository root
```

## Build

```sh
npm run build        # production build -> dist/
npm run preview      # serve the last build locally
```

`npm run build` must produce zero TypeScript errors (`tsc -b` runs first) and
should stay well under ~600 KB gzipped total.

Run `npm test` for the date-range refresh regression tests. Relative presets
follow the current Seoul date on manual/automatic refresh, within 60 seconds
of a date change, and when returning to the tab. Custom dates stay fixed.

## Structure

- `src/api/types.ts` — every API response/request shape, mirroring `docs/API.md`.
- `src/api/client.ts` — the single fetch wrapper (auth header injection, 401
  handling).
- `src/state/` — `AuthContext` (token/login/logout) and `FiltersContext`
  (global date range + auto-refresh, shared by every page via the top bar).
- `src/pages/` — one file per route (Overview, Agents, Models, Projects,
  DeviceUsage (기기), Devices (기기 관리), Downloads, Events, Pricing) plus Login.
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
