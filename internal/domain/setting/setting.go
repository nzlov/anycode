package setting

import (
	"context"
	"errors"
	"io"
	"time"
)

type QuickCommandID string

type QuickCommand struct {
	ID        QuickCommandID
	Content   string
	CreatedAt time.Time
}

type QuickCommandQuery struct {
	Page     int
	PageSize int
}

type QuickCommandPage struct {
	Items    []QuickCommand
	Page     int
	PageSize int
	Total    int
}

type WallpaperColorScheme string

type BackgroundType string

const (
	BackgroundTypeSolid BackgroundType = "solid"
	BackgroundTypeImage BackgroundType = "image"
	BackgroundTypeBing  BackgroundType = "bing"
	BackgroundTypeNASA  BackgroundType = "nasa"
)

func (backgroundType BackgroundType) Valid() bool {
	switch backgroundType {
	case BackgroundTypeSolid, BackgroundTypeImage, BackgroundTypeBing, BackgroundTypeNASA:
		return true
	default:
		return false
	}
}

type SolidTheme string

const (
	SolidThemeVermilion SolidTheme = "vermilion"
	SolidThemeAmber     SolidTheme = "amber"
	SolidThemeBamboo    SolidTheme = "bamboo"
	SolidThemeAzure     SolidTheme = "azure"
	SolidThemeIndigo    SolidTheme = "indigo"
	SolidThemePurple    SolidTheme = "purple"
	SolidThemePeach     SolidTheme = "peach"
	SolidThemeInk       SolidTheme = "ink"
)

func (theme SolidTheme) Valid() bool {
	switch theme {
	case SolidThemeVermilion,
		SolidThemeAmber,
		SolidThemeBamboo,
		SolidThemeAzure,
		SolidThemeIndigo,
		SolidThemePurple,
		SolidThemePeach,
		SolidThemeInk:
		return true
	default:
		return false
	}
}

const (
	WallpaperColorSchemeContent    WallpaperColorScheme = "content"
	WallpaperColorSchemeFidelity   WallpaperColorScheme = "fidelity"
	WallpaperColorSchemeTonalSpot  WallpaperColorScheme = "tonal_spot"
	WallpaperColorSchemeVibrant    WallpaperColorScheme = "vibrant"
	WallpaperColorSchemeExpressive WallpaperColorScheme = "expressive"
	WallpaperColorSchemeRainbow    WallpaperColorScheme = "rainbow"
	WallpaperColorSchemeFruitSalad WallpaperColorScheme = "fruit_salad"
	WallpaperColorSchemeNeutral    WallpaperColorScheme = "neutral"
	WallpaperColorSchemeMonochrome WallpaperColorScheme = "monochrome"
)

func (scheme WallpaperColorScheme) Valid() bool {
	switch scheme {
	case WallpaperColorSchemeContent,
		WallpaperColorSchemeFidelity,
		WallpaperColorSchemeTonalSpot,
		WallpaperColorSchemeVibrant,
		WallpaperColorSchemeExpressive,
		WallpaperColorSchemeRainbow,
		WallpaperColorSchemeFruitSalad,
		WallpaperColorSchemeNeutral,
		WallpaperColorSchemeMonochrome:
		return true
	default:
		return false
	}
}

type SystemConfiguration struct {
	BackgroundType       BackgroundType
	SolidTheme           SolidTheme
	BackgroundMask       int
	WallpaperColorScheme WallpaperColorScheme
	WallpaperID          string
	WallpaperFilename    string
	WallpaperMimeType    string
}

func DefaultSystemConfiguration() SystemConfiguration {
	return SystemConfiguration{
		BackgroundType:       BackgroundTypeBing,
		SolidTheme:           SolidThemeVermilion,
		BackgroundMask:       0,
		WallpaperColorScheme: WallpaperColorSchemeContent,
	}
}

var ErrQuickCommandNotFound = errors.New("quick command not found")

type Repository interface {
	Create(ctx context.Context, command QuickCommand) error
	List(ctx context.Context, query QuickCommandQuery) (QuickCommandPage, error)
	Delete(ctx context.Context, id QuickCommandID) error
	GetSystemConfiguration(ctx context.Context) (SystemConfiguration, error)
	SaveSystemConfiguration(ctx context.Context, configuration SystemConfiguration) error
}

type WallpaperStore interface {
	SaveWallpaper(ctx context.Context, id string, reader io.Reader) error
	OpenWallpaper(ctx context.Context, id string) (io.ReadCloser, error)
	DeleteWallpaper(ctx context.Context, id string) error
}

type RemoteWallpaper struct {
	MimeType string
	Reader   io.ReadCloser
}

type NASAWallpaperSource interface {
	Open(ctx context.Context) (RemoteWallpaper, error)
}
