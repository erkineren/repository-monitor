package store

import "github.com/erkineren/repository-monitor/internal/models"

type Store interface {
	Close() error
	AddGitHubAccount(chatID int64, githubToken, githubUsername string) error
	RemoveGitHubAccount(chatID int64, githubUsername string) error
	ToggleGitHubAccount(chatID int64, githubUsername string) error
	GetUser(chatID int64) (*models.User, bool)
	GetAllUsers() ([]*models.User, error)
	ShouldNotify(chatID int64, itemURL string, notificationType string, contentHash string, renotifyInterval int) (bool, error)
	RecordNotification(chatID int64, itemURL string, notificationType string, contentHash string) error
	CleanOldNotifications(renotifyInterval int) error
}
