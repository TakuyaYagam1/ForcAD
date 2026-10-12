package operator

import "fmt"

// ValidateConfig checks YAML without modifying it or connecting to storage.
func ValidateConfig(path string, full bool) error {
	const location = "operator.ValidateConfig"
	data, err := readRegularFile(path)
	if err != nil {
		return fmt.Errorf("%s - read configuration: %w", location, err)
	}
	document, tree, err := decodeSetupDocument(data)
	if err != nil {
		return err
	}
	if !full {
		return nil
	}
	if document.Admin == nil || document.Storages == nil {
		return fmt.Errorf("%s - full configuration requires admin and storages; run setup first", location)
	}
	// Both sections are present, so this validates credentials and fills defaults only in memory.
	return completeCredentials(&document, tree)
}
