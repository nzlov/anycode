package filetype

import (
	"encoding/binary"
	"testing"
)

func TestRefineBrowserMediaMIMETypeRecognizesVerifiedFormats(t *testing.T) {
	avif := make([]byte, 24)
	binary.BigEndian.PutUint32(avif[:4], uint32(len(avif)))
	copy(avif[4:8], "ftyp")
	copy(avif[8:12], "mif1")
	copy(avif[16:20], "avif")
	tests := []struct {
		name     string
		detected string
		sample   []byte
		want     string
	}{
		{"icon.svg", "text/plain; charset=utf-8", []byte("<?xml version=\"1.0\"?><svg xmlns=\"http://www.w3.org/2000/svg\"/>"), "image/svg+xml"},
		{"photo.avif", "application/octet-stream", avif, "image/avif"},
		{"sound.flac", "application/octet-stream", []byte("fLaCdata"), "audio/flac"},
		{"sound.aac", "application/octet-stream", []byte{0xff, 0xf1, 0x50, 0x80}, "audio/aac"},
		{"sound.oga", "application/ogg", []byte("OggS"), "audio/ogg"},
		{"clip.ogv", "application/ogg", []byte("OggS"), "video/ogg"},
		{"sound.m4a", "video/mp4", []byte("mp4"), "audio/mp4"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := RefineBrowserMediaMIMEType(test.name, test.detected, test.sample); got != test.want {
				t.Fatalf("RefineBrowserMediaMIMEType() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRefineBrowserMediaMIMETypeRejectsExtensionOnlyMatches(t *testing.T) {
	for _, filename := range []string{"forged.svg", "forged.avif", "forged.flac", "forged.aac"} {
		if got := RefineBrowserMediaMIMEType(filename, "application/octet-stream", []byte("not media")); got != "application/octet-stream" {
			t.Errorf("RefineBrowserMediaMIMEType(%q) = %q", filename, got)
		}
	}
}
