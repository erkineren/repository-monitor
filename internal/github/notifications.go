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

	// Get user's repositories
	opts := &github.RepositoryListOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		repos, resp, err := c.client.Repositories.List(ctx, username, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list repositories: %v", err)
		}

		for _, repo := range repos {
			// Check pull requests
			prNotifications, err := c.checkPullRequests(ctx, repo)
			if err != nil {
				continue
			}
			notifications = append(notifications, prNotifications...)

			// Check issues
			issueNotifications, err := c.checkIssues(ctx, repo)
			if err != nil {
				continue
			}
			notifications = append(notifications, issueNotifications...)

			// Check releases
			releaseNotifications, err := c.checkReleases(ctx, repo)
			if err != nil {
				continue
			}
			notifications = append(notifications, releaseNotifications...)
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

	opts := &github.PullRequestListOptions{
		State:     "open",
		Sort:      "updated",
		Direction: "desc",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	prs, _, err := c.client.PullRequests.List(ctx, repo.GetOwner().GetLogin(), repo.GetName(), opts)
	if err != nil {
		return nil, err
	}

	for _, pr := range prs {
		if time.Since(pr.GetUpdatedAt().Time) > 24*time.Hour {
			continue
		}

		notification := models.Notification{
			Type:    "pull_request",
			Message: fmt.Sprintf("[%s] PR #%d: %s", repo.GetFullName(), pr.GetNumber(), pr.GetTitle()),
			URL:     pr.GetHTMLURL(),
		}
		notifications = append(notifications, notification)
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
