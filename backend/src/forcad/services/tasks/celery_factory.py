from celery import Celery

from forcad.lib import config


def get_celery_app():
    celery_config = config.get_celery_config()
    app = Celery(
        __name__,
        include=[
            'forcad.services.tasks.actions',
            'forcad.services.tasks.handlers',
        ],
    )

    app.conf.update(celery_config.model_dump())
    return app
