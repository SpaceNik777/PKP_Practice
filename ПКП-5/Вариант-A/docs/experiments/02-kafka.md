# Эксперимент с Kafka-first для заявки

## Стенд

Эксперимент выполнен 19.10.2026 в Docker Desktop 4.48.0. Использовались Apache Kafka 4.3.1 в режиме KRaft и `franz-go v1.21.5`. Локального Go в `PATH` не было, поэтому форматирование и проверки запускались в контейнере `golang:1.26.8`.

Топик создаётся сервисом `kafka-init`:

```text
application.commands.v1: 3 partitions, replication factor 1
```

Команда проверки KRaft:

```text
docker exec orderflow-kafka /opt/kafka/bin/kafka-metadata-quorum.sh \
  --bootstrap-server localhost:9092 describe --status
```

Она показала `ClusterId=4L6g3nShT-eMCtK--X86sw`, `LeaderId=1` и одного voter с ролью controller. ZooKeeper в конфигурации отсутствует.

## Вариант A: команда Application

В варианте A конверт передаёт команду `application.create`. Это намерение создать заявку, а не сообщение о том, что она уже создана:

```json
{
  "command_id": "cmd-app-1",
  "command_type": "application.create",
  "command_version": 1,
  "aggregate_id": "app-42",
  "requested_at": "2026-10-19T15:30:00Z",
  "correlation_id": "application-request:app-42",
  "payload": {
    "author": "Ivan",
    "topic": "Budget 2027"
  }
}
```

Ключ Kafka равен `aggregate_id`, то есть `app-42`. Команды одной заявки попадают в одну партицию и читаются по порядку offset. `command_id` используется для идемпотентной обработки.

В репозитории также сохранён общий walkthrough для `Order`. Он показывает ту же транспортную схему, но не относится к варианту A. Все результаты этого эксперимента получены для `Application`, топика `application.commands.v1` и группы `application-command-v1`.

## Producer и consumer

Producer `cmd/application-command-producer` ждёт подтверждение всех синхронных реплик (`acks=all`) и только затем сообщает partition и offset. Worker `cmd/application-command-worker` использует группу `application-command-v1`, `DisableAutoCommit` и `BlockRebalanceOnPoll`. Для каждой записи порядок такой:

1. декодировать envelope и проверить тип/версию;
2. проверить payload и выполнить создание заявки;
3. вызвать `CommitRecords` только после успешной обработки;
4. вызвать `AllowRebalance` после завершения всего результата `PollFetches`.

При ошибке обработки текущая запись не фиксируется, worker завершается с ошибкой. Следующий запуск сможет прочитать её повторно. Обработчик хранит обработанные `command_id`; в промышленной версии этот реестр должен находиться в той же устойчивой БД, что и заявка.

## Два consumer и rebalance

Первый worker первоначально получил партиции `[0 1 2]`. После запуска второго в журналах были `partitions revoked`, затем группа в состоянии `Stable` распределила партиции так:

```text
consumer 1: application.commands.v1:0,1
consumer 2: application.commands.v1:2
```

После остановки второго процесса первый получил все три партиции. Это подтверждено callback-журналом worker и `kafka-consumer-groups.sh --members --verbose`.

## Backlog drain

В топик отправлено 30 разных команд заявок. Итоговое распределение по журналу составило 8, 9 и 13 записей по партициям. После обработки команда описания группы показала:

```text
partition 0: current-offset 8,  log-end-offset 8,  lag 0
partition 1: current-offset 9,  log-end-offset 9,  lag 0
partition 2: current-offset 13, log-end-offset 13, lag 0
```

Надёжное время drain по этому запуску не приводится. Команды создавались последовательными `go run` в одноразовых контейнерах, причём каждый запуск заново загружал зависимости. Такое измерение в основном характеризовало бы запуск toolchain и сеть модулей, а не разбор Kafka backlog. Для чистого замера нужно заранее собрать бинарники, остановить группу, пакетно записать сообщения, зафиксировать начальный lag и одной меткой времени измерить переход к `lag=0` после запуска двух готовых worker.

## Runbook повторения

```text
docker compose up -d kafka
docker compose up kafka-init
docker run --rm --network host -v "${PWD}:/src" -w /src golang:1.26.8 go run ./cmd/application-command-worker
docker run --rm --network host -v "${PWD}:/src" -w /src golang:1.26.8 go run ./cmd/application-command-worker
docker run --rm --network host -v "${PWD}:/src" -w /src golang:1.26.8 go run ./cmd/application-command-producer app-42 Ivan "Budget 2027"
docker exec orderflow-kafka /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:9092 --describe --group application-command-v1
```

Для наблюдения rebalance надо остановить один worker и повторить `--describe --group ... --members --verbose`.

## Контракт HTTP 202

Ingress для Kafka-first должен отвечать `202` только после успешной записи команды в Kafka:

```http
HTTP/1.1 202 Accepted
Location: /v1/operations/cmd-app-1
Content-Type: application/json

{"operation_id":"cmd-app-1","status":"ACCEPTED"}
```

`ACCEPTED` означает только транспортное принятие. После обработки статус операции меняется на `SUCCEEDED` либо `REJECTED`; временная ошибка остаётся `PROCESSING` до retry. Если Kafka не подтвердила запись, `202` возвращать нельзя. Повтор с тем же `Idempotency-Key` обязан вернуть тот же `operation_id` и не публиковать новую логическую команду. Повтор с тем же ключом, но другим телом должен завершаться `409 Conflict`.

## Проверки кода

Успешно выполнены:

```text
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
docker compose config
```
