package submit

import (
	"github.com/armosec/k8s-interface/k8sinterface"
	"github.com/armosec/kubescape/v2/core/cautils"
	"github.com/armosec/kubescape/v2/core/cautils/getter"
	"github.com/armosec/kubescape/v2/core/cautils/logger"
	"github.com/armosec/kubescape/v2/core/cautils/logger/helpers"
	"github.com/armosec/kubescape/v2/core/meta"
	"github.com/armosec/kubescape/v2/core/meta/cliinterfaces"
	v1 "github.com/armosec/kubescape/v2/core/meta/datastructures/v1"

	reporterv1 "github.com/armosec/kubescape/v2/core/pkg/resultshandling/reporter/v1"

	"github.com/armosec/rbac-utils/rbacscanner"
	"github.com/spf13/cobra"
)

// getRBACCmd represents the RBAC command
func getRBACCmd(ks meta.IKubescape, submitInfo *v1.Submit) *cobra.Command {
	return &cobra.Command{
		Use:   "rbac \nExample:\n$ kubescape submit rbac",
		Short: "Submit cluster's Role-Based Access Control(RBAC)",
		Long:  ``,
		RunE: func(cmd *cobra.Command, args []string) error {

			k8s := k8sinterface.NewKubernetesApi()

			// get config
			clusterConfig := getTenantConfig(&submitInfo.Credentials, "", k8s)
			if err := clusterConfig.SetTenant(); err != nil {
				logger.L().Error("failed setting account ID", helpers.Error(err))
			}

			// list RBAC
			rbacObjects := cautils.NewRBACObjects(rbacscanner.NewRbacScannerFromK8sAPI(k8s, clusterConfig.GetAccountID(), clusterConfig.GetContextName()))

			// submit resources
			r := reporterv1.NewReportEventReceiver(clusterConfig.GetConfigObj())

			submitInterfaces := cliinterfaces.SubmitInterfaces{
				ClusterConfig: clusterConfig,
				SubmitObjects: rbacObjects,
				Reporter:      r,
			}

			if err := ks.Submit(submitInterfaces); err != nil {
				logger.L().Fatal(err.Error())
			}
			return nil
		},
	}

}

// getKubernetesApi
func getKubernetesApi() *k8sinterface.KubernetesApi {
	if !k8sinterface.IsConnectedToCluster() {
		return nil
	}
	return k8sinterface.NewKubernetesApi()
}
func getTenantConfig(credentials *cautils.Credentials, clusterName string, k8s *k8sinterface.KubernetesApi) cautils.ITenantConfig {
	if !k8sinterface.IsConnectedToCluster() || k8s == nil {
		return cautils.NewLocalConfig(getter.GetArmoAPIConnector(), credentials, clusterName)
	}
	return cautils.NewClusterConfig(k8s, getter.GetArmoAPIConnector(), credentials, clusterName)
}
