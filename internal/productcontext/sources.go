package productcontext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/omerufuk/marketing-os/internal/domain"
	gh "github.com/omerufuk/marketing-os/internal/github"
)

type GitHubReader interface {
	Repository(context.Context, string) (gh.Repository, error)
	ReadFile(context.Context, string, string, string) ([]byte, error)
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type sourceRecord struct {
	Type, URL, ExternalID, Content string
	Metadata                       map[string]any
}

func (s *Service) collectSources(ctx context.Context, product domain.Product) ([]sourceRecord, []string, error) {
	publicProduct := map[string]any{"id": product.ID, "name": product.Name, "repository": product.Repository, "website": product.Website, "documentation_url": product.DocumentationURL, "pricing_url": product.PricingURL, "changelog_url": product.ChangelogURL, "product_type": product.ProductType, "primary_conversion_action": product.PrimaryConversionAction, "default_language": product.DefaultLanguage}
	configJSON, _ := json.MarshalIndent(publicProduct, "", "  ")
	sources := []sourceRecord{{Type: "product_config", URL: "product://" + product.ID, ExternalID: "product-config", Content: string(configJSON), Metadata: map[string]any{"kind": "registration"}}}
	var warnings []string
	if product.LocalRepository != "" {
		local, localWarnings, err := collectLocalRepository(product.LocalRepository)
		if err != nil {
			return nil, nil, err
		}
		sources = append(sources, local...)
		warnings = append(warnings, localWarnings...)
	} else if product.Repository != "" && s.GitHub != nil {
		repository, err := s.GitHub.Repository(ctx, product.Repository)
		if err != nil {
			warnings = append(warnings, "GitHub repository metadata could not be read: "+err.Error())
		} else {
			for _, name := range []string{"README.md", "CHANGELOG.md"} {
				content, readErr := s.GitHub.ReadFile(ctx, product.Repository, name, repository.DefaultBranch)
				if readErr != nil {
					warnings = append(warnings, name+" could not be read from GitHub")
					continue
				}
				sources = append(sources, sourceRecord{Type: "github_file", URL: repository.HTMLURL + "/blob/" + repository.DefaultBranch + "/" + name, ExternalID: "github:" + repository.DefaultBranch + ":" + name, Content: boundedText(content, 256*1024), Metadata: map[string]any{"path": name, "ref": repository.DefaultBranch}})
			}
		}
	}
	client := s.HTTP
	if client == nil {
		client = &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(request *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return errors.New("source redirect limit exceeded")
				}
				if err := validateSourceURL(request.URL); err != nil {
					return fmt.Errorf("unsafe source redirect: %w", err)
				}
				return nil
			},
		}
	}
	for name, rawURL := range map[string]string{"website": product.Website, "documentation": product.DocumentationURL, "pricing": product.PricingURL, "changelog": product.ChangelogURL} {
		if strings.TrimSpace(rawURL) == "" {
			continue
		}
		content, err := fetchSourceURL(ctx, client, rawURL)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s source could not be fetched: %v", name, err))
			continue
		}
		sources = append(sources, sourceRecord{Type: "web_page", URL: rawURL, ExternalID: "url:" + rawURL, Content: content, Metadata: map[string]any{"kind": name}})
	}
	return sources, warnings, nil
}

func collectLocalRepository(root string) ([]sourceRecord, []string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	resolvedRoot, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve local repository: %w", err)
	}
	candidates := []string{"README.md", "README", "CHANGELOG.md", filepath.Join("docs", "README.md"), filepath.Join("docs", "index.md")}
	var result []sourceRecord
	for _, candidate := range candidates {
		path := filepath.Join(resolvedRoot, candidate)
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, nil, err
		}
		rel, err := filepath.Rel(resolvedRoot, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, nil, errors.New("repository source path escapes configured root")
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		content, err := os.ReadFile(resolved)
		if err != nil {
			return nil, nil, err
		}
		result = append(result, sourceRecord{Type: "local_repository_file", URL: "file://" + filepath.ToSlash(candidate), ExternalID: "local:" + filepath.ToSlash(candidate), Content: boundedText(content, 256*1024), Metadata: map[string]any{"path": filepath.ToSlash(candidate)}})
	}
	warnings := []string{}
	if len(result) == 0 {
		warnings = append(warnings, "No allowlisted README, changelog, or docs index file was found in the local repository.")
	}
	return result, warnings, nil
}

func fetchSourceURL(ctx context.Context, client HTTPDoer, rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if err := validateSourceURL(parsed); err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "marketing-os/1.0")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
	if err != nil {
		return "", err
	}
	if len(body) > 2*1024*1024 {
		return "", errors.New("source page exceeds 2 MiB")
	}
	contentType := response.Header.Get("Content-Type")
	if strings.Contains(contentType, "html") || strings.Contains(strings.ToLower(string(body[:min(len(body), 256)])), "<html") {
		return htmlToText(string(body)), nil
	}
	return boundedText(body, 512*1024), nil
}

func validateSourceURL(parsed *url.URL) error {
	if parsed == nil || parsed.Host == "" {
		return errors.New("source URL is invalid")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	host := parsed.Hostname()
	if parsed.Scheme == "http" && (host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()) {
		return nil
	}
	return errors.New("source URL must use HTTPS unless it is loopback")
}

var scriptPattern = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>|<style[^>]*>.*?</style>|<noscript[^>]*>.*?</noscript>`)
var tagPattern = regexp.MustCompile(`(?s)<[^>]+>`)
var whitespacePattern = regexp.MustCompile(`[ \t\f\v]+`)
var blankPattern = regexp.MustCompile(`\n{3,}`)

func htmlToText(value string) string {
	value = scriptPattern.ReplaceAllString(value, " ")
	value = strings.ReplaceAll(value, "</p>", "\n")
	value = strings.ReplaceAll(value, "<br>", "\n")
	value = tagPattern.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	value = whitespacePattern.ReplaceAllString(value, " ")
	value = blankPattern.ReplaceAllString(value, "\n\n")
	return strings.TrimSpace(value)
}
func boundedText(content []byte, limit int) string {
	if len(content) > limit {
		content = content[:limit]
	}
	return strings.TrimSpace(string(content))
}
