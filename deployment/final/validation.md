## Evidence

Проверка 2026-11-01. Ветка `chrp-final` основана на имеющемся
`origin/main`, commit `2b20811`. До обновления локальный main отставал на пять
коммитов. Проверки этого отчета выполнены до публикации ветки.

Статический расчет из реального Compose: 160 checker slots, 216 клиентов pgx,
136 серверных соединений PgBouncer, PostgreSQL max_connections 200, до 928
клиентов Redis. После уточнения владельца жесткие лимиты памяти контейнеров
отключены. Сумма soft reservations 26.875 GiB; реальный потолок RAM и остаток
из этих настроек не следуют. Добавленный GOMAXPROCS=2 убран, а базовый
лимит отдельного checker-процесса возвращен с 256 на исходные 1024 MiB.
Zeek финального bundle использует MemoryHigh/CPUWeight без hard memory/CPU cap.
Подробности и допущения находятся в `README.md` и `capacity.json`.

Исторический пятичасовой отчет 2026-10-13 содержит 180000000 REST-запросов и
2653 ответа 503. Тест не прошел strict zero-error criterion. Прием флагов
проверялся отдельно: 720000 batch-запросов, 2041676 принятых флагов совпали с
БД. Причина всех всплесков 503 не установлена. Этот тест не моделировал
40 PFR places на команду с большими ответами attack_data.

Локальные checker-прогоны перед решением перенести нагрузку на i9:

| Основа / slots | Итог первой минуты | Ошибки сохраненных действий |
| --- | --- | --- |
| Старый main 2e104c4 / 160 | UP 62/84 workflow | CHECK и PUT только 101 |
| Новый main 2b20811 / 160 | UP 14/84 workflow | CHECK и PUT только 101 |
| Новый main 2b20811 / 80 | UP 75/84 workflow | CHECK и PUT только 101 |

Все три прогона FAIL: начальная минута не вместила все workflow. Увеличением
deadline результат не маскировался. Эти цифры не являются контролируемым
сравнением скорости двух версий: профили и состояние хоста различались.

Хост этих прогонов: Ryzen 7 8845HS, 8 физических ядер / 16 потоков,
около 30.7 GiB доступной Docker памяти, rootless Docker. В третьем прогоне
сбор диагностик начинался до ожидания первого раунда. В пяти выборках
суммарная загрузка контейнеров составляла 1280-1448%, worker 1163-1327%.
Это примерно 12-13 логических CPU у worker. Число соединений PostgreSQL
31-33, отмен ожидания pgx 0, таймаутов Redis 0, dropped app logs 0.
В четырех из пяти resource-выборок lock_waits=0, в одной lock_waits=1.
Отдельные waits ticker видны в более частых SQL-выборках; полностью исключать
влияние БД нельзя. CPU-профиль и данные целевого сервера еще нужны для
объяснения полной стоимости каждого действия.

Сообщения `job is not ready for execution` наблюдались при публикации. Сами
по себе они не доказывают потерю задания: consumer возвращает его в очередь.
Отдельная ошибка публикации как причина этих FAIL не установлена.

Полные отчеты локальных прогонов находятся в ignored каталоге:

- `backend/load_test/reports/20261101-144709-checker-3672035663/report.json`
- `backend/load_test/reports/20261101-145251-checker-1792108048/report.json`
- `backend/load_test/reports/20261101-145804-checker-1776470523/`

## Changed Files

- `deployment/final/`: лимиты, калькулятор, расчет, сценарии репетиции и этот отчет.
- `deployment/docker/compose*.yml`: управляемые реплики, per-role RAM/GC,
  параметры PostgreSQL и Prometheus, ротация логов.
- `deployment/monitoring/`: ограниченный reader Grafana и alerts worker capacity.
- `infrastracture/`: опциональный capacity profile, 7 VPN-пиров, шаблон 28 команд.
- `backend/load_test/` и deployment test harness: совместный мониторинг,
  224 наблюдателя, PFR fixtures, ранние диагностики, точный аудит раундов,
  отдельные клиентские корзины изолированного генератора.

Рабочие конфиги, секреты, системные sysctl и существующие данные игры
этими настройками не перезаписывались. DB schema этой работой не менялась.

## Commands Run

```sh
python3 deployment/final/capacity.py
python3 -m unittest discover -s deployment/final -p 'test_*.py' -v
TMPDIR=/home/takuya/.codex/.tmp make -C infrastracture test
go -C backend test ./config ./internal/operator ./internal/bootstrap ./load_test
python3 -m unittest discover -s backend/load_test/scripts -p test_prepare.py -v
python3 -m unittest discover -s backend/load_test/test-data/checkers -p test_client.py -v
shellcheck deployment/final/rehearse.sh backend/integration_test/scripts/test_deployment.sh
ruff check deployment/final/*.py infrastracture/render.py infrastracture/tests/test_render.py
```

Также выполнен `make -C deployment test-monitoring` на изолированном стенде
и `promtool check rules` для обоих файлов alerts. Эфемерные контейнеры удалены
самим test harness; после проверок работает только ранее существовавший Portainer.

## Validation Result

Калькулятор проверяет согласованность soft reservation/client budget и явно
показывает неограниченную память через null. Проверки отсутствия hard limits
памяти контейнеров и нового плана reservations прошли. Тест
7 участников сначала воспроизвел отказ старого ограничения, затем прошел с
новым адресным планом. Infrastructure suite: 250 тестов, PASS.
Целевые Go unit tests и тесты PFR protocol/config generator: PASS.
Мониторинг: PASS для replicas, HTTP/flag/WebSocket metrics, Grafana queries,
столбцов таблиц и missing-source handling. Prometheus rule syntax: PASS.

## Skipped Checks

По решению владельца новые тяжелые прогоны перенесены на i9-13900KF / 32 ГБ.
Новый совместный профиль после добавления PFR, 224 клиентов и мониторинга
здесь не запускался. Семь одновременно открытых вкладок Grafana,
пятичасовой новый soak, финальные три сервиса на 28 вулнбоксах, сетевой
packet replay с Zeek, проверка полного 64 GiB бюджета и rollout/rollback на
финальных хостах еще не выполнены. Image scans не повторялись: новых версий
образов или зависимостей эта работа не добавляет.

## Residual Risks

Настройки готовы к репетиции. Допуск к финалу пока не подтвержден.
Остаются неизвестными реальные latency/CPU/RSS двух чекеров, WAN/NIC и
характеристики диска сервера. Новый нагрузочный адаптер не доказывает
сохранение реального client IP через Router/VPN. Семантика его rate buckets
описана в README; production limiter не изменен.

## Next Best Action

Перенести ветку на i9, выполнить `rehearse.sh checker`, затем `combined` и
`soak`. Отдельно провести репетицию финальных сервисов и сети. Проверить
ошибки, полноту PUT и accepted steals, время раунда и запас CPU/RAM, а не
только HTTP RPS. Рабочий deployment этими проверками не изменялся.
