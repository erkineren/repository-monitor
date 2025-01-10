# GitHub Repository Monitor

A Telegram bot that monitors your GitHub repositories and sends notifications about:
- Pull Requests
- Issues
- Releases

## Project Structure

```
repository-monitor/
├── cmd/
│   └── monitor/
│       └── main.go           # Application entry point
├── internal/
│   ├── bot/
│   │   ├── handler.go        # Telegram bot command handlers
│   │   └── telegram.go       # Telegram bot implementation
│   ├── github/
│   │   ├── client.go         # GitHub client
│   │   └── notifications.go  # GitHub notifications logic
│   ├── models/
│   │   ├── account.go        # GitHub account model
│   │   ├── notification.go   # Notification models
│   │   └── user.go          # User model
│   ├── store/
│   │   ├── postgres/
│   │   │   └── store.go     # PostgreSQL implementation
│   │   └── store.go         # Store interface
│   └── config/
│       └── config.go        # Configuration management
├── .env.example             # Example environment variables
├── docker-compose.yml       # Docker Compose configuration
├── Dockerfile              # Docker build configuration
└── README.md              # This file

```

## Features

- Monitor multiple GitHub accounts
- Receive notifications for:
  - New or updated Pull Requests
  - New or updated Issues
  - New Releases
- Toggle notifications per GitHub account
- Configurable notification intervals
- Persistent storage using PostgreSQL

## Configuration

Copy `.env.example` to `.env` and configure the following variables:

- `TELEGRAM_TOKEN`: Your Telegram bot token (get it from [@BotFather](https://t.me/botfather))
- `POSTGRES_URL`: PostgreSQL connection URL
- `RENOTIFY_INTERVAL`: Hours to wait before re-notifying about the same item (default: 24)
- `NOTIFY_INTERVAL`: Minutes between GitHub checks (default: 5)
- `POLLING_TIMEOUT`: Seconds for Telegram long polling timeout (default: 60)
- `DEBUG`: Enable debug logging (default: false)

## Running with Docker

1. Configure environment variables in `.env`
2. Run with Docker Compose:
   ```bash
   docker-compose up -d
   ```

## Running Locally

1. Install dependencies:
   ```bash
   go mod download
   ```

2. Configure environment variables in `.env`

3. Run the application:
   ```bash
   go run cmd/monitor/main.go
   ```

## Bot Commands

- `/start` - Show welcome message and available commands
- `/add <username> <token>` - Add a GitHub account to monitor
- `/remove <username>` - Remove a GitHub account
- `/toggle <username>` - Toggle notifications for a GitHub account
- `/list` - List monitored GitHub accounts
- `/help` - Show help message

## Development

The project follows standard Go project layout and best practices:
- Code is organized into packages by functionality
- Dependencies are managed with Go modules
- Configuration is handled through environment variables
- Business logic is separated from infrastructure concerns
- Interfaces are used for dependency injection and testing

## License

This project is licensed under the MIT License - see the LICENSE file for details. 