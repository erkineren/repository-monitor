package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v57/github"
	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
)

type Notification struct {
	Type    string
	Message string
	URL     string
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found")
	}

	// Initialize GitHub client
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: os.Getenv("GITHUB_TOKEN")},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	pollInterval, _ := strconv.Atoi(os.Getenv("POLL_INTERVAL"))
	if pollInterval == 0 {
		pollInterval = 60
	}

	log.Printf("Starting GitHub notification monitor. Polling every %d seconds...", pollInterval)

	for {
		notifications, err := checkGitHubNotifications(ctx, client)
		if err != nil {
			log.Printf("Error checking notifications: %v", err)
			continue
		}

		for _, notification := range notifications {
			if err := sendTelegramMessage(notification); err != nil {
				log.Printf("Error sending telegram message: %v\nNotification: %+v", err, notification)
			} else {
				log.Printf("Sent notification: %s", notification.Message)
			}
		}

		time.Sleep(time.Duration(pollInterval) * time.Second)
	}
}

func checkGitHubNotifications(ctx context.Context, client *github.Client) ([]Notification, error) {
	username := os.Getenv("GITHUB_USERNAME")
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
			// Extract PR number from the API URL
			apiURL := notif.Subject.GetURL()
			parts := strings.Split(apiURL, "/")
			prNumber := parts[len(parts)-1]

			// Get PR details to check if it's merged
			owner := notif.Repository.GetOwner().GetLogin()
			repo := notif.Repository.GetName()
			prNum, _ := strconv.Atoi(prNumber)

			pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNum)
			if err != nil {
				log.Printf("Error getting PR details: %v", err)
				continue
			}

			// Skip if PR is merged or closed
			if pr.GetMerged() || pr.GetState() == "closed" {
				continue
			}

			message := fmt.Sprintf("%s: %s", notif.GetReason(), notif.Subject.GetTitle())

			// If it's a mention, fetch the comment details
			if notif.GetReason() == "mention" {
				// Get the comment URL and fetch comment details
				commentURL := notif.Subject.GetLatestCommentURL()
				if commentURL != "" {
					// Parse the comment URL to get the comment ID
					commentParts := strings.Split(commentURL, "/")
					commentIDStr := commentParts[len(commentParts)-1]
					commentID, _ := strconv.Atoi(commentIDStr)

					// Fetch the comment details
					comment, _, err := client.Issues.GetComment(ctx, owner, repo, int64(commentID))
					if err == nil && comment != nil {
						// Skip if the comment author is the authenticated user
						if comment.GetUser().GetLogin() == username {
							continue
						}

						// Get all comments after this mention
						sort := "created"
						direction := "asc"
						since := comment.GetCreatedAt().Time
						opts := &github.IssueListCommentsOptions{
							Since:     &since,
							Sort:      &sort,
							Direction: &direction,
						}
						laterComments, _, err := client.Issues.ListComments(ctx, owner, repo, prNum, opts)
						if err == nil {
							hasReplied := false
							for _, laterComment := range laterComments {
								// Skip the original comment
								if laterComment.GetID() == comment.GetID() {
									continue
								}
								// Check if user has replied after this mention
								if laterComment.GetUser().GetLogin() == username {
									hasReplied = true
									break
								}
							}
							// Skip if user has already replied
							if hasReplied {
								continue
							}
						}

						message = fmt.Sprintf("%s: %s\n👤 %s commented: %s",
							notif.GetReason(),
							notif.Subject.GetTitle(),
							comment.GetUser().GetLogin(),
							comment.GetBody(),
						)
					}
				}
			}

			notifications = append(notifications, Notification{
				Type:    notif.GetReason(),
				Message: message,
				URL:     pr.GetHTMLURL(),
			})
		}
	}

	// Check review requests (already filtered for open PRs by the search query)
	searchQuery := fmt.Sprintf("review-requested:%s is:open is:pr", username)
	searchOpts := &github.SearchOptions{}
	prs, _, err := client.Search.Issues(ctx, searchQuery, searchOpts)
	if err != nil {
		return nil, err
	}

	for _, pr := range prs.Issues {
		notifications = append(notifications, Notification{
			Type:    "review_requested",
			Message: fmt.Sprintf("Review requested: %s", pr.GetTitle()),
			URL:     pr.GetHTMLURL(),
		})
	}

	return notifications, nil
}

func sendTelegramMessage(notification Notification) error {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")

	// Escape special characters for MarkdownV2
	escapedType := strings.NewReplacer(
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
	).Replace(notification.Type)

	escapedMessage := strings.NewReplacer(
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
	).Replace(notification.Message)

	message := fmt.Sprintf("*%s*\n%s\n[View on GitHub](%s)",
		escapedType,
		escapedMessage,
		notification.URL,
	)

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	resp, err := http.PostForm(url, map[string][]string{
		"chat_id":    {chatID},
		"text":       {message},
		"parse_mode": {"MarkdownV2"},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned non-200 status code: %d", resp.StatusCode)
	}

	return nil
}
