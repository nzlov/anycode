package filetype

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"mime"
	"path/filepath"
	"strings"
)

func RefineBrowserMediaMIMEType(filename string, detected string, sample []byte) string {
	originalDetected := strings.ToLower(strings.TrimSpace(detected))
	detected = normalizeMIMEType(detected)
	byExtension := normalizeMIMEType(mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))))
	switch {
	case byExtension == "image/svg+xml" &&
		(detected == "text/plain" || detected == "application/octet-stream") && isSVG(sample):
		return byExtension
	case byExtension == "image/avif" && detected == "application/octet-stream" && isAVIF(sample):
		return byExtension
	case byExtension == "audio/flac" &&
		(detected == "application/octet-stream" || detected == "text/plain") && bytes.HasPrefix(sample, []byte("fLaC")):
		return byExtension
	case byExtension == "audio/aac" && detected == "application/octet-stream" && isAAC(sample):
		return byExtension
	case detected == "application/ogg" && (strings.HasPrefix(byExtension, "audio/") || strings.HasPrefix(byExtension, "video/")):
		return byExtension
	case detected == "video/mp4" && byExtension == "audio/mp4":
		return byExtension
	default:
		return originalDetected
	}
}

func normalizeMIMEType(value string) string {
	if mediaType, _, err := mime.ParseMediaType(value); err == nil {
		return strings.ToLower(mediaType)
	}
	return strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
}

func isSVG(sample []byte) bool {
	decoder := xml.NewDecoder(bytes.NewReader(sample))
	for {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		if start, ok := token.(xml.StartElement); ok {
			return strings.EqualFold(start.Name.Local, "svg")
		}
	}
}

func isAVIF(sample []byte) bool {
	if len(sample) < 16 || string(sample[4:8]) != "ftyp" {
		return false
	}
	size := int(binary.BigEndian.Uint32(sample[:4]))
	if size < 16 || size > len(sample) {
		return false
	}
	for offset := 8; offset+4 <= size; offset += 4 {
		brand := string(sample[offset : offset+4])
		if brand == "avif" || brand == "avis" {
			return true
		}
	}
	return false
}

func isAAC(sample []byte) bool {
	return len(sample) >= 2 && sample[0] == 0xff && sample[1]&0xf6 == 0xf0
}
