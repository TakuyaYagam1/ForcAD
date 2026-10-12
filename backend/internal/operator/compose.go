package operator

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"go.yaml.in/yaml/v3"
)

func externalCompose(paths projectPaths, options SetupOptions) ([]byte, error) {
	const location = "operator.externalCompose"
	data, err := readRegularFile(paths.canonicalCompose)
	if err != nil {
		return nil, fmt.Errorf("%s - read canonical Compose file: %w", location, err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s - canonical Compose file must contain a mapping", location)
	}
	services, ok := mappingValue(root.Content[0], "services")
	if !ok || services.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s - canonical Compose file has no services mapping", location)
	}
	removed := make(map[string]struct{}, 4)
	if options.Redis != "" {
		removeMappingPair(services, "redis")
		removed["redis"] = struct{}{}
	}
	if options.Database != "" {
		removeMappingPair(services, "postgres")
		removeMappingPair(services, "postgres-data")
		removed["postgres"] = struct{}{}
		removed["postgres-data"] = struct{}{}
	}
	if options.RabbitMQ != "" {
		removeMappingPair(services, "rabbitmq")
		removed["rabbitmq"] = struct{}{}
	}
	if len(removed) > 0 {
		visited := make(map[*yaml.Node]struct{})
		for i := 1; i < len(services.Content); i += 2 {
			pruneComposeDependencies(services.Content[i], removed, visited)
		}
	}
	var encoded bytes.Buffer
	if err := yaml.NewEncoder(&encoded).Encode(root.Content[0]); err != nil {
		return nil, fmt.Errorf("%s - encode generated Compose file: %w", location, err)
	}
	return append([]byte(generatedComposeMarker+"\n"), encoded.Bytes()...), nil
}

// pruneComposeDependencies follows anchors and merge aliases so overrides do
// not retain dependencies on storage services removed from the project.
func pruneComposeDependencies(node *yaml.Node, removed map[string]struct{}, visited map[*yaml.Node]struct{}) {
	if node == nil {
		return
	}
	if node.Kind == yaml.AliasNode {
		pruneComposeDependencies(node.Alias, removed, visited)
		return
	}
	if _, ok := visited[node]; ok {
		return
	}
	visited[node] = struct{}{}
	switch node.Kind {
	case yaml.DocumentNode, yaml.ScalarNode, yaml.AliasNode:
		return
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if key.Value == "depends_on" {
				pruneDependencyList(value, removed, visited)
				continue
			}
			pruneComposeDependencies(value, removed, visited)
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			pruneComposeDependencies(item, removed, visited)
		}
	}
}

func pruneDependencyList(node *yaml.Node, removed map[string]struct{}, visited map[*yaml.Node]struct{}) {
	if node == nil {
		return
	}
	if node.Kind == yaml.AliasNode {
		pruneDependencyList(node.Alias, removed, visited)
		return
	}
	if _, ok := visited[node]; ok {
		return
	}
	visited[node] = struct{}{}
	switch node.Kind {
	case yaml.DocumentNode, yaml.ScalarNode, yaml.AliasNode:
		return
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); {
			key := node.Content[i]
			if key.Value == "<<" {
				pruneDependencyList(node.Content[i+1], removed, visited)
				i += 2
				continue
			}
			if _, ok := removed[key.Value]; ok {
				node.Content = append(node.Content[:i], node.Content[i+2:]...)
				continue
			}
			i += 2
		}
	case yaml.SequenceNode:
		kept := node.Content[:0]
		for _, item := range node.Content {
			if name, ok := yamlScalarValue(item); ok {
				if _, remove := removed[name]; remove {
					continue
				}
			}
			pruneDependencyList(item, removed, visited)
			kept = append(kept, item)
		}
		node.Content = kept
	}
}

func yamlScalarValue(node *yaml.Node) (string, bool) {
	if node == nil {
		return "", false
	}
	if node.Kind == yaml.AliasNode {
		return yamlScalarValue(node.Alias)
	}
	return node.Value, node.Kind == yaml.ScalarNode
}

func mappingPairIndex(node *yaml.Node, key string) int {
	if node == nil || node.Kind != yaml.MappingNode {
		return -1
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func mappingValue(node *yaml.Node, key string) (*yaml.Node, bool) {
	index := mappingPairIndex(node, key)
	if index < 0 {
		return nil, false
	}
	return node.Content[index+1], true
}

func hasPath(root *yaml.Node, path ...string) bool {
	current := root
	for _, key := range path {
		var ok bool
		current, ok = mappingValue(current, key)
		if !ok {
			return false
		}
	}
	return true
}

func removeMappingPair(node *yaml.Node, key string) {
	index := mappingPairIndex(node, key)
	if index < 0 {
		return
	}
	node.Content = append(node.Content[:index], node.Content[index+2:]...)
}

func removeGeneratedCompose(paths projectPaths) error {
	const location = "operator.removeGeneratedCompose"
	info, err := os.Lstat(paths.generatedCompose)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s - inspect generated Compose file: %w", location, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%s - not a regular generated Compose file: %s", location, paths.generatedCompose)
	}
	if _, err := effectiveComposePath(paths); err != nil {
		return err
	}
	if err := os.Remove(paths.generatedCompose); err != nil {
		return fmt.Errorf("%s - remove generated Compose file: %w", location, err)
	}
	return nil
}
