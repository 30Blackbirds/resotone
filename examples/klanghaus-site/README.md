# 30blackbirds.dk on Resotone

[30blackbirds.dk](https://30blackbirds.dk), rebuilt so that every word and photo on it comes from
Resotone. The front-end decides only how things look — it uses the same
[grundtone](https://github.com/KlangHaus/grundtone) components and the same stylesheets as the
live site, so the front page renders the same text at the same height (measured against the live
site at 1280 px: identical `main` text, page height 4950 px on both).

It is also a test bed: a real landing page, a text page, a landing page made only of placeholder
content, and a blog, all edited in Resotone.

| Route | Resotone document | What it shows |
| --- | --- | --- |
| `/` | `page` `forside` | 30blackbirds.dk's front page, text unchanged |
| `/privatlivspolitik` | `page` `privatlivspolitik` | the privacy policy, text unchanged, marked as a **test copy** |
| `/bunker-udlejning` | `page` `bunker-udlejning` | a second landing page — **placeholder content**, not an offer |
| `/blog`, `/blog/:slug` | every published `post` | the blog posts from Resotone's own demo seed |

## Run it

With Tilt, from the repository root:

```bash
tilt up -- --dev
```

Then press **resotone-seed** (the blog posts) and **30blackbirds-site-seed** (the pages and photos) in
the Tilt UI, and open <http://localhost:3050>.

Without Tilt, against any Resotone running with `RESONANS_DEV_AUTH=1`:

```bash
pnpm install
RESONANS_URL=http://localhost:3000 pnpm seed
NUXT_RESONANS_URL=http://localhost:3000 pnpm dev --port 3050
```

The seed is safe to run again: a page that exists is updated and republished, not duplicated.

## Live preview in the editor

This site is wired into Resotone's visual editing (the reference is
`examples/visual-editing`). Open a page in the editor and press **Visuel**: the
site is framed beside the form, every keystroke re-renders it from the unsaved
document, and a click on an element in the preview selects its field.

- **Route:** `/preview/{type}/{slug}?token=…&locale=…` renders the draft from
  `GET /delivery/v1/preview/…` with the editor's short-lived token. No token, a
  wrong or an expired one is a 401 and no content.
- **Fenced off:** `Content-Security-Policy: frame-ancestors <editor origin>`,
  `Cache-Control: no-store`, `X-Robots-Tag: noindex` — on that route only. The
  editor origin is `NUXT_PUBLIC_STUDIO_ORIGIN`, never read from the URL.
- **Bridge:** only the preview route loads `<resotone>/bridge/v1/resotone-bridge.js`
  (`NUXT_PUBLIC_RESONANS_PUBLIC_URL`) and marks values with `data-resotone-path`.
  The public pages get neither.
- **No sanitizer in the site.** Resotone cleans rich text with one server-side
  policy, both when a document is saved and in the unsaved envelope the editor
  pushes live, so what reaches `v-html` here is already clean.
- **Set up by the seed:** `pnpm seed` sets the tenant's preview URL to
  `${SITE_URL}/preview/{type}/{slug}?token={token}&locale={locale}`
  (`SITE_URL` defaults to `http://localhost:3050`).

## How a page becomes a landing page

A Resotone `page` is a flat list of blocks. This site reads it as sections:

- everything before the first `section_header` is the opening — a `hero`, and a `feature_grid`
  with variant `facts` under it;
- a `hero` heading uses `|` for a line break and `*…*` for the emphasised part
  (`30blackbirds er ikke et studie.|Det er *et hjem for musikere.*`): the editor
  edits headings in a single-line field, where a newline would be lost;
- each `section_header` starts a section (`eyebrow`, `heading`, `lead`, `anchor` for `#links`,
  `navLabel` for the menu) that runs until the next one.

The menu is built from the front page's section headers, so adding a section in Resotone adds it
to the menu.

Layout is chosen by the `variant` a block carries. Resotone stores it and never interprets it:

| Block | `variant` | Renders as |
| --- | --- | --- |
| `feature_grid` | `facts` | the number row under the hero |
| `feature_grid` | `cards` | icon cards |
| `feature_grid` | `steps` | numbered steps |
| `feature_grid` | `checklist` | the ✓ list beside a `quote` |
| `feature_grid` | `plan` | the studio overview and floor plan |
| `section_header` | `band` | a section on the tinted band |
| `section_header` | `waitlist` | button and note in one row |
| `section_header` | `contact` | the contact block, with rich text as the card |
| `columns` of `image` | — | a photo row |

### The studio overview

A `feature_grid` with variant `plan`, one feature per studio: `title` "Studie 3", `body` "15 m²",
and `icon` the date it is free from (`2026-11-01`), or empty when it is not. On 30blackbirds.dk this
lives in code and changes with a deploy; here an editor changes it and the next request shows it.
"Fra 1. november" turns into "nu" on the day, because the page is rendered per request. Which
studio sits where in the building is layout, not content, and stays in `app/site.ts`.

## Not content, on purpose

- The header and footer frame (`app/site.ts`): name, logo, e-mail, the company line.
- `assets/styles/brand.scss` and `landing.scss` are 30blackbirds.dk's own, copied unchanged;
  `site.scss` adds only what its one-pager never needed (rich text, callouts, code, the blog).

## Known differences from 30blackbirds.dk

- Photos are served by Resotone as one JPEG each; the live site serves AVIF/WebP/JPEG at two
  sizes.
- The live site is static and has no scripts; this one is a Nuxt app with server-side rendering.
  That is why the privacy policy is marked as a test copy: its own premises describe 30blackbirds.dk.
- The live site's JSON-LD and `llms.txt` are not reproduced.
