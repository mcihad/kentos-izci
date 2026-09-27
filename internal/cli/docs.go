package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mcihad/kentos-izci/docs"
)

func newDocsCmd(a *cliApp) *cobra.Command {
	cmd := &cobra.Command{Use: "docs", Short: "Documentation helpers"}
	var full bool
	llms := &cobra.Command{
		Use:   "llms",
		Short: "Print llms.txt (or llms-full.txt with --full) with links to this server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := a.load(); err != nil {
				return err
			}
			_, err := fmt.Fprint(cmd.OutOrStdout(), docs.LLMs(a.cfg.PublicURL, full))
			return err
		},
	}
	llms.Flags().BoolVar(&full, "full", false, "include every document instead of an index")
	cmd.AddCommand(llms)
	return cmd
}
