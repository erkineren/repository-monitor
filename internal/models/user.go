package models

type User struct {
	ChatID   int64
	Accounts map[string]*GitHubAccount
}
