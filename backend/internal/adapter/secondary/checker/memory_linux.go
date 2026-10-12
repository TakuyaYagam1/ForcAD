//go:build linux

package checker

import (
	"fmt"
	"strconv"
)

func applyMemoryLimit(executable string, args []string, memoryLimitMB int, envPath string) (string, []string, error) {
	const location = "checker.applyMemoryLimit"
	if memoryLimitMB <= 0 {
		return executable, args, nil
	}
	limiter, err := resolveExecutable("prlimit", patchedEnvironment(envPath))
	if err != nil {
		return "", nil, fmt.Errorf("%s - checker memory limit requires prlimit: %w", location, err)
	}
	limitedArgs := make([]string, 0, len(args)+3)
	limitedArgs = append(limitedArgs, "--as="+strconv.FormatInt(int64(memoryLimitMB)*1024*1024, 10), "--", executable)
	limitedArgs = append(limitedArgs, args...)
	return limiter, limitedArgs, nil
}
