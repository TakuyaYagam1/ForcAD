import logging

from flask import jsonify, request

from lib import storage
from lib.helpers import events

from .authentication import check_session
from .utils import make_err_response

logger = logging.getLogger(__name__)


def change_game_state(action):
    check_session()
    if action not in {'pause', 'resume', 'finish'}:
        return make_err_response('Unknown game action', status=404)
    if action == 'finish':
        data = request.get_json(silent=True)
        if not isinstance(data, dict) or data.get('confirm') is not True:
            return make_err_response('Game finish confirmation is required', status=400)
    try:
        storage.game.change_game_state(action)
    except ValueError as error:
        return make_err_response(str(error), status=409)
    logger.info('Organizer requested game action: %s', action)
    events.refresh_scoreboard_after_commit()
    return jsonify(storage.game.get_runtime_status())
