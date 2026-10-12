# Team logos

Place public team images in this directory. The Vite public-asset plugin accepts
`.svg`, `.png`, `.jpg`, `.jpeg`, `.webp`, and `.gif` files and serves them at
`/team-logos/<filename>` in development and in the production bundle. Nginx
serves the same URL; the source directory itself is not the public URL.

Set the team's `logo_path` to `/team-logos/<filename>`. The backend validator
also accepts an empty value, a safe root-relative path, or an HTTP(S) URL. Local
paths are limited to 255 characters and reject traversal segments, backslashes,
and control characters. Leave `logo_path` empty when there is no logo.

See the [Vite asset handler](../../frontend/vitePublicAssets.ts),
[backend validator](../../backend/config/document.go), and
[nginx routes](../../deployment/nginx/http.conf.template).
