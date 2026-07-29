package filetype

import (
	"path/filepath"
	"strings"
)

var modelMIMETypesByExtension = map[string]string{
	".glb":  "model/gltf-binary",
	".gltf": "model/gltf+json",
	".obj":  "model/obj",
	".stl":  "model/stl",
	".3mf":  "model/3mf",
}

func ModelMIMEType(filename string) string {
	return modelMIMETypesByExtension[strings.ToLower(filepath.Ext(filename))]
}

func IsModelMIMEType(mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	for _, supported := range modelMIMETypesByExtension {
		if mimeType == supported {
			return true
		}
	}
	return false
}
