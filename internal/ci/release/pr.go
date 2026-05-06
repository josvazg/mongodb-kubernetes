package release

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// releaseJSON is the typed shape of release.json.
// Field declaration order controls marshal order on round-trip.
type releaseJSON struct {
	MongodbToolsBundle           json.RawMessage `json:"mongodbToolsBundle,omitempty"`
	MongodbOperator              string          `json:"mongodbOperator,omitempty"`
	InitDatabaseVersion          string          `json:"initDatabaseVersion,omitempty"`
	InitOpsManagerVersion        string          `json:"initOpsManagerVersion,omitempty"`
	DatabaseImageVersion         string          `json:"databaseImageVersion,omitempty"`
	AgentVersion                 string          `json:"agentVersion,omitempty"`
	ReadinessProbeVersion        string          `json:"readinessProbeVersion,omitempty"`
	VersionUpgradeHookVersion    string          `json:"versionUpgradeHookVersion,omitempty"`
	Openshift                    json.RawMessage `json:"openshift,omitempty"`
	Search                       json.RawMessage `json:"search,omitempty"`
	LatestOpsManagerAgentMapping json.RawMessage `json:"latestOpsManagerAgentMapping,omitempty"`
	SupportedImages              supportedImages `json:"supportedImages"`
}

// supportedImages lists images in the order they appear in release.json.
// Images updated on every operator release are typed; others pass through as raw JSON.
type supportedImages struct {
	OpsManager              json.RawMessage `json:"ops-manager,omitempty"`
	MongodbKubernetes       imageEntry      `json:"mongodb-kubernetes"`
	MongodbKubernetesOp     json.RawMessage `json:"mongodb-kubernetes-operator,omitempty"`
	MongodbAgent            json.RawMessage `json:"mongodb-agent,omitempty"`
	InitOpsManager          imageEntry      `json:"init-ops-manager"`
	InitDatabase            imageEntry      `json:"init-database"`
	Database                imageEntry      `json:"database"`
	MongodbEnterpriseServer json.RawMessage `json:"mongodb-enterprise-server,omitempty"`
}

func (si *supportedImages) imageByName(name string) (*imageEntry, bool) {
	switch name {
	case "mongodb-kubernetes":
		return &si.MongodbKubernetes, true
	case "init-ops-manager":
		return &si.InitOpsManager, true
	case "init-database":
		return &si.InitDatabase, true
	case "database":
		return &si.Database, true
	}
	return nil, false
}

// imageEntry is the common shape for the four operator-release images.
// Field order matches the file.
type imageEntry struct {
	Description string   `json:"Description,omitempty"`
	SsdlcName   string   `json:"ssdlc_name,omitempty"`
	Versions    []string `json:"versions"`
	Variants    []string `json:"variants,omitempty"`
}

// DefaultReleasedImages are the supportedImages keys updated on every operator release.
var DefaultReleasedImages = []string{
	"mongodb-kubernetes",
	"init-ops-manager",
	"init-database",
	"database",
}

// PROpener branches, commits, pushes, and opens a pull request, returning its URL.
// filesToStage is the list of repo-root-relative paths to include in the commit.
type PROpener interface {
	Open(repoRoot, branch, title, body string, filesToStage []string) (prURL string, err error)
}

// PRInputs are the parameters for a release PR operation.
type PRInputs struct {
	Version  string // required
	RepoRoot string // path to repo root; if empty, auto-detected from cwd
	DryRun   bool   // if true, compute changes but do not write files or open a PR
}

// AppendVersionToImages appends version to the versions array of each named image
// in the release.json content. Returns an error if any image is not found.
// The operation is idempotent: if version is already present it is not duplicated.
func AppendVersionToImages(data []byte, images []string, version string) ([]byte, error) {
	var doc releaseJSON
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse release.json: %w", err)
	}

	for _, imgName := range images {
		entry, ok := doc.SupportedImages.imageByName(imgName)
		if !ok {
			return nil, fmt.Errorf("image %q not found in supportedImages", imgName)
		}
		if slices.Contains(entry.Versions, version) {
			continue
		}
		entry.Versions = append(entry.Versions, version)
	}

	result, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(result, '\n'), nil
}

// ReleasePR updates release.json on disk then delegates branching, committing,
// pushing, and PR creation entirely to the opener.
func ReleasePR(inputs PRInputs, opener PROpener) (string, error) {
	if inputs.Version == "" {
		return "", errors.New("version is required")
	}

	repoRoot := inputs.RepoRoot
	if repoRoot == "" {
		out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
		if err != nil {
			return "", fmt.Errorf("not in a git repository: %w", err)
		}
		repoRoot = strings.TrimSpace(string(out))
	}

	releaseJSONPath := filepath.Join(repoRoot, "release.json")
	data, err := os.ReadFile(releaseJSONPath)
	if err != nil {
		return "", fmt.Errorf("read release.json: %w", err)
	}

	updated, err := AppendVersionToImages(data, DefaultReleasedImages, inputs.Version)
	if err != nil {
		return "", fmt.Errorf("update release.json: %w", err)
	}

	filesToStage := append([]string{"release.json"}, DockerfileDests(inputs.Version)...)

	if !inputs.DryRun {
		if err := os.WriteFile(releaseJSONPath, updated, 0o644); err != nil {
			return "", fmt.Errorf("write release.json: %w", err)
		}
		if err := CopyDockerfiles(repoRoot, inputs.Version); err != nil {
			return "", fmt.Errorf("copy dockerfiles: %w", err)
		}
	}

	branch := "release-" + inputs.Version
	title := "Release " + inputs.Version
	body := fmt.Sprintf("Adds operator version %s to `release.json` supported image lists.", inputs.Version)
	return opener.Open(repoRoot, branch, title, body, filesToStage)
}
