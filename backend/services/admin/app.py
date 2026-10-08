import logging

from flask import Flask
from prometheus_flask_exporter import PrometheusMetrics
from viewsets import admin_bp
from werkzeug.middleware.proxy_fix import ProxyFix

from lib.helpers.http_limits import install_request_limits

app = Flask('forcad_admin')
install_request_limits(app)
# The single trusted proxy is the Nginx service in docker-compose. It must
# overwrite X-Forwarded-Proto with the external request scheme.
app.wsgi_app = ProxyFix(app.wsgi_app, x_proto=1)
PrometheusMetrics(
    app,
    path='/api/admin/metrics',
    group_by='url_rule',
    default_latency_as_histogram=True,
)

app.register_blueprint(admin_bp, url_prefix='/api/admin/')

if __name__ == '__main__':
    app.run(host="0.0.0.0", port=5000, debug=True)
else:
    gunicorn_logger = logging.getLogger('gunicorn.error')
    logging.basicConfig(
        level=gunicorn_logger.level,
        handlers=gunicorn_logger.handlers,
    )
