PYTHON ?= python3

.PHONY: render test test-network test-game test-dashboards

render:
	PYTHONDONTWRITEBYTECODE=1 $(PYTHON) render.py

test:
	PYTHONDONTWRITEBYTECODE=1 $(PYTHON) -m unittest discover -s tests -p 'test_*.py' -v

test-network:
	PYTHONDONTWRITEBYTECODE=1 $(PYTHON) tests/run_network.py

test-game:
	PYTHONDONTWRITEBYTECODE=1 $(PYTHON) tests/run_game.py

test-dashboards:
	PYTHONDONTWRITEBYTECODE=1 $(PYTHON) tests/run_dashboards.py
