# Branding

For `./control.sh start`, copy `frontend/.env.example` to
`deployment/docker/.env` and edit its `VITE_BRAND_*` values. Compose loads that
file from its project directory and passes the values as image build arguments.
Keep the example values to use the default ForcAD brand.

For local Vite development, use `frontend/.env.local`. Vite compiles these values
into the bundle; changing nginx runtime environment variables does not change
the brand. Rebuild the frontend image after changing the Compose `.env`.

- `VITE_BRAND_NAME` sets the name shown in the header, footer, login page, and
  browser title.
- `VITE_BRAND_LOGO` is a root-relative URL to an asset under repository `public/`,
  such as `/brand/forcad-mark.svg`.
- `VITE_BRAND_PRIMARY` and `VITE_BRAND_ACCENT` set the main and accent colors.

Defaults are defined in [brand.ts](src/app/config/brand.ts): `ForcAD`,
`/brand/forcad-mark.svg`, `#5B8DEF`, and `#38B7B0`. Colors must use `#RRGGBB`.
The logo path must start with one `/` and cannot contain `.` or `..` path
segments. Missing or invalid color and logo values use their defaults; a blank
name uses `ForcAD`. Quote hex values in `.env` files because `#` starts a
comment. These values are public and must not contain secrets.

Team images in `public/teams/` are served separately at `/team-logos/`; see the
[team logo guide](../public/teams/README.md).
