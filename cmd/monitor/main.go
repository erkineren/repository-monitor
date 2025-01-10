package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"sync"
	"syscall"
	"time"

	"github.com/erkineren/repository-monitor/internal/bot"
	"github.com/erkineren/repository-monitor/internal/config"
	"github.com/erkineren/repository-monitor/internal/github"
	"github.com/erkineren/repository-monitor/internal/store/postgres"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	log.Println("Starting GitHub Repository Monitor...")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Printf("Configuration loaded successfully. Poll interval: %d seconds, Renotify interval: %d seconds", cfg.PollInterval, cfg.RenotifyInterval)

	// Initialize store
	log.Printf("Connecting to database: %s", maskDatabaseURL(cfg.DatabaseURL))
	store, err := postgres.New(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}
	log.Println("Database connection established successfully")
	defer store.Close()

	// Initialize Telegram bot
	log.Println("Initializing Telegram bot...")
	telegramBot, err := bot.New(cfg.TelegramBotToken)
	if err != nil {
		log.Fatalf("Failed to initialize Telegram bot: %v", err)
	}
	log.Println("Telegram bot initialized successfully")

	// Initialize bot handler
	handler := bot.NewHandler(telegramBot, store)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle system signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("Received signal %v, initiating shutdown...", sig)
		cancel()
	}()

	// Start workers
	var wg sync.WaitGroup

	// Start notification worker
	log.Println("Starting notification worker...")
	wg.Add(1)
	go func() {
		defer wg.Done()
		notificationWorker(ctx, store, cfg)
	}()

	// Start bot update worker
	log.Println("Starting bot update worker...")
	wg.Add(1)
	go func() {
		defer wg.Done()
		botWorker(ctx, handler, cfg)
	}()

	log.Println("Application is now running. Press Ctrl+C to stop.")

	// Wait for workers to finish
	wg.Wait()
	log.Println("Application shutdown complete")
}

func maskDatabaseURL(url string) string {
	// Simple masking to hide sensitive information while keeping the structure visible
	return regexp.MustCompile(`://[^:]+:[^@]+@`).ReplaceAllString(url, "://*****:*****@")
}

func notificationWorker(ctx context.Context, store *postgres.Store, cfg *config.Config) {
	log.Printf("Notification worker started with %d seconds interval", cfg.PollInterval)
	ticker := time.NewTicker(time.Duration(cfg.PollInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Notification worker shutting down...")
			return
		case <-ticker.C:
			log.Println("Starting notification check cycle...")
			if err := processNotifications(ctx, store, cfg); err != nil {
				log.Printf("Error processing notifications: %v", err)
			}
			log.Println("Notification check cycle completed")
		}
	}
}

func processNotifications(ctx context.Context, store *postgres.Store, cfg *config.Config) error {
	users, err := store.GetAllUsers()
	if err != nil {
		return fmt.Errorf("failed to get users: %v", err)
	}
	log.Printf("Processing notifications for %d users", len(users))

	for _, user := range users {
		activeAccounts := 0
		for _, account := range user.Accounts {
			if !account.IsActive {
				continue
			}
			activeAccounts++

			log.Printf("Checking GitHub notifications for user %s", account.Username)
			githubClient := github.NewClient(account.Token)
			notifications, err := githubClient.GetNotifications(ctx, account.Username)
			if err != nil {
				log.Printf("Error getting notifications for %s: %v", account.Username, err)
				continue
			}
			log.Printf("Found %d notifications for user %s", len(notifications), account.Username)

			notificationsSent := 0
			for _, notification := range notifications {
				contentHash := fmt.Sprintf("%x", sha256.Sum256([]byte(notification.Message)))
				shouldNotify, err := store.ShouldNotify(user.ChatID, notification.URL, notification.Type, contentHash, cfg.RenotifyInterval)
				if err != nil {
					log.Printf("Error checking notification status: %v", err)
					continue
				}

				if shouldNotify {
					telegramBot, err := bot.New(cfg.TelegramBotToken)
					if err != nil {
						log.Printf("Error creating Telegram bot: %v", err)
						continue
					}

					if err := telegramBot.SendNotification(user.ChatID, notification); err != nil {
						log.Printf("Error sending notification: %v", err)
						continue
					}

					if err := store.RecordNotification(user.ChatID, notification.URL, notification.Type, contentHash); err != nil {
						log.Printf("Error recording notification: %v", err)
						continue
					}
					notificationsSent++
				}
			}
			log.Printf("Sent %d new notifications for user %s", notificationsSent, account.Username)
		}
		log.Printf("Processed %d active accounts for user %d", activeAccounts, user.ChatID)
	}

	log.Println("Cleaning old notifications...")
	if err := store.CleanOldNotifications(cfg.RenotifyInterval); err != nil {
		log.Printf("Error cleaning old notifications: %v", err)
	}
	return nil
}

func botWorker(ctx context.Context, handler *bot.Handler, cfg *config.Config) {
	log.Printf("Bot worker started with %d seconds polling timeout", cfg.PollingTimeout)
	u := tgbotapi.NewUpdate(0)
	u.Timeout = cfg.PollingTimeout

	updates := handler.Bot.API.GetUpdatesChan(u)
	log.Println("Bot is now listening for updates")

	for {
		select {
		case <-ctx.Done():
			log.Println("Bot worker shutting down...")
			return
		case update := <-updates:
			if update.Message != nil && update.Message.IsCommand() {
				log.Printf("Received command: %s from user %d", update.Message.Command(), update.Message.From.ID)
			}
			if err := handler.HandleUpdate(update); err != nil {
				log.Printf("Error handling update: %v", err)
			}
		}
	}
}
