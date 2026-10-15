# dreamtunnel (Python SDK)

Python-обёртка над CLI `dt`: запускает туннель **из кода**, отдаёт публичный
URL, присылает события (включая переподключения) и корректно всё останавливает.

При первом запуске устройство само регистрируется на релее и получает личный
токен — настраивать ничего не нужно.

```bash
pip install dreamtunnel
```

Пакет опубликован на PyPI: <https://pypi.org/project/dreamtunnel/>. Из исходников — так же просто:

```bash
git clone https://github.com/dreamcatchered/dreamTunnel
pip install -e dreamTunnel/sdk/python
```

Если `dt` ещё нет, SDK скачает его сам при первом запуске.

## Быстрый старт

```python
from dreamtunnel import Tunnel

with Tunnel(port=8080) as tun:
    print(tun.url)        # https://k7f3m9ab.maiuni.online
    print(tun.device_id)  # id этого устройства на сервере
    ...                   # туннель живёт, пока мы внутри блока
# выход из блока — туннель закрыт
```

Свой поддомен:

```python
with Tunnel(port=8080, subdomain="my-api") as tun:
    print(tun.url)        # https://my-api.maiuni.online
```

## Управление вручную

```python
tun = Tunnel(port=3000, subdomain="dev")
tun.start()               # ждёт, пока релей подтвердит туннель
print(tun.url, tun.host, tun.local_url, tun.device_id, tun.pid)

tun.stop()                # SIGTERM, при необходимости SIGKILL
tun.restart()             # перезапуск с тем же поддоменом

tun.connect()             # синоним start()
tun.disconnect()          # синоним stop()
```

## События и переподключение

`dt` сам держит соединение и переподключается при обрыве. SDK отдаёт это наружу:

```python
def on_event(event: dict) -> None:
    # event["event"]: enrolled | ready | reconnected | disconnected | error | request | response
    print(event)

tun = Tunnel(port=8080, on_event=on_event)
tun.start()

tun.events          # все события, что видели
tun.connected       # True, пока туннель на связи
tun.device_id       # id устройства на сервере (None у старых сборок dt)
tun.stderr          # что писал dt в stderr
```

## Запуск приложения вместе с туннелем

```python
import threading, time
from flask import Flask
from dreamtunnel import Tunnel

app = Flask(__name__)

@app.route("/")
def index():
    return "привет"

threading.Thread(
    target=lambda: app.run(host="127.0.0.1", port=8080, use_reloader=False),
    daemon=True,
).start()
time.sleep(0.5)

with Tunnel(port=8080, subdomain="my-api") as tun:
    print(tun.url)
    while True:
        time.sleep(1)
```

Рабочие примеры — в [`examples/`](examples): `flask_app.py`, `fastapi_app.py`.

## CLI

```bash
dreamtunnel http 8080                 # как `dt http`, но через SDK
dreamtunnel http 8080 my-api --json   # машинный вывод (NDJSON)
dreamtunnel tcp 25565
python -m dreamtunnel http 8080
```

## Параметры `Tunnel`

| Параметр | По умолчанию | Смысл |
|---|---|---|
| `port` | — | локальный порт |
| `subdomain` | случайный | желаемое имя: `my-api` → `my-api.maiuni.online` |
| `mode` | `"http"` | `"http"` или `"tcp"` |
| `server` | `tunnel.swag.best:444` | адрес control-канала |
| `token` | встроенный | свой токен; по умолчанию устройство регистрируется само |
| `dt_binary` | авто | путь к `dt` |
| `auto_install` | `True` | скачивать `dt`, если не найден |
| `ready_timeout` | `30` | сколько ждать подтверждения |
| `on_event` | `None` | колбэк на каждое событие |
| `session` | `True` | свой файл настроек и сессия для этого туннеля |
| `config_path` | — | явный путь к файлу настроек `dt` |

## Исключения

| Исключение | Когда |
|---|---|
| `BinaryNotFound` | `dt` не найден и автоустановка выключена |
| `TunnelTimeout` | релей не ответил за `ready_timeout` |
| `TunnelExited` | процесс `dt` умер до подключения (в `.output` — его вывод) |
| `TunnelError` | релей отказал (плохой токен, занятый поддомен) |

## Переменные окружения

| Переменная | Смысл |
|---|---|
| `DT_BINARY` | путь к `dt` |
| `DT_TOKEN` | токен доступа |
| `DT_SERVER` | адрес control-канала |
| `DT_SUB` | поддомен |
| `DT_CONFIG` | файл настроек `dt` (SDK задаёт свой на каждый туннель) |
| `DREAMTUNNEL_BASE_URL` | откуда скачивать бинарник (по умолчанию `https://tunnel.swag.best`) |
| `DREAMTUNNEL_HOME` | куда класть скачанный бинарник и сессии (по умолчанию `~/.dreamtunnel`) |

## Как это устроено

SDK не переписывает протокол: он запускает `dt <mode> <port> --json` и
разбирает поток NDJSON-событий (`enrolled`, `ready`, `disconnected`, `error`, …). Если
попался старый бинарник без `--json`, строки вида
`Forwarding https://… -> http://…` разбираются регуляркой — поддержка
обратной совместимости встроена.

При первом запуске на новой машине `dt` регистрирует устройство на релее
(событие `enrolled`, id виден в `tun.device_id`) и сохраняет полученный токен в
своём файле настроек, поэтому дальше подключение идёт уже по личному токену
этого устройства.

Каждому туннелю SDK выдаёт **свой** файл настроек `dt` (переменная
`DT_CONFIG`), поэтому сессии не пересекаются: несколько туннелей с одной
машины (и даже из одного процесса) работают одновременно и не выбивают друг
друга. `tun.stop()` сохраняет файл — перезапуск получает тот же адрес,
`tun.close()` его удаляет.

Практический смысл: `dt` хранит session-id в `~/.dreamtunnel/config.env`, и
если два процесса используют один файл, второй забирает сессию себе, а первый
теряет туннель.

## Тесты

```bash
pip install -e ".[test]"
pytest                       # юнит-тесты на фейковом dt
DREAMTUNNEL_LIVE=1 pytest tests/test_live.py -v   # боевой релей
```

Лицензия: MIT.
