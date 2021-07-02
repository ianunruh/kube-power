package cmd

import (
	"github.com/spf13/cobra"
)

var resumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resumes workloads in dependency order",
	RunE: func(cmd *cobra.Command, args []string) error {
		return ctrl.ResumeCluster()
	},
}
