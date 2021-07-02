package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/ianunruh/kube-power/pkg/controller"
)

var (
	dryRun         = false
	kubeConfigPath = ""

	ctrl *controller.Controller
)

var rootCmd = &cobra.Command{
	Use:   "kube-power",
	Short: "Tool for suspending and resuming Kubernetes workloads for power maintenace",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		log, err := zap.NewDevelopment()
		if err != nil {
			return err
		}

		restConfig, err := controller.LoadKubeConfig(kubeConfigPath)
		if err != nil {
			return err
		}

		ctrl, err = controller.NewController(dryRun, restConfig, log)
		if err != nil {
			return err
		}

		ctrl.WaitForCacheSync()
		log.Debug("Controller cache synced")

		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "Plan actions but skip performing them")
	rootCmd.PersistentFlags().StringVar(&kubeConfigPath, "kubeconfig", "", "Path to kube config")

	rootCmd.AddCommand(resumeCmd)
	rootCmd.AddCommand(phaseCmd)
	rootCmd.AddCommand(suspendCmd)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
