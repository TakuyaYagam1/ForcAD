# Team logos

Place the image in `public/teams/`. For example, `public/teams/egida.png` is served at `/team-logos/egida.png` by both Vite and nginx. Supported formats: SVG, PNG, JPEG (`.jpg` or `.jpeg`), WebP and GIF.

| Where you set the logo | Field | Example |
| --- | --- | --- |
| `settings.toml`, under `[[teams.entries]]` | `logo` | `"egida.png"` |
| Game YAML or API | `logo_path` | `"/team-logos/egida.png"` |

Leave `logo_path` empty for no logo. The [backend](../../backend/config/document.go) accepts root-relative paths and HTTP(S) URLs up to 255 bytes. It rejects `..` path segments, backslashes and control characters.
