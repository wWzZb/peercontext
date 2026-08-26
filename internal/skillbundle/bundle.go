// Package skillbundle exposes the version-matched peer-context Skill embedded
// in peerctx. The runtime never reads repository Skill files.
package skillbundle

import (
	"errors"
	"sort"
)

//go:generate go run ./cmd/generate

const Name = "peer-context"

func Paths() []string {
	paths := make([]string, 0, len(generatedFiles))
	for path := range generatedFiles {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func Read(path string) ([]byte, error) {
	content, ok := generatedFiles[path]
	if !ok {
		return nil, errors.New("skill file not found")
	}
	return []byte(content), nil
}
