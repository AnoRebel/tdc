package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// parseFile reads a configuration file.
//
// The format is deliberately minimal: "section.key = value", one per line,
// with # for comments. A small format means an operator can read and edit it
// without knowing a schema language, and it maps directly onto environment
// variables, so the same setting is written the same way in both places.
func parseFile(path string) (map[string]string, error) {
	handle, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", errMissingFile, path)
		}
		return nil, fmt.Errorf("open configuration file: %w", err)
	}
	defer func() { _ = handle.Close() }()

	values := map[string]string{}
	scanner := bufio.NewScanner(handle)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			return nil, fmt.Errorf("%s:%d: expected \"key = value\", got %q", path, lineNumber, line)
		}

		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("%s:%d: empty key", path, lineNumber)
		}

		values[key] = unquote(strings.TrimSpace(value))
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read configuration file: %w", err)
	}

	return values, nil
}

// unquote strips one layer of matching quotes, so a value with leading or
// trailing spaces can be written explicitly.
func unquote(value string) string {
	if len(value) < 2 {
		return value
	}
	first, last := value[0], value[len(value)-1]
	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}
