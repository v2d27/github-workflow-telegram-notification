package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/syumai/workers-go/cloudflare/fetch"
)

const apiBase = "https://api.github.com"

// AppClient authenticates as a GitHub App installation to fetch details the
// workflow_run webhook payload doesn't carry: which jobs/steps failed, and
// who authored/merged/requested the underlying commit or pull request.
type AppClient struct {
	appID          string
	installationID string
	privateKeyPEM  string
	http           *fetch.Client
}

func NewAppClient(appID, installationID, privateKeyPEM string) *AppClient {
	return &AppClient{
		appID:          appID,
		installationID: installationID,
		privateKeyPEM:  privateKeyPEM,
		http:           fetch.NewClient(),
	}
}

// RunDetails enriches a workflow_run event with data only available from
// the GitHub REST API.
type RunDetails struct {
	FailedJobs            []FailedJob
	CommitAuthorLogin     string
	CommitAuthorAvatarURL string
	PRNumber              int64
	PRTitle               string
	PRRequesterLogin      string
	PRRequesterAvatarURL  string
	PRMerged              bool
	PRMergedByLogin       string
}

// OwnerAvatarURL returns the avatar of whoever "owns" this run: the PR
// requester when the commit belongs to a pull request, otherwise the
// commit author. size requests a resized square image via the "s" query
// parameter honored by both GitHub's avatar service and Gravatar (the
// fallback for accounts without a GitHub-hosted avatar); size <= 0 leaves
// the URL unmodified.
func (d *RunDetails) OwnerAvatarURL(size int) string {
	if d == nil {
		return ""
	}
	raw := d.CommitAuthorAvatarURL
	if d.PRNumber > 0 && d.PRRequesterAvatarURL != "" {
		raw = d.PRRequesterAvatarURL
	}
	if raw == "" || size <= 0 {
		return raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("s", strconv.Itoa(size))
	u.RawQuery = q.Encode()
	return u.String()
}

type FailedJob struct {
	Name  string
	Steps []string
}

// FetchRunDetails calls the GitHub API for the given run/commit. includeJobs
// should be false for successful runs, since they have no failed jobs to
// report and the call would just be wasted.
func (c *AppClient) FetchRunDetails(ctx context.Context, repoFullName string, runID int64, headSHA string, includeJobs bool) (*RunDetails, error) {
	token, err := c.installationToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("get installation token: %w", err)
	}

	details := &RunDetails{}

	if includeJobs {
		var jr jobsResponse
		url := fmt.Sprintf("%s/repos/%s/actions/runs/%d/jobs?per_page=100", apiBase, repoFullName, runID)
		if err := c.get(ctx, token, url, &jr); err != nil {
			return nil, fmt.Errorf("list jobs: %w", err)
		}
		for _, j := range jr.Jobs {
			if j.Conclusion != "failure" {
				continue
			}
			fj := FailedJob{Name: j.Name}
			for _, s := range j.Steps {
				if s.Conclusion == "failure" {
					fj.Steps = append(fj.Steps, s.Name)
				}
			}
			details.FailedJobs = append(details.FailedJobs, fj)
		}
	}

	var commit commitResponse
	commitURL := fmt.Sprintf("%s/repos/%s/commits/%s", apiBase, repoFullName, headSHA)
	if err := c.get(ctx, token, commitURL, &commit); err != nil {
		return nil, fmt.Errorf("get commit: %w", err)
	}
	if commit.Author != nil {
		details.CommitAuthorLogin = commit.Author.Login
		details.CommitAuthorAvatarURL = commit.Author.AvatarURL
	}

	var pulls []pullSummary
	pullsURL := fmt.Sprintf("%s/repos/%s/commits/%s/pulls", apiBase, repoFullName, headSHA)
	if err := c.get(ctx, token, pullsURL, &pulls); err != nil {
		return nil, fmt.Errorf("list associated pulls: %w", err)
	}
	if len(pulls) > 0 {
		p := pulls[0]
		details.PRNumber = p.Number
		details.PRTitle = p.Title
		details.PRRequesterLogin = p.User.Login
		details.PRRequesterAvatarURL = p.User.AvatarURL

		if p.MergedAt != nil {
			var pd pullDetail
			detailURL := fmt.Sprintf("%s/repos/%s/pulls/%d", apiBase, repoFullName, p.Number)
			if err := c.get(ctx, token, detailURL, &pd); err != nil {
				return nil, fmt.Errorf("get pull #%d: %w", p.Number, err)
			}
			if pd.Merged && pd.MergedBy != nil {
				details.PRMerged = true
				details.PRMergedByLogin = pd.MergedBy.Login
			}
		}
	}

	return details, nil
}

func (c *AppClient) installationToken(ctx context.Context) (string, error) {
	jwt, err := generateAppJWT(c.appID, c.privateKeyPEM)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/app/installations/%s/access_tokens", apiBase, c.installationID)
	req, err := fetch.NewRequest(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "github-workflow-telegram-notification")

	res, err := c.http.Do(req, nil)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		body, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("unexpected status %d: %s", res.StatusCode, string(body))
	}

	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Token, nil
}

func (c *AppClient) get(ctx context.Context, token, url string, out any) error {
	req, err := fetch.NewRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "github-workflow-telegram-notification")

	res, err := c.http.Do(req, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf("GET %s: unexpected status %d: %s", url, res.StatusCode, string(body))
	}
	return json.NewDecoder(res.Body).Decode(out)
}

type jobsResponse struct {
	Jobs []jobItem `json:"jobs"`
}

type jobItem struct {
	Name       string     `json:"name"`
	Conclusion string     `json:"conclusion"`
	Steps      []stepItem `json:"steps"`
}

type stepItem struct {
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
}

type commitResponse struct {
	Author *ghUser `json:"author"`
}

type ghUser struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

type pullSummary struct {
	Number   int64   `json:"number"`
	Title    string  `json:"title"`
	User     ghUser  `json:"user"`
	MergedAt *string `json:"merged_at"`
}

type pullDetail struct {
	Merged   bool    `json:"merged"`
	MergedBy *ghUser `json:"merged_by"`
}
