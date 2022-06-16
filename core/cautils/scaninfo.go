package cautils

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/armosec/armoapi-go/armotypes"
	apisv1 "github.com/armosec/opa-utils/httpserver/apis/v1"

	giturl "github.com/armosec/go-git-url"
	"github.com/armosec/k8s-interface/k8sinterface"
	"github.com/armosec/kubescape/v2/core/cautils/getter"
	"github.com/armosec/kubescape/v2/core/cautils/logger"
	"github.com/armosec/kubescape/v2/core/cautils/logger/helpers"
	"github.com/armosec/opa-utils/reporthandling"
	reporthandlingv2 "github.com/armosec/opa-utils/reporthandling/v2"
	"github.com/google/uuid"
)

const (
	ScanCluster                string = "cluster"
	ScanLocalFiles             string = "yaml"
	localControlInputsFilename string = "controls-inputs.json"
	localExceptionsFilename    string = "exceptions.json"
)

type BoolPtrFlag struct {
	valPtr *bool
}

func NewBoolPtr(b *bool) BoolPtrFlag {
	return BoolPtrFlag{valPtr: b}
}

func (bpf *BoolPtrFlag) Type() string {
	return "bool"
}

func (bpf *BoolPtrFlag) String() string {
	if bpf.valPtr != nil {
		return fmt.Sprintf("%v", *bpf.valPtr)
	}
	return ""
}
func (bpf *BoolPtrFlag) Get() *bool {
	return bpf.valPtr
}
func (bpf *BoolPtrFlag) GetBool() bool {
	if bpf.valPtr == nil {
		return false
	}
	return *bpf.valPtr
}

func (bpf *BoolPtrFlag) SetBool(val bool) {
	bpf.valPtr = &val
}

func (bpf *BoolPtrFlag) Set(val string) error {
	switch val {
	case "true":
		bpf.SetBool(true)
	case "false":
		bpf.SetBool(false)
	}
	return nil
}

// TODO - UPDATE
type ViewTypes string

const (
	ResourceViewType ViewTypes = "resource"
	ControlViewType  ViewTypes = "control"
)

type PolicyIdentifier struct {
	Name        string                        // policy name e.g. nsa,mitre,c-0012
	Kind        apisv1.NotificationPolicyKind // policy kind e.g. Framework,Control,Rule
	Designators armotypes.PortalDesignator
}

type ScanInfo struct {
	Getters                               // TODO - remove from object
	PolicyIdentifier   []PolicyIdentifier // TODO - remove from object
	UseExceptions      string             // Load file with exceptions configuration
	ControlsInputs     string             // Load file with inputs for controls
	UseFrom            []string           // Load framework from local file (instead of download). Use when running offline
	UseDefault         bool               // Load framework from cached file (instead of download). Use when running offline
	UseArtifactsFrom   string             // Load artifacts from local path. Use when running offline
	VerboseMode        bool               // Display all of the input resources and not only failed resources
	View               string             // Display all of the input resources and not only failed resources
	Format             string             // Format results (table, json, junit ...)
	Output             string             // Store results in an output file, Output file name
	FormatVersion      string             // Output object can be differnet between versions, this is for testing and backward compatibility
	ExcludedNamespaces string             // used for host scanner namespace
	IncludeNamespaces  string             //
	InputPatterns      []string           // Yaml files input patterns
	Silent             bool               // Silent mode - Do not print progress logs
	FailThreshold      float32            // Failure score threshold
	Submit             bool               // Submit results to Armo BE
	ScanID             string             // Report id of the current scan
	HostSensorEnabled  BoolPtrFlag        // Deploy ARMO K8s host scanner to collect data from certain controls
	HostSensorYamlPath string             // Path to hostsensor file
	Local              bool               // Do not submit results
	Credentials        Credentials        // account ID
	KubeContext        string             // context name
	FrameworkScan      bool               // false if scanning control
	ScanAll            bool               // true if scan all frameworks
}

type Getters struct {
	ExceptionsGetter     getter.IExceptionsGetter
	ControlsInputsGetter getter.IControlsInputsGetter
	PolicyGetter         getter.IPolicyGetter
}

func (scanInfo *ScanInfo) Init() {
	scanInfo.setUseFrom()
	scanInfo.setOutputFile()
	scanInfo.setUseArtifactsFrom()
	if scanInfo.ScanID == "" {
		scanInfo.ScanID = uuid.NewString()
	}

}

func (scanInfo *ScanInfo) setUseArtifactsFrom() {
	if scanInfo.UseArtifactsFrom == "" {
		return
	}
	// UseArtifactsFrom must be a path without a filename
	dir, file := filepath.Split(scanInfo.UseArtifactsFrom)
	if dir == "" {
		scanInfo.UseArtifactsFrom = file
	} else if strings.Contains(file, ".json") {
		scanInfo.UseArtifactsFrom = dir
	}
	// set frameworks files
	files, err := ioutil.ReadDir(scanInfo.UseArtifactsFrom)
	if err != nil {
		logger.L().Fatal("failed to read files from directory", helpers.String("dir", scanInfo.UseArtifactsFrom), helpers.Error(err))
	}
	framework := &reporthandling.Framework{}
	for _, f := range files {
		filePath := filepath.Join(scanInfo.UseArtifactsFrom, f.Name())
		file, err := os.ReadFile(filePath)
		if err == nil {
			if err := json.Unmarshal(file, framework); err == nil {
				scanInfo.UseFrom = append(scanInfo.UseFrom, filepath.Join(scanInfo.UseArtifactsFrom, f.Name()))
			}
		}
	}
	// set config-inputs file
	scanInfo.ControlsInputs = filepath.Join(scanInfo.UseArtifactsFrom, localControlInputsFilename)
	// set exceptions
	scanInfo.UseExceptions = filepath.Join(scanInfo.UseArtifactsFrom, localExceptionsFilename)
}

func (scanInfo *ScanInfo) setUseFrom() {
	if scanInfo.UseDefault {
		for _, policy := range scanInfo.PolicyIdentifier {
			scanInfo.UseFrom = append(scanInfo.UseFrom, getter.GetDefaultPath(policy.Name+".json"))
		}
	}
}

func (scanInfo *ScanInfo) setOutputFile() {
	if scanInfo.Output == "" {
		return
	}
	if scanInfo.Format == "json" {
		if filepath.Ext(scanInfo.Output) != ".json" {
			scanInfo.Output += ".json"
		}
	}
	if scanInfo.Format == "junit" {
		if filepath.Ext(scanInfo.Output) != ".xml" {
			scanInfo.Output += ".xml"
		}
	}
	if scanInfo.Format == "pdf" {
		if filepath.Ext(scanInfo.Output) != ".pdf" {
			scanInfo.Output += ".pdf"
		}
	}
}

func (scanInfo *ScanInfo) GetScanningEnvironment() string {
	if len(scanInfo.InputPatterns) != 0 {
		return ScanLocalFiles
	}
	return ScanCluster
}

func (scanInfo *ScanInfo) SetPolicyIdentifiers(policies []string, kind apisv1.NotificationPolicyKind) {
	for _, policy := range policies {
		if !scanInfo.contains(policy) {
			newPolicy := PolicyIdentifier{}
			newPolicy.Kind = kind
			newPolicy.Name = policy
			scanInfo.PolicyIdentifier = append(scanInfo.PolicyIdentifier, newPolicy)
		}
	}
}

func (scanInfo *ScanInfo) contains(policyName string) bool {
	for _, policy := range scanInfo.PolicyIdentifier {
		if policy.Name == policyName {
			return true
		}
	}
	return false
}

func scanInfoToScanMetadata(scanInfo *ScanInfo) *reporthandlingv2.Metadata {
	metadata := &reporthandlingv2.Metadata{}

	metadata.ScanMetadata.Format = scanInfo.Format
	metadata.ScanMetadata.FormatVersion = scanInfo.FormatVersion
	metadata.ScanMetadata.Submit = scanInfo.Submit

	// TODO - Add excluded and included namespaces
	// if len(scanInfo.ExcludedNamespaces) > 1 {
	// 	opaSessionObj.Metadata.ScanMetadata.ExcludedNamespaces = strings.Split(scanInfo.ExcludedNamespaces[1:], ",")
	// }
	// if len(scanInfo.IncludeNamespaces) > 1 {
	// 	opaSessionObj.Metadata.ScanMetadata.IncludeNamespaces = strings.Split(scanInfo.IncludeNamespaces[1:], ",")
	// }

	// scan type
	if len(scanInfo.PolicyIdentifier) > 0 {
		metadata.ScanMetadata.TargetType = string(scanInfo.PolicyIdentifier[0].Kind)
	}
	// append frameworks
	for _, policy := range scanInfo.PolicyIdentifier {
		metadata.ScanMetadata.TargetNames = append(metadata.ScanMetadata.TargetNames, policy.Name)
	}

	metadata.ScanMetadata.KubescapeVersion = BuildNumber
	metadata.ScanMetadata.VerboseMode = scanInfo.VerboseMode
	metadata.ScanMetadata.FailThreshold = scanInfo.FailThreshold
	metadata.ScanMetadata.HostScanner = scanInfo.HostSensorEnabled.GetBool()
	metadata.ScanMetadata.VerboseMode = scanInfo.VerboseMode
	metadata.ScanMetadata.ControlsInputs = scanInfo.ControlsInputs

	metadata.ScanMetadata.ScanningTarget = reporthandlingv2.Cluster
	if scanInfo.GetScanningEnvironment() == ScanLocalFiles {
		metadata.ScanMetadata.ScanningTarget = reporthandlingv2.File
	}

	inputFiles := ""
	if len(scanInfo.InputPatterns) > 0 {
		inputFiles = scanInfo.InputPatterns[0]
	}
	setContextMetadata(&metadata.ContextMetadata, inputFiles)

	return metadata
}

func setContextMetadata(contextMetadata *reporthandlingv2.ContextMetadata, input string) {
	//  cluster
	if input == "" {
		contextMetadata.ClusterContextMetadata = &reporthandlingv2.ClusterMetadata{
			ContextName: k8sinterface.GetContextName(),
		}
		return
	}

	// url
	if gitParser, err := giturl.NewGitURL(input); err == nil {
		if gitParser.GetBranch() == "" {
			gitParser.SetDefaultBranch()
		}
		contextMetadata.RepoContextMetadata = &reporthandlingv2.RepoContextMetadata{
			Repo:   gitParser.GetRepo(),
			Owner:  gitParser.GetOwner(),
			Branch: gitParser.GetBranch(),
		}
		return
	}

	if !filepath.IsAbs(input) {
		if o, err := os.Getwd(); err == nil {
			input = filepath.Join(o, input)
		}
	}

	//  single file
	if IsFile(input) {
		contextMetadata.FileContextMetadata = &reporthandlingv2.FileContextMetadata{
			FilePath: input,
			HostName: getHostname(),
		}
		return
	}

	//  dir/glob
	if !IsFile(input) {
		contextMetadata.DirectoryContextMetadata = &reporthandlingv2.DirectoryContextMetadata{
			BasePath: input,
			HostName: getHostname(),
		}
		return
	}

}

func getHostname() string {
	if h, e := os.Hostname(); e == nil {
		return h
	}
	return ""
}
