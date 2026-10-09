"""Bundled team logos, selected by team name rather than database ID."""

TEAM_LOGOS = {
    "danosito's ai farm": '/team-logos/danositos-ai-farm.jpg',
    'daywave.': '/team-logos/daywave.jpg',
    # This team's proper name is Cyrillic, matching the configured team name.
    'сборная г. москва': '/team-logos/moscow.jpg',  # noqa: RUF001
    'w0lv3s_ctf': '/team-logos/w0lv3s-ctf.jpg',
    'baba is win': '/team-logos/baba-is-win.jpg',
    'trippy troppy': '/team-logos/trippy-troppy.png',
    'caplag': '/team-logos/caplag.jpg',
    'молоток': '/team-logos/molotok.jpg',
    'ibeee': '/team-logos/ibeee.jpg',
    '.dot': '/team-logos/dot.png',
    'segfault': '/team-logos/segfault.png',
    'мяу мяу': '/team-logos/meow-meow.jpg',
    'chetire': '/team-logos/chetire.jpg',
    'под эгидой': '/team-logos/pod-egidoy.png',
    'cut': '/team-logos/cut.png',
    'c4ptur3_th3_b0br': '/team-logos/c4ptur3-th3-b0br.png',
    'sigan': '/team-logos/sigan.jpg',
    'non@me13': '/team-logos/noname13.jpg',
    'hacktr1ckr0mp': '/team-logos/hacktr1ckr0mp.jpg',
    'ncf (ex best it)': '/team-logos/ncf.jpg',
}


def default_logo(name: str) -> str:
    return TEAM_LOGOS.get(name.strip().casefold(), "")
