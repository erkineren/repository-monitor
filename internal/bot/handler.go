package bot

import (
	"fmt"
	"strings"

	"github.com/erkineren/repository-monitor/internal/store"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Handler struct {
	Bot   *Bot
	store store.Store
}

func NewHandler(bot *Bot, store store.Store) *Handler {
	return &Handler{
		Bot:   bot,
		store: store,
	}
}

func (h *Handler) HandleUpdate(update tgbotapi.Update) error {
	if update.Message == nil || !update.Message.IsCommand() {
		return nil
	}

	var err error
	switch update.Message.Command() {
	case "start":
		err = h.handleStart(update.Message)
	case "add":
		err = h.handleAdd(update.Message)
	case "remove":
		err = h.handleRemove(update.Message)
	case "toggle":
		err = h.handleToggle(update.Message)
	case "list":
		err = h.handleList(update.Message)
	case "help":
		err = h.handleHelp(update.Message)
	default:
		err = h.handleUnknown(update.Message)
	}

	if err != nil {
		reply := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Error: %v", err))
		_, _ = h.Bot.API.Send(reply)
	}

	return err
}

func (h *Handler) handleStart(message *tgbotapi.Message) error {
	text := `Welcome to GitHub Repository Monitor!
	
Available commands:
/add <username> <token> - Add a GitHub account to monitor
/remove <username> - Remove a GitHub account
/toggle <username> - Toggle notifications for a GitHub account
/list - List monitored GitHub accounts
/help - Show this help message`

	reply := tgbotapi.NewMessage(message.Chat.ID, text)
	_, err := h.Bot.API.Send(reply)
	return err
}

func (h *Handler) handleAdd(message *tgbotapi.Message) error {
	args := strings.Fields(message.CommandArguments())
	if len(args) != 2 {
		return fmt.Errorf("usage: /add <username> <token>")
	}

	username, token := args[0], args[1]
	err := h.store.AddGitHubAccount(message.Chat.ID, token, username)
	if err != nil {
		return err
	}

	reply := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Successfully added GitHub account: %s", username))
	_, err = h.Bot.API.Send(reply)
	return err
}

func (h *Handler) handleRemove(message *tgbotapi.Message) error {
	username := strings.TrimSpace(message.CommandArguments())
	if username == "" {
		return fmt.Errorf("usage: /remove <username>")
	}

	err := h.store.RemoveGitHubAccount(message.Chat.ID, username)
	if err != nil {
		return err
	}

	reply := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Successfully removed GitHub account: %s", username))
	_, err = h.Bot.API.Send(reply)
	return err
}

func (h *Handler) handleToggle(message *tgbotapi.Message) error {
	username := strings.TrimSpace(message.CommandArguments())
	if username == "" {
		return fmt.Errorf("usage: /toggle <username>")
	}

	err := h.store.ToggleGitHubAccount(message.Chat.ID, username)
	if err != nil {
		return err
	}

	reply := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Toggled notifications for GitHub account: %s", username))
	_, err = h.Bot.API.Send(reply)
	return err
}

func (h *Handler) handleList(message *tgbotapi.Message) error {
	user, exists := h.store.GetUser(message.Chat.ID)
	if !exists || len(user.Accounts) == 0 {
		reply := tgbotapi.NewMessage(message.Chat.ID, "No GitHub accounts configured.")
		_, err := h.Bot.API.Send(reply)
		return err
	}

	var text strings.Builder
	text.WriteString("Monitored GitHub accounts:\n\n")
	for username, account := range user.Accounts {
		status := "🟢 Active"
		if !account.IsActive {
			status = "🔴 Inactive"
		}
		text.WriteString(fmt.Sprintf("%s: %s\n", username, status))
	}

	reply := tgbotapi.NewMessage(message.Chat.ID, text.String())
	_, err := h.Bot.API.Send(reply)
	return err
}

func (h *Handler) handleHelp(message *tgbotapi.Message) error {
	return h.handleStart(message)
}

func (h *Handler) handleUnknown(message *tgbotapi.Message) error {
	reply := tgbotapi.NewMessage(message.Chat.ID, "Unknown command. Use /help to see available commands.")
	_, err := h.Bot.API.Send(reply)
	return err
}
