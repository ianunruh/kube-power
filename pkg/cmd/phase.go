package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ianunruh/kube-power/pkg/controller"
)

var phaseCmd = &cobra.Command{
	Use:   "phase [name to run]",
	Short: "Executes a single phase and waits until its completion",
	Long: `Executes a single phase and waits until its completion.

Available phases:

* IsCephHealthOK
* ResumeAllDeploys
* ResumeCephConsumers
* ResumeCephDaemons
* ResumeCephOperator
* ResumeCephRGW
* ResumeCronJobs
* ResumeOperators
* SetCephFlags
* SuspendCephConsumers
* SuspendCephDaemons
* SuspendCronJobs
* SuspendOperators
* UnsetCephFlags
* WaitForCephHealthOK`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "IsCephHealthOK":
			ok, err := ctrl.IsCephHealthOK()
			fmt.Printf("Ceph HEALTH_OK: %v\n", ok)
			return err
		case "ResumeAllDeploys":
			return ctrl.ResumeDeploys(controller.SuspendedSelector())
		case "ResumeCephConsumers":
			return ctrl.ResumeCephConsumers()
		case "ResumeCephDaemons":
			return ctrl.ResumeCephDaemons()
		case "ResumeCephOperator":
			return ctrl.ResumeCephOperator()
		case "ResumeCephRGW":
			return ctrl.ResumeCephRGW()
		case "ResumeCronJobs":
			return ctrl.ResumeCronJobs()
		case "ResumeOperators":
			return ctrl.ResumeOperators()
		case "SetCephFlags":
			return ctrl.UpdateCephFlags(true)
		case "SuspendCephConsumers":
			return ctrl.SuspendCephConsumers()
		case "SuspendCephDaemons":
			return ctrl.SuspendCephDaemons()
		case "SuspendCronJobs":
			return ctrl.SuspendCronJobs()
		case "SuspendOperators":
			return ctrl.SuspendOperators()
		case "UnsetCephFlags":
			return ctrl.UpdateCephFlags(false)
		case "WaitForCephHealthOK":
			return ctrl.WaitForCephHealthOK()
		default:
			return errors.New("unknown phase")
		}
	},
}
