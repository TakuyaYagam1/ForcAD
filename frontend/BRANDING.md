# Branding

The first four settings in [the Compose sample](../deployment/docker/.env.example)
control the frontend brand. From the repository root:

```sh
cp -n deployment/docker/.env.example deployment/docker/.env
```

Edit the `VITE_BRAND_*` values in `deployment/docker/.env`. Compose passes them
to the nginx image build; restarting an existing container does not change the
brand. To rebuild and apply just the frontend:

```sh
./control.sh compose up -d --no-deps --build nginx
```

For `pnpm dev` without Docker, export only the `VITE_BRAND_*` values you want
to override before starting Vite. Do not copy the deployment `.env` into `frontend/`.

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
