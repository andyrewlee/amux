package daytona

import "strings"

const artifactPrefix = "dtn_artifact_k39fd2:"

// ParseArtifacts extracts artifacts from stdout text.
func ParseArtifacts(output string) ExecutionArtifacts {
	lines := strings.Split(output, "\n")
	filtered := make([]string, 0, len(lines))

	for _, line := range lines {
		if strings.HasPrefix(line, artifactPrefix) {
			continue
		}
		filtered = append(filtered, line)
	}

	return ExecutionArtifacts{Stdout: strings.Join(filtered, "\n")}
}
