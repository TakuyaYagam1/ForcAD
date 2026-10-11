"""Validation helpers for optional team logo URLs."""

from urllib.parse import unquote, urlsplit


def validate_logo_path(value: str | None) -> str:
    """Return a safe path or HTTP(S) URL for a team logo."""
    if value is None:
        return ''
    if not isinstance(value, str):
        raise ValueError('logo_path: expected a string')

    value = value.strip()
    if not value:
        return ''
    if len(value) > 255:
        raise ValueError('logo_path: expected at most 255 characters')
    if '\\' in value or any(ord(char) < 32 or ord(char) == 127 for char in value):
        raise ValueError('logo_path: expected a safe URL path')

    try:
        parts = urlsplit(value)
    except ValueError as exc:
        raise ValueError('logo_path: expected a safe URL path') from exc

    if parts.scheme:
        if any((
            parts.scheme not in ('http', 'https'),
            not parts.hostname,
            parts.username is not None,
            parts.password is not None,
        )):
            raise ValueError('logo_path: expected a root-relative path or HTTP(S) URL')
    elif parts.netloc or not parts.path or parts.path.startswith('//'):
        raise ValueError('logo_path: expected a root-relative path or HTTP(S) URL')

    decoded_path = parts.path
    while True:
        unquoted_path = unquote(decoded_path)
        if unquoted_path == decoded_path:
            break
        decoded_path = unquoted_path

    if any((
        '\\' in decoded_path,
        decoded_path.startswith('//'),
        any(
            ord(char) < 32 or ord(char) == 127 for char in decoded_path
        ),
        '..' in decoded_path.split('/'),
    )):
        raise ValueError('logo_path: path traversal is not allowed')

    return value


def safe_logo_path(value: str | None) -> str:
    """Use an empty path when a stored logo value is invalid."""
    try:
        return validate_logo_path(value)
    except ValueError:
        return ''
