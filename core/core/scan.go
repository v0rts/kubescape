package core

import (
	"fmt"

	apisv1 "github.com/armosec/opa-utils/httpserver/apis/v1"

	"github.com/armosec/k8s-interface/k8sinterface"

	"github.com/armosec/kubescape/v2/core/cautils"
	"github.com/armosec/kubescape/v2/core/cautils/getter"
	"github.com/armosec/kubescape/v2/core/cautils/logger"
	"github.com/armosec/kubescape/v2/core/cautils/logger/helpers"
	"github.com/armosec/kubescape/v2/core/pkg/hostsensorutils"
	"github.com/armosec/kubescape/v2/core/pkg/opaprocessor"
	"github.com/armosec/kubescape/v2/core/pkg/policyhandler"
	"github.com/armosec/kubescape/v2/core/pkg/resourcehandler"
	"github.com/armosec/kubescape/v2/core/pkg/resultshandling"
	"github.com/armosec/kubescape/v2/core/pkg/resultshandling/printer"
	"github.com/armosec/kubescape/v2/core/pkg/resultshandling/reporter"

	"github.com/armosec/opa-utils/resources"
)

type componentInterfaces struct {
	tenantConfig      cautils.ITenantConfig
	resourceHandler   resourcehandler.IResourceHandler
	report            reporter.IReport
	printerHandler    printer.IPrinter
	hostSensorHandler hostsensorutils.IHostSensor
}

func getInterfaces(scanInfo *cautils.ScanInfo) componentInterfaces {

	// ================== setup k8s interface object ======================================
	var k8s *k8sinterface.KubernetesApi
	if scanInfo.GetScanningEnvironment() == cautils.ScanCluster {
		k8s = getKubernetesApi()
		if k8s == nil {
			logger.L().Fatal("failed connecting to Kubernetes cluster")
		}
	}

	// ================== setup tenant object ======================================

	tenantConfig := getTenantConfig(&scanInfo.Credentials, scanInfo.KubeContext, k8s)

	// Set submit behavior AFTER loading tenant config
	setSubmitBehavior(scanInfo, tenantConfig)

	// Do not submit yaml scanning
	if len(scanInfo.InputPatterns) > 0 {
		scanInfo.Submit = false
	}

	if scanInfo.Submit {
		// submit - Create tenant & Submit report
		if err := tenantConfig.SetTenant(); err != nil {
			logger.L().Error(err.Error())
		}
	}

	// ================== version testing ======================================

	v := cautils.NewIVersionCheckHandler()
	v.CheckLatestVersion(cautils.NewVersionCheckRequest(cautils.BuildNumber, policyIdentifierNames(scanInfo.PolicyIdentifier), "", scanInfo.GetScanningEnvironment()))

	// ================== setup host scanner object ======================================

	hostSensorHandler := getHostSensorHandler(scanInfo, k8s)
	if err := hostSensorHandler.Init(); err != nil {
		logger.L().Error("failed to init host scanner", helpers.Error(err))
		hostSensorHandler = &hostsensorutils.HostSensorHandlerMock{}
	}
	// excluding hostsensor namespace
	if len(scanInfo.IncludeNamespaces) == 0 && hostSensorHandler.GetNamespace() != "" {
		scanInfo.ExcludedNamespaces = fmt.Sprintf("%s,%s", scanInfo.ExcludedNamespaces, hostSensorHandler.GetNamespace())
	}

	// ================== setup registry adaptors ======================================

	registryAdaptors, err := resourcehandler.NewRegistryAdaptors()
	if err != nil {
		logger.L().Error("failed to initialize registry adaptors", helpers.Error(err))
	}

	// ================== setup resource collector object ======================================

	resourceHandler := getResourceHandler(scanInfo, tenantConfig, k8s, hostSensorHandler, registryAdaptors)

	// ================== setup reporter & printer objects ======================================

	// reporting behavior - setup reporter
	reportHandler := getReporter(tenantConfig, scanInfo.ScanID, scanInfo.Submit, scanInfo.FrameworkScan)

	// setup printer
	printerHandler := resultshandling.NewPrinter(scanInfo.Format, scanInfo.FormatVersion, scanInfo.VerboseMode, cautils.ViewTypes(scanInfo.View))
	printerHandler.SetWriter(scanInfo.Output)

	// ================== return interface ======================================

	return componentInterfaces{
		tenantConfig:      tenantConfig,
		resourceHandler:   resourceHandler,
		report:            reportHandler,
		printerHandler:    printerHandler,
		hostSensorHandler: hostSensorHandler,
	}
}

func (ks *Kubescape) Scan(scanInfo *cautils.ScanInfo) (*resultshandling.ResultsHandler, error) {
	logger.L().Info("ARMO security scanner starting")

	// ===================== Initialization =====================
	scanInfo.Init() // initialize scan info

	interfaces := getInterfaces(scanInfo)

	cautils.ClusterName = interfaces.tenantConfig.GetContextName() // TODO - Deprecated
	cautils.CustomerGUID = interfaces.tenantConfig.GetAccountID()  // TODO - Deprecated
	interfaces.report.SetClusterName(interfaces.tenantConfig.GetContextName())
	interfaces.report.SetCustomerGUID(interfaces.tenantConfig.GetAccountID())

	downloadReleasedPolicy := getter.NewDownloadReleasedPolicy() // download config inputs from github release

	// set policy getter only after setting the customerGUID
	scanInfo.Getters.PolicyGetter = getPolicyGetter(scanInfo.UseFrom, interfaces.tenantConfig.GetTenantEmail(), scanInfo.FrameworkScan, downloadReleasedPolicy)
	scanInfo.Getters.ControlsInputsGetter = getConfigInputsGetter(scanInfo.ControlsInputs, interfaces.tenantConfig.GetAccountID(), downloadReleasedPolicy)
	scanInfo.Getters.ExceptionsGetter = getExceptionsGetter(scanInfo.UseExceptions)

	// TODO - list supported frameworks/controls
	if scanInfo.ScanAll {
		scanInfo.SetPolicyIdentifiers(listFrameworksNames(scanInfo.Getters.PolicyGetter), apisv1.KindFramework)
	}

	// remove host scanner components
	defer func() {
		if err := interfaces.hostSensorHandler.TearDown(); err != nil {
			logger.L().Error("failed to tear down host scanner", helpers.Error(err))
		}
	}()

	resultsHandling := resultshandling.NewResultsHandler(interfaces.report, interfaces.printerHandler)

	// ===================== policies & resources =====================
	policyHandler := policyhandler.NewPolicyHandler(interfaces.resourceHandler)
	scanData, err := policyHandler.CollectResources(scanInfo.PolicyIdentifier, scanInfo)
	if err != nil {
		return resultsHandling, err
	}

	// ========================= opa testing =====================
	deps := resources.NewRegoDependenciesData(k8sinterface.GetK8sConfig(), interfaces.tenantConfig.GetContextName())
	reportResults := opaprocessor.NewOPAProcessor(scanData, deps)
	if err := reportResults.ProcessRulesListenner(); err != nil {
		// TODO - do something
		return resultsHandling, err
	}

	// ========================= results handling =====================
	resultsHandling.SetData(scanData)

	// if resultsHandling.GetRiskScore() > float32(scanInfo.FailThreshold) {
	// 	return resultsHandling, fmt.Errorf("scan risk-score %.2f is above permitted threshold %.2f", resultsHandling.GetRiskScore(), scanInfo.FailThreshold)
	// }

	return resultsHandling, nil
}

// func askUserForHostSensor() bool {
// 	return false

// 	if !isatty.IsTerminal(os.Stdin.Fd()) {
// 		return false
// 	}
// 	if ssss, err := os.Stdin.Stat(); err == nil {
// 		// fmt.Printf("Found stdin type: %s\n", ssss.Mode().Type())
// 		if ssss.Mode().Type()&(fs.ModeDevice|fs.ModeCharDevice) > 0 { //has TTY
// 			fmt.Fprintf(os.Stderr, "Would you like to scan K8s nodes? [y/N]. This is required to collect valuable data for certain controls\n")
// 			fmt.Fprintf(os.Stderr, "Use --enable-host-scan flag to suppress this message\n")
// 			var b []byte = make([]byte, 1)
// 			if n, err := os.Stdin.Read(b); err == nil {
// 				if n > 0 && len(b) > 0 && (b[0] == 'y' || b[0] == 'Y') {
// 					return true
// 				}
// 			}
// 		}
// 	}
// 	return false
// }
