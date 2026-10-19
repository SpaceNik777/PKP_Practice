# Эксперимент 02: contention ограниченной очереди

Дата измерения: 19 октября 2026 года.

Окружение:

- Go `go1.26.8 windows/amd64`;
- ОС `Microsoft Windows NT 10.0.19045.0`;
- CPU `Intel(R) Core(TM) i9-10850K CPU @ 3.60GHz`;
- пакет `example.com/orderflow/internal/platform/workqueue`.

Команда:

```text
go test ./internal/platform/workqueue -run '^$' -bench BenchmarkQueueContention -benchmem -count=3
```

Результаты:

| Workers | Запуск 1, ns/op | Запуск 2, ns/op | Запуск 3, ns/op | B/op | allocs/op |
|---:|---:|---:|---:|---:|---:|
| 1 | 2143 | 1853 | 1860 | 24 | 1 |
| 2 | 1164 | 1389 | 1082 | 24 | 1 |
| 4 | 1023 | 656,1 | 598,3 | 24 | 1 |
| 8 | 536,6 | 504,5 | 519,8 | 24 | 1 |

В этом CPU-bound сценарии наименьшее время получено при восьми workers. Benchmark выполняет 2048 шага вычисления на каждое задание, поэтому результат относится только к этой нагрузке и данной машине. Объём выделений при росте числа workers не изменился.

Профиль получен командой:

```text
go test ./internal/platform/workqueue -run '^$' -bench 'BenchmarkQueueContention/workers=8$' -benchtime=3s -cpuprofile %TEMP%\pkp3-contention-cpu.out
```

Контрольный benchmark профиля: `6783944` операций, `534,6 ns/op`, `24 B/op`, `1 allocs/op`. Профиль: длительность `4,19s`, суммарно `18,17s` CPU samples. Наиболее крупные flat-узлы:

| Узел | Flat | Flat % |
|---|---:|---:|
| benchmark job | 8,05s | 44,30% |
| `runtime.semasleep` | 4,31s | 23,72% |
| `runtime.procyieldAsm` | 1,32s | 7,26% |
| `runtime.semawakeup` | 1,19s | 6,55% |
| `runtime.lock2` | 0,62s | 3,41% |
| `runtime.unlock2` | 0,57s | 3,14% |

Профиль содержит полезное вычисление и расходы runtime на синхронизацию очереди. Файл профиля оставлен во временном каталоге и в проект не включён.
