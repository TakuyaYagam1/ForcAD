import inspect

import engineio
from flask import jsonify, request
from werkzeug.exceptions import RequestEntityTooLarge

MAX_JSON_BODY_BYTES = 64 * 1024
MAX_FLAG_BODY_BYTES = 10 * 1024
MAX_SOCKETIO_BUFFER_BYTES = 64 * 1024


def install_request_limits(
        app,
        json_limit_bytes: int = MAX_JSON_BODY_BYTES,
        flag_limit_bytes: int = MAX_FLAG_BODY_BYTES,
):
    @app.before_request
    def enforce_request_limits():
        if request.blueprint == 'http_receiver':
            limit = flag_limit_bytes
        else:
            limit = json_limit_bytes

        request.max_content_length = limit
        if request.content_length is not None and request.content_length > limit:
            return jsonify({'error': 'Request body too large'}), 413

        content_encoding = request.headers.get('Content-Encoding', '')
        if content_encoding.strip().lower() not in ('', 'identity'):
            return jsonify({'error': 'Unsupported Content-Encoding'}), 415

        body_present = any((
            request.content_length not in (None, 0),
            bool(request.headers.get('Transfer-Encoding')),
        ))
        if all((
            request.content_length is None,
            request.environ.get('wsgi.input_terminated'),
        )):
            # Gunicorn sets this flag even for an empty GET. It permits a bounded
            # stream read; it does not indicate that a body actually exists.
            request.max_content_length = limit + 1
            try:
                body = request.get_data(cache=True)
            finally:
                request.max_content_length = limit
            if len(body) > limit:
                raise RequestEntityTooLarge()
            body_present = bool(body)
        socketio_request = request.path.rstrip('/').endswith('/socket.io')
        if all((body_present, not socketio_request, not request.is_json)):
            return jsonify({'error': 'Unsupported Content-Type'}), 415
        return None

    @app.errorhandler(RequestEntityTooLarge)
    def request_too_large(_error):
        return jsonify({'error': 'Request body too large'}), 413


def socketio_options():
    options = {
        'max_http_buffer_size': MAX_SOCKETIO_BUFFER_BYTES,
        'http_compression': False,
    }
    parameters = inspect.signature(engineio.Server.__init__).parameters
    return {
        name: value for name, value in options.items()
        if name in parameters
    }


def install_socketio_websocket_limits(socketio):
    """Cap threading WebSocket messages and refuse per-message compression."""
    from engineio.async_drivers._websocket_wsgi import SimpleWebSocketWSGI

    server = socketio.server.eio
    if server.async_mode != 'threading':
        raise RuntimeError('WebSocket limits require Engine.IO threading mode')

    class BoundedWebSocketWSGI(SimpleWebSocketWSGI):
        def __init__(self, handler, engineio_server, **kwargs):
            kwargs['max_message_size'] = min(
                engineio_server.max_http_buffer_size,
                MAX_SOCKETIO_BUFFER_BYTES,
            )
            super().__init__(handler, engineio_server, **kwargs)

        def __call__(self, environ, start_response):
            environ.pop('HTTP_SEC_WEBSOCKET_EXTENSIONS', None)
            return super().__call__(environ, start_response)

    server._async = dict(server._async)
    server._async['websocket'] = BoundedWebSocketWSGI
