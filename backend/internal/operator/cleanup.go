package operator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const (
	maintenanceOutputLimit    = 4 << 20
	maxComposeContainers      = 4096
	composeStopTimeoutSeconds = "30"
)

const inspectPostgresDataScript = `set -eu
if [ -L /cleanup/data ]; then
    echo 'Refusing a symlinked PostgreSQL directory' >&2
    exit 1
fi
if [ -e /cleanup/data ] && [ ! -d /cleanup/data ]; then
    echo 'PostgreSQL data path is not a directory' >&2
    exit 1
fi
if [ -d /cleanup/data ] && [ -n "$(ls -A /cleanup/data)" ]; then
    if [ -e /cleanup/data/PG_VERSION ] || [ -L /cleanup/data/PG_VERSION ]; then
        if [ ! -f /cleanup/data/PG_VERSION ] || [ -L /cleanup/data/PG_VERSION ]; then
            echo 'Refusing data without a regular PG_VERSION file' >&2
            exit 1
        fi
    elif [ -L /cleanup/data/18 ] || [ ! -d /cleanup/data/18 ] \
            || [ -L /cleanup/data/18/docker ] \
            || [ ! -d /cleanup/data/18/docker ] \
            || [ ! -f /cleanup/data/18/docker/PG_VERSION ] \
            || [ -L /cleanup/data/18/docker/PG_VERSION ]; then
        echo 'Refusing data without a supported regular PG_VERSION file' >&2
        exit 1
    fi
fi
if [ -d /cleanup/data ] && find /cleanup/data -type l -print -quit | grep -q .; then
    echo 'Refusing symlinks in PostgreSQL data' >&2
    exit 1
fi
`

const deletePostgresDataScript = inspectPostgresDataScript + `rm -rf -- /cleanup/data
test ! -e /cleanup/data && test ! -L /cleanup/data
`

var imageIDPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// MaintenanceRunner runs one Docker command. Maintenance keeps command output
// private because Compose can include resolved configuration in diagnostics.
type MaintenanceRunner func(
	context.Context,
	string,
	[]string,
	io.Writer,
	io.Writer,
) error

// MaintenanceOptions selects a confirmed local reset or clean operation.
type MaintenanceOptions struct {
	Confirm bool
	Full    bool
	Clean   bool
}

// RunMaintenance resets game state, or performs the guarded local full reset.
// Clean additionally removes only ForcAD-generated runtime files.
func RunMaintenance(
	ctx context.Context,
	root string,
	options MaintenanceOptions,
	out io.Writer,
	runner MaintenanceRunner,
	resetData func(context.Context) error,
) error {
	const location = "operator.RunMaintenance"

	if err := validateMaintenance(ctx, options, runner, resetData); err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}

	paths, err := pathsFor(root)
	if err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	if err := validateMaintenanceFiles(paths); err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	composeArgs, err := ComposeArgs(root, false)
	if err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	project, err := readComposeProject(ctx, runner, composeArgs)
	if err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	if err := verifyProjectOwnership(ctx, runner, project.Name, paths.deployment); err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}

	if options.Full || options.Clean {
		if err := removeLocalPostgresData(ctx, runner, composeArgs, project, paths); err != nil {
			return fmt.Errorf("%s - remove verified PostgreSQL data: %w", location, err)
		}
	} else if err := resetGameData(ctx, runner, composeArgs, project, resetData); err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}

	if options.Clean {
		if err := removeGeneratedRuntimeFiles(paths); err != nil {
			return fmt.Errorf("%s - %w", location, err)
		}
	}
	if err := writeMaintenanceMessage(ctx, options.completionMessage(), out); err != nil {
		return fmt.Errorf("%s - write maintenance result: %w", location, err)
	}
	return nil
}

func removeLocalPostgresData(
	ctx context.Context,
	runner MaintenanceRunner,
	composeArgs []string,
	project composeProject,
	paths projectPaths,
) error {
	const location = "operator.removeLocalPostgresData"
	target, err := prepareFullReset(ctx, runner, project, paths)
	if err != nil {
		return fmt.Errorf("%s - prepare guarded reset: %w", location, err)
	}
	if err := stopComposeProject(ctx, runner, composeArgs); err != nil {
		return fmt.Errorf("%s - stop verified Compose project: %w", location, err)
	}
	if err := runPostgresCleanupContainer(ctx, runner, target.imageID, filepath.Dir(target.dataPath), true); err != nil {
		return fmt.Errorf("%s - %w", location, maintenanceCommandError(ctx, "remove the verified local PostgreSQL data directory", err))
	}
	return nil
}

func (options MaintenanceOptions) completionMessage() string {
	if options.Clean {
		return "Local game data and generated runtime files removed"
	}
	if options.Full {
		return "Local game data removed"
	}
	return "Game state reset"
}

func validateMaintenanceFiles(paths projectPaths) error {
	const location = "operator.validateMaintenanceFiles"
	root := filepath.Dir(filepath.Dir(paths.config))
	if err := rejectSymlinkComponents(root); err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	for _, directory := range []string{paths.configDir, paths.deployment, paths.runtime} {
		info, err := os.Lstat(directory)
		if err != nil {
			return fmt.Errorf("%s - required maintenance directory is unavailable: %s", location, directory)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%s - refusing a non-directory maintenance path: %s", location, directory)
		}
	}
	for _, path := range []string{paths.config, paths.canonicalCompose, paths.adminEnv, paths.postgresEnv, paths.natsEnv, paths.redisEnv} {
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("%s - required maintenance file is unavailable: %s", location, path)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("%s - refusing a non-regular maintenance file: %s", location, path)
		}
	}
	if _, err := effectiveComposePath(paths); err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	if err := validateGeneratedRuntimeFiles(paths); err != nil {
		return fmt.Errorf("%s - validate generated runtime files: %w", location, err)
	}
	return nil
}

func validateGeneratedRuntimeFiles(paths projectPaths) error {
	const location = "operator.validateGeneratedRuntimeFiles"
	for _, path := range []string{paths.adminEnv, paths.postgresEnv, paths.postgresRuntimeEnv, paths.gameConfig, paths.natsEnv, paths.legacyBrokerEnv, paths.redisEnv, paths.generatedCompose} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("%s - inspect generated runtime file: %s", location, path)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("%s - refusing a non-regular generated runtime file: %s", location, path)
		}
	}
	return nil
}

func verifyFullResetSupport(project composeProject) error {
	const location = "operator.verifyFullResetSupport"
	for _, name := range []string{"postgres", "redis", "nats"} {
		if _, ok := project.Services[name]; !ok {
			return fmt.Errorf("%s - full reset supports only the built-in PostgreSQL, Redis, and NATS services", location)
		}
	}
	if err := verifyResetStorageVolumes(project); err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	postgres := project.Services["postgres"]
	if strings.TrimSpace(postgres.Image) != "forcad_postgres:local" || len(postgres.Volumes) != 1 {
		return fmt.Errorf("%s - full reset requires the standard PostgreSQL data mount", location)
	}
	mount := postgres.Volumes[0]
	if mount.Type != "bind" || !supportedPostgresTarget(mount.Target) {
		return fmt.Errorf("%s - full reset requires the standard PostgreSQL data bind mount", location)
	}
	return nil
}

func verifyResetStorageVolumes(project composeProject) error {
	const location = "operator.verifyFullResetSupport"
	for name, volume := range project.Volumes {
		if volume.External || (name != "forcad-config" && name != "forcad-nats") ||
			(volume.Name != "" && volume.Name != project.Name+"_"+name) {
			return fmt.Errorf("%s - full reset refuses external or unrecognized Docker volumes", location)
		}
	}
	if len(project.Services["redis"].Volumes) != 0 {
		return fmt.Errorf("%s - full reset refuses nonstandard Redis mounts", location)
	}
	natsMounts := project.Services["nats"].Volumes
	if _, ok := project.Volumes["forcad-nats"]; !ok || len(natsMounts) != 1 ||
		natsMounts[0].Type != "volume" || natsMounts[0].Source != "forcad-nats" || natsMounts[0].Target != "/data" {
		return fmt.Errorf("%s - full reset requires the standard NATS data volume", location)
	}
	return nil
}

func runPostgresCleanupContainer(
	ctx context.Context,
	runner MaintenanceRunner,
	imageID, parent string,
	removeData bool,
) error {
	const location = "operator.runPostgresCleanupContainer"
	if strings.ContainsAny(parent, ",\r\n\x00") {
		return fmt.Errorf("%s - unsupported character in PostgreSQL storage path", location)
	}
	mount := "type=bind,src=" + parent + ",dst=/cleanup,bind-recursive=disabled"
	if !removeData {
		mount += ",readonly"
	}
	script := inspectPostgresDataScript
	if removeData {
		script = deletePostgresDataScript
	}
	args := []string{
		"run", "--rm", "--pull=never", "--network=none", "--user=0:0", "--read-only",
		"--cap-drop=ALL", "--cap-add=DAC_OVERRIDE", "--cap-add=FOWNER",
		"--security-opt=no-new-privileges", "--mount", mount,
		"--entrypoint=/bin/sh", imageID, "-c", script,
	}
	if err := runDocker(ctx, runner, args, io.Discard); err != nil {
		return fmt.Errorf("%s - run guarded PostgreSQL cleanup container: %w", location, err)
	}
	return nil
}

func verifyPostgresMount(project composeProject, dataPath string) error {
	const location = "operator.verifyPostgresMount"
	mount := project.Services["postgres"].Volumes[0]
	source, err := filepath.Abs(mount.Source)
	if err != nil || filepath.Clean(source) != filepath.Clean(dataPath) {
		return fmt.Errorf("%s - full reset requires the standard docker_volumes/postgres/data bind mount", location)
	}
	return nil
}

func supportedPostgresTarget(target string) bool {
	target = strings.TrimRight(target, "/")
	return target == "/var/lib/postgresql" || target == "/var/lib/postgresql/data"
}

func presentStorageServices(services map[string]composeService) []string {
	var found []string
	for _, name := range []string{"postgres", "redis", "nats"} {
		if _, ok := services[name]; ok {
			found = append(found, name)
		}
	}
	return found
}

func postgresDataPath(paths projectPaths) (string, error) {
	const location = "operator.postgresDataPath"
	root := filepath.Dir(filepath.Dir(paths.config))
	data := filepath.Join(root, "docker_volumes", "postgres", "data")
	if err := rejectSymlinkComponents(root); err != nil {
		return "", fmt.Errorf("%s - %w", location, err)
	}
	for _, path := range []string{filepath.Join(root, "docker_volumes"), filepath.Join(root, "docker_volumes", "postgres")} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return data, nil
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("%s - refusing an unsupported PostgreSQL storage path: %s", location, path)
		}
	}
	return data, nil
}

func ensureNoMountedPaths(dataPath string) error {
	const location = "operator.ensureNoMountedPaths"
	if runtime.GOOS != "linux" {
		return fmt.Errorf("%s - full reset mount validation is supported only on Linux", location)
	}
	contents, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return fmt.Errorf("%s - could not inspect local mount table; refusing full reset", location)
	}
	if err := hasMountedPath(dataPath, contents); err != nil {
		return fmt.Errorf("%s - validate nested mounts: %w", location, err)
	}
	return nil
}

func hasMountedPath(dataPath string, contents []byte) error {
	const location = "operator.hasMountedPath"
	dataPath = filepath.Clean(dataPath)
	for line := range strings.SplitSeq(string(contents), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		mount, err := unescapeMountPath(fields[4])
		if err != nil {
			return fmt.Errorf("%s - local mount table is malformed; refusing full reset", location)
		}
		mount = filepath.Clean(mount)
		if mount == dataPath || strings.HasPrefix(mount, dataPath+string(os.PathSeparator)) {
			return fmt.Errorf("%s - refusing mounted filesystems inside PostgreSQL data", location)
		}
	}
	return nil
}

func unescapeMountPath(value string) (string, error) {
	const location = "operator.unescapeMountPath"
	var result strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' {
			result.WriteByte(value[i])
			continue
		}
		if i+3 >= len(value) {
			return "", fmt.Errorf("%s - incomplete mount escape", location)
		}
		escape := value[i+1 : i+4]
		switch escape {
		case "040":
			result.WriteByte(' ')
		case "011":
			result.WriteByte('\t')
		case "012":
			result.WriteByte('\n')
		case "134":
			result.WriteByte('\\')
		default:
			return "", fmt.Errorf("%s - unknown mount escape", location)
		}
		i += 3
	}
	return result.String(), nil
}

func rejectSymlinkComponents(path string) error {
	const location = "operator.rejectSymlinkComponents"
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("%s - could not resolve maintenance path", location)
	}
	volume := filepath.VolumeName(absolute)
	remainder := strings.TrimPrefix(absolute, volume)
	current := volume + string(os.PathSeparator)
	for part := range strings.SplitSeq(strings.TrimLeft(remainder, string(os.PathSeparator)), string(os.PathSeparator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("%s - maintenance path component is unavailable: %s", location, current)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s - refusing a symlink in the maintenance path: %s", location, current)
		}
	}
	return nil
}

func runDocker(
	ctx context.Context,
	runner MaintenanceRunner,
	args []string,
	stdout io.Writer,
) error {
	const location = "operator.runDocker"

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if err := runner(ctx, "docker", args, stdout, io.Discard); err != nil {
		return fmt.Errorf("%s - execute Docker command: %w", location, err)
	}
	return nil
}

func maintenanceCommandError(ctx context.Context, operation string, _ error) error {
	const location = "operator.maintenanceCommandError"
	if ctx.Err() != nil {
		return fmt.Errorf("%s - maintenance operation canceled: %w", location, ctx.Err())
	}
	return fmt.Errorf("%s - could not %s", location, operation)
}

func writeMaintenanceMessage(ctx context.Context, message string, out io.Writer) error {
	const location = "operator.writeMaintenanceMessage"
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s - maintenance canceled: %w", location, err)
	}
	if out == nil {
		out = io.Discard
	}
	_, err := fmt.Fprintln(out, message)
	if err != nil {
		return fmt.Errorf("%s - write maintenance result: %w", location, err)
	}
	return nil
}

func removeGeneratedRuntimeFiles(paths projectPaths) error {
	const location = "operator.removeGeneratedRuntimeFiles"
	if err := validateGeneratedRuntimeFiles(paths); err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	root, err := os.OpenRoot(paths.runtime)
	if err != nil {
		return fmt.Errorf("%s - open generated runtime directory: %w", location, err)
	}
	defer func() {
		_ = root.Close()
	}()
	for _, name := range []string{"admin.env", "postgres_environment.env", "postgres_runtime.env", "game.yml", "nats_environment.env", "rabbitmq_environment.env", "redis_environment.env"} {
		if err := root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s - remove generated runtime file %s: %w", location, name, err)
		}
	}
	if err := removeGeneratedCompose(paths); err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	return nil
}

func validateMaintenance(
	ctx context.Context,
	options MaintenanceOptions,
	runner MaintenanceRunner,
	resetData func(context.Context) error,
) error {
	const location = "operator.validateMaintenance"
	if ctx == nil {
		return fmt.Errorf("%s - maintenance context must not be nil", location)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s - maintenance canceled: %w", location, err)
	}
	if !options.Confirm {
		return fmt.Errorf("%s - maintenance requires explicit --confirm", location)
	}
	if options.Clean && options.Full {
		return fmt.Errorf("%s - clean already performs a full reset", location)
	}
	if !options.Clean && !options.Full && resetData == nil {
		return fmt.Errorf("%s - game data reset operation is required", location)
	}
	if runner == nil {
		return fmt.Errorf("%s - Docker command runner is required", location)
	}

	return nil
}

type fullResetTarget struct {
	dataPath string
	imageID  string
}

func prepareFullReset(
	ctx context.Context,
	runner MaintenanceRunner,
	project composeProject,
	paths projectPaths,
) (fullResetTarget, error) {
	const location = "operator.prepareFullReset"

	if err := verifyFullResetSupport(project); err != nil {
		return fullResetTarget{}, fmt.Errorf("%s - %w", location, err)
	}
	if err := requireLocalDocker(ctx, runner); err != nil {
		return fullResetTarget{}, fmt.Errorf("%s - %w", location, err)
	}
	dataPath, err := postgresDataPath(paths)
	if err != nil {
		return fullResetTarget{}, fmt.Errorf("%s - %w", location, err)
	}
	if err := verifyPostgresMount(project, dataPath); err != nil {
		return fullResetTarget{}, fmt.Errorf("%s - %w", location, err)
	}
	if err := ensureNoMountedPaths(dataPath); err != nil {
		return fullResetTarget{}, fmt.Errorf("%s - %w", location, err)
	}
	imageID, err := cachedPostgresImage(ctx, project.Services["postgres"].Image, runner)
	if err != nil {
		return fullResetTarget{}, fmt.Errorf("%s - %w", location, err)
	}
	if err := runPostgresCleanupContainer(ctx, runner, imageID, filepath.Dir(dataPath), false); err != nil {
		return fullResetTarget{}, fmt.Errorf("%s - %w", location, maintenanceCommandError(ctx, "validate the local PostgreSQL data directory", err))
	}
	return fullResetTarget{
		dataPath: dataPath,
		imageID:  imageID,
	}, nil
}

func resetGameData(
	ctx context.Context,
	runner MaintenanceRunner,
	composeArgs []string,
	project composeProject,
	resetData func(context.Context) error,
) error {
	const location = "operator.resetGameData"
	if err := runDocker(ctx, runner, append(append([]string(nil), composeArgs[1:]...), "stop", "--timeout", composeStopTimeoutSeconds), io.Discard); err != nil {
		return fmt.Errorf("%s - %w", location, maintenanceCommandError(ctx, "stop game services", err))
	}
	storages := presentStorageServices(project.Services)
	if len(storages) != 0 {
		args := append(append([]string(nil), composeArgs[1:]...), "up", "-d", "--wait", "--no-deps")
		args = append(args, storages...)
		if err := runDocker(ctx, runner, args, io.Discard); err != nil {
			return fmt.Errorf("%s - %w", location, maintenanceCommandError(ctx, "start storage services", err))
		}
	}
	if err := resetData(ctx); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s - maintenance canceled: %w", location, ctx.Err())
		}
		return fmt.Errorf("%s - storage reset failed; game services remain stopped", location)
	}
	if err := stopComposeProject(ctx, runner, composeArgs); err != nil {
		return fmt.Errorf("%s - remove stopped Compose project: %w", location, err)
	}
	return nil
}

func stopComposeProject(ctx context.Context, runner MaintenanceRunner, composeArgs []string) error {
	const location = "operator.stopComposeProject"
	args := append(append([]string(nil), composeArgs[1:]...), "down", "--volumes", "--remove-orphans", "--timeout", composeStopTimeoutSeconds)
	if err := runDocker(ctx, runner, args, io.Discard); err != nil {
		return fmt.Errorf("%s - %w", location, maintenanceCommandError(ctx, "stop the Compose project", err))
	}
	return nil
}
