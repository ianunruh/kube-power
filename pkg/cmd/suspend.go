package cmd

import (
	"github.com/spf13/cobra"
)

var suspendCmd = &cobra.Command{
	Use:   "suspend",
	Short: "Suspends workloads in dependency order",
	RunE: func(cmd *cobra.Command, args []string) error {
		return ctrl.SuspendCluster()
	},
}
