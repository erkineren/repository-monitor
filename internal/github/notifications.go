package github

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/erkineren/repository-monitor/internal/models"
	"github.com/google/go-github/v57/github"
)

func (c *Client) GetNotifications(ctx context.Context, username string) ([]models.Notification, error) {
	var notifications []models.Notification

	opts := &github.NotificationListOptions{
		All:           true,
		Participating: true,
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	for {
		ghNotifications, resp, err := c.client.Activity.ListNotifications(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list notifications: %v", err)
		}

		for _, n := range ghNotifications {
			if n.GetUnread() {
				notification := models.Notification{
					Type:    string(n.GetReason()),
					Message: fmt.Sprintf("[%s] %s", n.GetRepository().GetFullName(), n.GetSubject().GetTitle()),
					URL:     n.GetSubject().GetURL(),
				}
				notifications = append(notifications, notification)
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return notifications, nil
}

func (c *Client) checkPullRequests(ctx context.Context, repo *github.Repository) ([]models.Notification, error) {
	var notifications []models.Notification

	// Check for open PRs
	openOpts := &github.PullRequestListOptions{
		State:     "open",
		Sort:      "updated",
		Direction: "desc",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	openPRs, _, err := c.client.PullRequests.List(ctx, repo.GetOwner().GetLogin(), repo.GetName(), openOpts)
	if err != nil {
		return nil, err
	}

	// Check for recently merged PRs
	mergedOpts := &github.PullRequestListOptions{
		State:     "closed",
		Sort:      "updated",
		Direction: "desc",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	mergedPRs, _, err := c.client.PullRequests.List(ctx, repo.GetOwner().GetLogin(), repo.GetName(), mergedOpts)
	if err != nil {
		return nil, err
	}

	// Process open PRs (new PRs)
	for _, pr := range openPRs {
		// Only notify about PRs created in the last 24 hours
		if time.Since(pr.GetCreatedAt().Time) <= 24*time.Hour {
			notification := models.Notification{
				Type:    "new_pull_request",
				Message: fmt.Sprintf("[%s] New PR #%d: %s by %s", repo.GetFullName(), pr.GetNumber(), pr.GetTitle(), pr.GetUser().GetLogin()),
				URL:     pr.GetHTMLURL(),
			}
			notifications = append(notifications, notification)
		}
	}

	// Process merged PRs
	for _, pr := range mergedPRs {
		// Only notify about PRs merged in the last 24 hours
		if pr.GetMerged() && time.Since(pr.GetUpdatedAt().Time) <= 24*time.Hour {
			notification := models.Notification{
				Type:    "merged_pull_request",
				Message: fmt.Sprintf("[%s] Merged PR #%d: %s by %s", repo.GetFullName(), pr.GetNumber(), pr.GetTitle(), pr.GetUser().GetLogin()),
				URL:     pr.GetHTMLURL(),
			}
			notifications = append(notifications, notification)
		}
	}

	return notifications, nil
}

func (c *Client) checkIssues(ctx context.Context, repo *github.Repository) ([]models.Notification, error) {
	var notifications []models.Notification

	opts := &github.IssueListByRepoOptions{
		State:     "open",
		Sort:      "updated",
		Direction: "desc",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	issues, _, err := c.client.Issues.ListByRepo(ctx, repo.GetOwner().GetLogin(), repo.GetName(), opts)
	if err != nil {
		return nil, err
	}

	for _, issue := range issues {
		if issue.IsPullRequest() || time.Since(issue.GetUpdatedAt().Time) > 24*time.Hour {
			continue
		}

		notification := models.Notification{
			Type:    "issue",
			Message: fmt.Sprintf("[%s] Issue #%d: %s", repo.GetFullName(), issue.GetNumber(), issue.GetTitle()),
			URL:     issue.GetHTMLURL(),
		}
		notifications = append(notifications, notification)
	}

	return notifications, nil
}

func (c *Client) checkReleases(ctx context.Context, repo *github.Repository) ([]models.Notification, error) {
	var notifications []models.Notification

	opts := &github.ListOptions{
		PerPage: 5,
	}

	releases, _, err := c.client.Repositories.ListReleases(ctx, repo.GetOwner().GetLogin(), repo.GetName(), opts)
	if err != nil {
		return nil, err
	}

	for _, release := range releases {
		if time.Since(release.GetCreatedAt().Time) > 24*time.Hour {
			continue
		}

		message := fmt.Sprintf("[%s] New release: %s", repo.GetFullName(), release.GetTagName())
		if notes := release.GetBody(); notes != "" {
			message += "\n" + strings.Split(notes, "\n")[0] // First line of release notes
		}

		notification := models.Notification{
			Type:    "release",
			Message: message,
			URL:     release.GetHTMLURL(),
		}
		notifications = append(notifications, notification)
	}

	return notifications, nil
}
