package filestore

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestAppearanceWallpaperStoreUsesManagedDirectory(t *testing.T) {
	store := New(t.TempDir())
	id := "0123456789abcdef0123456789abcdef"
	want := []byte("wallpaper")
	if err := store.SaveWallpaper(context.Background(), id, bytes.NewReader(want)); err != nil {
		t.Fatalf("SaveWallpaper() error = %v", err)
	}
	reader, err := store.OpenWallpaper(context.Background(), id)
	if err != nil {
		t.Fatalf("OpenWallpaper() error = %v", err)
	}
	got, _ := io.ReadAll(reader)
	reader.Close()
	if !bytes.Equal(got, want) {
		t.Fatalf("OpenWallpaper() = %q", got)
	}
	if err := store.DeleteWallpaper(context.Background(), id); err != nil {
		t.Fatalf("DeleteWallpaper() error = %v", err)
	}
	if _, err := store.OpenWallpaper(context.Background(), id); err == nil {
		t.Fatal("OpenWallpaper() after delete expected error")
	}
}

func TestAppearanceWallpaperStoreRejectsUnmanagedID(t *testing.T) {
	store := New(t.TempDir())
	if err := store.SaveWallpaper(context.Background(), "../outside", bytes.NewReader([]byte("x"))); err == nil {
		t.Fatal("SaveWallpaper() expected invalid id error")
	}
	if _, err := store.OpenWallpaper(context.Background(), "../outside"); err == nil {
		t.Fatal("OpenWallpaper() expected invalid id error")
	}
}
