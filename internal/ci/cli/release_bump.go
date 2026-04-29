package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mongodb/mongodb-kubernetes/internal/ci/release"
	"github.com/mongodb/mongodb-kubernetes/internal/ci/runner"
)

const releaseJSONPath = "release.json"

func newReleaseBumpCmd() *cobra.Command {
	var (
		version string
		dryRun  bool
	)
	cmd := &cobra.Command{
		Use:   "bump",
		Short: "Bump release.json mongodbOperator and regenerate release artifacts",
		Long: `Bumps release.json mongodbOperator to --version, runs 'make precommit-full'
to regenerate the Helm chart, manifests, CSV, licenses and RBAC, commits the
result on the current branch, and re-runs 'make precommit-full' as an
idempotency check. On success, leaves a local "Release MCK <version>" commit
ready to push.

Run from a feature branch with a clean worktree.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReleaseBumpCmd(cmd.Context(), version, dryRun)
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "target operator version (required, e.g. 1.8.1)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print actions without executing them")
	_ = cmd.MarkFlagRequired("version")
	return cmd
}

func runReleaseBumpCmd(ctx context.Context, version string, dryRun bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	r := runner.New(dryRun, cwd)

	if err := preflightFromGit(ctx, r, version); err != nil {
		return err
	}
	if err := bumpFiles(ctx, r, version); err != nil {
		return err
	}
	return commitAndVerify(ctx, r, version)
}

// bumpFiles is the worktree-modifying half of a release bump: rewrite
// release.json and run the precommit-full fix-up pass. Caller is responsible
// for preflight (`preflightFromGit`) and for committing + verifying via
// `commitAndVerify`. Splitting it this way lets `release pr` interleave
// dockerfile copies between the worktree changes and the commit so everything
// lands in a single "Release MCK <version>" commit.
func bumpFiles(ctx context.Context, r *runner.Runner, version string) error {
	if err := bumpReleaseJSON(r, version); err != nil {
		return err
	}
	// Pass 1: fix-up. Pre-commit hooks rewrite files in place, so a non-zero
	// exit here is normal — that's the whole point of this pass.
	if err := r.Exec(ctx, "make", "precommit-full"); err != nil {
		fmt.Fprintf(r.LogOut, "→ make precommit-full pass 1 exited non-zero (expected fix-up): %v\n", err)
	}
	return nil
}

// commitAndVerify commits the current worktree as "Release MCK <version>" and
// runs the precommit-full idempotency check (must exit clean AND leave the
// worktree clean against the new commit). On success the branch has one new
// release commit ready to push.
func commitAndVerify(ctx context.Context, r *runner.Runner, version string) error {
	if err := commitReleaseChanges(ctx, r, version); err != nil {
		return err
	}
	// Pass 2: idempotency check. Must exit clean AND leave the worktree clean
	// against the commit we just made. Either condition failing means a real
	// error or non-deterministic regeneration — in both cases the user needs
	// to inspect manually.
	if err := r.Exec(ctx, "make", "precommit-full"); err != nil {
		return fmt.Errorf("make precommit-full (idempotency check): %w", err)
	}
	if r.DryRun {
		return nil
	}
	clean, err := isWorktreeClean(ctx, r)
	if err != nil {
		return fmt.Errorf("check worktree after idempotency pass: %w", err)
	}
	if !clean {
		return fmt.Errorf("precommit-full modified files on a second run; regeneration is not idempotent. Inspect with `git status` / `git diff`")
	}
	return nil
}

func commitReleaseChanges(ctx context.Context, r *runner.Runner, version string) error {
	if r.DryRun {
		fmt.Fprintln(r.LogOut, "[dry-run] would `git add -A` and commit release changes")
		return nil
	}
	clean, err := isWorktreeClean(ctx, r)
	if err != nil {
		return fmt.Errorf("check worktree post-precommit: %w", err)
	}
	if clean {
		fmt.Fprintln(r.LogOut, "→ no changes to commit (release.json already at target and regen produced no diffs)")
		return nil
	}
	if err := r.Exec(ctx, "git", "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	msg := fmt.Sprintf("Release MCK %s", version)
	if err := r.Exec(ctx, "git", "commit", "-m", msg); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

// preflightFromGit collects branch + worktree state from git and validates the
// shared release preconditions. Lives here so any release subcommand can call
// it; takes only what it needs (runner + version) so tests can drive it
// directly via release.PreflightInputs.
func preflightFromGit(ctx context.Context, r *runner.Runner, version string) error {
	branch, err := r.Capture(ctx, "git", "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}
	clean, err := isWorktreeClean(ctx, r)
	if err != nil {
		return fmt.Errorf("check worktree: %w", err)
	}
	return (release.PreflightInputs{
		Branch:        branch,
		WorktreeClean: clean,
		WantVersion:   version,
	}).Validate()
}

func bumpReleaseJSON(r *runner.Runner, version string) error {
	if r.DryRun {
		fmt.Fprintf(r.LogOut, "[dry-run] would bump %s mongodbOperator to %s\n", releaseJSONPath, version)
		return nil
	}
	oldVersion, changed, err := release.BumpOperatorVersion(releaseJSONPath, version)
	if err != nil {
		return fmt.Errorf("bump %s: %w", releaseJSONPath, err)
	}
	if changed {
		fmt.Fprintf(r.LogOut, "→ bumped %s mongodbOperator: %s → %s\n", releaseJSONPath, oldVersion, version)
	} else {
		fmt.Fprintf(r.LogOut, "→ %s mongodbOperator already at %s; skipping write\n", releaseJSONPath, version)
	}
	return nil
}

// isWorktreeClean returns true if both the working tree and the index are
// clean. `git diff --quiet` exits 0 when clean, 1 when dirty; any other
// non-zero is a real error.
func isWorktreeClean(ctx context.Context, r *runner.Runner) (bool, error) {
	for _, args := range [][]string{
		{"diff", "--quiet"},
		{"diff", "--cached", "--quiet"},
	} {
		err := r.CheckExitCode(ctx, "git", args...)
		switch runner.ExitCode(err) {
		case 0:
			continue
		case 1:
			return false, nil
		default:
			return false, err
		}
	}
	return true, nil
}
