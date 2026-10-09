import logging

from flask import jsonify, request

from lib import storage
from lib.helpers import events

from .authentication import check_session
from .utils import make_err_response

logger = logging.getLogger(__name__)


def change_game_state(action):
    check_session()
    if action not in {
        'start', 'start_practice', 'start_final', 'pause', 'resume', 'finish',
    }:
        return make_err_response('Unknown game action', status=404)
    data = request.get_json(silent=True)
    generation = data.get('generation') if isinstance(data, dict) else None
    if generation is not None and type(generation) is not int:
        return make_err_response('Invalid game generation', status=400)
    if action in {'finish', 'start_final'}:
        if not isinstance(data, dict) or data.get('confirm') is not True:
            return make_err_response('Game action confirmation is required', status=400)
    try:
        if generation is None:
            storage.game.change_game_state(action)
        else:
            storage.game.change_game_state(action, generation=generation)
    except ValueError as error:
        return make_err_response(str(error), status=409)
    logger.info('Organizer requested game action: %s', action)
    events.refresh_scoreboard_after_commit()
    return jsonify(storage.game.get_runtime_status())
