package bot

import (
	"fmt"
	"strings"

	"github.com/erkineren/repository-monitor/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Bot struct {
	API *tgbotapi.BotAPI
}

func New(token string) (*Bot, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %v", err)
	}

	return &Bot{
		API: bot,
	}, nil
}

func (b *Bot) SendNotification(chatID int64, notification models.Notification) error {
	message := fmt.Sprintf("%s\n%s", notification.Message, notification.URL)
	msg := tgbotapi.NewMessage(chatID, escapeMarkdown(message))
	msg.ParseMode = tgbotapi.ModeMarkdownV2

	_, err := b.API.Send(msg)
	if err != nil {
		return fmt.Errorf("failed to send message: %v", err)
	}

	return nil
}

func escapeMarkdown(text string) string {
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return replacer.Replace(text)
}
