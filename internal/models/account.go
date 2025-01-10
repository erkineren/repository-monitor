package models

type GitHubAccount struct {
	Token    string `json:"token"`
	Username string `json:"username"`
	IsActive bool   `json:"is_active"`
}
