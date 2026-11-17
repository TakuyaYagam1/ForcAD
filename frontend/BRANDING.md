# Branding

From the repository root, copy the Compose environment sample, then edit its
`VITE_BRAND_*` values:

```sh
cp -n frontend/.env.example deployment/docker/.env
```

Compose reads `deployment/docker/.env` from its project directory and passes
the values as image build arguments. Keep the example values to use the ForcAD
brand.

For local Vite development, put values in `frontend/.env.local`. Vite compiles
them into the bundle, so changing nginx runtime variables does not change the
brand. Rebuild the frontend image after changing the Compose `.env` file.

| Variable | Effect |
| --- | --- |
| `VITE_BRAND_NAME` | Name in the header, footer, admin login, and browser title. |
| `VITE_BRAND_LOGO` | Root-relative URL to an asset under repository `public/`, such as `/brand/forcad-mark.svg`. |
| `VITE_BRAND_PRIMARY` | Main color. |
| `VITE_BRAND_ACCENT` | Accent color. |

Defaults in [brand.ts](src/app/config/brand.ts) are `ForcAD`,
`/brand/forcad-mark.svg`, `#E6E8EB`, and `#7898B3`. Colors must use `#RRGGBB`.
The logo path must start with one `/` and cannot contain `.` or `..` path
segments. Missing or invalid colors and logo paths use their defaults; a blank
name uses `ForcAD`. Quote hex values in `.env` files because `#` starts a
comment. These values are public and must not contain secrets.

Team images are served separately from `public/teams/` at `/team-logos/`; see
the [team logo guide](../public/teams/README.md).

The default interface uses a dark industrial catalogue theme: canvas `#141619`,
panels `#1b1e22`, text `#e6e8eb`, hairline dividers, square corners, no gradients or shadows.
Inter Variable is hosted under `public/brand/fonts/` with its OFL license.
Shared design tokens live in `src/app/styles/index.scss`. The saturated checker
status palette remains independent of brand colors.

For a local preview with sample data, use `pnpm dev:demo`
from `frontend/`. Use `pnpm dev` to connect to the configured real backend.
