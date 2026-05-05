package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/mongodb/mongodb-kubernetes/internal/ci/release"
	"github.com/mongodb/mongodb-kubernetes/internal/ci/runner"
	"github.com/spf13/cobra"
)

func newReleasePRCmd() *cobra.Command {
	var version string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Open a release PR that appends the version to release.json supported image lists",
		RunE: func(cmd *cobra.Command, _ []string) error {
			prURL, err := release.ReleasePR(
				release.PRInputs{Version: version, DryRun: dryRun},
				&ghPROpener{dryRun: dryRun},
			)
			if err != nil {
				return err
			}
			if prURL != "" {
				fmt.Fprintln(cmd.OutOrStdout(), prURL)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&version, "version", "", "release version to cut (required)")
	_ = cmd.MarkFlagRequired("version")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would happen without making changes")

	return cmd
}

// ghPROpener implements PROpener: branches, commits, pushes, then opens the PR via gh.
type ghPROpener struct{ dryRun bool }

func (g *ghPROpener) Open(repoRoot, branch, title, body string) (string, error) {
	ctx := context.Background()
	r := runner.New(g.dryRun, repoRoot)

	for _, args := range [][]string{
		{"checkout", "-b", branch},
		{"add", "release.json"},
		{"commit", "-m", title},
		{"push", "-f", "-u", "origin", branch},
	} {
		if err := r.Exec(ctx, "git", args...); err != nil {
			return "", fmt.Errorf("git %v: %w", args, err)
		}
	}

	repo, err := repoFromOrigin(r)
	if err != nil {
		return "", err
	}

	ghArgs := []string{"pr", "create", "--repo", repo, "--title", title, "--body", body, "--label", "skip-changelog"}
	if g.dryRun {
		_ = r.Exec(ctx, "gh", ghArgs...)
		return "", nil
	}
	prURL, err := r.Capture(ctx, "gh", ghArgs...)
	if err != nil {
		return "", fmt.Errorf("gh pr create: %w", err)
	}
	return prURL, nil
}

// repoFromOrigin extracts "owner/repo" from the origin remote URL,
// supporting both HTTPS and SSH GitHub remotes.
func repoFromOrigin(r *runner.Runner) (string, error) {
	u, err := r.Capture(context.Background(), "git", "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("git remote get-url origin: %w", err)
	}
	u = strings.TrimSuffix(u, ".git")
	for _, prefix := range []string{"github.com/", "github.com:"} {
		if _, after, ok := strings.Cut(u, prefix); ok {
			return after, nil
		}
	}
	return "", fmt.Errorf("could not parse GitHub repo from origin URL %q", u)
}
