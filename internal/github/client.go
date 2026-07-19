package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrAmbiguousWrite = errors.New("GitHub write outcome is ambiguous and must be reconciled")

type Options struct {
	BaseURL    string
	Token      string
	Timeout    time.Duration
	MaxRetries int
	HTTPClient *http.Client
}

type Client struct {
	baseURL, token string
	maxRetries     int
	http           *http.Client
}

type Repository struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Description   string `json:"description"`
	HTMLURL       string `json:"html_url"`
}

type Release struct {
	ID          int64     `json:"id"`
	Tag         string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
}

type CreateIssueRequest struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels,omitempty"`
}

type Issue struct {
	ID      int64  `json:"id"`
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	State   string `json:"state"`
}

func NewClient(options Options) (*Client, error) {
	parsed, err := url.Parse(options.BaseURL)
	if err != nil || parsed.Host == "" {
		return nil, errors.New("GitHub base URL is invalid")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback(parsed.Hostname())) {
		return nil, errors.New("GitHub base URL must use HTTPS unless it is loopback")
	}
	if options.Timeout <= 0 || options.MaxRetries < 0 || options.MaxRetries > 5 {
		return nil, errors.New("GitHub timeout or retry options are invalid")
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: options.Timeout}
	}
	return &Client{baseURL: strings.TrimRight(options.BaseURL, "/"), token: options.Token, maxRetries: options.MaxRetries, http: httpClient}, nil
}

func (c *Client) Repository(ctx context.Context, repository string) (Repository, error) {
	path, err := repositoryPath(repository)
	if err != nil {
		return Repository{}, err
	}
	var result Repository
	if err := c.read(ctx, "/repos/"+path, nil, &result); err != nil {
		return Repository{}, err
	}
	return result, nil
}

func (c *Client) LatestPublishedRelease(ctx context.Context, repository string) (Release, error) {
	path, err := repositoryPath(repository)
	if err != nil {
		return Release{}, err
	}
	var releases []Release
	if err := c.read(ctx, "/repos/"+path+"/releases", url.Values{"per_page": {"20"}}, &releases); err != nil {
		return Release{}, err
	}
	for _, release := range releases {
		if !release.Draft && !release.PublishedAt.IsZero() {
			return release, nil
		}
	}
	return Release{}, errors.New("no published GitHub release found")
}

func (c *Client) Release(ctx context.Context, repository string, releaseID int64) (Release, error) {
	path, err := repositoryPath(repository)
	if err != nil {
		return Release{}, err
	}
	if releaseID <= 0 {
		return Release{}, errors.New("release id must be positive")
	}
	var result Release
	if err := c.read(ctx, fmt.Sprintf("/repos/%s/releases/%d", path, releaseID), nil, &result); err != nil {
		return Release{}, err
	}
	if result.Draft {
		return Release{}, errors.New("draft releases are not marketable inputs")
	}
	return result, nil
}

func (c *Client) ReadFile(ctx context.Context, repository, filePath, ref string) ([]byte, error) {
	repositoryURLPath, err := repositoryPath(repository)
	if err != nil {
		return nil, err
	}
	fileURLPath, err := safeFilePath(filePath)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if ref != "" {
		query.Set("ref", ref)
	}
	var response struct {
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	if err := c.read(ctx, "/repos/"+repositoryURLPath+"/contents/"+fileURLPath, query, &response); err != nil {
		return nil, err
	}
	if response.Encoding != "base64" {
		return nil, fmt.Errorf("unsupported GitHub file encoding %q", response.Encoding)
	}
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(response.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("decode GitHub file: %w", err)
	}
	if len(content) > 2*1024*1024 {
		return nil, errors.New("GitHub file exceeds 2 MiB limit")
	}
	return content, nil
}

func (c *Client) CreateIssue(ctx context.Context, repository string, request CreateIssueRequest) (Issue, error) {
	path, err := repositoryPath(repository)
	if err != nil {
		return Issue{}, err
	}
	if strings.TrimSpace(request.Title) == "" || strings.TrimSpace(request.Body) == "" {
		return Issue{}, errors.New("issue title and body are required")
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return Issue{}, err
	}
	var issue Issue
	if err := c.do(ctx, http.MethodPost, "/repos/"+path+"/issues", nil, encoded, &issue, false); err != nil {
		return Issue{}, fmt.Errorf("%w: %v", ErrAmbiguousWrite, err)
	}
	return issue, nil
}

func (c *Client) FindIssueByMarker(ctx context.Context, repository, marker string) (Issue, bool, error) {
	path, err := repositoryPath(repository)
	if err != nil {
		return Issue{}, false, err
	}
	if strings.TrimSpace(marker) == "" {
		return Issue{}, false, errors.New("approval marker is required")
	}
	for page := 1; page <= 10; page++ {
		var response []struct {
			Issue
			PullRequest json.RawMessage `json:"pull_request"`
		}
		query := url.Values{"state": {"all"}, "per_page": {"100"}, "page": {strconv.Itoa(page)}}
		if err := c.read(ctx, "/repos/"+path+"/issues", query, &response); err != nil {
			return Issue{}, false, err
		}
		for _, candidate := range response {
			if len(candidate.PullRequest) != 0 && string(candidate.PullRequest) != "null" {
				continue
			}
			if strings.Contains(candidate.Body, marker) {
				return candidate.Issue, true, nil
			}
		}
		if len(response) < 100 {
			return Issue{}, false, nil
		}
	}
	return Issue{}, false, errors.New("approval issue reconciliation exceeded pagination limit")
}

func (c *Client) read(ctx context.Context, path string, query url.Values, output any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, output, true)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body []byte, output any, retry bool) error {
	attempts := 1
	if retry {
		attempts += c.maxRetries
	}
	var last error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		endpoint := c.baseURL + path
		if len(query) > 0 {
			endpoint += "?" + query.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if len(body) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			last = errors.New("GitHub transport failed")
		} else {
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				defer resp.Body.Close()
				if output == nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					return nil
				}
				if err := json.NewDecoder(io.LimitReader(resp.Body, 4*1024*1024)).Decode(output); err != nil {
					return fmt.Errorf("decode GitHub response: %w", err)
				}
				return nil
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			last = fmt.Errorf("GitHub returned HTTP %d", resp.StatusCode)
			if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
				return last
			}
		}
		if !retry || attempt+1 >= attempts {
			break
		}
		delay := time.Duration(100*(1<<attempt)) * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return last
}

func repositoryPath(repository string) (string, error) {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Contains(repository, "..") {
		return "", errors.New("repository must use safe owner/name format")
	}
	return url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]), nil
}

func safeFilePath(path string) (string, error) {
	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") {
		return "", errors.New("repository file path must be relative")
	}
	parts := strings.Split(path, "/")
	encoded := make([]string, len(parts))
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("repository file path traversal is not allowed")
		}
		encoded[i] = url.PathEscape(part)
	}
	return strings.Join(encoded, "/"), nil
}

func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
