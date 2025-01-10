package models

import "time"

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
