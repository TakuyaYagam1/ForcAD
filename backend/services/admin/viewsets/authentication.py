import secrets

from flask import request, jsonify

from lib import storage, config
from .utils import abort_with_error


def check_session():
    if 'session' not in request.cookies:
        abort_with_error('No session set', 403)

    session = request.cookies['session']
    with storage.utils.redis_pipeline(transaction=False) as pipe:
        (data,) = pipe.get(storage.keys.CacheKeys.session(session)).execute()

    creds = config.get_web_credentials()

    if data != creds.username:
        abort_with_error('Invalid session', 403)

    return True


def set_session(session: str, username: str):
    with storage.utils.redis_pipeline(transaction=False) as pipe:
        pipe.set(storage.keys.CacheKeys.session(session), username).execute()


def login():
    data = request.get_json(silent=True)
    if not isinstance(data, dict):
        abort_with_error('Ожидается JSON объект', 400)
    username = data.get('username')
    password = data.get('password')

    creds = config.get_web_credentials()
    if username != creds.username or password != creds.password:
        abort_with_error('Invalid credentials', 403)

    session = secrets.token_hex(32)
    set_session(session, username)

    response = jsonify({'status': 'ok', 'username': creds.username})
    response.set_cookie('session', session, httponly=True, samesite='Lax')
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
    response.delete_cookie('session', httponly=True, samesite='Lax')
    return response
