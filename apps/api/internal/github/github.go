package github

import (
	"context"
	"fmt"
	"strings"

	gh "github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
)

type Client struct {
	client *gh.Client
	owner  string
	repo   string
}

func NewClient(token, owner, repo string) *Client {
	if token == "" || owner == "" || repo == "" {
		return nil
	}

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)

	return &Client{
		client: gh.NewClient(tc),
		owner:  owner,
		repo:   repo,
	}
}

// ReportMissingDependency creates a GitHub issue if no existing open or closed issue matches the tool
func (c *Client) ReportMissingDependency(ctx context.Context, toolName, srcExt, targetExt string) error {
	if c == nil {
		return nil
	}

	title := fmt.Sprintf("[Dependency Needed] Install '%s' for %s -> %s conversion", toolName, srcExt, targetExt)

	// Search for existing issues to deduplicate
	query := fmt.Sprintf("repo:%s/%s is:issue in:title \"%s\"", c.owner, c.repo, toolName)
	opts := &gh.SearchOptions{
		ListOptions: gh.ListOptions{PerPage: 5},
	}

	result, _, err := c.client.Search.Issues(ctx, query, opts)
	if err == nil && result.GetTotal() > 0 {
		// Already reported, deduplicate
		return nil
	}

	body := fmt.Sprintf(`### Missing Dependency Report

A file conversion request requires an uninstalled tool.

- **Required Tool:** %s
- **Conversion:** %s -> %s
- **Status:** Automatically requested dynamic install or container enhancement needed.

Please consider adding %s to the default worker Dockerfile.
`, toolName, srcExt, targetExt, toolName)

	labels := []string{"dependency-needed", "automated"}
	issueRequest := &gh.IssueRequest{
		Title:  &title,
		Body:   &body,
		Labels: &labels,
	}

	_, _, err = c.client.Issues.Create(ctx, c.owner, c.repo, issueRequest)
	return err
}

// ReportConversionFailure creates a deduplicated GitHub issue when a CLI command fails
func (c *Client) ReportConversionFailure(ctx context.Context, toolName, srcExt, targetExt, command, sanitizedLogs string) error {
	if c == nil {
		return nil
	}

	title := fmt.Sprintf("[Conversion Failure] Tool '%s' failed for %s -> %s", toolName, srcExt, targetExt)

	// Check if already reported
	query := fmt.Sprintf("repo:%s/%s is:issue in:title \"Tool '%s' failed for %s -> %s\"", c.owner, c.repo, toolName, srcExt, targetExt)
	opts := &gh.SearchOptions{
		ListOptions: gh.ListOptions{PerPage: 5},
	}

	result, _, err := c.client.Search.Issues(ctx, query, opts)
	if err == nil && result.GetTotal() > 0 {
		// Deduplicated
		return nil
	}

	body := fmt.Sprintf(`### Conversion Execution Failure

The conversion execution failed in worker sandbox.

- **Tool:** %s
- **Flow:** %s to %s
- **Command:** `+"`%s`"+`

<details>
<summary>Sanitized Error Logs</summary>

`+"```text\n%s\n```"+`

</details>
`, toolName, srcExt, targetExt, command, sanitize(sanitizedLogs))

	labels := []string{"bug", "conversion-error", "automated"}
	issueRequest := &gh.IssueRequest{
		Title:  &title,
		Body:   &body,
		Labels: &labels,
	}

	_, _, err = c.client.Issues.Create(ctx, c.owner, c.repo, issueRequest)
	return err
}

func sanitize(log string) string {
	// Redact paths or sensitive tokens if any
	sanitized := strings.ReplaceAll(log, "/tmp/", "[TMP]/")
	return sanitized
}
