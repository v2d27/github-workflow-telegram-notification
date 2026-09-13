package github

// WorkflowRunEvent is the subset of GitHub's workflow_run webhook payload
// (https://docs.github.com/en/webhooks/webhook-events-and-payloads#workflow_run)
// this worker needs. Most reporting fields (conclusion, branch, commit,
// who triggered/reran/cancelled the run) are carried directly in the
// payload; only per-job/step failure detail and commit-author/PR-merge
// identities require calling back into the GitHub API (see api_client.go).
type WorkflowRunEvent struct {
	Action      string      `json:"action"`
	WorkflowRun WorkflowRun `json:"workflow_run"`
	Repository  Repository  `json:"repository"`
	Sender      Actor       `json:"sender"`
}

type WorkflowRun struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	RunNumber       int64      `json:"run_number"`
	RunAttempt      int64      `json:"run_attempt"`
	Status          string     `json:"status"`
	Conclusion      string     `json:"conclusion"`
	HTMLURL         string     `json:"html_url"`
	HeadBranch      string     `json:"head_branch"`
	HeadSHA         string     `json:"head_sha"`
	HeadCommit      HeadCommit `json:"head_commit"`
	Actor           Actor      `json:"actor"`
	TriggeringActor Actor      `json:"triggering_actor"`
	CreatedAt       string     `json:"created_at"`
	UpdatedAt       string     `json:"updated_at"`
}

// HeadCommit carries the git-level (not GitHub-account-level) identity of
// the commit. Author is a fallback for CommitAuthorLogin in RunDetails when
// the commit author's email isn't linked to a GitHub account. Message is a
// fallback title for the commit line when there's no associated PR title.
type HeadCommit struct {
	ID      string         `json:"id"`
	Message string         `json:"message"`
	Author  CommitIdentity `json:"author"`
}

type CommitIdentity struct {
	Name string `json:"name"`
}

type Actor struct {
	Login string `json:"login"`
}

type Repository struct {
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
}
