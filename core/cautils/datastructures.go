package cautils

import (
	"github.com/armosec/armoapi-go/armotypes"
	"github.com/armosec/k8s-interface/workloadinterface"
	"github.com/armosec/opa-utils/reporthandling"
	apis "github.com/armosec/opa-utils/reporthandling/apis"
	"github.com/armosec/opa-utils/reporthandling/results/v1/resourcesresults"
	reporthandlingv2 "github.com/armosec/opa-utils/reporthandling/v2"
)

// K8SResources map[<api group>/<api version>/<resource>][]<resourceID>
type K8SResources map[string][]string
type ArmoResources map[string][]string

type OPASessionObj struct {
	K8SResources          *K8SResources                          // input k8s objects
	ArmoResource          *ArmoResources                         // input ARMO objects
	Policies              []reporthandling.Framework             // list of frameworks to scan
	AllResources          map[string]workloadinterface.IMetadata // all scanned resources, map[<rtesource ID>]<resource>
	ResourcesResult       map[string]resourcesresults.Result     // resources scan results, map[<rtesource ID>]<resource result>
	ResourceSource        map[string]string                      // resources sources, map[<rtesource ID>]<resource result>
	PostureReport         *reporthandling.PostureReport          // scan results v1 - Remove
	Report                *reporthandlingv2.PostureReport        // scan results v2 - Remove
	Exceptions            []armotypes.PostureExceptionPolicy     // list of exceptions to apply on scan results
	RegoInputData         RegoInputData                          // input passed to rgo for scanning. map[<control name>][<input arguments>]
	Metadata              *reporthandlingv2.Metadata
	InfoMap               map[string]apis.StatusInfo // Map errors of resources to StatusInfo
	ResourceToControlsMap map[string][]string        // map[<apigroup/apiversion/resource>] = [<control_IDs>]
	SessionID             string                     // SessionID
}

func NewOPASessionObj(frameworks []reporthandling.Framework, k8sResources *K8SResources, scanInfo *ScanInfo) *OPASessionObj {
	return &OPASessionObj{
		Report:                &reporthandlingv2.PostureReport{},
		Policies:              frameworks,
		K8SResources:          k8sResources,
		AllResources:          make(map[string]workloadinterface.IMetadata),
		ResourcesResult:       make(map[string]resourcesresults.Result),
		InfoMap:               make(map[string]apis.StatusInfo),
		ResourceToControlsMap: make(map[string][]string),
		ResourceSource:        make(map[string]string),
		SessionID:             scanInfo.ScanID,
		PostureReport: &reporthandling.PostureReport{
			ClusterName:  ClusterName,
			CustomerGUID: CustomerGUID,
		},
		Metadata: scanInfoToScanMetadata(scanInfo),
	}
}

func NewOPASessionObjMock() *OPASessionObj {
	return &OPASessionObj{
		Policies:        nil,
		K8SResources:    nil,
		AllResources:    make(map[string]workloadinterface.IMetadata),
		ResourcesResult: make(map[string]resourcesresults.Result),
		Report:          &reporthandlingv2.PostureReport{},
		PostureReport: &reporthandling.PostureReport{
			ClusterName:  "",
			CustomerGUID: "",
			ReportID:     "",
			JobID:        "",
		},
	}
}

type ComponentConfig struct {
	Exceptions Exception `json:"exceptions"`
}

type Exception struct {
	Ignore        *bool                      `json:"ignore"`        // ignore test results
	MultipleScore *reporthandling.AlertScore `json:"multipleScore"` // MultipleScore number - float32
	Namespaces    []string                   `json:"namespaces"`
	Regex         string                     `json:"regex"` // not supported
}

type RegoInputData struct {
	PostureControlInputs map[string][]string `json:"postureControlInputs"`
	// ClusterName          string              `json:"clusterName"`
	// K8sConfig            RegoK8sConfig       `json:"k8sconfig"`
}

type Policies struct {
	Frameworks []string
	Controls   map[string]reporthandling.Control // map[<control ID>]<control>
}
