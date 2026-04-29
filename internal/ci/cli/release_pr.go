package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mongodb/mongodb-kubernetes/internal/ci/gitops"
	"github.com/mongodb/mongodb-kubernetes/internal/ci/release"
	"github.com/mongodb/mongodb-kubernetes/internal/ci/runner"
)

func newReleasePRCmd() *cobra.Command {
	var (
		version      string
		draft        bool
		dryRun       bool
		repoOverride string
	)
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Bump release.json, copy Dockerfiles, commit, push, and open a release PR",
		Long: `Runs the full release-PR pipeline on the current branch:

  1. Bump release.json mongodbOperator to --version
  2. 'make precommit-full' (fix-up pass) — regenerate Helm chart, manifests,
     CSV, licenses and RBAC
  3. Copy release Dockerfiles to public/dockerfiles/<image>/<version>/ubi/
  4. Commit "Release MCK <version>" (single commit, all changes)
  5. 'make precommit-full' (idempotency check) — must exit clean
  6. git push -u origin <branch>
  7. gh pr create

By default the PR targets the same GitHub repo as origin (so a fork's origin
yields a fork-internal PR). Use --repo owner/repo to override.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReleasePRCmd(cmd.Context(), version, draft, dryRun, repoOverride)
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "target operator version (required, e.g. 1.8.1)")
	cmd.Flags().BoolVar(&draft, "draft", false, "open the PR as draft")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print actions without executing them")
	cmd.Flags().StringVar(&repoOverride, "repo", "", "target repo for the PR (owner/repo); defaults to origin")
	_ = cmd.MarkFlagRequired("version")
	return cmd
}

func runReleasePRCmd(ctx context.Context, version string, draft, dryRun bool, repoOverride string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	r := runner.New(dryRun, cwd)

	if err := preflightFromGit(ctx, r, version); err != nil {
		return err
	}

	branch, err := r.Capture(ctx, "git", "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}
	originRepo, err := gitops.DetectOriginRepo(ctx, r)
	if err != nil {
		return fmt.Errorf("detect origin repo (use --repo to override): %w", err)
	}
	headOwner := gitops.OwnerFromRepo(originRepo)
	if headOwner == "" {
		return fmt.Errorf("could not parse owner from origin repo %q", originRepo)
	}
	targetRepo := repoOverride
	if targetRepo == "" {
		targetRepo = originRepo
	}
	fmt.Fprintf(r.LogOut, "→ PR target: %s (head %s:%s)\n", targetRepo, headOwner, branch)

	if err := bumpFiles(ctx, r, version); err != nil {
		return err
	}
	if err := copyDockerfilesPlanned(r.LogOut, version, defaultDockerfilesDest, dryRun); err != nil {
		return err
	}
	if err := commitAndVerify(ctx, r, version); err != nil {
		return err
	}

	if err := r.Exec(ctx, "git", "push", "-u", "origin", branch); err != nil {
		return fmt.Errorf("git push: %w", err)
	}

	body, err := release.RenderPRBody(version)
	if err != nil {
		return err
	}
	ghArgs := []string{
		"pr", "create",
		"--repo", targetRepo,
		"--base", "master",
		"--head", headOwner + ":" + branch,
		"--title", release.PRTitle(version),
		"--body", body,
	}
	if draft {
		ghArgs = append(ghArgs, "--draft")
	}
	if err := r.Exec(ctx, "gh", ghArgs...); err != nil {
		return fmt.Errorf("gh pr create: %w", err)
	}
	return nil
}
