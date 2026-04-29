package cli

import "github.com/spf13/cobra"

// newReleaseCmd builds the `release` subcommand group. Its sole purpose is to
// host release-automation subcommands; running it bare prints help.
func newReleaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "release",
		Short:        "Release-automation subcommands",
		Long:         "Subcommands for cutting an MCK release. They operate on the current branch and assume a clean worktree.",
		SilenceUsage: true,
		RunE: func(c *cobra.Command, _ []string) error {
			return c.Help()
		},
	}
	cmd.AddCommand(newReleaseBumpCmd())
	cmd.AddCommand(newReleaseDockerfilesCmd())
	return cmd
}
