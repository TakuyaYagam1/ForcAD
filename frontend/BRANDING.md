# Branding

For `./control.sh start`, copy `frontend/.env.example` to
`deployment/docker/.env` and edit its `VITE_BRAND_*` values. Compose reads that
file from its project directory and passes the values as image build arguments.
Keep the example values to use the ForcAD brand.

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
`/brand/forcad-mark.svg`, `#5B8DEF`, and `#38B7B0`. Colors must use `#RRGGBB`.
The logo path must start with one `/` and cannot contain `.` or `..` path
segments. Missing or invalid colors and logo paths use their defaults; a blank
name uses `ForcAD`. Quote hex values in `.env` files because `#` starts a
comment. These values are public and must not contain secrets.

Team images are served separately from `public/teams/` at `/team-logos/`; see
the [team logo guide](../public/teams/README.md).
