# GitHub Attention Notifier

A Go application that sends Telegram notifications when you are mentioned or assigned as a reviewer on GitHub pull requests and comments.

## Features

- Notifications for mentions in PRs and comments
- Notifications for PR review requests
- Configurable polling interval
- Docker support

## Setup

### Prerequisites

- Go 1.21 or higher (if running locally)
- Docker (if running containerized)
- GitHub account
- Telegram account

### Configuration

1. **GitHub Token**
   - Go to GitHub Settings > Developer settings > Personal access tokens > Tokens (classic)
   - Generate a new token with the following permissions:
     - `notifications` - to read notifications
     - `repo` - to access repository data
   - Copy the token

2. **Telegram Bot Setup**
   - Message [@BotFather](https://t.me/botfather) on Telegram
   - Use the `/newbot` command to create a new bot
   - Copy the bot token provided
   - Start a chat with your bot
   - Get your chat ID by:
     1. Starting the bot
     2. Sending it a message
     3. Accessing: `https://api.telegram.org/bot<YourBOTToken>/getUpdates`
     4. Look for the `chat.id` in the response

3. **Environment Variables**
   - Copy `.env.example` to `.env`
   - Fill in the values:
     ```
     GITHUB_TOKEN=your_github_personal_access_token
     GITHUB_USERNAME=your_github_username
     TELEGRAM_BOT_TOKEN=your_telegram_bot_token
     TELEGRAM_CHAT_ID=your_telegram_chat_id
     POLL_INTERVAL=60
     ```

## Running the Application

### Local Development

```bash
go mod download
go run main.go
```

### Docker

Build the image:
```bash
docker build -t github-attention-notifier .
```

Run the container:
```bash
docker run -d \
  --name github-notifier \
  --env-file .env \
  github-attention-notifier
```

## License

MIT 