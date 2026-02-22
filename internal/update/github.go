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

	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

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

	hasUpdate := latest != "" && latest != current

	return Result{
		HasUpdate: hasUpdate,
		Version:   latest,
		URL:       release.HTMLURL,
		Notes:     release.Body,
	}, nil
}

func normalize(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	return v
}
