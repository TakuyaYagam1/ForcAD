## Evidence

Профиль `pr-final` рассчитан статически для шести игровых команд и одной
NPC-команды на VM 8 vCPU / 16 GiB. Рабочая нагрузка: kincare 5 PUT / 3 GET,
wahrwelt 10 PUT / 1 GET, sentinel 3 PUT / 3 GET и по одному CHECK на сервис.
Это 28 действий на команду и верхняя граница 196 действий на раунд.

Compose и `limits.env.example` задают 2 API, 1 receiver, 2 worker по 8 action
slots и 1 ticker. Сумма hard memory limits долгоживущих контейнеров с
Prometheus и Grafana равна 13.5 GiB; статический остаток VM равен 2.5 GiB.
PgBouncer получает 52 application clients, 52 server connections и глобальный
client limit 128. PostgreSQL `max_connections` равен 80.

Это исходные бюджеты, а не результаты нагрузочного теста.

## Validation Result

`python3 deployment/final/capacity.py` отрендерил основной Compose вместе с
monitoring overlay: 13.5 GiB hard limits, 2.5 GiB остатка, `errors: []`.
Восемь unit-тестов калькулятора и 30 тестов renderer прошли. `bash -n` для
репетиции и deployment harness, а также целевые Go-тесты профиля нагрузки прошли.
Реальные чекеры и 240 раундов на VM 8 vCPU / 16 GiB не запускались.

## Residual Risks

Hard limit worker 3 GiB ограничивает контейнер целиком, тогда как каждый из
восьми checker-процессов сохраняет RLIMIT 1024 MiB. Высокий фактический RSS
может вызвать OOM. На 8 vCPU 16 action slots также могут не уложиться в
60 секунд при CPU-bound чекерах. Prometheus и Grafana ограничены по памяти,
поэтому тяжелые запросы мониторинга могут завершаться ошибкой.

## Next Best Action

Провести репетицию с реальными чекерами и мониторингом на VM целевого размера.
Допуск основывать на полноте действий каждого раунда, latency, очереди,
CPU/RSS, OOM/restarts, состоянии PostgreSQL и работоспособности мониторинга.
