package imagescan

import (
	"errors"
	"net/http"
	"path"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/anchore/grype/grype/db"
	grypedb "github.com/anchore/grype/grype/db/v5"
	"github.com/anchore/grype/grype/match"
	"github.com/anchore/grype/grype/pkg"
	"github.com/anchore/grype/grype/presenter/models"
	"github.com/anchore/grype/grype/vulnerability"
	syftPkg "github.com/anchore/syft/syft/pkg"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestVulnerabilityAndSeverityExceptions(t *testing.T) {
	go func() {
		_ = http.ListenAndServe(":8000", http.FileServer(http.Dir("testdata"))) //nolint:gosec
	}()
	dbCfg := db.Config{
		DBRootDir:  path.Join(xdg.CacheHome, "grype-light", "db"),
		ListingURL: "http://localhost:8000/listing.json",
	}
	svc := NewScanService(dbCfg)
	creds := RegistryCredentials{}

	tests := []struct {
		name                    string
		image                   string
		vulnerabilityExceptions []string
		ignoredLen              int
		severityExceptions      []string
		filteredLen             int
	}{
		{
			name:               "alpine:3.19.1 without medium vulnerabilities",
			image:              "alpine:3.19.1",
			ignoredLen:         0,
			severityExceptions: []string{"MEDIUM"},
			filteredLen:        0,
		},
		{
			name:                    "alpine:3.9.6",
			image:                   "alpine:3.9.6",
			vulnerabilityExceptions: []string{"CVE-2020-1971", "CVE-2020-28928", "CVE-2021-23840"},
			ignoredLen:              6,
			severityExceptions:      []string{"HIGH", "MEDIUM"},
			filteredLen:             8,
		},
		{
			name:                    "alpine:3.9.6 with invalid vulnerability and severity exceptions",
			image:                   "alpine:3.9.6",
			vulnerabilityExceptions: []string{"invalid-cve", "CVE-2020-28928", "CVE-2021-23840"},
			ignoredLen:              4,
			severityExceptions:      []string{"CRITICAL", "MEDIUM", "invalid-severity"},
			filteredLen:             10,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store, status, dbCloser, err := NewVulnerabilityDB(svc.dbCfg, true)
			assert.NoError(t, validateDBLoad(err, status))

			packages, pkgContext, _, err := pkg.Provide(tc.image, getProviderConfig(creds))
			assert.NoError(t, err)

			if dbCloser != nil {
				defer dbCloser.Close()
			}

			remainingMatches, ignoredMatches, err := getIgnoredMatches(tc.vulnerabilityExceptions, store, packages, pkgContext)
			assert.NoError(t, err)
			assert.Equal(t, tc.ignoredLen, len(ignoredMatches))

			filteredMatches := filterMatchesBasedOnSeverity(tc.severityExceptions, *remainingMatches, store)
			assert.Equal(t, tc.filteredLen, filteredMatches.Count())
		})
	}
}

// fakeMetaProvider is a test double that fakes an actual MetadataProvider
type fakeMetaProvider struct {
	vulnerabilities map[string]map[string][]grypedb.Vulnerability
	metadata        map[string]map[string]*grypedb.VulnerabilityMetadata
}

func newFakeMetaProvider() *fakeMetaProvider {
	d := fakeMetaProvider{
		vulnerabilities: make(map[string]map[string][]grypedb.Vulnerability),
		metadata:        make(map[string]map[string]*grypedb.VulnerabilityMetadata),
	}
	d.fillWithData()
	return &d
}
func (d *fakeMetaProvider) GetAllVulnerabilityMetadata() (*[]grypedb.VulnerabilityMetadata, error) {
	return nil, nil
}
func (d *fakeMetaProvider) GetVulnerabilityMatchExclusion(id string) ([]grypedb.VulnerabilityMatchExclusion, error) {
	return nil, nil
}
func (d *fakeMetaProvider) GetVulnerabilityMetadata(id, namespace string) (*grypedb.VulnerabilityMetadata, error) {
	return d.metadata[id][namespace], nil
}
func (d *fakeMetaProvider) fillWithData() {
	d.metadata["CVE-2014-fake-1"] = map[string]*grypedb.VulnerabilityMetadata{
		"debian:distro:debian:8": {
			ID:        "CVE-2014-fake-1",
			Namespace: "debian:distro:debian:8",
			Severity:  "medium",
		},
	}
	d.vulnerabilities["debian:distro:debian:8"] = map[string][]grypedb.Vulnerability{
		"neutron": {
			{
				PackageName:       "neutron",
				Namespace:         "debian:distro:debian:8",
				VersionConstraint: "< 2014.1.3-6",
				ID:                "CVE-2014-fake-1",
				VersionFormat:     "deb",
			},
		},
	}
}

func TestParseSeverity(t *testing.T) {
	tests := []struct {
		name string
		want vulnerability.Severity
	}{
		{
			name: "",
			want: vulnerability.UnknownSeverity,
		},
		{
			name: "negligible",
			want: vulnerability.NegligibleSeverity,
		},
		{
			name: "low",
			want: vulnerability.LowSeverity,
		},
		{
			name: "medium",
			want: vulnerability.MediumSeverity,
		},
		{
			name: "high",
			want: vulnerability.HighSeverity,
		},
		{
			name: "critical",
			want: vulnerability.CriticalSeverity,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSeverity(tt.name)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsEmpty(t *testing.T) {
	tests := []struct {
		name  string
		creds RegistryCredentials
		want  bool
	}{
		{
			name: "Both Non Empty",
			creds: RegistryCredentials{
				Username: "username",
				Password: "password",
			},
			want: false,
		},
		{
			name: "Password Empty",
			creds: RegistryCredentials{
				Username: "username",
				Password: "",
			},
			want: true,
		},
		{
			name: "Username Empty",
			creds: RegistryCredentials{
				Username: "",
				Password: "password",
			},
			want: true,
		},
		{
			name: "Both empty",
			creds: RegistryCredentials{
				Username: "",
				Password: "",
			},
			want: true,
		},
		{
			name:  "Empty struct",
			creds: RegistryCredentials{},
			want:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.creds.IsEmpty())
		})
	}
}

func TestNewDefaultDBConfig(t *testing.T) {
	config, shouldUpdate := NewDefaultDBConfig()
	assert.NotNil(t, config)
	assert.Equal(t, true, shouldUpdate)
	assert.Contains(t, config.DBRootDir, "grypedb")
	assert.Equal(t, "https://toolbox-data.anchore.io/grype/databases/listing.json", config.ListingURL)
}

func TestValidateDBLoad(t *testing.T) {
	currentTime := time.Now()
	tests := []struct {
		name               string
		loadErr            error
		status             *db.Status
		expectedErrMessage string
	}{
		{
			name:               "status nil",
			loadErr:            nil,
			status:             nil,
			expectedErrMessage: "unable to determine the status of the vulnerability db",
		},
		{
			name:    "loadErr nil and status error nil",
			loadErr: nil,
			status: &db.Status{
				Built:         currentTime,
				SchemaVersion: 7,
				Location:      "New Delhi",
				Checksum:      "invalid",
				Err:           nil,
			},
			expectedErrMessage: "",
		},
		{
			name:    "loadErr nil but status error not nil",
			loadErr: nil,
			status: &db.Status{
				Built:         currentTime,
				SchemaVersion: 7,
				Location:      "New Delhi",
				Checksum:      "invalid",
				Err:           errors.New("Some error"),
			},
			expectedErrMessage: "db could not be loaded: Some error",
		},
		{
			name:    "loadErr not nil",
			loadErr: errors.New("Some error"),
			status: &db.Status{
				Built:         currentTime,
				SchemaVersion: 7,
				Location:      "New Delhi",
				Checksum:      "invalid",
				Err:           errors.New("Some error"),
			},
			expectedErrMessage: "failed to load vulnerability db: Some error",
		},
		{
			name:               "load Error, no db status",
			loadErr:            errors.New("Some error"),
			status:             nil,
			expectedErrMessage: "failed to load vulnerability db: Some error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDBLoad(tt.loadErr, tt.status)
			if err != nil {
				assert.Equal(t, tt.expectedErrMessage, err.Error())
			}
		})
	}
}

func TestGetProviderConfig(t *testing.T) {
	tests := []struct {
		name  string
		creds RegistryCredentials
	}{
		{
			name: "Both Non Empty",
			creds: RegistryCredentials{
				Username: "username",
				Password: "password",
			},
		},
		{
			name: "Password Empty",
			creds: RegistryCredentials{
				Username: "username",
				Password: "",
			},
		},
		{
			name: "Username Empty",
			creds: RegistryCredentials{
				Username: "",
				Password: "password",
			},
		},
		{
			name: "Both empty",
			creds: RegistryCredentials{
				Username: "",
				Password: "",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providerConfig := getProviderConfig(tt.creds)
			assert.NotNil(t, providerConfig)
			assert.Equal(t, true, providerConfig.SynthesisConfig.GenerateMissingCPEs)
		})
	}
}

func TestNewScanService(t *testing.T) {
	defaultConfig, _ := NewDefaultDBConfig()
	svc := NewScanService(defaultConfig)
	assert.Equal(t, defaultConfig, svc.dbCfg)
}

func TestExceedsSeverityThreshold(t *testing.T) {
	my_matches := match.NewMatches()
	my_matches.Add(match.Match{
		Vulnerability: vulnerability.Vulnerability{
			ID:        "CVE-2014-fake-1",
			Namespace: "debian:distro:debian:8",
		},
		Package: pkg.Package{
			ID:      pkg.ID(uuid.NewString()),
			Name:    "the-package",
			Version: "v0.1",
			Type:    syftPkg.RpmPkg,
		},
		Details: match.Details{
			{
				Type: match.ExactDirectMatch,
			},
		},
	})
	my_metadataProvider := db.NewVulnerabilityMetadataProvider(newFakeMetaProvider())

	type args struct {
		scanResults *models.PresenterConfig
		severity    vulnerability.Severity
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "No severity set",
			args: args{
				scanResults: &models.PresenterConfig{
					Matches:          match.NewMatches(),
					MetadataProvider: nil,
				},
				severity: vulnerability.UnknownSeverity,
			},
			want: false,
		},
		{
			name: "No MetadataProvider",
			args: args{
				scanResults: &models.PresenterConfig{
					Matches:          my_matches,
					MetadataProvider: nil,
				},
				severity: vulnerability.MediumSeverity,
			},
			want: false,
		},
		{
			name: "Severity higher than vulnerability",
			args: args{
				scanResults: &models.PresenterConfig{
					Matches:          my_matches,
					MetadataProvider: my_metadataProvider,
				},
				severity: vulnerability.HighSeverity,
			},
			want: false,
		},
		{
			name: "Severity equal to vulnerability",
			args: args{
				scanResults: &models.PresenterConfig{
					Matches:          my_matches,
					MetadataProvider: my_metadataProvider,
				},
				severity: vulnerability.MediumSeverity,
			},
			want: true,
		},
		{
			name: "Fail severity below found vulnerability",
			args: args{
				scanResults: &models.PresenterConfig{
					Matches:          my_matches,
					MetadataProvider: my_metadataProvider,
				},
				severity: vulnerability.LowSeverity,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			got := ExceedsSeverityThreshold(tt.args.scanResults, tt.args.severity)

			assert.Equal(t, tt.want, got)
		})
	}
}
