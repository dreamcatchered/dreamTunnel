<h1 align="center">dreamTunnel</h1>
<p align="center"><strong>Мгновенные публичные HTTPS-туннели</strong> — self-hosted аналог ngrok: одна команда, свой поддомен, сертификат включён, переподключение автоматическое.</p>

<p align="center">
  <a href="https://tunnel.swag.best"><strong>tunnel.swag.best</strong></a>
  &nbsp;&bull;&nbsp;
  <a href="#установка">Установка</a>
  &nbsp;&bull;&nbsp;
  <a href="#использование">Использование</a>
  &nbsp;&bull;&nbsp;
  <a href="#свой-сервер">Свой сервер</a>
</p>

---

## Что это

**dreamTunnel** пробрасывает локальный порт вашего компьютера в публичный интернет через исходящее TLS-соединение — NAT, файрвол и CG-NAT не помеха.

- **Одна команда** — `dt http 8080` → `https://my-site.maiuni.online`
- **HTTPS без предупреждений** — wildcard-сертификат на весь пул поддоменов
- **Не падает** — heartbeat + автовосстановление с экспоненциальным бэкоффом, сессия сохраняется на тот же поддомен
- **Кроссплатформенно** — один бинарник на Go: Windows / Linux / macOS, amd64 / arm64

## Установка

**Windows (PowerShell):**
```powershell
iwr -useb https://tunnel.swag.best/install.ps1 | iex
```

**Linux / macOS:**
```bash
curl -fsSL https://tunnel.swag.best/install.sh | bash
```

Или скачайте бинарник из [Releases](https://github.com/dreamcatchered/dreamTunnel/releases) и положите в PATH.

## Использование

```bash
dt http 8080                # туннель к HTTP-серверу, случайный поддомен
dt http 8080 my-api         # постоянное имя: https://my-api.maiuni.online
dt tcp 25565                # сырой TCP-туннель (игровой сервер, БД)
dt login                    # сохранить сервер и токен
dt config                   # показать конфигурацию
```

Токен доступа задаётся `DT_TOKEN` в окружении или в `~/.dreamtunnel/config.env`
(также поддерживаются `DT_SERVER`, `DT_SUB`).

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

- **Control-канал** (:444, TLS): аутентификация по токену, heartbeat каждые 10 с, регистрация поддомена, мультиплексированные стримы (бинарные фреймы: Open/Data/Close/Ping).
- **Edge** (за nginx stream по SNI): терминирует TLS wildcard-сертификатом для http-режима (обычный локальный HTTP-сервер «просто работает»), для tcp-режима проксирует сырой поток.
- **Переподключение**: клиент хранит session-id, при обрыве реконнектится с бэкоффом 1с→60с + джиттер и получает тот же поддомен. Сервер вычищает мёртвые сессии по таймауту простоя.
- **Фильтр контента**: серверный переключатель `DT_FILTER_ENABLED` — блокирует туннели для явно нелегального контента (узкий список, срабатывание только на однозначные фразы/сочетания).

## Сборка

```bash
go build -o dt .        # текущая платформа
./release.sh            # все платформы: windows/linux/darwin x amd64/arm64
```

## Свой сервер

1. **Бинарник**: `dt server` (systemd-юнит в `deploy/dreamtunnel.service`).
2. **Конфиг** `/etc/dreamtunnel/server.conf`:
   ```
   DT_SECRET=<случайный hex>       # master-секрет
   DT_DOMAIN=example.com           # домен пула поддоменов
   DT_CONTROL_ADDR=:444
   DT_EDGE_ADDR=:8453
   DT_FILTER_ENABLED=1             # фильтр контента вкл/выкл
   DT_CERT_DIR=/etc/dreamtunnel/certs
   ```
3. **Wildcard-сертификат**: `acme.sh --issue --dns dns_cf -d example.com -d '*.example.com'`.
4. **DNS**: wildcard A-запись `*.example.com → IP сервера`.
5. **nginx stream** (SNI-роутинг :443): поддомены пула → `127.0.0.1:8453`, остальное — обычный HTTPS-стек nginx.
6. **Клиентский токен**: `dt token <мастер-секрет>` — обфусцированная форма для раздачи пользователям.

## Стек

| Слой | Технология |
|------|-----------|
| Язык | Go 1.22+ |
| TLS | crypto/tls, wildcard-сертификат Let's Encrypt (DNS-01 через Cloudflare) |
| Протокол | собственный бинарный фрейминг поверх TLS (мультиплексирование стримов) |
| Роутинг | nginx stream + ssl_preread (SNI) |
| Процесс | systemd (Restart=always) |

## Лицензия

MIT
