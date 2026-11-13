# Product site

The public website for Open Linux Router: capability overview, screenshot
gallery, and the documentation set. It is a **separate, static site** — it does
not ship inside the router binary and does not share a build with `web/`.

```
site/
├── src/
│   ├── pages/            /, /capabilities, /gallery, /docs, /docs/[slug]
│   │   └── zh/           the same routes under /zh/
│   ├── components/       page bodies, shared by the two locales
│   ├── layouts/          document shell, header, footer, search
│   ├── lib/              copy, capability model, docs index, image map
│   ├── content/          reserved for future long-form pages
│   └── assets/screens/   committed WebP captures (79 files, ~1.9 MB)
├── scripts/
│   ├── optimize-screens.mjs   marketing capture library → src/assets/screens
│   ├── prepare-docs.mjs       docs/*.md → src/lib/docs.generated.json
│   └── markdown.mjs           the build-time Markdown renderer
└── public/               favicon, static files
```

## Commands

Node 24, npm 11. From `site/`:

| Command | What it does |
| --- | --- |
| `npm install` | install dependencies |
| `npm run dev` | dev server on `http://127.0.0.1:4321` |
| `npm run build` | pre-render docs, build the static site to `dist/`, index it with Pagefind |
| `npm run preview` | serve the built site |

`npm run build` is the only build that includes working search. Pagefind writes
`dist/pagefind/` after the site build; the dev server has no index and says so.

## Where the content comes from

Nothing here is hand-copied when it does not have to be:

- **Docs** — `../docs/*.md` is the only copy. `prepare-docs.mjs` renders it to
  `src/lib/docs.generated.json` at build time, so editing a doc and rebuilding
  is enough. Chinese titles and summaries live in `scripts/prepare-docs.mjs`;
  a full Chinese body can be added as `docs/zh/<slug>.md` and is picked up
  automatically.
- **Gallery** — `marketing/` is gitignored working material, so the chosen
  captures are copied in as WebP. Re-run `node scripts/optimize-screens.mjs`
  (optionally with `SHOT_SRC=…`) when the capture library changes, then commit
  the new files and `src/lib/screens.manifest.json`.
- **Copy** — `src/lib/content.ts` (capabilities, beliefs) and the `text` objects
  at the top of each page component. Both locales are in one object so a missing
  translation is a type error, not a silent fallback.

## Language and theme

English is the root (`/`), Chinese is `/zh/`. Both are built from the same
components with a `lang` prop. Theme is light/dark/system, resolved before first
paint; `?theme=dark` or `?theme=light` forces one for previewing or for sharing
a screenshot of a particular theme.

## Search

Pagefind indexes the built HTML, so search works with no server. Each page
carries `data-pagefind-body` and a `lang` filter, and `<title data-pagefind-meta="title">`
gives results their label. The dialog only queries the current language side.

## Deploying

`npm run build` produces `dist/` — a plain static directory. Serve it from any
static host (Cloudflare Pages, Netlify, GitHub Pages, nginx). Two things to set
once the domain is known:

1. `site` in `astro.config.mjs`, so canonical and `hreflang` links are absolute.
2. The host's 404 fallback to `dist/404.html`.

## Checks

```sh
npm run build          # must succeed
npx astro check        # optional type check; needs @astrojs/check
```

There is no test suite yet; the interaction-heavy parts (search dialog,
lightbox) are worth a smoke test before a release.
