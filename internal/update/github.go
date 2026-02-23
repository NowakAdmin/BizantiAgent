package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/NowakAdmin/BizantiAgent/internal/version"
)

type LatestRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
}

type Result struct {
	HasUpdate bool
	Version   string
	URL       string
	Notes     string
}

func CheckGitHubRelease(ctx context.Context, repo string) (Result, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return Result{}, fmt.Errorf("repozytorium GitHub nie może być puste")
	}

	// Spróbuj releases/latest (published releases)
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	result, err := checkReleaseURL(ctx, url)
	
	// Jeśli nie ma published release (404), spróbuj tags
	if err != nil && strings.Contains(err.Error(), "status: 404") {
		url = fmt.Sprintf("https://api.github.com/repos/%s/tags", repo)
		return checkTagsURL(ctx, url)
	}
	
	return result, err
}

func checkReleaseURL(ctx context.Context, url string) (Result, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Result{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode >= 300 {
		return Result{}, fmt.Errorf("github api zwróciło status: %d", response.StatusCode)
	}

	var release LatestRelease
	if err = json.NewDecoder(response.Body).Decode(&release); err != nil {
		return Result{}, err
	}

	latest := normalize(release.TagName)
	current := normalize(version.Version)

	hasUpdate := isNewerVersion(latest, current)

	return Result{
		HasUpdate: hasUpdate,
		Version:   latest,
		URL:       release.HTMLURL,
		Notes:     release.Body,
	}, nil
}

func checkTagsURL(ctx context.Context, url string) (Result, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Result{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode >= 300 {
		return Result{}, fmt.Errorf("github api zwróciło status: %d", response.StatusCode)
	}

	var tags []struct {
		Name string `json:"name"`
	}
	if err = json.NewDecoder(response.Body).Decode(&tags); err != nil {
		return Result{}, err
	}

	if len(tags) == 0 {
		return Result{}, fmt.Errorf("brak wersji w repozytorium")
	}

	// Pobierz pierwszy (najnowszy) tag
	latest := normalize(tags[0].Name)
	current := normalize(version.Version)

	hasUpdate := isNewerVersion(latest, current)

	return Result{
		HasUpdate: hasUpdate,
		Version:   latest,
		URL:       fmt.Sprintf("https://github.com/%s/releases/tag/%s", extractRepo(url), tags[0].Name),
		Notes:     "",
	}, nil
}

func extractRepo(url string) string {
	// Wyciągnij owner/repo z URL API
	parts := strings.Split(url, "/repos/")
	if len(parts) > 1 {
		repoPath := strings.Split(parts[1], "/")[0:2]
		return strings.Join(repoPath, "/")
	}
	return ""
}

func normalize(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	return v
}

func isNewerVersion(latest, current string) bool {
	if latest == "" || current == "" {
		return false
	}

	latestParts := parseVersion(latest)
	currentParts := parseVersion(current)

	for i := 0; i < 3; i++ {
		if latestParts[i] > currentParts[i] {
			return true
		}
		if latestParts[i] < currentParts[i] {
			return false
		}
	}

	return false
}

func parseVersion(v string) [3]int {
	parts := strings.Split(v, ".")
	result := [3]int{0, 0, 0}

	for i := 0; i < len(parts) && i < 3; i++ {
		value := 0
		for _, ch := range parts[i] {
			if ch < '0' || ch > '9' {
				break
			}
			value = value*10 + int(ch-'0')
		}
		result[i] = value
	}

	return result
}
