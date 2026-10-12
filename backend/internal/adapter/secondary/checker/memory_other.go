//go:build !linux

package checker

func applyMemoryLimit(executable string, args []string, _ int, _ string) (string, []string, error) {
	return executable, args, nil
}
