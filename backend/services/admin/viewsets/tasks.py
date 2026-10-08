from flask import request, jsonify
from psycopg2 import IntegrityError

from lib import models, storage
from lib.helpers import events
from .api_base import ApiSet
from .utils import make_err_response
from .validation import validate_data


class TaskApi(ApiSet):
    model = 'task'

    @staticmethod
    def retrieve(task_id):
        item = next(
            (item for item in storage.tasks.get_all_tasks() if item.id == task_id), None
        )
        if item is None:
            return make_err_response('No such task', status=404)
        return jsonify(item.to_dict())

    @staticmethod
    def list():
        return jsonify([item.to_dict() for item in storage.tasks.get_all_tasks()])

    @staticmethod
    def create():
        try:
            data = request.get_json(silent=True)
            item = models.Task.from_dict(validate_data(data, models.Task))
            created = storage.tasks.create_task(item)
        except (TypeError, KeyError, ValueError) as exc:
            return make_err_response(str(exc))
        except IntegrityError:
            return make_err_response(
                'Данные нарушают ограничения базы. '
                'Проверьте значения и уникальность токена.',
                status=409,
            )
        events.refresh_scoreboard_after_commit()
        return jsonify(created.to_dict()), 201

    @staticmethod
    def update(task_id):
        if not any(item.id == task_id for item in storage.tasks.get_all_tasks()):
            return make_err_response('No such task', status=404)
        try:
            data = validate_data(request.get_json(silent=True), models.Task)
            data['id'] = task_id
            item = models.Task.from_dict(data)
            updated = storage.tasks.update_task(item)
        except (TypeError, KeyError, ValueError) as exc:
            return make_err_response(str(exc))
        except IntegrityError:
            return make_err_response(
                'Данные нарушают ограничения базы. '
                'Проверьте значения и уникальность токена.',
                status=409,
            )
        events.refresh_scoreboard_after_commit()
        return jsonify(updated.to_dict())

    @staticmethod
    def destroy(task_id):
        if not any(item.id == task_id for item in storage.tasks.get_all_tasks()):
            return make_err_response('No such task', status=404)
        storage.tasks.delete_task(task_id)
        events.refresh_scoreboard_after_commit()
        return jsonify('ok')
