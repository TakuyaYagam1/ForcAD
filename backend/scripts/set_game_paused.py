import argparse

from lib.storage.game import change_game_state

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=('pause', 'resume'))
    args = parser.parse_args()
    change_game_state(args.action)
