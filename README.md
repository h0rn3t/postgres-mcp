# postgres-mcp

MCP-сервер і CLI для прямих запитів до PostgreSQL та перегляду схеми. SQL передає MCP-клієнт; сервер не викликає LLM і не надсилає схему бази зовнішньому сервісу.

## Збірка

```sh
go build -o postgres-mcp-server ./server
go build -o postgres-mcp-client ./client
```

## Налаштування сервера

Задайте `DATABASE_URL` або окремі параметри підключення:

```sh
export DATABASE_URL='postgres://user:password@localhost:5432/mydb?sslmode=disable'
./postgres-mcp-server
```

Підтримуються змінні `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_DATABASE`, `POSTGRES_USER`, `POSTGRES_PASSWORD` і `POSTGRES_SSLMODE`. Також можна використовувати відповідні `PG_HOST`, `PG_PORT`, `PG_DATABASE`, `PG_USER`, `PG_PASSWORD` і `PG_SSLMODE`. Якщо їх не задано, підтримуються імена libpq без підкреслення: `PGHOST`, `PGPORT` тощо.

За замовчуванням сервер слухає Streamable HTTP на `:8080/mcp`. Щоб запустити його через stdio, задайте `MCP_TRANSPORT=stdio` або передайте `--transport stdio`. HTTP налаштовується змінними `HTTP_ADDR`, `HTTP_PATH` і, за потреби, `AUTH_BEARER`.

| Змінна | За замовчуванням | Призначення |
| --- | --- | --- |
| `MAX_ROWS` | `50` | Максимум рядків для `query` |
| `QUERY_TIMEOUT` | `25s` | Таймаут запиту |
| `PG_ALLOW_WRITE` | `false` | Дозволяє `execute` надсилати SQL у PostgreSQL, якщо значення `true` |
| `PG_ENABLE_RUNTIME_CONNECT` | `false` | Реєструє інструмент `connect_db`, якщо значення `true` |

## Інструменти MCP

- `query`: виконує один запит лише для читання `SELECT`, `WITH`, `EXPLAIN` або `SHOW`. Для значень використовуйте `$1`, `$2` тощо та масив `params`. Кількість результатів обмежує `MAX_ROWS`: база зупиняється на ліміті й не читає решту рядків. Відповідь колонкова — `columns`, `rows` (масиви значень у порядку `columns`) і `truncated: true`, якщо рядків більше за ліміт. `uuid` і `interval` повертаються рядками, `bytea` — hex, значення довші за 500 байт обрізаються.
- `list_schemas`: перелічує користувацькі схеми.
- `list_tables`: перелічує таблиці й представлення у схемі; за замовчуванням `public`.
- `describe_table`: повертає стовпці, типи, допустимість `NULL`, значення за замовчуванням і ознаки первинного ключа; за замовчуванням схема `public`.
- `execute`: виконує один SQL-запит; кілька інструкцій в одному запиті відхиляються. Інструмент з'являється в списку лише з `PG_ALLOW_WRITE=true`.
- `search`: шукає текст у текстових стовпцях бази.
- `connect_db`: перемикає активну базу сервера на підключення з аргументів інструмента. Реєструється лише з `PG_ENABLE_RUNTIME_CONNECT=true`.

`query` завжди працює в транзакції PostgreSQL лише для читання. `PG_ALLOW_WRITE` керує інструментом `execute` і не змінює режим `query`. Надавайте серверу роль PostgreSQL лише з потрібними правами.

Приклад виклику MCP:

```json
{
  "name": "query",
  "arguments": {
    "sql": "SELECT email FROM users WHERE id = $1",
    "params": [42],
    "max_rows": 50
  }
}
```

## Підключення до MCP-клієнтів

Для локальної роботи зручніше stdio: клієнт сам запускає сервер окремим процесом, і HTTP-порт не потрібен. У прикладах нижче замініть `/path/to/postgres-mcp-server` на абсолютний шлях до зібраного бінарника. Замість `DATABASE_URL` у `env` можна передати `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_DATABASE`, `POSTGRES_USER`, `POSTGRES_PASSWORD` і `POSTGRES_SSLMODE`, а прапорець `--transport stdio` можна замінити змінною `MCP_TRANSPORT=stdio`.

Щоб підключитися до вже запущеного HTTP-сервера (наприклад, у Docker чи Kubernetes), використовуйте URL `http://host:8080/mcp`. Заголовок `Authorization: Bearer ...` потрібен, лише якщо сервер запущено з `AUTH_BEARER`.

Не комітьте паролі в конфігураційні файли проєкту: там, де клієнт це підтримує, підставляйте їх зі змінних середовища.

### Claude Code

Додайте сервер командою (`--scope project` записує його в `.mcp.json` у корені проєкту, `--scope user` — для всіх проєктів):

```sh
claude mcp add postgres --scope project \
  --env DATABASE_URL='postgres://user:password@localhost:5432/mydb?sslmode=disable' \
  -- /path/to/postgres-mcp-server --transport stdio
```

Або напишіть `.mcp.json` вручну; `${DATABASE_URL}` Claude Code підставить із середовища, тож пароль не потрапить у репозиторій:

```json
{
  "mcpServers": {
    "postgres": {
      "command": "/path/to/postgres-mcp-server",
      "args": ["--transport", "stdio"],
      "env": {
        "DATABASE_URL": "${DATABASE_URL}"
      }
    }
  }
}
```

HTTP-сервер:

```sh
claude mcp add --transport http postgres http://127.0.0.1:8080/mcp \
  --header "Authorization: Bearer your-token"
```

Перевірити підключення можна командою `claude mcp list` або `/mcp` у сесії.

### Codex

Командою:

```sh
codex mcp add postgres \
  --env DATABASE_URL='postgres://user:password@localhost:5432/mydb?sslmode=disable' \
  -- /path/to/postgres-mcp-server --transport stdio
```

Або у файлі `~/.codex/config.toml`:

```toml
[mcp_servers.postgres]
command = "/path/to/postgres-mcp-server"
args = ["--transport", "stdio"]

[mcp_servers.postgres.env]
DATABASE_URL = "postgres://user:password@localhost:5432/mydb?sslmode=disable"
```

HTTP-сервер; токен Codex читає зі змінної середовища, назву якої задає `bearer_token_env_var`:

```toml
[mcp_servers.postgres]
url = "http://127.0.0.1:8080/mcp"
bearer_token_env_var = "POSTGRES_MCP_TOKEN"
```

### Cursor

Файл `.cursor/mcp.json` у проєкті або `~/.cursor/mcp.json` для всіх проєктів; `${env:DATABASE_URL}` Cursor підставить із середовища:

```json
{
  "mcpServers": {
    "postgres": {
      "command": "/path/to/postgres-mcp-server",
      "args": ["--transport", "stdio"],
      "env": {
        "DATABASE_URL": "${env:DATABASE_URL}"
      }
    }
  }
}
```

HTTP-сервер:

```json
{
  "mcpServers": {
    "postgres": {
      "url": "http://127.0.0.1:8080/mcp",
      "headers": {
        "Authorization": "Bearer your-token"
      }
    }
  }
}
```

### GitHub Copilot (VS Code)

Файл `.vscode/mcp.json` у проєкті (або команда **MCP: Open User Configuration** для глобальних налаштувань). Інструменти доступні в режимі Agent чату Copilot. Блок `inputs` запитує пароль при першому запуску й зберігає його в захищеному сховищі VS Code, а не у файлі:

```json
{
  "inputs": [
    {
      "type": "promptString",
      "id": "pg-password",
      "description": "PostgreSQL password",
      "password": true
    }
  ],
  "servers": {
    "postgres": {
      "type": "stdio",
      "command": "/path/to/postgres-mcp-server",
      "args": ["--transport", "stdio"],
      "env": {
        "POSTGRES_HOST": "localhost",
        "POSTGRES_PORT": "5432",
        "POSTGRES_DATABASE": "mydb",
        "POSTGRES_USER": "postgres",
        "POSTGRES_PASSWORD": "${input:pg-password}",
        "POSTGRES_SSLMODE": "disable"
      }
    }
  }
}
```

HTTP-сервер:

```json
{
  "servers": {
    "postgres": {
      "type": "http",
      "url": "http://127.0.0.1:8080/mcp",
      "headers": {
        "Authorization": "Bearer your-token"
      }
    }
  }
}
```

### Примітки

- `POSTGRES_SSLMODE` (або `sslmode` у `DATABASE_URL`): `disable` для локального Postgres без TLS, `require` для хмарних баз (RDS, Neon, Supabase).
- За замовчуванням доступ лише для читання: `execute` не з'являється серед інструментів. Щоб дозволити запис, додайте в `env` `"PG_ALLOW_WRITE": "true"` і дайте ролі PostgreSQL лише потрібні права.
- Логи сервер пише в stderr, тож вони не заважають протоколу в stdio-режимі. Детальніші логи — `"LOG_LEVEL": "debug"`.

## CLI

За замовчуванням клієнт підключається до `http://127.0.0.1:8080/mcp`. Параметри `-url` і `-bearer` задають інший endpoint і bearer-токен.

```sh
./postgres-mcp-client -query 'SELECT id, email FROM users ORDER BY id LIMIT 20' -format table
./postgres-mcp-client -query 'SELECT * FROM orders WHERE customer_id = 42' -format json
./postgres-mcp-client -search 'john' -format csv
```

Прапорець `-query` можна повторювати. Формати виводу: `table`, `json` і `csv`; параметр `-max-rows` задає ліміт, який клієнт запитує в сервера.

## Docker і Kubernetes

Зберіть образ командою `docker build -t postgres-mcp:local .` і запустіть із URL бази:

```sh
docker run -e DATABASE_URL='postgres://user:password@host:5432/mydb' -p 8080:8080 postgres-mcp:local
```

Маніфести Kubernetes розташовані в [`examples/k8s`](examples/k8s). Передайте `DATABASE_URL` через Kubernetes Secret. Ключ моделі не потрібен.

## Ліцензія

Apache 2.0. Деталі — у файлі [LICENSE](LICENSE).
