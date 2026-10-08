import hashlib
import hmac
import json
import secrets

from flask import jsonify, request

from lib import config, storage

from .utils import abort_with_error

SESSION_TTL_SECONDS = 8 * 60 * 60


def _password_version(password: str, session: str, username: str) -> str:
    """Return a session-specific marker without storing the password."""
    message = json.dumps(
        [session, username],
        ensure_ascii=False,
        separators=(',', ':'),
    ).encode('utf-8')
    return hmac.new(
        password.encode('utf-8'),
        message,
        hashlib.sha256,
    ).hexdigest()


def _valid_session_data(
    session_data,
    session: str,
    username: str,
    password: str,
) -> bool:
    if not isinstance(session_data, dict):
        return False
    if not all(isinstance(value, str) for value in (session, username, password)):
        return False

    stored_username = session_data.get('username')
    if not isinstance(stored_username, str):
        return False
    if not hmac.compare_digest(
        stored_username.encode('utf-8'),
        username.encode('utf-8'),
    ):
        return False

    password_version = session_data.get('password_version')
    if not isinstance(password_version, str) or not password_version.isascii():
        return False

    expected = _password_version(password, session, username)
    return hmac.compare_digest(
        password_version.encode('ascii'),
        expected.encode('ascii'),
    )


def check_session():
    if 'session' not in request.cookies:
        abort_with_error('No session set', 403)

    session = request.cookies['session']
    with storage.utils.redis_pipeline(transaction=False) as pipe:
        (data,) = pipe.get(storage.keys.CacheKeys.session(session)).execute()

    creds = config.get_web_credentials()

    try:
        session_data = json.loads(data)
    except (TypeError, json.JSONDecodeError):
        session_data = None

    if not _valid_session_data(
        session_data,
        session,
        creds.username,
        creds.password,
    ):
        with storage.utils.redis_pipeline(transaction=False) as pipe:
            pipe.delete(storage.keys.CacheKeys.session(session)).execute()
        abort_with_error('Invalid session', 403)

    return True


def set_session(session: str, username: str, password: str):
    data = json.dumps(
        {
            'username': username,
            'password_version': _password_version(password, session, username),
        },
        separators=(',', ':'),
    )
    with storage.utils.redis_pipeline(transaction=False) as pipe:
        pipe.set(
            storage.keys.CacheKeys.session(session),
            data,
            ex=SESSION_TTL_SECONDS,
        ).execute()


def login():
    data = request.get_json(silent=True)
    if not isinstance(data, dict):
        abort_with_error('Expected a JSON object', 400)
    username = data.get('username')
    password = data.get('password')

    creds = config.get_web_credentials()
    if not isinstance(username, str) or not isinstance(password, str):
        abort_with_error('Invalid credentials', 403)
    if not isinstance(creds.username, str) or not isinstance(creds.password, str):
        abort_with_error('Invalid credentials', 403)
    if not hmac.compare_digest(
        username.encode('utf-8'),
        creds.username.encode('utf-8'),
    ) or not hmac.compare_digest(
        password.encode('utf-8'),
        creds.password.encode('utf-8'),
    ):
        abort_with_error('Invalid credentials', 403)

    session = secrets.token_hex(32)
    set_session(session, username, creds.password)

    response = jsonify({'status': 'ok', 'username': creds.username})
    response.set_cookie(
        'session',
        session,
        max_age=SESSION_TTL_SECONDS,
        httponly=True,
        secure=request.is_secure,
        samesite='Lax',
    )
    return response


def status():
    check_session()
    return jsonify({'status': 'ok', 'username': config.get_web_credentials().username})


def logout():
    session = request.cookies.get('session')
    if session:
        with storage.utils.redis_pipeline(transaction=False) as pipe:
            pipe.delete(storage.keys.CacheKeys.session(session)).execute()
    response = jsonify({'status': 'ok'})
    response.delete_cookie(
        'session',
        httponly=True,
        secure=request.is_secure,
        samesite='Lax',
    )
    return response
