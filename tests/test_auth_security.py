import sys
from pathlib import Path
from types import SimpleNamespace
from unittest import TestCase
from unittest.mock import patch
from http.cookies import SimpleCookie

from flask import Flask
import fakeredis

PROJECT_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(PROJECT_DIR / 'backend'))
sys.path.insert(0, str(PROJECT_DIR / 'backend/services/admin'))

from lib import storage
from lib.helpers.http_limits import MAX_JSON_BODY_BYTES
from lib.storage.keys import CacheKeys
from services.admin.app import app as admin_app
from viewsets import admin_bp, authentication


class AdminAuthenticationSecurityTests(TestCase):
    def setUp(self):
        self.redis = fakeredis.FakeRedis(decode_responses=True)
        self.redis_patch = patch.object(
            storage.utils.RedisStorage, 'get', return_value=self.redis
        )
        self.redis_patch.start()
        self.addCleanup(self.redis_patch.stop)

        self.credentials = SimpleNamespace(
            username='organizer',
            password='old-test-password',
        )
        self.credentials_patch = patch(
            'viewsets.authentication.config.get_web_credentials',
            return_value=self.credentials,
        )
        self.credentials_patch.start()
        self.addCleanup(self.credentials_patch.stop)

        self.app = Flask('admin-auth-security-test')
        self.app.register_blueprint(admin_bp, url_prefix='/api/admin/')
        self.client = self.app.test_client()

    def test_admin_app_rejects_oversized_json_before_login(self):
        client = admin_app.test_client()
        response = client.post(
            '/api/admin/login/',
            json={
                'username': 'x' * MAX_JSON_BODY_BYTES,
                'password': 'test-password',
            },
        )
        self.assertEqual(response.status_code, 413)

    def login(self, client=None, base_url='http://localhost'):
        client = client or self.client
        return client.post(
            '/api/admin/login/',
            json={
                'username': self.credentials.username,
                'password': self.credentials.password,
            },
            base_url=base_url,
        )

    def test_session_has_eight_hour_ttl_and_no_password(self):
        response = self.login()
        self.assertEqual(response.status_code, 200)
        session = self.client.get_cookie('session').value
        stored = self.redis.get(CacheKeys.session(session))
        self.assertNotIn(self.credentials.password, stored)
        ttl = self.redis.ttl(CacheKeys.session(session))
        self.assertGreaterEqual(
            ttl,
            authentication.SESSION_TTL_SECONDS - 2,
        )
        self.assertLessEqual(
            ttl,
            authentication.SESSION_TTL_SECONDS,
        )
        cookie = SimpleCookie()
        cookie.load(response.headers['Set-Cookie'])
        self.assertEqual(
            cookie['session']['max-age'],
            str(authentication.SESSION_TTL_SECONDS),
        )
        self.assertTrue(cookie['session']['httponly'])
        self.assertEqual(cookie['session']['samesite'], 'Lax')

    def test_password_marker_is_unique_per_session(self):
        self.assertEqual(self.login().status_code, 200)
        first_session = self.client.get_cookie('session').value
        first = self.redis.get(CacheKeys.session(first_session))

        other_client = self.app.test_client()
        self.assertEqual(self.login(other_client).status_code, 200)
        second_session = other_client.get_cookie('session').value
        second = self.redis.get(CacheKeys.session(second_session))

        self.assertNotEqual(first_session, second_session)
        self.assertNotEqual(first, second)

    def test_login_requires_string_credentials(self):
        for field, value in (
            ('username', None),
            ('username', 123),
            ('password', None),
            ('password', ['not', 'a', 'string']),
        ):
            with self.subTest(field=field, value=value):
                credentials = {
                    'username': self.credentials.username,
                    'password': self.credentials.password,
                }
                credentials[field] = value
                response = self.client.post(
                    '/api/admin/login/',
                    json=credentials,
                )
                self.assertEqual(response.status_code, 403)

    def test_secure_cookie_follows_https_and_forwarded_proxy_scheme(self):
        secure_response = self.login(base_url='https://localhost')
        secure_cookie = SimpleCookie()
        secure_cookie.load(secure_response.headers['Set-Cookie'])
        self.assertTrue(secure_cookie['session']['secure'])

        proxy_response = admin_app.test_client().post(
            '/api/admin/login/',
            json={
                'username': self.credentials.username,
                'password': self.credentials.password,
            },
            headers={'X-Forwarded-Proto': 'https'},
        )
        proxy_cookie = SimpleCookie()
        proxy_cookie.load(proxy_response.headers['Set-Cookie'])
        self.assertTrue(proxy_cookie['session']['secure'])

        local_response = self.login()
        local_cookie = SimpleCookie()
        local_cookie.load(local_response.headers['Set-Cookie'])
        self.assertFalse(local_cookie['session']['secure'])

    def test_password_rotation_invalidates_existing_session(self):
        self.assertEqual(self.login().status_code, 200)
        session = self.client.get_cookie('session').value
        self.credentials.password = 'new-test-password'

        response = self.client.get('/api/admin/status/')
        self.assertEqual(response.status_code, 403)
        self.assertFalse(self.redis.exists(CacheKeys.session(session)))

    def test_logout_revokes_session_and_clears_cookie(self):
        self.assertEqual(self.login(base_url='https://localhost').status_code, 200)
        session = self.client.get_cookie('session').value

        response = self.client.post(
            '/api/admin/logout/',
            base_url='https://localhost',
            headers={'Origin': 'https://localhost'},
        )
        self.assertEqual(response.status_code, 200)
        self.assertFalse(self.redis.exists(CacheKeys.session(session)))
        cookie = SimpleCookie()
        cookie.load(response.headers['Set-Cookie'])
        self.assertEqual(cookie['session']['max-age'], '0')
        self.assertTrue(cookie['session']['secure'])

    def test_cross_origin_mutation_is_rejected_and_same_origin_allowed(self):
        self.assertEqual(self.login(base_url='https://localhost').status_code, 200)
        session = self.client.get_cookie('session').value

        rejected = self.client.post(
            '/api/admin/logout/',
            base_url='https://localhost',
            headers={'Origin': 'https://other.example'},
        )
        self.assertEqual(rejected.status_code, 403)
        self.assertTrue(self.redis.exists(CacheKeys.session(session)))
        self.assertEqual(
            self.client.get(
                '/api/admin/status/',
                base_url='https://localhost',
            ).status_code,
            200,
        )

        accepted = self.client.post(
            '/api/admin/logout/',
            base_url='https://localhost',
            headers={'Origin': 'https://localhost'},
        )
        self.assertEqual(accepted.status_code, 200)
        self.assertFalse(self.redis.exists(CacheKeys.session(session)))

    def test_admin_app_does_not_enable_cross_origin_credentials(self):
        response = admin_app.test_client().options(
            '/api/admin/logout/',
            headers={
                'Origin': 'https://other.example',
                'Access-Control-Request-Method': 'POST',
            },
        )
        self.assertNotIn('Access-Control-Allow-Origin', response.headers)
        self.assertNotIn('Access-Control-Allow-Credentials', response.headers)
