<h1 align="center">dreamTunnel</h1>
<p align="center"><strong>Мгновенные публичные HTTPS-туннели</strong> — self-hosted аналог ngrok: одна команда, свой поддомен, сертификат включён, переподключение автоматическое.</p>

<p align="center">
  <a href="https://tunnel.swag.best"><strong>tunnel.swag.best</strong></a>
  &nbsp;&bull;&nbsp;
  <a href="#установка">Установка</a>
  &nbsp;&bull;&nbsp;
  <a href="#использование">Использование</a>
  &nbsp;&bull;&nbsp;
  <a href="#python-sdk">Python SDK</a>
  &nbsp;&bull;&nbsp;
  <a href="#свой-сервер">Свой сервер</a>
</p>

---

## Что это

**dreamTunnel** пробрасывает локальный порт вашего компьютера в публичный интернет через исходящее TLS-соединение — NAT, файрвол и CG-NAT не помеха.

- **Ноль настройки** — токен доступа уже вкомпилирован в бинарник: установил и сразу `dt http 8080`. Никакого `dt login`.
- **Одна команда** — `dt http 8080` → `https://my-site.maiuni.online`
- **HTTPS без предупреждений** — wildcard-сертификат на весь пул поддоменов
- **Не падает** — heartbeat + автовосстановление с экспоненциальным бэкоффом, сессия сохраняется на тот же поддомен
- **Машинный вывод** — `--json` отдаёт события NDJSON: удобно для скриптов, CI и SDK
- **Python SDK** — туннель поднимается прямо из кода, с событиями и корректным завершением
- **Кроссплатформенно** — один бинарник на Go: Windows / Linux / macOS, amd64 / arm64 (в том числе 386)

## Установка

**Windows (PowerShell):**
```powershell
iwr -useb https://tunnel.swag.best/install.ps1 | iex
```

**Linux / macOS:**
```bash
curl -fsSL https://tunnel.swag.best/install.sh | bash
```

Установщик сам определяет платформу, качает бинарник с `https://tunnel.swag.best/dl/` и проверяет его по `SHA256SUMS`.

Вручную (любая платформа) — файлы `dt-linux-amd64`, `dt-linux-arm64`, `dt-darwin-amd64`, `dt-darwin-arm64`, `dt-windows-amd64.exe`, `dt-windows-386.exe` лежат в [`/dl/`](https://tunnel.swag.best/dl/), контрольные суммы — в [`/dl/SHA256SUMS`](https://tunnel.swag.best/dl/SHA256SUMS). Страница загрузок — <https://tunnel.swag.best/>.

## Использование

```bash
dt http 8080                # туннель к HTTP-серверу, случайный поддомен
dt http 8080 my-api         # постоянное имя: https://my-api.maiuni.online
dt tcp 25565                # сырой TCP-туннель (игровой сервер, БД)
dt http 8080 --json         # события NDJSON на stdout
dt config                   # показать конфигурацию (сервер, токен, поддомен, сессия)
dt login                    # необязательно: сохранить свой сервер и токен
dt token <секрет>           # обфусцированная форма токена (для раздачи клиентам)
```

Настраивать ничего не нужно: публичный токен сервера встроен в бинарник при сборке. Свой токен или другой сервер задаются файлом `~/.dreamtunnel/config.env`, переменными окружения или `dt login`.

| Переменная | Смысл |
|---|---|
| `DT_TOKEN` | токен доступа (без него используется встроенный публичный) |
| `DT_SERVER` | адрес control-канала, по умолчанию `tunnel.swag.best:444` |
| `DT_SUB` | желаемый поддомен |
| `DT_CONFIG` | путь к файлу настроек (по умолчанию `~/.dreamtunnel/config.env`) |

`DT_CONFIG` полезен, когда нужно несколько независимых туннелей с одной машины: в файле хранится session-id, и два процесса на одном файле будут отбирать сессию друг у друга.

### `--json`: события для скриптов

С флагом `--json` на stdout идёт по одному JSON-объекту на строку, всё человекочитаемое уезжает в stderr:

```json
{"event":"ready","url":"https://my-api.maiuni.online","host":"my-api.maiuni.online","local":"http://127.0.0.1:8080","mode":"http","localPort":8080,"reconnect":false,"server":"tunnel.swag.best:444"}
{"event":"request","method":"GET","path":"/health"}
{"event":"response","status":200,"duration_ms":3}
{"event":"disconnected","error":"EOF","retry_in":"2s"}
{"event":"ready","reconnect":true,...}
```

Ошибки авторизации приходят как `{"event":"error","error":"server denied: ..."}`.

## Python SDK

Туннель поднимается из кода: SDK запускает `dt`, ждёт подтверждения от релея, отдаёт публичный адрес, отдаёт наружу события (включая переподключение) и корректно всё завершает. Бинарник скачивается автоматически, если его нет.

```bash
pip install "git+https://github.com/dreamcatchered/dreamTunnel.git#subdirectory=sdk/python"
# или из клона репозитория:
pip install -e sdk/python
```

```python
from dreamtunnel import Tunnel

with Tunnel(port=8080, subdomain="my-api") as tun:
    print(tun.url)      # https://my-api.maiuni.online
    # ... работает ваше приложение, туннель живёт
# выход из блока — туннель закрыт
```

```python
tun = Tunnel(port=8080, on_event=lambda e: print(e["event"]))
tun.start()
tun.connected    # True, пока туннель на связи
tun.restart()    # перезапуск с тем же адресом
tun.stop()       # мягкое завершение (SIGTERM, при необходимости SIGKILL)
```

Пакет целиком живёт в [`sdk/python`](sdk/python): `examples/` (Flask, FastAPI, обычный `http.server`), тесты, `dreamtunnel`-команда в терминале.

```bash
cd sdk/python
pip install -e ".[test]"
pytest                                          # юнит-тесты на фейковом dt
DREAMTUNNEL_LIVE=1 pytest tests/test_live.py    # боевой релей
```

## Архитектура

```
клиент                        сервер dreamTunnel                  браузер
┌────────────┐  TLS control  ┌─────────────────────┐   HTTPS    ┌────────┐
│ dt http …  ├──────────────>│ :444  контроль      │<───────────┤ визитёр │
│ локальный  │<──────────────┤ :8453 edge (за       ├──────────>└────────┘
│ web-сервер │  стримы       │ nginx stream SNI)   │
└────────────┘               │ wildcard *.domain    │
                             └─────────────────────┘
```

- **Control-канал** (:444, TLS): аутентификация по токену (мастер-секрет или публичный токен), heartbeat каждые 10 с, регистрация поддомена, мультиплексированные стримы (бинарные фреймы: Open/Data/Close/Ping).
- **Edge** (за nginx stream по SNI): терминирует TLS wildcard-сертификатом для http-режима (обычный локальный HTTP-сервер «просто работает»), для tcp-режима проксирует сырой поток. Если nginx отдаёт `proxy_protocol on`, edge сам снимает заголовок PROXY v1/v2, поэтому работает и напрямую, и за nginx.
- **Переподключение**: клиент хранит session-id, при обрыве реконнектится с бэкоффом 1с→60с + джиттер и получает тот же поддомен. Сервер вычищает мёртвые сессии по таймауту простоя.
- **Фильтр контента**: серверный переключатель `DT_FILTER_ENABLED` — блокирует туннели для явно нелегального контента (узкий список, срабатывание только на однозначные фразы/сочетания).

## Сборка

```bash
go build -o dt .        # текущая платформа
go vet ./... && go test ./...

# сборка релиза со встроенным публичным токеном:
export DT_DEFAULT_TOKEN="$(./dt token "$DT_PUBLIC_TOKEN")"   # обфусцированная форма
./release.sh v1.1.1                                          # dist/ для 6 платформ
```

Без `DT_DEFAULT_TOKEN` сборка получается «чистой»: клиентам придётся задать токен (`dt login` или `DT_TOKEN`).

## Тесты

```bash
go test ./...                       # юнит-тесты сервера (auth, роутер, фильтр)
./scripts/e2e.sh 127.0.0.1:444 127.0.0.1:443 <public-token> prod <master-secret>
```

`scripts/e2e.sh` поднимает локальный HTTP-сервер, проводит туннель через живой релей и проверяет пять вещей: запуск без настройки, выданный URL, реальный ответ через туннель, сохранение поддомена после перезапуска, отказ при мусорном токене и работу мастер-секрета.

## Свой сервер

1. **Бинарник**: `dt server` (systemd-юнит в `deploy/dreamtunnel.service`).
2. **Конфиг** `/etc/dreamtunnel/server.conf`:
   ```
   DT_SECRET=<случайный hex>       # master-секрет (полный доступ)
   DT_PUBLIC_TOKEN=<hex>           # общий токен для клиентов «из коробки» (можно не задавать)
   DT_DOMAIN=example.com           # домен пула поддоменов
   DT_CONTROL_ADDR=:444
   DT_EDGE_ADDR=:8453
   DT_ADMIN_PASS=<пароль админки>  # админка на 127.0.0.1:8484, логин admin
   DT_FILTER_ENABLED=1             # фильтр контента вкл/выкл
   DT_CERT_DIR=/etc/dreamtunnel/certs
   ```
3. **Wildcard-сертификат**: `acme.sh --issue --dns dns_cf -d example.com -d '*.example.com'`.
4. **DNS**: wildcard A-запись `*.example.com → IP сервера`.
5. **nginx stream** (SNI-роутинг :443): поддомены пула → `127.0.0.1:8453`, остальное — обычный HTTPS-стек nginx.
6. **Раздача клиентов**: положите релизные бинарники в каталог `/dl/` вашего сайта, а `SHA256SUMS` рядом; установщики `web/install.sh` и `web/install.ps1` качают именно оттуда.
7. **Публичный токен**: сгенерируйте (`openssl rand -hex 24`), положите в `DT_PUBLIC_TOKEN` и передайте обфусцированную форму в сборку:
   ```bash
   ./dt token <публичный-токен>     # обфусцированная форма
   export DT_DEFAULT_TOKEN=<обфусцированная-форма>
   ./release.sh v1.1.1
   ```
   Мастер-секрет при этом остаётся только на сервере. Приватный токен для конкретного пользователя делается так же: `dt token <мастер-секрет>` без пересборки.

## Стек

| Слой | Технология |
|------|-----------|
| Язык | Go 1.22+ |
| TLS | crypto/tls, wildcard-сертификат Let's Encrypt (DNS-01 через Cloudflare) |
| Протокол | собственный бинарный фрейминг поверх TLS (мультиплексирование стримов) |
| Роутинг | nginx stream + ssl_preread (SNI), PROXY protocol v1/v2 |
| SDK | Python 3.10+ (subprocess + NDJSON), пакет `sdk/python` |
| Процесс | systemd (Restart=always) |

## Лицензия

MIT
