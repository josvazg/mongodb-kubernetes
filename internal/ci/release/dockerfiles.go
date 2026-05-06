package release

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type dockerfileCopy struct {
	src  string   // path relative to repoRoot
	dsts []string // paths relative to repoRoot; {version} is replaced at runtime
}

// releasedDockerfiles defines which Dockerfiles are copied on every operator release.
var releasedDockerfiles = []dockerfileCopy{
	{
		src:  "docker/mongodb-kubernetes-operator/Dockerfile",
		dsts: []string{"public/dockerfiles/mongodb-kubernetes/{version}/ubi/Dockerfile"},
	},
	{
		src:  "docker/mongodb-kubernetes-init-ops-manager/Dockerfile",
		dsts: []string{"public/dockerfiles/mongodb-kubernetes-init-ops-manager/{version}/ubi/Dockerfile"},
	},
	{
		src: "docker/mongodb-kubernetes-init-database/Dockerfile",
		dsts: []string{
			"public/dockerfiles/mongodb-kubernetes-init-database/{version}/ubi/Dockerfile",
			"public/dockerfiles/mongodb-kubernetes-init-appdb/{version}/ubi/Dockerfile",
		},
	},
	{
		src:  "docker/mongodb-kubernetes-database/Dockerfile",
		dsts: []string{"public/dockerfiles/mongodb-kubernetes-database/{version}/ubi/Dockerfile"},
	},
}

// DockerfileDests returns the destination paths (relative to repoRoot) that
// CopyDockerfiles would write for the given version. Pure computation, no I/O.
func DockerfileDests(version string) []string {
	var dsts []string
	for _, c := range releasedDockerfiles {
		for _, dst := range c.dsts {
			dsts = append(dsts, strings.ReplaceAll(dst, "{version}", version))
		}
	}
	return dsts
}

// CopyDockerfiles copies each source Dockerfile into its versioned public destination(s).
func CopyDockerfiles(repoRoot, version string) error {
	for _, c := range releasedDockerfiles {
		data, err := os.ReadFile(filepath.Join(repoRoot, c.src))
		if err != nil {
			return fmt.Errorf("read %s: %w", c.src, err)
		}
		for _, dst := range c.dsts {
			full := filepath.Join(repoRoot, strings.ReplaceAll(dst, "{version}", version))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", filepath.Dir(full), err)
			}
			if err := os.WriteFile(full, data, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", full, err)
			}
		}
	}
	return nil
}
