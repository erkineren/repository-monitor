package postgres

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/erkineren/repository-monitor/internal/models"
	_ "github.com/lib/pq"
)

type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

func New(dbURL string) (*Store, error) {
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %v", err)
	}

	if err := initDatabase(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize database: %v", err)
	}

	return &Store{
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
		`CREATE TABLE IF NOT EXISTS sent_notifications (
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

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) AddGitHubAccount(chatID int64, githubToken, githubUsername string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("INSERT INTO users (chat_id) VALUES ($1) ON CONFLICT DO NOTHING", chatID); err != nil {
		return fmt.Errorf("failed to insert user: %v", err)
	}

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

func (s *Store) RemoveGitHubAccount(chatID int64, githubUsername string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := "DELETE FROM github_accounts WHERE chat_id = $1 AND username = $2"
	if _, err := s.db.Exec(query, chatID, githubUsername); err != nil {
		return fmt.Errorf("failed to remove GitHub account: %v", err)
	}

	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM github_accounts WHERE chat_id = $1", chatID).Scan(&count); err != nil {
		return fmt.Errorf("failed to count remaining accounts: %v", err)
	}

	if count == 0 {
		if _, err := s.db.Exec("DELETE FROM users WHERE chat_id = $1", chatID); err != nil {
			return fmt.Errorf("failed to remove user: %v", err)
		}
	}

	return nil
}

func (s *Store) ToggleGitHubAccount(chatID int64, githubUsername string) error {
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

func (s *Store) GetUser(chatID int64) (*models.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user := &models.User{
		ChatID:   chatID,
		Accounts: make(map[string]*models.GitHubAccount),
	}

	query := `
		SELECT username, token, is_active
		FROM github_accounts
		WHERE chat_id = $1
	`
	rows, err := s.db.Query(query, chatID)
	if err != nil {
		return nil, false
	}
	defer rows.Close()

	exists := false
	for rows.Next() {
		exists = true
		var account models.GitHubAccount
		if err := rows.Scan(&account.Username, &account.Token, &account.IsActive); err != nil {
			continue
		}
		user.Accounts[account.Username] = &account
	}

	return user, exists
}

func (s *Store) GetAllUsers() ([]*models.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT DISTINCT chat_id FROM users")
	if err != nil {
		return nil, fmt.Errorf("failed to query users: %v", err)
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		var chatID int64
		if err := rows.Scan(&chatID); err != nil {
			return nil, fmt.Errorf("failed to scan chat_id: %v", err)
		}

		if user, exists := s.GetUser(chatID); exists {
			users = append(users, user)
		}
	}

	return users, nil
}

func (s *Store) ShouldNotify(chatID int64, itemURL string, notificationType string, contentHash string, renotifyInterval int) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var lastNotification time.Time
	err := s.db.QueryRow(`
		SELECT created_at 
		FROM sent_notifications 
		WHERE chat_id = $1 AND item_url = $2 AND notification_type = $3 AND content_hash = $4
		ORDER BY created_at DESC 
		LIMIT 1
	`, chatID, itemURL, notificationType, contentHash).Scan(&lastNotification)

	if err == sql.ErrNoRows {
		return true, nil
	} else if err != nil {
		return false, fmt.Errorf("failed to query notification: %v", err)
	}

	return time.Since(lastNotification) > time.Duration(renotifyInterval)*time.Hour, nil
}

func (s *Store) RecordNotification(chatID int64, itemURL string, notificationType string, contentHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO sent_notifications (chat_id, item_url, notification_type, content_hash)
		VALUES ($1, $2, $3, $4)
	`, chatID, itemURL, notificationType, contentHash)

	if err != nil {
		return fmt.Errorf("failed to record notification: %v", err)
	}

	return nil
}

func (s *Store) CleanOldNotifications(renotifyInterval int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		DELETE FROM sent_notifications 
		WHERE created_at < $1
	`, time.Now().Add(-time.Duration(renotifyInterval)*time.Hour))

	if err != nil {
		return fmt.Errorf("failed to clean old notifications: %v", err)
	}

	return nil
}
