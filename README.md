[![ci](https://github.com/h0rn3t/postgres-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/h0rn3t/postgres-mcp/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/h0rn3t/postgres-mcp)](https://goreportcard.com/report/github.com/h0rn3t/postgres-mcp)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

# postgres-mcp — сервер PostgreSQL Model Context Protocol

postgres-mcp підключає AI-асистентів до **будь-якої бази даних PostgreSQL** через запити природною мовою. Ставте запитання звичайною мовою та отримуйте структуровані SQL-результати з автоматичним стрімінгом і надійною обробкою помилок.

**Працює з**: Cursor, Claude Desktop, розширеннями VS Code та будь-яким [MCP-сумісним клієнтом](https://modelcontextprotocol.io/)

## Швидкий старт

postgres-mcp підключається до **вашої наявної бази даних PostgreSQL** і робить її доступною для AI-асистентів через запити природною мовою.

### Передумови
- База даних PostgreSQL (наявна база з вашою схемою)
- Ключ OpenAI API (необов'язково, для AI-генерації SQL)

### Базове використання

```bash
# Налаштування змінних середовища
export DATABASE_URL="postgres://user:password@localhost:5432/your-existing-db"
export OPENAI_API_KEY="your-api-key"  # Необов'язково

# Запуск сервера (з використанням готового бінарного файлу)
./postgres-mcp-server

# Тестування з клієнтом в іншому терміналі
./postgres-mcp-client -ask "What tables do I have?" -format table
./postgres-mcp-client -ask "Who is the customer that has placed the most orders?" -format table
./postgres-mcp-client -search "john" -format table
```

Як це працює:

```
👤 Користувач / AI-асистент
         │
         │ "Who are the top customers?"
         ▼
┌─────────────────────────────────────────────────────────────┐
│                  Будь-який MCP-клієнт                       │
│                                                             │
│  postgres-mcp CLI  │  Cursor  │  Claude Desktop  │  VS Code  │ ... │
│  JSON/CSV   │  Чат     │  AI-асистент     │  Редактор │     │
└─────────────────────────────────────────────────────────────┘
         │
         │ Streamable HTTP / MCP-протокол
         ▼
┌─────────────────────────────────────────────────────────────┐
│                 Сервер postgres-mcp                         │
│                                                             │
│  🔒 Безпека     🧠 AI-рушій       🌊 Стрімінг               │
│  • Валідація вводу • Кеш схеми    • Автопагінація           │
│  • Аудит-лог    • OpenAI API      • Керування пам'яттю      │
│  • SQL-захист   • Відновлення     • Пул з'єднань            │
│                 після помилок                               │
└─────────────────────────────────────────────────────────────┘
         │
         │ SQL-запити лише для читання
         ▼
┌─────────────────────────────────────────────────────────────┐
│                Ваша база даних PostgreSQL                   │
│                                                             │
│  Будь-яка схема: e-commerce, аналітика, CRM тощо.           │
│  Таблиці • Представлення • Індекси • Функції                │
└─────────────────────────────────────────────────────────────┘

Зовнішні AI-сервіси:
OpenAI API • Anthropic • Локальні LLM (Ollama тощо)

Ключові переваги:
✅ Працює з БУДЬ-ЯКОЮ базою PostgreSQL (без припущень щодо схеми)
✅ Не потребує змін схеми
✅ Доступ лише для читання (100% безпечно)
✅ Автоматичний стрімінг великих результатів
✅ Розумне розуміння запитів (однина/множина)
✅ Надійна обробка помилок (коректне відновлення після збоїв AI)
✅ Підтримка чутливості PostgreSQL до регістру (таблиці зі змішаним регістром)
✅ Готовність до продакшену: безпека та продуктивність
✅ Універсальна сумісність із базами даних
✅ Кілька форматів виводу (table, JSON, CSV)
✅ Повнотекстовий пошук за всіма стовпцями
✅ Підтримка автентифікації
✅ Комплексний набір тестів
```

## Можливості

- **Природна мова в SQL**: ставте запитання звичайною мовою
- **Автоматичний стрімінг**: автоматично обробляє великі набори результатів
- **Безпечний доступ лише для читання**: запобігає будь-яким операціям запису
- **Текстовий пошук**: пошук за всіма текстовими стовпцями
- **Кілька форматів виводу**: таблиця, JSON і CSV
- **Чутливість PostgreSQL до регістру**: коректно обробляє імена таблиць зі змішаним регістром
- **Універсальна сумісність**: працює з будь-якою базою даних PostgreSQL

### Змінні середовища

**Підключення до БД (один із двох варіантів):**
- Варіант A — `DATABASE_URL`: рядок підключення PostgreSQL до вашої наявної бази даних
- Варіант B — окремі змінні в стилі mssql-mcp (з них автоматично збирається `DATABASE_URL`):
  - `POSTGRES_HOST` (`PGHOST` як fallback) — хост, напр. `localhost`
  - `POSTGRES_PORT` (`PGPORT`, за замовчуванням `5432`)
  - `POSTGRES_DATABASE` (`POSTGRES_DB` / `PGDATABASE`) — ім'я бази
  - `POSTGRES_USER` (`POSTGRES_USERNAME` / `PGUSER`) — користувач
  - `POSTGRES_PASSWORD` (`PGPASSWORD`, може бути порожнім для trust-автентифікації)
  - `POSTGRES_SSLMODE` (`PGSSLMODE`, за замовчуванням `disable`) — `disable`, `require`, `verify-full` тощо.
  - `POSTGRES_URL` — аліас для `DATABASE_URL` (якщо зручніше)

**Необов'язкові:**
- `OPENAI_API_KEY`: ключ OpenAI API для AI-генерації SQL
- `OPENAI_MODEL`: модель для використання (за замовчуванням: "gpt-4o-mini")
- `MCP_TRANSPORT`: `stdio` або `http` (за замовчуванням: `http`; прапорець `--transport stdio` має пріоритет)
- `HTTP_ADDR`: адреса сервера в http-режимі (за замовчуванням: ":8080")
- `HTTP_PATH`: шлях MCP-ендпоінта в http-режимі (за замовчуванням: "/mcp")
- `AUTH_BEARER`: bearer-токен для автентифікації (тільки http-режим)

## Встановлення

### Завантаження готових бінарних файлів

1. Перейдіть до [GitHub Releases](https://github.com/h0rn3t/postgres-mcp/releases)
2. Завантажте бінарний файл для вашої платформи (Linux, macOS, Windows)
3. Розпакуйте та запустіть:

```bash
# Приклад для macOS/Linux
tar xzf postgres-mcp_*.tar.gz
cd postgres-mcp_*
./postgres-mcp-server
```

### Альтернативні варіанти

```bash
# Homebrew (macOS/Linux) — доступно після першого релізу
brew tap h0rn3t/homebrew-tap
brew install postgres-mcp

# Збірка з вихідного коду
go build -o postgres-mcp-server ./server
go build -o postgres-mcp-client ./client
```

Додайте `-ldflags="-s -w -extldflags=-static" -trimpath`, якщо хочете отримати очищені виконувані файли (без налагоджувальної інформації):
```console
go build -ldflags="-s -w -extldflags=-static" -trimpath -o postgres-mcp-server ./server
go build -ldflags="-s -w -extldflags=-static" -trimpath -o postgres-mcp-client ./client
```

### Docker/Kubernetes

```bash
# Docker
docker run -e DATABASE_URL="postgres://user:pass@host:5432/db" \
  -p 8080:8080 ghcr.io/h0rn3t/postgres-mcp:latest

# Kubernetes (повні маніфести див. у каталозі examples/)
kubectl create secret generic postgres-mcp-secret \
  --from-literal=database-url="postgres://user:pass@host:5432/db"
kubectl apply -f examples/k8s/
```

#### Швидкий старт

```bash
# Налаштування бази даних (необов'язково — працює з будь-якою наявною базою PostgreSQL)
export DATABASE_URL="postgres://user:password@localhost:5432/mydb"
psql $DATABASE_URL < schema.sql

# Запуск сервера
export OPENAI_API_KEY="your-api-key"
./postgres-mcp-server

# Тестування з клієнтом
./postgres-mcp-client -ask "Who is the user that places the most orders?" -format table
./postgres-mcp-client -ask "Show me the top 40 most reviewed items in the marketplace" -format table
```

### Змінні середовища

**Підключення до БД (один із двох варіантів):**
- Варіант A — `DATABASE_URL`: рядок підключення PostgreSQL
- Варіант B — окремі змінні в стилі mssql-mcp: `POSTGRES_HOST`, `POSTGRES_PORT` (за замовчуванням `5432`), `POSTGRES_DATABASE`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_SSLMODE` (за замовчуванням `disable`)

**Необов'язкові:**
- `OPENAI_API_KEY`: ключ OpenAI API для генерації SQL
- `OPENAI_MODEL`: модель для використання (за замовчуванням: "gpt-4o-mini")
- `MCP_TRANSPORT`: `stdio` або `http` (за замовчуванням: `http`)
- `HTTP_ADDR`: адреса сервера в http-режимі (за замовчуванням: ":8080")
- `HTTP_PATH`: шлях MCP-ендпоінта в http-режимі (за замовчуванням: "/mcp")
- `AUTH_BEARER`: bearer-токен для автентифікації (тільки http-режим)

## Конфігурація в стилі mssql-mcp (stdio)

Якщо ви звикли до такого формату mssql-mcp:

```json
{
  "mcpServers": {
    "mssql": {
      "command": "/path/to/bin/mssql-mcp",
      "env": {
        "MSSQL_SERVER": "localhost",
        "MSSQL_DATABASE": "YourDatabase",
        "MSSQL_USERNAME": "sa",
        "MSSQL_PASSWORD": "YourPassword",
        "MSSQL_ENCRYPT": "true",
        "MSSQL_TRUST_SERVER_CERTIFICATE": "true",
        "MSSQL_ACCESS_LEVEL": "READONLY"
      }
    }
  }
}
```

то для postgres-mcp еквівалент виглядає так (stdio-режим, окремий процес на клієнта, без HTTP-порта):

```json
{
  "mcpServers": {
    "postgres": {
      "command": "/path/to/bin/postgres-mcp-server",
      "args": ["--transport", "stdio"],
      "env": {
        "POSTGRES_HOST": "localhost",
        "POSTGRES_PORT": "5432",
        "POSTGRES_DATABASE": "YourDatabase",
        "POSTGRES_USER": "postgres",
        "POSTGRES_PASSWORD": "YourPassword",
        "POSTGRES_SSLMODE": "disable",
        "OPENAI_API_KEY": "your-api-key"
      }
    }
  }
}
```

Еквівалент одним рядком підключення (теж працює в stdio-режимі):

```json
{
  "mcpServers": {
    "postgres": {
      "command": "/path/to/bin/postgres-mcp-server",
      "args": ["--transport", "stdio"],
      "env": {
        "DATABASE_URL": "postgres://postgres:YourPassword@localhost:5432/YourDatabase?sslmode=disable",
        "OPENAI_API_KEY": "your-api-key"
      }
    }
  }
}
```

> Примітки:
> - `POSTGRES_SSLMODE`: `disable` для локального Docker/Postgres без TLS, `require` для хмарних БД (Neon, Supabase, RDS — зазвичай `require`).
> - Замість прапорця `--transport stdio` можна задати `"MCP_TRANSPORT": "stdio"` в `env`.
> - Доступ завжди лише для читання (аналог `MSSQL_ACCESS_LEVEL: READONLY`): сервер відхиляє INSERT/UPDATE/DELETE/DDL і виконує запити в read-only транзакціях.

### VS Code

Файл `.vscode/mcp.json` (або глобальний `mcp.json`):

```json
{
  "servers": {
    "postgres": {
      "command": "/path/to/bin/postgres-mcp-server",
      "args": ["--transport", "stdio"],
      "env": {
        "POSTGRES_HOST": "localhost",
        "POSTGRES_PORT": "5432",
        "POSTGRES_DATABASE": "YourDatabase",
        "POSTGRES_USER": "postgres",
        "POSTGRES_PASSWORD": "YourPassword",
        "POSTGRES_SSLMODE": "disable"
      }
    }
  }
}
```

### Claude Desktop

Файл `~/.config/claude-desktop/claude_desktop_config.json` (macOS/Linux) або `%APPDATA%\Claude\claude_desktop_config.json` (Windows):

```json
{
  "mcpServers": {
    "postgres": {
      "command": "/path/to/bin/postgres-mcp-server",
      "args": ["--transport", "stdio"],
      "env": {
        "DATABASE_URL": "postgres://postgres:YourPassword@localhost:5432/YourDatabase?sslmode=disable",
        "OPENAI_API_KEY": "your-api-key"
      }
    }
  }
}
```

### Хмарна БД (Neon / Supabase / RDS)

```json
{
  "mcpServers": {
    "postgres": {
      "command": "/path/to/bin/postgres-mcp-server",
      "args": ["--transport", "stdio"],
      "env": {
        "POSTGRES_HOST": "ep-xxx.neon.tech",
        "POSTGRES_PORT": "5432",
        "POSTGRES_DATABASE": "YourDatabase",
        "POSTGRES_USER": "neondb_owner",
        "POSTGRES_PASSWORD": "YourPassword",
        "POSTGRES_SSLMODE": "require",
        "OPENAI_API_KEY": "your-api-key"
      }
    }
  }
}
```

### Локальний Postgres без пароля (trust auth)

```json
{
  "mcpServers": {
    "postgres": {
      "command": "/path/to/bin/postgres-mcp-server",
      "args": ["--transport", "stdio"],
      "env": {
        "POSTGRES_HOST": "localhost",
        "POSTGRES_DATABASE": "YourDatabase",
        "POSTGRES_USER": "postgres",
        "POSTGRES_SSLMODE": "disable"
      }
    }
  }
}
```

### Docker в stdio-режимі

```json
{
  "mcpServers": {
    "postgres": {
      "command": "docker",
      "args": ["run", "-i", "--rm", "ghcr.io/h0rn3t/postgres-mcp:latest", "--transport", "stdio"],
      "env": {
        "DATABASE_URL": "postgres://postgres:YourPassword@host.docker.internal:5432/YourDatabase?sslmode=disable"
      }
    }
  }
}
```

### Перевірка конфігурації в терміналі

```bash
# 1. Збірка
go build -o postgres-mcp-server ./server

# 2. Без змінних — має сказати що саме відсутнє
./postgres-mcp-server --transport stdio

# 3. З POSTGRES_* — має піти далі (помилка підключення, а не конфігурації)
POSTGRES_HOST=localhost POSTGRES_PORT=5432 POSTGRES_DATABASE=YourDatabase \
POSTGRES_USER=postgres POSTGRES_PASSWORD=YourPassword \
./postgres-mcp-server --transport stdio
```

## Приклади використання

```bash
# Запитання природною мовою
./postgres-mcp-client -ask "What are the top 5 customers?" -format table
./postgres-mcp-client -ask "How many orders were placed today?" -format json

# Пошук за всіма текстовими полями
./postgres-mcp-client -search "john" -format table

# Кілька запитань одночасно
./postgres-mcp-client -ask "Show tables" -ask "Count users" -format table

# Різні формати виводу
./postgres-mcp-client -ask "Export all data" -format csv -max-rows 1000
```

## Приклад бази даних

Проєкт містить дві схеми:
- **`schema.sql`**: повноцінний маркетплейс на кшталт Amazon із 5000+ записами
- **`schema_minimal.sql`**: мінімальна тестова схема з таблицею `"Categories"` зі змішаним регістром

**Ключові особливості:**
- **Імена таблиць зі змішаним регістром** (`"Categories"`) для тестування чутливості до регістру
- **Складені первинні ключі** (`order_items`) для тестування припущень AI
- **Реалістичні зв'язки** та типи даних

Використання власної бази даних:
```bash
export DATABASE_URL="postgres://user:pass@host:5432/your_db"
./postgres-mcp-server
./postgres-mcp-client -ask "What tables do I have?"
```

## Обробка помилок AI

Коли AI генерує некоректний SQL, postgres-mcp коректно це обробляє:

```json
{
  "error": "Column not found in generated query",
  "suggestion": "Try rephrasing your question or ask about specific tables",
  "original_sql": "SELECT non_existent_column FROM table..."
}
```

Замість аварійного завершення система надає корисний зворотний зв'язок і продовжує працювати.

## Інтеграція з MCP

### Інтеграція з Cursor

```bash
# Запуск сервера
export DATABASE_URL="postgres://user:pass@localhost:5432/your_db"
./postgres-mcp-server
```

Додайте до налаштувань Cursor:
```json
{
  "mcp.servers": {
    "postgres-mcp": {
      "transport": {
        "type": "http",
        "url": "http://localhost:8080/mcp"
      }
    }
  }
}
```

### Інтеграція з Claude Desktop

Відредагуйте `~/.config/claude-desktop/claude_desktop_config.json`:
```json
{
  "mcpServers": {
    "postgres-mcp": {
      "transport": {
        "type": "http",
        "url": "http://localhost:8080/mcp"
      }
    }
  }
}
```

## Інструменти API

- **`ask`**: запитання природною мовою → SQL-запити з автоматичним стрімінгом
- **`search`**: повнотекстовий пошук за всіма текстовими стовпцями бази даних
- **`stream`**: розширений стрімінг для дуже великих наборів результатів із пагінацією

## Функції безпеки

- **Примусовий режим лише для читання**: блокує операції запису (INSERT, UPDATE, DELETE тощо)
- **Таймаути запитів**: запобігають довготривалим запитам
- **Валідація вводу**: очищує та перевіряє весь ввід користувача
- **Ізоляція транзакцій**: усі запити виконуються в транзакціях лише для читання

## Тестування

```bash
# Юніт-тести
go test ./server -v

# Інтеграційні тести (потрібен PostgreSQL)
go test ./server -tags=integration -v
```

## Ліцензія

Apache 2.0 — деталі див. у файлі LICENSE.

## Пов'язані проєкти

- [Model Context Protocol](https://modelcontextprotocol.io/) — специфікація базового протоколу
- [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) — Go-реалізація MCP

---

postgres-mcp робить вашу базу даних PostgreSQL доступною для AI-асистентів через природну мову, зберігаючи безпеку завдяки контролю доступу лише для читання.
