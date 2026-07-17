# ContextMatrix Web

The React + TypeScript frontend for ContextMatrix, embedded into the Go binary
at compile time.

## Stack

React 19, TypeScript, Vite, Tailwind CSS, dnd-kit (`@dnd-kit/core`,
`@dnd-kit/sortable`), `@uiw/react-md-editor`.

## Development

```bash
npm install
npm run dev
```

`npm run dev` starts the Vite dev server on its default port. Requests to `/api`
are proxied to the backend at `http://localhost:8080` (see `vite.config.ts`).

## Scripts

| Script            | Purpose                                    |
| ----------------- | ------------------------------------------ |
| `npm run dev`     | Vite dev server with HMR                   |
| `npm run build`   | Type-check (`tsc -b`) and build to `dist/` |
| `npm run lint`    | ESLint                                     |
| `npm run preview` | Preview the production build               |
| `npm run test`    | Vitest (jsdom)                             |

## Build and embed

`npm run build` writes the production bundle to `dist/`. That directory is
embedded into the Go binary via `web/embed.go`, so run `npm run build` before
`make build` for the backend.

## Backend

The Go backend runs as a separate process on `:8080`. See the repo root
`README.md` for backend setup.

## Theme

Three palettes ship with the UI: `everforest` (default), `radix`, and
`catppuccin`. The server-side default is set by the `theme:` field in
`config.yaml`. Users can override the palette per-browser via the
PaletteSelector in `AppHeader` (persisted to `localStorage` under the key
`palette`) - see `web/src/components/AppHeader/PaletteSelector.tsx`.

## Conventions

Frontend conventions, palette tokens, and UI semantic mappings live in
`web/CLAUDE.md`.
