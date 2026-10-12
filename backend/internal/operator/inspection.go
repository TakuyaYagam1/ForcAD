package operator

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func readComposeProject(ctx context.Context, runner MaintenanceRunner, composeArgs []string) (composeProject, error) {
	const location = "operator.readComposeProject"
	args := append(append([]string(nil), composeArgs[1:]...), "config", "--format", "json")
	output := &limitedBuffer{limit: maintenanceOutputLimit}
	if err := runDocker(ctx, runner, args, output); err != nil {
		return composeProject{}, maintenanceCommandError(ctx, "inspect the Compose project", err)
	}
	if output.exceeded {
		return composeProject{}, fmt.Errorf("%s - Compose project configuration exceeds the inspection limit", location)
	}
	var project composeProject
	if err := json.Unmarshal(output.Bytes(), &project); err != nil {
		return composeProject{}, fmt.Errorf("%s - Docker Compose returned invalid project configuration", location)
	}
	if strings.TrimSpace(project.Name) == "" || len(project.Services) == 0 {
		return composeProject{}, fmt.Errorf("%s - Docker Compose project configuration is incomplete", location)
	}
	return project, nil
}

type composeProject struct {
	Name     string                    `json:"name"`
	Services map[string]composeService `json:"services"`
	Volumes  map[string]composeVolume  `json:"volumes"`
}

type composeService struct {
	Image   string         `json:"image"`
	Volumes []composeMount `json:"volumes"`
}

type composeMount struct {
	Type   string `json:"type"`
	Source string `json:"source"`
	Target string `json:"target"`
}

type composeVolume struct {
	External bool `json:"external"`
}

func verifyProjectOwnership(ctx context.Context, runner MaintenanceRunner, name, deployment string) error {
	const location = "operator.verifyProjectOwnership"
	args := []string{"ps", "-aq", "--filter", "label=com.docker.compose.project=" + name}
	output := &limitedBuffer{limit: maintenanceOutputLimit}
	if err := runDocker(ctx, runner, args, output); err != nil {
		return maintenanceCommandError(ctx, "check Compose project ownership", err)
	}
	if output.exceeded {
		return fmt.Errorf("%s - too many Compose containers to verify ownership", location)
	}
	ids := strings.Fields(output.String())
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > maxComposeContainers {
		return fmt.Errorf("%s - too many Compose containers to verify ownership", location)
	}
	inspectArgs := make([]string, 0, 3+len(ids))
	inspectArgs = append(inspectArgs, "inspect", "--format", "{{json .Config.Labels}}")
	inspectArgs = append(inspectArgs, ids...)
	labelsOutput := &limitedBuffer{limit: maintenanceOutputLimit}
	if err := runDocker(ctx, runner, inspectArgs, labelsOutput); err != nil {
		return maintenanceCommandError(ctx, "verify Compose container ownership", err)
	}
	if labelsOutput.exceeded {
		return fmt.Errorf("%s - Compose container ownership output exceeds the inspection limit", location)
	}
	return verifyOwnershipLabels(labelsOutput.Bytes(), deployment, len(ids))
}

func verifyOwnershipLabels(labelsData []byte, deployment string, expectedCount int) error {
	const location = "operator.verifyOwnershipLabels"
	expected, err := filepath.Abs(deployment)
	if err != nil {
		return fmt.Errorf("%s - could not resolve Compose project directory", location)
	}
	expected = filepath.Clean(expected)
	scanner := bufio.NewScanner(bytes.NewReader(labelsData))
	scanner.Buffer(make([]byte, 4096), maintenanceOutputLimit)
	count := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var labels map[string]string
		if err := json.Unmarshal([]byte(line), &labels); err != nil {
			return fmt.Errorf("%s - Docker returned invalid Compose container labels", location)
		}
		workingDir := labels["com.docker.compose.project.working_dir"]
		if workingDir == "" {
			return fmt.Errorf("%s - Compose container has no project directory label", location)
		}
		actual, err := filepath.Abs(workingDir)
		if err != nil || filepath.Clean(actual) != expected {
			return fmt.Errorf("%s - Compose project belongs to another directory; refusing maintenance", location)
		}
		count++
	}
	if err := scanner.Err(); err != nil || count != expectedCount {
		return fmt.Errorf("%s - could not verify all Compose container ownership labels", location)
	}
	return nil
}

func cachedPostgresImage(ctx context.Context, image string, runner MaintenanceRunner) (string, error) {
	const location = "operator.cachedPostgresImage"
	output := &limitedBuffer{limit: 4096}
	if err := runDocker(ctx, runner, []string{"image", "inspect", image, "--format", "{{.Id}}"}, output); err != nil {
		return "", maintenanceCommandError(ctx, "inspect the cached PostgreSQL image", err)
	}
	if output.exceeded {
		return "", fmt.Errorf("%s - cached PostgreSQL image identity exceeds the limit", location)
	}
	imageID := strings.TrimSpace(output.String())
	if !imageIDPattern.MatchString(imageID) {
		return "", fmt.Errorf("%s - cached PostgreSQL image has an invalid identity", location)
	}
	return imageID, nil
}

func requireLocalDocker(ctx context.Context, runner MaintenanceRunner) error {
	const location = "operator.requireLocalDocker"
	if runtime.GOOS != "linux" {
		return fmt.Errorf("%s - full reset is supported only with a local Linux Docker daemon", location)
	}
	if host, ok := os.LookupEnv("DOCKER_HOST"); ok && strings.TrimSpace(host) != "" {
		if strings.HasPrefix(host, "unix://") {
			return nil
		}
		return fmt.Errorf("%s - full reset requires a local Unix-socket Docker daemon", location)
	}
	output := &limitedBuffer{limit: 4096}
	if err := runDocker(ctx, runner, []string{"context", "inspect", "--format", "{{.Endpoints.docker.Host}}"}, output); err != nil {
		return maintenanceCommandError(ctx, "inspect the Docker context", err)
	}
	if output.exceeded || !strings.HasPrefix(strings.TrimSpace(output.String()), "unix://") {
		return fmt.Errorf("%s - full reset requires a local Unix-socket Docker daemon", location)
	}
	return nil
}

type limitedBuffer struct {
	bytes.Buffer

	limit    int
	exceeded bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = b.Buffer.Write(p[:remaining])
			b.exceeded = true
		} else {
			_, _ = b.Buffer.Write(p)
		}
	} else if len(p) > 0 {
		b.exceeded = true
	}
	return len(p), nil
}
