package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/go-github/v57/github"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"golang.org/x/oauth2"
)

type GitHubAccount struct {
	Token    string `json:"token"`
	Username string `json:"username"`
	IsActive bool   `json:"is_active"`
}

type User struct {
	ChatID   int64
	Accounts map[string]*GitHubAccount
}

type UserStore struct {
	db *sql.DB
	mu sync.RWMutex
}

type Notification struct {
	Type    string
	Message string
	URL     string
}

type NotificationRecord struct {
	ID               int64
	ChatID           int64
	ItemURL          string
	NotificationType string
	ContentHash      string
	CreatedAt        time.Time
}

func NewUserStore(dbURL string) (*UserStore, error) {
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %v", err)
	}

	// Create tables if they don't exist
	if err := initDatabase(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize database: %v", err)
	}

	return &UserStore{
		db: db,
	}, nil
}

func initDatabase(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
			chat_id BIGINT PRIMARY KEY
		)`,
		`CREATE TABLE IF NOT EXISTS github_accounts (
			id SERIAL PRIMARY KEY,
			chat_id BIGINT,
			username TEXT NOT NULL,
			token TEXT NOT NULL,
			is_active BOOLEAN DEFAULT true,
			FOREIGN KEY (chat_id) REFERENCES users(chat_id),
			UNIQUE(chat_id, username)
		)`,
		// Drop and recreate sent_notifications table
		`DROP TABLE IF EXISTS sent_notifications`,
		`CREATE TABLE sent_notifications (
			id SERIAL PRIMARY KEY,
			chat_id BIGINT NOT NULL,
			item_url TEXT NOT NULL,
			notification_type TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (chat_id) REFERENCES users(chat_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_chat_url_type 
			ON sent_notifications(chat_id, item_url, notification_type, content_hash)`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("failed to execute query %q: %v", query, err)
		}
	}

	return nil
}

func (s *UserStore) Close() error {
	return s.db.Close()
}

func (s *UserStore) AddGitHubAccount(chatID int64, githubToken, githubUsername string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	// Insert or ignore user
	if _, err := tx.Exec("INSERT INTO users (chat_id) VALUES ($1) ON CONFLICT DO NOTHING", chatID); err != nil {
		return fmt.Errorf("failed to insert user: %v", err)
	}

	// Insert or replace GitHub account
	query := `
		INSERT INTO github_accounts (chat_id, username, token, is_active)
		VALUES ($1, $2, $3, true)
		ON CONFLICT (chat_id, username) DO UPDATE SET token = $3, is_active = true
	`
	if _, err := tx.Exec(query, chatID, githubUsername, githubToken); err != nil {
		return fmt.Errorf("failed to insert GitHub account: %v", err)
	}

	return tx.Commit()
}

func (s *UserStore) RemoveGitHubAccount(chatID int64, githubUsername string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := "DELETE FROM github_accounts WHERE chat_id = $1 AND username = $2"
	if _, err := s.db.Exec(query, chatID, githubUsername); err != nil {
		return fmt.Errorf("failed to remove GitHub account: %v", err)
	}

	// Check if user has any remaining accounts
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM github_accounts WHERE chat_id = $1", chatID).Scan(&count); err != nil {
		return fmt.Errorf("failed to count remaining accounts: %v", err)
	}

	// If no accounts left, remove the user
	if count == 0 {
		if _, err := s.db.Exec("DELETE FROM users WHERE chat_id = $1", chatID); err != nil {
			return fmt.Errorf("failed to remove user: %v", err)
		}
	}

	return nil
}

func (s *UserStore) ToggleGitHubAccount(chatID int64, githubUsername string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
		UPDATE github_accounts
		SET is_active = NOT is_active
		WHERE chat_id = $1 AND username = $2
	`
	result, err := s.db.Exec(query, chatID, githubUsername)
	if err != nil {
		return fmt.Errorf("failed to toggle GitHub account: %v", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %v", err)
	}

	if rows == 0 {
		return fmt.Errorf("account not found")
	}

	return nil
}

func (s *UserStore) GetUser(chatID int64) (*User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user := &User{
		ChatID:   chatID,
		Accounts: make(map[string]*GitHubAccount),
	}

	query := `
		SELECT username, token, is_active
		FROM github_accounts
		WHERE chat_id = $1
	`
	rows, err := s.db.Query(query, chatID)
	if err != nil {
		log.Printf("Error querying user accounts: %v", err)
		return nil, false
	}
	defer rows.Close()

	exists := false
	for rows.Next() {
		exists = true
		var account GitHubAccount
		if err := rows.Scan(&account.Username, &account.Token, &account.IsActive); err != nil {
			log.Printf("Error scanning account row: %v", err)
			continue
		}
		user.Accounts[account.Username] = &account
	}

	return user, exists
}

func (s *UserStore) GetAllUsers() ([]*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// First, get all unique chat_ids
	rows, err := s.db.Query("SELECT DISTINCT chat_id FROM users")
	if err != nil {
		return nil, fmt.Errorf("failed to query users: %v", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var chatID int64
		if err := rows.Scan(&chatID); err != nil {
			return nil, fmt.Errorf("failed to scan chat_id: %v", err)
		}

		user, exists := s.GetUser(chatID)
		if exists {
			users = append(users, user)
		}
	}

	return users, nil
}

func createCommandButtons() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Add Account", "/add"),
			tgbotapi.NewInlineKeyboardButtonData("❌ Remove Account", "/remove"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Toggle Account", "/toggle"),
			tgbotapi.NewInlineKeyboardButtonData("📊 Status", "/status"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❓ Help", "/help"),
		),
	)
}

func escapeMarkdown(text string) string {
	return strings.NewReplacer(
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
	).Replace(text)
}

func sendTelegramMessage(bot *tgbotapi.BotAPI, chatID int64, notification Notification) error {
	// Create a standardized emoji map for notification types
	emojiMap := map[string]string{
		"mention":          "💬",
		"review_requested": "👀",
	}

	// Get emoji for notification type, default to 🔔 if type not found
	emoji := emojiMap[notification.Type]
	if emoji == "" {
		emoji = "🔔"
	}

	// Format the type in a more readable way
	typeDisplay := strings.ReplaceAll(notification.Type, "_", " ")
	typeDisplay = strings.Title(typeDisplay)

	// Escape all parts of the message
	escapedType := escapeMarkdown(typeDisplay)
	escapedMessage := escapeMarkdown(notification.Message)
	escapedURL := escapeMarkdown(notification.URL)

	// Build a standardized message format:
	// Emoji Type
	// Message content
	// Link to GitHub
	message := fmt.Sprintf("%s *%s*\n\n%s\n\n🔗 [View on GitHub](%s)",
		emoji,
		escapedType,
		escapedMessage,
		escapedURL,
	)

	msg := tgbotapi.NewMessage(chatID, message)
	msg.ParseMode = "MarkdownV2"

	_, err := bot.Send(msg)
	return err
}

func checkGitHubNotifications(ctx context.Context, client *github.Client, username string) ([]Notification, error) {
	var notifications []Notification

	// Check notifications
	opts := &github.NotificationListOptions{
		All: true,
	}
	notifs, _, err := client.Activity.ListNotifications(ctx, opts)
	if err != nil {
		return nil, err
	}

	for _, notif := range notifs {
		if notif.GetReason() == "mention" || notif.GetReason() == "review_requested" {
			// Extract number from the API URL
			apiURL := notif.Subject.GetURL()
			parts := strings.Split(apiURL, "/")
			number := parts[len(parts)-1]

			// Get repository details
			owner := notif.Repository.GetOwner().GetLogin()
			repo := notif.Repository.GetName()
			itemNum, _ := strconv.Atoi(number)

			var htmlURL string
			var isClosed bool

			// Check if it's a PR or an Issue based on the subject type
			subjectType := notif.Subject.GetType()
			switch subjectType {
			case "PullRequest":
				pr, resp, err := client.PullRequests.Get(ctx, owner, repo, itemNum)
				if err != nil {
					// Check if it's a 404 error
					if resp != nil && resp.StatusCode == 404 {
						log.Printf("PR not found (might be deleted or private) - owner: %s, repo: %s, PR: %d", owner, repo, itemNum)
						// Mark notification as read to avoid checking it again
						if _, err := client.Activity.MarkThreadRead(ctx, notif.GetID()); err != nil {
							log.Printf("Failed to mark notification as read: %v", err)
						}
						continue
					}
					log.Printf("Error getting PR details: %v", err)
					continue
				}
				htmlURL = pr.GetHTMLURL()
				isClosed = pr.GetState() == "closed" || pr.GetMerged()

			case "Issue":
				issue, resp, err := client.Issues.Get(ctx, owner, repo, itemNum)
				if err != nil {
					// Check if it's a 404 error
					if resp != nil && resp.StatusCode == 404 {
						log.Printf("Issue not found (might be deleted or private) - owner: %s, repo: %s, Issue: %d", owner, repo, itemNum)
						// Mark notification as read to avoid checking it again
						if _, err := client.Activity.MarkThreadRead(ctx, notif.GetID()); err != nil {
							log.Printf("Failed to mark notification as read: %v", err)
						}
						continue
					}
					log.Printf("Error getting Issue details: %v", err)
					continue
				}
				htmlURL = issue.GetHTMLURL()
				isClosed = issue.GetState() == "closed"

			default:
				log.Printf("Unknown notification subject type: %s", subjectType)
				continue
			}

			// Skip if item is closed
			if isClosed {
				// Mark notification as read for closed items
				if _, err := client.Activity.MarkThreadRead(ctx, notif.GetID()); err != nil {
					log.Printf("Failed to mark notification as read: %v", err)
				}
				continue
			}

			// Build a standardized message format
			var message string
			repoInfo := fmt.Sprintf("📁 %s/%s", owner, repo)
			title := fmt.Sprintf("📝 %s", notif.Subject.GetTitle())

			// Handle different notification types
			switch notif.GetReason() {
			case "review_requested":
				// Get PR details to show who requested the review
				var requestedBy string
				if subjectType == "PullRequest" {
					pr, _, err := client.PullRequests.Get(ctx, owner, repo, itemNum)
					if err == nil {
						requestedBy = pr.GetUser().GetLogin()
					}
				}

				if requestedBy != "" {
					message = fmt.Sprintf("%s\n%s\n\n👀 %s requested your review on this pull request",
						repoInfo,
						title,
						requestedBy,
					)
				} else {
					message = fmt.Sprintf("%s\n%s\n\n👀 Your review was requested on this pull request",
						repoInfo,
						title,
					)
				}

			case "mention":
				// Get the comment URL and fetch comment details
				commentURL := notif.Subject.GetLatestCommentURL()

				// Check if user has already replied to this PR/Issue
				hasReplied := false
				comments, _, err := client.Issues.ListComments(ctx, owner, repo, itemNum, nil)
				if err == nil {
					for _, comment := range comments {
						if comment.GetUser().GetLogin() == username {
							hasReplied = true
							break
						}
					}
				}

				// If user has replied, mark as read and skip
				if hasReplied {
					if _, err := client.Activity.MarkThreadRead(ctx, notif.GetID()); err != nil {
						log.Printf("Failed to mark notification as read: %v", err)
					}
					continue
				}

				if commentURL == "" || !strings.Contains(commentURL, "/comments/") {
					// If there's no comment URL or it's not a comment URL (i.e., it's a mention in the issue/PR body)
					var authorUsername string
					var mentionContent string

					if subjectType == "PullRequest" {
						pr, _, err := client.PullRequests.Get(ctx, owner, repo, itemNum)
						if err == nil {
							authorUsername = pr.GetUser().GetLogin()
							mentionContent = pr.GetBody()
						}
					} else if subjectType == "Issue" {
						issue, _, err := client.Issues.Get(ctx, owner, repo, itemNum)
						if err == nil {
							authorUsername = issue.GetUser().GetLogin()
							mentionContent = issue.GetBody()
						}
					}

					// Skip if it's a self-mention
					if authorUsername == username {
						// Mark notification as read since it's a self-mention
						if _, err := client.Activity.MarkThreadRead(ctx, notif.GetID()); err != nil {
							log.Printf("Failed to mark notification as read: %v", err)
						}
						continue
					}

					// Truncate long descriptions to a reasonable length
					if len(mentionContent) > 300 {
						mentionContent = mentionContent[:297] + "..."
					}

					message = fmt.Sprintf("%s\n%s\n\n👤 %s mentioned you in the %s description:\n\n%s",
						repoInfo,
						title,
						authorUsername,
						strings.ToLower(subjectType),
						mentionContent,
					)
				} else {
					// Handle comment mentions
					// Parse the comment URL to get the comment ID
					commentParts := strings.Split(commentURL, "/")
					commentIDStr := commentParts[len(commentParts)-1]
					commentID, _ := strconv.Atoi(commentIDStr)

					// Get comment details
					var comment *github.IssueComment
					var err error
					if strings.Contains(commentURL, "/issues/comments/") {
						comment, _, err = client.Issues.GetComment(ctx, owner, repo, int64(commentID))
					} else if strings.Contains(commentURL, "/pulls/comments/") {
						var prComment *github.PullRequestComment
						prComment, _, err = client.PullRequests.GetComment(ctx, owner, repo, int64(commentID))
						if err == nil && prComment != nil {
							comment = &github.IssueComment{
								User: prComment.User,
								Body: prComment.Body,
							}
						}
					}

					if err != nil {
						log.Printf("Error getting comment details: %v", err)
						continue
					}

					// Skip if the comment author is the authenticated user (self-mention)
					if comment.GetUser().GetLogin() == username {
						// Mark notification as read since it's a self-mention
						if _, err := client.Activity.MarkThreadRead(ctx, notif.GetID()); err != nil {
							log.Printf("Failed to mark notification as read: %v", err)
						}
						continue
					}

					// Truncate long comments to a reasonable length (e.g., 300 characters)
					commentBody := comment.GetBody()
					if len(commentBody) > 300 {
						commentBody = commentBody[:297] + "..."
					}

					message = fmt.Sprintf("%s\n%s\n\n👤 %s mentioned you in a comment:\n\n%s",
						repoInfo,
						title,
						comment.GetUser().GetLogin(),
						commentBody,
					)
				}
			}

			if message != "" {
				notifications = append(notifications, Notification{
					Type:    notif.GetReason(),
					Message: message,
					URL:     htmlURL,
				})
			}
		}
	}

	// Check review requests from search (keep existing code but update message format)
	searchQuery := fmt.Sprintf("review-requested:%s is:open is:pr", username)
	searchOpts := &github.SearchOptions{}
	prs, resp, err := client.Search.Issues(ctx, searchQuery, searchOpts)
	if err != nil {
		// Check if it's a 404 error
		if resp != nil && resp.StatusCode == 404 {
			log.Printf("Search query failed with 404 (might be invalid token or rate limit) - query: %s", searchQuery)
			return notifications, nil
		}
		return nil, err
	}

	for _, pr := range prs.Issues {
		repoURL := pr.GetRepositoryURL()
		repoParts := strings.Split(repoURL, "/")
		owner := repoParts[len(repoParts)-2]
		repo := repoParts[len(repoParts)-1]

		message := fmt.Sprintf("📁 %s/%s\n📝 %s\n\n👀 %s requested your review on this pull request",
			owner,
			repo,
			pr.GetTitle(),
			pr.GetUser().GetLogin(),
		)

		notifications = append(notifications, Notification{
			Type:    "review_requested",
			Message: message,
			URL:     pr.GetHTMLURL(),
		})
	}

	return notifications, nil
}

// Add methods to handle notification records
func (s *UserStore) shouldNotify(chatID int64, itemURL string, notificationType string, contentHash string, renotifyInterval int) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if we have a recent notification for this item with the same type and content
	query := `
		SELECT created_at 
		FROM sent_notifications 
		WHERE chat_id = $1 AND item_url = $2 AND notification_type = $3 AND content_hash = $4
		ORDER BY created_at DESC 
		LIMIT 1
	`

	var lastNotificationTime time.Time
	err := s.db.QueryRow(query, chatID, itemURL, notificationType, contentHash).Scan(&lastNotificationTime)
	if err == sql.ErrNoRows {
		return true, nil // No previous notification found
	}
	if err != nil {
		return false, fmt.Errorf("error checking notification history: %v", err)
	}

	// Check if enough time has passed since the last notification
	return time.Since(lastNotificationTime) > time.Duration(renotifyInterval)*time.Second, nil
}

func (s *UserStore) recordNotification(chatID int64, itemURL string, notificationType string, contentHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
		INSERT INTO sent_notifications (chat_id, item_url, notification_type, content_hash, created_at)
		VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP)
	`

	_, err := s.db.Exec(query, chatID, itemURL, notificationType, contentHash)
	if err != nil {
		return fmt.Errorf("error recording notification: %v", err)
	}

	return nil
}

func (s *UserStore) cleanOldNotifications(renotifyInterval int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
		DELETE FROM sent_notifications 
		WHERE created_at < CURRENT_TIMESTAMP - INTERVAL '1 second' * $1
	`

	_, err := s.db.Exec(query, renotifyInterval)
	if err != nil {
		return fmt.Errorf("error cleaning old notifications: %v", err)
	}

	return nil
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found. But it's okay, we'll use the environment variables.")
	}

	bot, err := tgbotapi.NewBotAPI(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	// Enable debugging mode
	bot.Debug = true
	log.Printf("Authorized on account %s", bot.Self.UserName)

	// Initialize database with PostgreSQL URL
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is required but not set")
	}

	userStore, err := NewUserStore(dbURL)
	if err != nil {
		log.Fatalf("Failed to create user store: %v", err)
	}
	defer userStore.Close()

	// Set up bot updates
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)
	log.Printf("Bot started and listening for updates...")

	pollInterval, _ := strconv.Atoi(os.Getenv("POLL_INTERVAL"))
	if pollInterval == 0 {
		pollInterval = 60
	}

	renotifyInterval, _ := strconv.Atoi(os.Getenv("RENOTIFY_INTERVAL"))
	if renotifyInterval == 0 {
		renotifyInterval = 3600 // Default to 1 hour if not specified
	}

	log.Printf("Starting GitHub notification monitor. Polling every %d seconds with renotification interval of %d seconds...", pollInterval, renotifyInterval)

	// Start notification checker in a separate goroutine
	go func() {
		for {
			// Clean up old notifications first
			if err := userStore.cleanOldNotifications(renotifyInterval); err != nil {
				log.Printf("Error cleaning old notifications: %v", err)
			}

			users, err := userStore.GetAllUsers()
			if err != nil {
				log.Printf("Error getting users: %v", err)
				time.Sleep(time.Duration(pollInterval) * time.Second)
				continue
			}

			for _, user := range users {
				for _, account := range user.Accounts {
					if !account.IsActive {
						continue
					}

					ctx := context.Background()
					ts := oauth2.StaticTokenSource(
						&oauth2.Token{AccessToken: account.Token},
					)
					tc := oauth2.NewClient(ctx, ts)
					client := github.NewClient(tc)

					notifications, err := checkGitHubNotifications(ctx, client, account.Username)
					if err != nil {
						log.Printf("Error checking notifications for user %d (account %s): %v", user.ChatID, account.Username, err)
						continue
					}

					for _, notification := range notifications {
						// Generate a content hash based on the notification type and message
						contentHash := fmt.Sprintf("%x", sha256.Sum256([]byte(notification.Message)))

						// Check if we should send this notification
						shouldNotify, err := userStore.shouldNotify(user.ChatID, notification.URL, notification.Type, contentHash, renotifyInterval)
						if err != nil {
							log.Printf("Error checking notification status: %v", err)
							continue
						}

						if !shouldNotify {
							log.Printf("Skipping notification for user %d (account %s): %s - too recent", user.ChatID, account.Username, notification.URL)
							continue
						}

						if err := sendTelegramMessage(bot, user.ChatID, notification); err != nil {
							log.Printf("Error sending telegram message to user %d: %v", user.ChatID, err)
						} else {
							// Record the sent notification
							if err := userStore.recordNotification(user.ChatID, notification.URL, notification.Type, contentHash); err != nil {
								log.Printf("Error recording notification: %v", err)
							}
							log.Printf("Sent notification to user %d (account %s): %s", user.ChatID, account.Username, notification.Message)
						}
					}
				}
			}

			time.Sleep(time.Duration(pollInterval) * time.Second)
		}
	}()

	// Handle bot commands
	for update := range updates {
		if update.Message == nil && update.CallbackQuery == nil {
			continue
		}

		var chatID int64
		var command string
		var reply string

		if update.CallbackQuery != nil {
			chatID = update.CallbackQuery.Message.Chat.ID
			command = strings.TrimPrefix(update.CallbackQuery.Data, "/")
			// Answer the callback query to remove the "loading" state
			callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
			bot.Request(callback)
			log.Printf("Received callback query for command: %s", command)
		} else if update.Message != nil {
			chatID = update.Message.Chat.ID
			// Check if the message starts with a command
			if strings.HasPrefix(update.Message.Text, "/") {
				parts := strings.Fields(update.Message.Text)
				command = strings.TrimPrefix(parts[0], "/")
				log.Printf("Received command: %s from chat ID: %d", command, chatID)
			} else {
				log.Printf("Received non-command message: %s", update.Message.Text)
				continue
			}
		}

		log.Printf("Processing command: %s", command)
		switch command {
		case "start":
			log.Printf("Processing /start command for chat ID: %d", chatID)
			reply = "Welcome to GitHub Notification Bot! 🚀\n\n" +
				"I can help you monitor your GitHub notifications from multiple accounts.\n\n" +
				"Here are the available commands:\n\n" +
				"➕ /add - Add a GitHub account\n" +
				"❌ /remove - Remove a GitHub account\n" +
				"🔄 /toggle - Enable/disable notifications\n" +
				"📊 /status - List your accounts\n" +
				"❓ /help - Show help message\n\n" +
				"Click the buttons below to get started!"

			msg := tgbotapi.NewMessage(chatID, reply)
			msg.ReplyMarkup = createCommandButtons()
			if _, err := bot.Send(msg); err != nil {
				log.Printf("Error sending message: %v", err)
			}
			continue

		case "add":
			log.Printf("Processing /add command for chat ID: %d", chatID)
			if update.CallbackQuery != nil {
				log.Printf("Received callback query for /add command")
				reply = "To add a GitHub account, use the command:\n" +
					"`/add <github_token> <github_username>`\n\n" +
					"Example:\n" +
					"`/add ghp_1234567890abcdef username`"
				break
			}

			// Get arguments from the full message text
			parts := strings.Fields(update.Message.Text)
			args := parts[1:] // Skip the command part
			log.Printf("Add command received with %d arguments: %v", len(args), args)

			if len(args) != 2 {
				log.Printf("Invalid number of arguments for /add command")
				reply = "Please provide both GitHub token and username.\nUsage: /add <github_token> <github_username>"
				break
			}

			githubToken := args[0]
			githubUsername := args[1]
			log.Printf("Attempting to validate GitHub token for username: %s", githubUsername)

			// Validate GitHub token
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			ts := oauth2.StaticTokenSource(
				&oauth2.Token{AccessToken: githubToken},
			)
			tc := oauth2.NewClient(ctx, ts)
			client := github.NewClient(tc)

			log.Printf("Making GitHub API request to validate token")
			user, resp, err := client.Users.Get(ctx, "")
			if err != nil {
				log.Printf("GitHub token validation failed: %v", err)
				if resp != nil {
					log.Printf("GitHub API response status: %s", resp.Status)
				}
				reply = fmt.Sprintf("Invalid GitHub token. Please check and try again. Error: %v", err)
				break
			}
			log.Printf("GitHub token validated successfully for user: %s", user.GetLogin())

			log.Printf("Adding GitHub account to user store")
			if err := userStore.AddGitHubAccount(chatID, githubToken, githubUsername); err != nil {
				log.Printf("Failed to add GitHub account: %v", err)
				reply = "Failed to add account. Please try again later."
				break
			}
			log.Printf("Successfully added GitHub account for user %d: %s", chatID, githubUsername)
			reply = fmt.Sprintf("Successfully added GitHub account: %s!", githubUsername)

		case "remove":
			if update.CallbackQuery != nil {
				reply = "To remove a GitHub account, use the command:\n" +
					"`/remove <github_username>`\n\n" +
					"Example:\n" +
					"`/remove username`"
				break
			}

			args := strings.Fields(update.Message.Text)[1:]
			if len(args) != 1 {
				reply = "Please provide the GitHub username.\nUsage: /remove <github_username>"
				break
			}

			githubUsername := args[0]
			if err := userStore.RemoveGitHubAccount(chatID, githubUsername); err != nil {
				reply = "Failed to remove account. Please try again later."
				break
			}
			reply = fmt.Sprintf("Successfully removed GitHub account: %s", githubUsername)

		case "toggle":
			if update.CallbackQuery != nil {
				reply = "To toggle a GitHub account, use the command:\n" +
					"`/toggle <github_username>`\n\n" +
					"Example:\n" +
					"`/toggle username`"
				break
			}

			args := strings.Fields(update.Message.Text)[1:]
			if len(args) != 1 {
				reply = "Please provide the GitHub username.\nUsage: /toggle <github_username>"
				break
			}

			githubUsername := args[0]
			if err := userStore.ToggleGitHubAccount(chatID, githubUsername); err != nil {
				reply = fmt.Sprintf("Failed to toggle account: %v", err)
				break
			}

			user, _ := userStore.GetUser(chatID)
			account := user.Accounts[githubUsername]
			status := "enabled"
			if !account.IsActive {
				status = "disabled"
			}
			reply = fmt.Sprintf("Successfully %s notifications for account: %s", status, githubUsername)

		case "status":
			if user, exists := userStore.GetUser(chatID); exists {
				var accounts []string
				for _, account := range user.Accounts {
					status := "🟢"
					if !account.IsActive {
						status = "🔴"
					}
					accounts = append(accounts, fmt.Sprintf("%s %s", status, account.Username))
				}
				if len(accounts) > 0 {
					reply = "Your GitHub accounts:\n" + strings.Join(accounts, "\n")
				} else {
					reply = "You have no GitHub accounts registered."
				}
			} else {
				reply = "You have no GitHub accounts registered. Use /add to add an account."
			}

		case "help":
			reply = "Available commands:\n" +
				"/start - Get started with the bot\n" +
				"/add <token> <username> - Add a GitHub account\n" +
				"/remove <username> - Remove a GitHub account\n" +
				"/toggle <username> - Enable/disable notifications for an account\n" +
				"/status - List your GitHub accounts\n" +
				"/help - Show this help message"

		default:
			reply = "Unknown command. Type /help for available commands."
		}

		log.Printf("Preparing to send reply: %s", reply)
		msg := tgbotapi.NewMessage(chatID, reply)
		if strings.Contains(reply, "`") || strings.Contains(reply, "*") {
			log.Printf("Message contains markdown formatting, setting ParseMode to MarkdownV2")
			msg.ParseMode = "MarkdownV2"
			// Escape the message but preserve code blocks
			parts := strings.Split(reply, "`")
			for i := 0; i < len(parts); i++ {
				if i%2 == 0 { // Not in code block
					parts[i] = escapeMarkdown(parts[i])
				}
			}
			msg.Text = strings.Join(parts, "`")
			log.Printf("Escaped message: %s", msg.Text)
		}

		log.Printf("Sending message to chat ID %d", chatID)
		if _, err := bot.Send(msg); err != nil {
			log.Printf("Error sending message: %v\nMessage: %s", err, msg.Text)
			// Try sending without markdown if it fails
			if msg.ParseMode == "MarkdownV2" {
				log.Printf("Retrying without markdown formatting")
				msg.ParseMode = ""
				msg.Text = reply
				if _, err := bot.Send(msg); err != nil {
					log.Printf("Error sending plain message: %v", err)
				} else {
					log.Printf("Successfully sent plain message")
				}
			}
		} else {
			log.Printf("Successfully sent reply for command %s to chat ID %d", command, chatID)
		}
	}
}
