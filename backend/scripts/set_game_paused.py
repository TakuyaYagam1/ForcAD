import argparse
from lib.storage.game import set_game_paused

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=('pause', 'resume'))
    args = parser.parse_args()
    set_game_paused(args.action == 'pause')
