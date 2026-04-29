package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/mongodb/mongodb-kubernetes/internal/ci/release"
)

const (
	buildInfoPath          = "build_info.json"
	defaultDockerfilesDest = "public/dockerfiles"
)

func newReleaseDockerfilesCmd() *cobra.Command {
	var (
		version string
		dest    string
		dryRun  bool
	)
	cmd := &cobra.Command{
		Use:   "dockerfiles",
		Short: "Copy release Dockerfiles into <dest>/<image>/<version>/ubi/",
		Long: `Reads build_info.json for each release image's source Dockerfile and copies
each into the public dir layout used for releases:

  <dest>/<image>/<version>/ubi/Dockerfile

By default <dest> is public/dockerfiles. The init-database Dockerfile is
copied to both mongodb-kubernetes-init-database and mongodb-kubernetes-init-appdb.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReleaseDockerfilesCmd(cmd, version, dest, dryRun)
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "release version (required), e.g. 1.8.1")
	cmd.Flags().StringVar(&dest, "dest", defaultDockerfilesDest, "target root directory")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print actions without writing files")
	_ = cmd.MarkFlagRequired("version")
	return cmd
}

func runReleaseDockerfilesCmd(cmd *cobra.Command, version, dest string, dryRun bool) error {
	return copyDockerfilesPlanned(cmd.ErrOrStderr(), version, dest, dryRun)
}

// copyDockerfilesPlanned plans + copies the release Dockerfiles, logging each
// step to out. Shared by `release dockerfiles` (cmd) and `release pr` (orchestrator)
// so the same logging and dry-run semantics apply in both contexts.
func copyDockerfilesPlanned(out io.Writer, version, dest string, dryRun bool) error {
	bi, err := release.ReadBuildInfo(buildInfoPath)
	if err != nil {
		return err
	}
	plan, err := release.PlanDockerfileCopies(bi, version, dest)
	if err != nil {
		return err
	}
	prefix := "→ copy"
	if dryRun {
		prefix = "[dry-run] would copy"
	}
	for _, p := range plan {
		fmt.Fprintf(out, "%s %s -> %s\n", prefix, p.Src, p.Dst)
	}
	if !dryRun {
		if err := release.CopyDockerfiles(plan); err != nil {
			return err
		}
	}
	verb := "copied"
	if dryRun {
		verb = "would copy"
	}
	fmt.Fprintf(out, "→ %s %d Dockerfile(s) for version %s\n", verb, len(plan), version)
	return nil
}
