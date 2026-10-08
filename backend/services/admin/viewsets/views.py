from urllib.parse import urlsplit

from flask import Blueprint, jsonify, request

from .utils import make_err_response

admin_bp = Blueprint('admin_api', __name__)


def _origin_tuple(value, allow_path=False):
    try:
        parsed = urlsplit(value)
        if parsed.scheme.lower() not in ('http', 'https'):
            return None
        if not parsed.hostname or parsed.username is not None:
            return None
        if parsed.password is not None:
            return None
        if not allow_path and (parsed.query or parsed.fragment):
            return None
        if not allow_path and parsed.path not in ('', '/'):
            return None
        port = parsed.port
    except (AttributeError, TypeError, ValueError):
        return None

    if port is None:
        port = 443 if parsed.scheme.lower() == 'https' else 80
    return parsed.scheme.lower(), parsed.hostname.lower(), port


@admin_bp.before_request
def enforce_same_origin_for_changes():
    if request.method not in {'POST', 'PUT', 'PATCH', 'DELETE'}:
        return None

    origin = request.headers.get('Origin')
    allow_path = False
    if origin is None:
        origin = request.headers.get('Referer')
        allow_path = True
    if origin is None:
        # Keep compatibility with non-browser API clients that do not send
        # browser origin metadata. Browsers send Origin or Referer on changes.
        return None

    received = _origin_tuple(origin, allow_path=allow_path)
    expected = _origin_tuple(request.host_url)
    if received is None or received != expected:
        return make_err_response('Cross-origin request is not allowed', status=403)
    return None


@admin_bp.route('/health/')
def health_check():
    return jsonify({'status': 'ok'})
