package telegram

import (
	"fmt"
	"html"
	"strings"

	ghevents "github.com/v2d27/github-workflow-telegram-notification/internal/github"
)

const (
	maxFailedJobsShown  = 3
	maxFailedStepsShown = 3
)

// FormatWorkflowRun renders a workflow_run event as a 4-6 line HTML-formatted
// Telegram message (see ParseMode in client.go), plus the avatar URL of
// whoever owns the run (the PR requester, or the commit author if there's no
// associated PR) for use as a photo attachment. details may be nil if the
// GitHub API enrichment call failed; the message (and avatar) degrade
// gracefully in that case rather than blocking the notification.
func FormatWorkflowRun(repository string, run ghevents.WorkflowRun, sender ghevents.Actor, details *ghevents.RunDetails) (text, avatarURL string) {
	var lines []string

	lines = append(lines, fmt.Sprintf(
		"%s <b>%s</b> #%d — %s",
		conclusionEmoji(run.Conclusion),
		html.EscapeString(run.Name),
		run.RunNumber,
		html.EscapeString(run.Conclusion),
	))

	lines = append(lines, fmt.Sprintf(
		"%s · <code>%s</code>",
		html.EscapeString(repository),
		html.EscapeString(run.HeadBranch),
	))

	lines = append(lines, commitLine(repository, run, details))

	if failed := failedJobsLine(details); failed != "" {
		lines = append(lines, failed)
	}

	lines = append(lines, triggerLine(run, sender))
	lines = append(lines, fmt.Sprintf(
		`<a href="%s">View run</a> · <a href="%s">View changes</a>`,
		run.HTMLURL,
		changesURL(repository, run.HeadSHA, details),
	))

	return strings.Join(lines, "\n"), details.OwnerAvatarURL()
}

// changesURL points at the most useful diff view: the PR's "Files changed"
// tab when the run's commit belongs to one, otherwise the commit's own diff.
func changesURL(repository, headSHA string, details *ghevents.RunDetails) string {
	if details != nil && details.PRNumber > 0 {
		return fmt.Sprintf("https://github.com/%s/pull/%d/files", repository, details.PRNumber)
	}
	return fmt.Sprintf("https://github.com/%s/commit/%s", repository, headSHA)
}

// commitLine shows the PR/commit title as a link straight to its diff on
// GitHub, so tapping it answers "what changed" without needing the
// View changes link at the bottom.
func commitLine(repository string, run ghevents.WorkflowRun, details *ghevents.RunDetails) string {
	title := commitTitle(run.HeadCommit.Message)
	authorLogin := ""
	if details != nil {
		if details.PRTitle != "" {
			title = details.PRTitle
		}
		authorLogin = details.CommitAuthorLogin
	}

	line := fmt.Sprintf(
		`<b><a href="%s">%s</a></b> by %s`,
		changesURL(repository, run.HeadSHA, details),
		html.EscapeString(title),
		formatUser(authorLogin, run.HeadCommit.Author.Name),
	)

	if details == nil || details.PRNumber == 0 {
		return line
	}

	line += fmt.Sprintf(" · PR #%d by %s", details.PRNumber, formatUser(details.PRRequesterLogin, ""))
	if details.PRMerged {
		line += ", merged by " + formatUser(details.PRMergedByLogin, "")
	}
	return line
}

// commitTitle returns the first line of a git commit message (its summary).
func commitTitle(message string) string {
	if i := strings.IndexByte(message, '\n'); i >= 0 {
		return message[:i]
	}
	return message
}

func failedJobsLine(details *ghevents.RunDetails) string {
	if details == nil || len(details.FailedJobs) == 0 {
		return ""
	}

	jobs := details.FailedJobs
	shown := jobs
	truncatedJobs := 0
	if len(jobs) > maxFailedJobsShown {
		shown = jobs[:maxFailedJobsShown]
		truncatedJobs = len(jobs) - maxFailedJobsShown
	}

	parts := make([]string, 0, len(shown))
	for _, job := range shown {
		entry := html.EscapeString(job.Name)
		if len(job.Steps) > 0 {
			steps := job.Steps
			truncatedSteps := 0
			if len(steps) > maxFailedStepsShown {
				steps = steps[:maxFailedStepsShown]
				truncatedSteps = len(job.Steps) - maxFailedStepsShown
			}
			stepNames := make([]string, len(steps))
			for i, s := range steps {
				stepNames[i] = html.EscapeString(s)
			}
			stepList := strings.Join(stepNames, ", ")
			if truncatedSteps > 0 {
				stepList += fmt.Sprintf(" (+%d more)", truncatedSteps)
			}
			entry += " ▸ " + stepList
		}
		parts = append(parts, entry)
	}

	line := "Failed: " + strings.Join(parts, "; ")
	if truncatedJobs > 0 {
		line += fmt.Sprintf(" (+%d more jobs)", truncatedJobs)
	}
	return line
}

func triggerLine(run ghevents.WorkflowRun, sender ghevents.Actor) string {
	if run.Conclusion == "cancelled" {
		who := sender.Login
		if who == "" {
			who = run.TriggeringActor.Login
		}
		return "Cancelled by " + formatUser(who, "")
	}

	who := run.TriggeringActor.Login
	if who == "" {
		who = run.Actor.Login
	}
	line := "Triggered by " + formatUser(who, "")
	if run.RunAttempt > 1 {
		line += fmt.Sprintf(" (rerun #%d)", run.RunAttempt)
	}
	return line
}

func formatUser(login, fallbackName string) string {
	if login != "" {
		return "<code>@" + html.EscapeString(login) + "</code>"
	}

	if fallbackName != "" {
		return "<code>" + html.EscapeString(fallbackName) + "</code>"
	}

	return "<code>unknown</code>"
}

func conclusionEmoji(conclusion string) string {
	switch conclusion {
	case "success":
		return "✅"
	case "failure", "timed_out":
		return "❌"
	case "cancelled":
		return "🚫"
	case "skipped", "neutral":
		return "⏭️"
	default:
		return "⚠️"
	}
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
