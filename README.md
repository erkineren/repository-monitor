# GitHub Repository Monitor

[![Go Report Card](https://goreportcard.com/badge/github.com/erkineren/repository-monitor)](https://goreportcard.com/report/github.com/erkineren/repository-monitor)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/github/go-mod/go-version/erkineren/repository-monitor)](https://go.dev/)

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

## Installation

### Prerequisites

- Go 1.19 or higher
- PostgreSQL database
- Telegram bot token (get it from [@BotFather](https://t.me/botfather))

### Configuration

Copy `.env.example` to `.env` and configure the following variables:

- `TELEGRAM_TOKEN`: Your Telegram bot token
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

1. Clone the repository:

   ```bash
   git clone https://github.com/erkineren/repository-monitor.git
   cd repository-monitor
   ```

2. Install dependencies:

   ```bash
   go mod download
   ```

3. Configure environment variables in `.env`

4. Run the application:
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

## Contributing

Contributions are welcome! Here's how you can contribute:

1. Fork the repository
2. Create a new branch (`git checkout -b feature/amazing-feature`)
3. Make your changes
4. Run tests to ensure everything works
5. Commit your changes (`git commit -m 'Add some amazing feature'`)
6. Push to the branch (`git push origin feature/amazing-feature`)
7. Open a Pull Request

Please make sure to update tests as appropriate and adhere to the existing coding style.

## Security

If you discover a security vulnerability within this project, please send an email to erkineren@gmail.com. All security vulnerabilities will be promptly addressed.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
