# Calendar — HTTP-сервис календаря событий

HTTP API для управления событиями и напоминаниями. Написан на стандартной библиотеке Go (`net/http`) без сторонних фреймворков, данные хранятся в PostgreSQL.

Фоновые механизмы:

1. **Напоминания.** Отдельная горутина получает события через канал и держит их в min-куче по времени напоминания, поэтому срабатывает ровно в нужный момент.
2. **Архивация.** По тикеру переносит старые события в архив.
3. **Асинхронный логгер.** Пишет логи через буферизованный канал, чтобы запись логов не замедляла ответы API; при переполнении буфера считает потерянные записи.


## Структура проекта

```
calendar/
├── go.mod / go.sum
├── Makefile
├── README.md
├── docker-compose.yml         
├── .env.example               
├── main.go                    
└── internal/
    ├── config/                
    ├── logger/                
    ├── storage/               
    │   └── migrations/        
    ├── calendar/              
    ├── archive/               
    ├── reminder/              
    ├── cleanup/               
    └── server/                
```

## Быстрый старт по шагам

### 0. Зависимости

Нужны установленными: Go ≥ 1.25, Docker (с `docker compose`).

### 1. Подтянуть зависимости Go

```bash
git clone https://github.com/lol-pick/calendar
cd calendar
make tidy
```

### 2. Поднять Postgres

```bash
make db-up       
docker compose ps   
```

Если вдруг порт 5433 занят — поменяй `ports:` в `docker-compose.yml`
и обнови `DATABASE_URL` соответственно.

### 3. Запустить сервис

```bash
# загрузить переменные из шаблона в текущий терминал
set -a; source .env.example; set +a

make run

# или с другими параметрами через флаги:
# ./calendar --addr=:9090 --cleanup-interval=1m --archive-after=10m
```

### 4. Подёргать API curl-ом

```bash
# создать событие
curl -X POST http://localhost:8080/create_event \
  -d 'user_id=1' -d 'date=2025-01-01' -d 'event=New Year party'

# создать событие с напоминанием через 30 секунд
REMIND=$(date -u -v+30S +"%Y-%m-%dT%H:%M:%SZ")   # macOS
# REMIND=$(date -u -d "+30 sec" +"%Y-%m-%dT%H:%M:%SZ")  # Linux
curl -X POST http://localhost:8080/create_event \
  -d 'user_id=1' -d 'date=2025-01-01' -d 'event=meeting' \
  -d "remind_at=$REMIND"

# выборка
curl 'http://localhost:8080/events_for_day?user_id=1&date=2025-01-01'
curl 'http://localhost:8080/events_for_week?user_id=1&date=2025-01-01'
curl 'http://localhost:8080/events_for_month?user_id=1&date=2025-01-01'

# через ~30 секунд напоминание сработает; смотрим:
curl 'http://localhost:8080/reminders?user_id=1'

# обновление и удаление
curl -X POST http://localhost:8080/update_event \
  -d 'id=1' -d 'user_id=1' -d 'date=2025-01-02' -d 'event=moved'
curl -X POST http://localhost:8080/delete_event \
  -d 'id=1' -d 'user_id=1'

# архив (события переедут сюда, как только дата + ARCHIVE_AFTER окажется
# в прошлом и сработает cleanup tick)
curl 'http://localhost:8080/archive?user_id=1'
```

Формат ответа:

```json
{ "result": ... }           // успех (2xx)
{ "error":  "описание" }    // ошибка (4xx/5xx)
```

### 5. Прогнать тесты

```bash
# unit-тесты (logger, cleanup-фейками). Без БД.
make test

# интеграционные тесты (storage, calendar, archive, reminder, server) —
# поднимут Postgres, прогонят миграции, потёргают реальную БД.
make test-integration
```

`test-integration` сам делает `docker compose up -d postgres`, ждёт
`pg_isready`, и запускает `go test` с `DATABASE_URL_TEST` =
`postgres://calendar:calendar@localhost:5433/calendar?sslmode=disable`.


### 6. Остановить и почистить

```bash
# контейнер остаётся, данные сохраняются в томе
make db-down

# выкинуть базу полностью (удалит volume)
make db-reset
```

## API кратко

| метод | путь                         | параметры                                       | назначение                          |
|-------|------------------------------|-------------------------------------------------|-------------------------------------|
| POST  | `/create_event`              | form: user_id, date, event, [remind_at]         | создать                              |
| POST  | `/update_event`              | form: id, user_id, date, event                  | обновить (без remind_at)            |
| POST  | `/delete_event`              | form: id, user_id                               | удалить                              |
| GET   | `/events_for_day`            | query: user_id, date                            | выборка за день                     |
| GET   | `/events_for_week`           | query: user_id, date                            | за 7 дней с date                    |
| GET   | `/events_for_month`          | query: user_id, date                            | за месяц с date                     |
| GET   | `/reminders`                 | query: user_id                                  | сработавшие напоминания             |
| GET   | `/archive`                   | query: user_id                                  | архив старых событий                |

Маппинг ошибок:

| ошибка                   | HTTP |
|--------------------------|------|
| невалидный вход          | 400  |
| событие не найдено       | 503* |
| внутренние (БД, etc.)    | 500  |

\* Код 503 для бизнес-ошибок задан условием исходного задания. В публичном REST API здесь был бы 404.

## Конфигурация

Все настройки лежат в `internal/config/config.go`.

| флаг / env                          | дефолт   | назначение                              |
|-------------------------------------|----------|-----------------------------------------|
| `--database-url` / `DATABASE_URL`   | —        | строка подключения к Postgres (обязат.) |
| `--addr` / `ADDR`                   | `:8080`  | HTTP-адрес                              |
| `--cleanup-interval` / `CLEANUP_INTERVAL` | `10m`    | как часто бежит чистка                  |
| `--archive-after` / `ARCHIVE_AFTER` | `24h`    | возраст «старого» события               |
| `--log-buffer` / `LOG_BUFFER`       | `256`    | буфер канала логгера                    |
| `--reminder-buffer` / `REMINDER_BUFFER` | `128`    | буфер канала воркера напоминаний        |
| `--shutdown-timeout` / `SHUTDOWN_TIMEOUT` | `5s`     | таймаут graceful shutdown               |
