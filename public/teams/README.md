# Team logos

Put team images in this directory. The Vite asset plugin accepts `.svg`,
`.png`, `.jpg`, `.jpeg`, `.webp`, and `.gif` files and serves them in
development and production at `/team-logos/<filename>`. Nginx serves the same
URL; `public/teams/` is the source directory.

In infrastructure `settings.toml`, set `logo` to the filename relative to this
directory:

```toml
[[teams.entries]]
name = "myteam"
logo = "team.jpg"
```

The renderer writes `/team-logos/team.jpg` to the game config. When editing the
game config or using the API directly, set `logo_path` to
`/team-logos/<filename>`. The backend also accepts an empty value, another safe
root-relative path, or an HTTP(S) URL. Local paths are limited to 255
characters and reject traversal segments, backslashes, and control characters.
Leave `logo_path` empty when the team has no logo.

See the [Vite asset handler](../../frontend/vitePublicAssets.ts),
[backend validator](../../backend/config/document.go), and
[nginx route](../../deployment/nginx/http.conf.template).
