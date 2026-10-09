# Эксперимент жизненного цикла

## Условие

- ОС: Windows 10 Pro (оболочка cmd.exe), разрядность amd64.
- Версия Go: `go version go1.26.8 windows/amd64` (тулчейн распакован из официального архива `go1.26.8.windows-amd64.zip`, контрольная сумма SHA256 сверена с опубликованной на go.dev).
- Способ запуска: собранный бинарный файл `application-command.exe` (`go build -o application-command.exe ./cmd/application-command`), запущенный в фоне с журналом в файл (`run1.log`, `run2.log`).
- Параметры запуска № 1: `SERVICE_NAME=application-api`, `LOG_LEVEL=info`, `SHUTDOWN_TIMEOUT=10s`, `PING_DELAY_MS` не задана (мгновенный успех адаптера).
- Параметры запуска № 2: то же, но `PING_DELAY_MS=1000` (технический адаптер отвечает с задержкой 1 с).
- Длительность `/slow`: 3 s.
- Проверка сигнального пути остановки на Windows из скрипта невозможна (graceful-остановка по сигналу не отправлялась; имитация Ctrl+C через `taskkill` не выполнялась — она не эквивалентна мягкой остановке). Путь остановки с активным запросом воспроизведён интеграционным тестом в процессе (`internal/platform/httpserver/lifecycle_test.go`):
  - сценарий «достаточный бюджет»: сигнал-эквивалент (вызов `SetReady(false)` + `server.Shutdown`) подаётся сразу после начала обработки `/slow`, бюджет 5 s;
  - сценарий «недостаточный бюджет»: то же, но бюджет 1 s (до конца обработки `/slow` остаётся ~3 s, то есть заведомо больше бюджета).

## Ожидание

- При достаточном бюджете (5 s > остатка работы ~3 s): `Shutdown` завершается без ошибки, активный `/slow` завершается успешно, клиент получает `200` и `{"status":"completed"}`.
- При недостаточном бюджете (1 s < остатка работы ~3 s): `Shutdown` возвращает ошибку `context deadline exceeded`; в ручном запуске через `main` процесс вышел бы с кодом 1, а незавершённый запрос обрывается именно завершением процесса (в интеграционном тесте соединение закрывает сам тест вызовом `server.Close()` — ресурсы теста управляются явно).
- Локальный флаг готовности выключается (`draining`); внешний ответ `503` после закрытия слушателя не гарантирован: новая попытка `/readyz` может попасть в уже закрытый слушатель.
- При `PING_DELAY_MS=1000` адаптер не укладывается в предел 300 мс: `/readyz` отвечает `503` с `{"status":"not_ready"}`, `/livez` при этом остаётся успешным — живость не связана с отказом адаптера.
- `/slow` без остановки процесса завершается примерно за 3 s.

## Наблюдение

Фактические результаты ручного запуска (HTTP-ответы клиента):

```
===1_livez===
HTTP/1.1 200 OK
Content-Type: application/json
X-Request-Id: 6793b20f012cd14e4b1d3ad28631f7e3
{"status":"alive"}

===2_readyz===            (запуск № 1, PING_DELAY_MS не задана)
HTTP/1.1 200 OK
Content-Type: application/json
X-Request-Id: cbcebd89977fcd783683a6ada9bb5b00
{"status":"ready"}

===3_valid_request_id===  (входящий X-Request-ID: Order-Flow-2026-10-04_01)
HTTP/1.1 200 OK
X-Request-Id: Order-Flow-2026-10-04_01
{"status":"alive"}

===4_invalid_request_id=== (входящий X-Request-ID: "bad id!" — вне контракта)
HTTP/1.1 200 OK
X-Request-Id: 502e21b3b9b2bcc359736050ac775946
{"status":"alive"}

===slow===
HTTP/1.1 200 OK
X-Request-Id: 36ff64232cd7a2e1ff2f79f6fc591409
{"status":"completed"}
time_total=3.002184s http_code=200

===readyz_with_1000ms_delay=== (запуск № 2)
HTTP/1.1 503 Service Unavailable
Content-Type: application/json
X-Request-Id: cc785944ab04046752a745c1202cedd3
{"status":"not_ready"}
time_total=0.330527s
```

Фактические строки журнала сервиса (файлы `run1.log`, `run2.log`):

```
{"time":"2026-10-04T23:33:03.1186115+03:00","level":"INFO","msg":"http server started","service":"application-api","address":":8080"}
{"time":"2026-10-04T23:47:41.8155916+03:00","level":"INFO","msg":"http request completed","method":"GET","path":"/livez","request_id":"6793b20f012cd14e4b1d3ad28631f7e3","duration_ms":0}
{"time":"2026-10-04T23:47:41.8707302+03:00","level":"INFO","msg":"http request completed","method":"GET","path":"/readyz","request_id":"cbcebd89977fcd783683a6ada9bb5b00","duration_ms":0}
{"time":"2026-10-04T23:47:41.8958287+03:00","level":"INFO","msg":"http request completed","method":"GET","path":"/livez","request_id":"Order-Flow-2026-10-04_01","duration_ms":0}
{"time":"2026-10-04T23:47:41.929099+03:00","level":"INFO","msg":"http request completed","method":"GET","path":"/livez","request_id":"502e21b3b9b2bcc359736050ac775946","duration_ms":0}
{"time":"2026-10-04T23:47:58.1605585+03:00","level":"INFO","msg":"http request completed","method":"GET","path":"/slow","request_id":"36ff64232cd7a2e1ff2f79f6fc591409","duration_ms":3000}
{"time":"2026-10-04T23:59:44.9224707+03:00","level":"WARN","msg":"readiness ping failed","request_id":"cc785944ab04046752a745c1202cedd3","error":"context deadline exceeded"}
{"time":"2026-10-04T23:59:44.9344332+03:00","level":"INFO","msg":"http request completed","method":"GET","path":"/readyz","request_id":"cc785944ab04046752a745c1202cedd3","duration_ms":312}
```

Сигнальный путь остановки (воспроизводимые факты интеграционного теста, `go test ./internal/platform/httpserver/ -run TestShutdown -v`):

```
=== RUN   TestShutdownWaitsForActiveSlowRequest
--- PASS: TestShutdownWaitsForActiveSlowRequest (3.23s)
=== RUN   TestShutdownTimesOutWhenBudgetInsufficient
--- PASS: TestShutdownTimesOutWhenBudgetInsufficient (1.00s)
PASS
ok  	example.com/orderflow/internal/platform/httpserver	4.840s
```

- Достаточный бюджет (5 s): `Shutdown` вернул `nil`; клиент `/slow` получил `200` и `{"status":"completed"}`.
- Недостаточный бюджет (1 s): `Shutdown` вернул `context.DeadlineExceeded`; клиент завершённого ответа не получил (после тестового `server.Close()` соединение оборвано).

Локальное состояние / HTTP-запрос / код выхода процесса — разделяются:

- Локальное состояние: флаг готовности (`SetReady(false)`) и закрытие слушателя наблюдаются только внутри процесса; внешний `503 draining` в момент остановки в ручном запуске не запрашивался и не измерялся.
- Результат HTTP-запроса: статусы и тела ответов приведены выше; `time_total` — клиентское время, измеренное curl.
- Код выхода процесса: в ручных запусках № 1 и № 2 процесс завершался принудительным `taskkill /F` (уборка тестовой среды, не имитация мягкой остановки), код выхода при этом не является проверкой сигнального пути. Код выхода проверен отдельно для отказа конфигурации: запуск без `SERVICE_NAME` выводит `load config: SERVICE_NAME is required: environment variable is not set` и завершается с кодом 1.
- Время от сигнала до выхода процесса: в ручном запуске не измерялось (сигнал не отправлялся); в интеграционном тесте Shutdown-фаза при достаточном бюджете длилась до завершения `/slow` (остаток ~3 s от начала обработки). Неизмеренные интервалы обозначены как неизмеренные.

## Вывод

- Результаты соответствуют бюджетам: при бюджете 5 s остаток работы `/slow` (~3 s) покрывается — `Shutdown` без ошибки, клиент получает `200 completed`; при бюджете 1 s остаток работы (~3 s) превышает бюджет — `Shutdown` возвращает `context deadline exceeded`. Исход определяется остатком работы на момент сигнала, а не только значением переменной.
- Бюджет 10 s, выбранный для демонстрационного запуска, покрывает `/slow` (3 s) с запасом на завершение записи ответов и накладные расходы остановки; для будущего сервиса с реальными операциями бюджет должен выбираться из допустимых длительностей бизнес-операций и внешнего предела платформы, а не из длительности учебного маршрута.
- Предел 300 мс проверки адаптера действует: при `PING_DELAY_MS=1000` ответ `/readyz` — `503 not_ready` примерно через 0.33 с (клиентское время 0.330527 s, серверное `duration_ms=312`), то есть производный контекст останавливает ожидание адаптера; `/livez` остаётся `200`, что подтверждает разделение живости и готовности.
- Идентификатор запроса ведёт себя по контракту: валидный входящий `Order-Flow-2026-10-04_01` сохранён в ответе и журнале; невалидный `bad id!` заменён сгенерированным и в журнал/ответ не попал; у каждого ответа присутствует заголовок `X-Request-ID`.
- Маршрутизация Kubernetes (EndpointSlice, поведение readiness-проб, доставка SIGTERM контейнеру в Linux) этим опытом не проверялась; локальные результаты не приписывают развёртыванию непроверенных свойств.
