from flask import jsonify, request
from psycopg2 import IntegrityError

from lib import models, storage
from lib.helpers import events

from .api_base import ApiSet
from .utils import make_err_response
from .validation import validate_data


class TeamApi(ApiSet):
    model = 'team'

    @staticmethod
    def retrieve(team_id):
        item = next(
            (item for item in storage.teams.get_all_teams() if item.id == team_id), None
        )
        if item is None:
            return make_err_response('No such team', status=404)
        return jsonify(item.to_dict())

    @staticmethod
    def list():
        return jsonify([item.to_dict() for item in storage.teams.get_all_teams()])

    @staticmethod
    def create():
        try:
            data = request.get_json(silent=True)
            if isinstance(data, dict):
                data = {**data, 'token': models.Team.generate_token()}
            item = models.Team.from_dict(validate_data(data, models.Team))
            created = storage.teams.create_team(item)
        except (TypeError, KeyError, ValueError) as exc:
            return make_err_response(str(exc))
        except IntegrityError:
            return make_err_response(
                'Data violates database constraints. '
                'Check the values and make sure the team token is unique.',
                status=409,
            )
        events.refresh_scoreboard_after_commit()
        return jsonify(created.to_dict()), 201

    @staticmethod
    def update(team_id):
        if not any(item.id == team_id for item in storage.teams.get_all_teams()):
            return make_err_response('No such team', status=404)
        try:
            data = validate_data(request.get_json(silent=True), models.Team)
            data['id'] = team_id
            item = models.Team.from_dict(data)
            if any(
                other.id != team_id and other.token == item.token
                for other in storage.teams.get_all_teams()
            ):
                return make_err_response(
                    'Token is already used by another team', status=409
                )
            updated = storage.teams.update_team(item)
        except (TypeError, KeyError, ValueError) as exc:
            return make_err_response(str(exc))
        except IntegrityError:
            return make_err_response(
                'Data violates database constraints. '
                'Check the values and make sure the team token is unique.',
                status=409,
            )
        events.refresh_scoreboard_after_commit()
        return jsonify(updated.to_dict())

    @staticmethod
    def destroy(team_id):
        if not any(item.id == team_id for item in storage.teams.get_all_teams()):
            return make_err_response('No such team', status=404)
        storage.teams.delete_team(team_id)
        events.refresh_scoreboard_after_commit()
        return jsonify('ok')
