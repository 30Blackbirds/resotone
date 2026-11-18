# Resotone — the development stack, run with `tilt up -- --dev` (see Tiltfile).
#
# Needs NO secrets (see docker-compose.tilt.yml): no login, tenant via
# X-Dev-Tenant (editor) / X-Scope-OrgID (delivery), boot auto-migrate. The
# editor runs as a dev server with live reload, and the examples run beside it.
# Server on http://localhost:3000 (healthz: /healthz).

# Host ports, overridable so this stack can run beside another one (the
# platform's Tilt holds 3000 and 5433). Defaults are the ports documented here.
#   RESONANS_API_PORT=3060 RESONANS_PG_PORT=5434 tilt up --port 10351
api_port = os.getenv('RESONANS_API_PORT', '3000')
pg_port = os.getenv('RESONANS_PG_PORT', '5433')
studio_port = os.getenv('RESONANS_STUDIO_PORT', '3030')
site_port = os.getenv('RESONANS_SITE_PORT', '3050')
api_url = 'http://localhost:' + api_port
studio_url = 'http://localhost:' + studio_port
site_url = 'http://localhost:' + site_port
# docker-compose.tilt.yml reads the two it maps.
os.putenv('RESONANS_API_PORT', api_port)
os.putenv('RESONANS_PG_PORT', pg_port)

docker_compose('docker-compose.tilt.yml')

# Tilt builds the Go image from the compose `build:` context and live-reloads on
# source changes. Group everything under a 'resotone' label.
dc_resource('postgres', labels=['resotone'])
dc_resource('resotone', labels=['resotone'])

# Manual button: seed the explainers demo course (all block types + quiz + page)
# for the dev tenant 11111111-1111-1111-1111-111111111111. Run once after up.
local_resource(
    'resotone-seed',
    cmd='docker compose -f docker-compose.tilt.yml --profile seed run --rm resotone-seed',
    labels=['resotone'],
    auto_init=False,
    trigger_mode=TRIGGER_MODE_MANUAL,
    resource_deps=['resotone'],
)

# --- Example: 30blackbirds.dk on Resotone (examples/30blackbirds-site) ---
# The marketing site, landing pages and blog of 30blackbirds.dk, with every word and
# photo served by this Resotone. Press "30blackbirds-site-seed" once after
# "resotone-seed" (the blog posts come from that one), then open
# http://localhost:3050.
local_resource(
    '30blackbirds-site-seed',
    cmd='node seed/seed.mjs',
    dir='examples/30blackbirds-site',
    env={'RESONANS_URL': api_url, 'RESONANS_TENANT': '11111111-1111-1111-1111-111111111111'},
    labels=['examples'],
    auto_init=False,
    trigger_mode=TRIGGER_MODE_MANUAL,
    resource_deps=['resotone'],
)
# The editor (Resotone Studio) in dev mode — no login, tenant via X-Dev-Tenant —
# so live preview works out of the box: open a page and press "Visuel". The
# site's /preview route accepts frames only from this origin (studioOrigin).
local_resource(
    'resotone-studio',
    serve_cmd='pnpm install --frozen-lockfile && pnpm dev --port ' + studio_port,
    serve_dir='studio',
    serve_env={
        'RESONANS_DEV_API': api_url,
        'NUXT_PUBLIC_DEV_TENANT': '11111111-1111-1111-1111-111111111111',
    },
    links=[link(studio_url, 'Resotone editor')],
    labels=['resotone'],
    resource_deps=['resotone'],
)
local_resource(
    '30blackbirds-site',
    serve_cmd='pnpm install --frozen-lockfile && pnpm dev --port ' + site_port,
    serve_dir='examples/30blackbirds-site',
    serve_env={
        'NUXT_RESONANS_URL': api_url,
        'NUXT_PUBLIC_RESONANS_PUBLIC_URL': api_url,
        'NUXT_PUBLIC_STUDIO_ORIGIN': studio_url,
    },
    links=[link(site_url, '30blackbirds.dk on Resotone')],
    labels=['examples'],
    resource_deps=['resotone'],
)
