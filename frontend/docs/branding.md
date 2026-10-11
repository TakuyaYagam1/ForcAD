# Brand configuration

The frontend reads four `VITE_BRAND_*` values at build time:

- `VITE_BRAND_NAME` sets the title shown in the header, footer, and browser tab.
- `VITE_BRAND_LOGO` points to a local asset under the repository `public` directory, such as `/brand/logo.svg`.
- `VITE_BRAND_PRIMARY` sets the main blue color.
- `VITE_BRAND_ACCENT` sets the secondary teal color.

Missing or blank values use the ForcAD defaults. Colors must use `#RRGGBB`; quote hex values in `.env` files because `#` starts a comment when unquoted. The logo must be a root-relative asset path without a protocol or parent-directory segment. Invalid values also use the defaults.

For a local Vite run, copy `.env.example` to `.env.local` and edit the values. Vite reads these values during both development and production builds. The Docker image also needs the same values as build arguments because nginx serves the already-built files; changing nginx runtime environment variables does not rebrand that bundle. These values are public and must not contain secrets.
