import importlib
import os
import subprocess
import sys
from pathlib import Path
from unittest import TestCase
from unittest.mock import patch

import socketio
from flask import Blueprint, Flask, jsonify, request

PROJECT_DIR = Path(__file__).resolve().parents[1]
BACKEND_DIR = PROJECT_DIR / 'backend'
TEST_TEAM_TOKEN = '01234567' + '89abcdef'
sys.path.insert(0, str(BACKEND_DIR))

from lib import flags, storage
from lib.helpers.http_limits import (
    MAX_FLAG_BODY_BYTES,
    MAX_JSON_BODY_BYTES,
    MAX_SOCKETIO_BUFFER_BYTES,
    install_request_limits,
    socketio_options,
)
from lib.models import AttackResult

with patch.object(flags, 'SubmitMonitor'), patch.object(flags, 'Judge'):
    receiver_views = importlib.import_module('services.http_receiver.views')


class RequestLimitTests(TestCase):
    @classmethod
    def setUpClass(cls):
        manager = socketio.Manager()
        with patch.object(storage.utils.SIOManager, 'read_write', return_value=manager):
            cls.events_module = importlib.import_module('services.events.app')
        cls.api_module = importlib.import_module('services.api.app')
        cls.receiver_module = importlib.import_module(
            'services.http_receiver.app'
        )

    def setUp(self):
        finished = patch.object(storage.game, 'is_game_finished', return_value=False)
        finished.start()
        self.addCleanup(finished.stop)
        self.app = Flask('request-limits-test')
        self.json_limit = 128
        self.flag_limit = 64
        install_request_limits(
            self.app,
            json_limit_bytes=self.json_limit,
            flag_limit_bytes=self.flag_limit,
        )

        api = Blueprint('test_api', __name__)
        receiver = Blueprint('http_receiver', __name__)

        @api.route('/json/', methods=['POST'])
        def receive_json():
            return jsonify({'data': request.get_json()})

        @api.route('/health/')
        def health():
            return jsonify({'ok': True})

        @receiver.route('/flags/', methods=['PUT'])
        def receive_flags():
            return jsonify(request.get_json(force=True))

        @api.route('/socket.io/', methods=['POST'])
        def receive_socketio_polling():
            return jsonify({'length': len(request.get_data())})

        self.app.register_blueprint(api, url_prefix='/api')
        self.app.register_blueprint(receiver, url_prefix='/flags')
        self.client = self.app.test_client()

    def test_gunicorn_terminated_stream_can_be_an_empty_get(self):
        response = self.client.get(
            '/api/health/', environ_overrides={'wsgi.input_terminated': True},
        )
        self.assertEqual(response.status_code, 200)

    def test_stream_without_content_length_still_checks_body_and_limit(self):
        from io import BytesIO

        for body, expected in ((b'body', 415), (b'x' * 129, 413)):
            with self.subTest(size=len(body)):
                response = self.client.open(
                    '/api/json/', method='POST', content_type='text/plain',
                    environ_overrides={
                        'wsgi.input_terminated': True,
                        'wsgi.input': BytesIO(body),
                        'CONTENT_LENGTH': None,
                    },
                )
                self.assertEqual(response.status_code, expected)

    def test_finished_game_rejects_submissions_before_judging(self):
        with patch.object(storage.teams, 'get_team_id_by_token', return_value=1), \
                patch.object(storage.game, 'get_real_round', return_value=14), \
                patch.object(storage.game, 'is_game_finished', return_value=True), \
                patch.object(receiver_views, 'judge') as judge:
            response = self.receiver_module.app.test_client().put('/flags/', json=['x' * 32])
        self.assertEqual(response.status_code, 410)
        judge.process_many.assert_not_called()

    def test_json_limit_accepts_boundary_and_rejects_next_byte(self):
        body = b'null' + b' ' * (self.json_limit - len(b'null'))
        response = self.client.post(
            '/api/json/',
            data=body,
            content_type='application/json',
        )
        self.assertEqual(response.status_code, 200)

        response = self.client.post(
            '/api/json/',
            data=body + b' ',
            content_type='application/json',
        )
        self.assertEqual(response.status_code, 413)
        self.assertEqual(response.get_json(), {'error': 'Request body too large'})

    def test_flag_body_limit_applies_even_to_forced_json_route(self):
        body = b'[]' + b' ' * (self.flag_limit - len(b'[]'))
        response = self.client.put(
            '/flags/flags/',
            data=body,
            content_type='application/json',
        )
        self.assertEqual(response.status_code, 200)

        response = self.client.put(
            '/flags/flags/',
            data=body + b' ',
            content_type='text/plain',
        )
        self.assertEqual(response.status_code, 413)

    def test_non_json_api_body_uses_general_limit_and_requires_json(self):
        body = b'x' * self.json_limit
        response = self.client.post(
            '/api/socket.io/',
            data=body,
            content_type='text/plain',
        )
        self.assertEqual(response.status_code, 200)

        response = self.client.post(
            '/api/socket.io/',
            data=body + b'x',
            content_type='text/plain',
        )
        self.assertEqual(response.status_code, 413)

        response = self.client.post(
            '/api/json/',
            data=b'body',
            content_type='text/plain',
        )
        self.assertEqual(response.status_code, 415)

    def test_production_body_limit_defaults(self):
        self.assertEqual(MAX_JSON_BODY_BYTES, 64 * 1024)
        self.assertEqual(MAX_FLAG_BODY_BYTES, 10 * 1024)

    def test_compressed_body_is_rejected_without_decoding(self):
        response = self.client.post(
            '/api/json/',
            data=b'null',
            content_type='application/json',
            headers={'Content-Encoding': 'gzip'},
        )
        self.assertEqual(response.status_code, 415)
        self.assertEqual(
            response.get_json(),
            {'error': 'Unsupported Content-Encoding'},
        )

        response = self.client.post(
            '/api/json/',
            data=b'null',
            content_type='application/json',
            headers={'Content-Encoding': 'identity'},
        )
        self.assertEqual(response.status_code, 200)

    def test_socketio_disables_polling_compression_and_limits_messages(self):
        options = socketio_options()
        self.assertEqual(
            options['max_http_buffer_size'],
            MAX_SOCKETIO_BUFFER_BYTES,
        )

        server = socketio.Server(async_mode='threading', **options)
        self.assertEqual(
            server.eio.max_http_buffer_size,
            MAX_SOCKETIO_BUFFER_BYTES,
        )
        if 'http_compression' in options:
            self.assertFalse(server.eio.http_compression)

        production_server = self.events_module.sio.server
        self.assertEqual(production_server.eio.async_mode, 'threading')
        self.assertEqual(
            production_server.eio.max_http_buffer_size,
            MAX_SOCKETIO_BUFFER_BYTES,
        )
        if 'http_compression' in options:
            self.assertFalse(production_server.eio.http_compression)

        adapter_class = production_server.eio._async['websocket']
        adapter = adapter_class(lambda _ws: None, production_server.eio)
        self.assertEqual(
            adapter.server_args['max_message_size'],
            MAX_SOCKETIO_BUFFER_BYTES,
        )

    def test_socketio_websocket_adapter_does_not_negotiate_compression(self):
        production_server = self.events_module.sio.server.eio
        adapter_class = production_server._async['websocket']
        adapter = adapter_class(lambda _ws: None, production_server)
        environ = {
            'HTTP_SEC_WEBSOCKET_EXTENSIONS': 'permessage-deflate',
        }

        with patch(
            'engineio.async_drivers._websocket_wsgi.SimpleWebSocketWSGI.__call__',
            return_value=None,
        ) as call_parent:
            adapter(environ, lambda *_args: None)

        self.assertNotIn('HTTP_SEC_WEBSOCKET_EXTENSIONS', environ)
        call_parent.assert_called_once()

    def test_service_apps_import_by_package_without_views_collision(self):
        self.assertIn('client_api', self.api_module.app.blueprints)
        self.assertIn('http_receiver', self.receiver_module.app.blueprints)
        self.assertIs(
            self.receiver_module.receiver_bp,
            receiver_views.receiver_bp,
        )

    def test_top_level_imports_match_gunicorn_service_directory(self):
        code = (
            'import socketio; '
            'from lib import flags; '
            'flags.SubmitMonitor = lambda **kwargs: object(); '
            'flags.Judge = lambda **kwargs: object(); '
            'from lib.storage.utils import SIOManager; '
            'SIOManager.read_write = classmethod('
            'lambda cls: socketio.Manager()); '
            'import app; assert app.app'
        )
        env = {
            'PATH': os.environ.get('PATH', ''),
            'PYTHONPATH': str(BACKEND_DIR),
        }
        for service in ('api', 'events', 'http_receiver'):
            with self.subTest(service=service):
                subprocess.run(
                    [sys.executable, '-c', code],
                    cwd=BACKEND_DIR / 'services' / service,
                    env=env,
                    check=True,
                    capture_output=True,
                    text=True,
                    timeout=10,
                )

    def test_paused_receiver_still_returns_503(self):
        client = self.receiver_module.app.test_client()
        with (
            patch.object(storage.teams, 'get_team_id_by_token', return_value=1),
            patch.object(storage.game, 'get_real_round', return_value=1),
            patch.object(storage.game, 'is_game_paused', return_value=True),
        ):
            response = client.put(
                '/flags/',
                json=['INVALID_FLAG'],
                headers={'X-Team-Token': TEST_TEAM_TOKEN},
            )
        self.assertEqual(response.status_code, 503)

    def test_flags_validate_count_length_and_element_type(self):
        client = self.receiver_module.app.test_client()
        with (
            patch.object(storage.teams, 'get_team_id_by_token', return_value=1),
            patch.object(storage.game, 'get_real_round', return_value=1),
            patch.object(storage.game, 'is_game_paused', return_value=False),
        ):
            for flags_data in (
                ['x'] * 101,
                ['x' * 33],
                ['INVALID_FLAG', 1],
            ):
                with self.subTest(flags_data=flags_data):
                    response = client.put(
                        '/flags/',
                        json=flags_data,
                        headers={'X-Team-Token': TEST_TEAM_TOKEN},
                    )
                    self.assertEqual(response.status_code, 400)
                    self.assertNotIn(b'INVALID_FLAG', response.data)

        receiver_views.judge.process_many.assert_not_called()

    def test_invalid_string_flag_keeps_per_item_batch_response(self):
        client = self.receiver_module.app.test_client()
        result = AttackResult(
            attacker_id=1,
            victim_id=2,
            task_id=3,
            submit_ok=False,
            message='Invalid flag',
            attacker_delta=0,
            victim_delta=0,
        )
        with (
            patch.object(storage.teams, 'get_team_id_by_token', return_value=1),
            patch.object(storage.game, 'get_real_round', return_value=1),
            patch.object(storage.game, 'is_game_paused', return_value=False),
            patch.object(storage.teams, 'get_teams', return_value=[]),
            patch.object(storage.tasks, 'get_tasks', return_value=[]),
            patch.object(receiver_views.logger, 'debug') as debug_log,
        ):
            receiver_views.judge.process_many.return_value = [result]
            response = client.put(
                '/flags/',
                json=['INVALID_FLAG'],
                headers={'X-Team-Token': TEST_TEAM_TOKEN},
            )

        self.assertEqual(response.status_code, 200)
        self.assertEqual(
            response.get_json(),
            [{'msg': '[INVALID_FLAG] Invalid flag', 'flag': 'INVALID_FLAG'}],
        )
        self.assertNotIn('INVALID_FLAG', str(debug_log.call_args_list))
