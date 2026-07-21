package setting

import (
	"context"
	"errors"
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
	WallpaperColorScheme WallpaperColorScheme
}

func DefaultSystemConfiguration() SystemConfiguration {
	return SystemConfiguration{WallpaperColorScheme: WallpaperColorSchemeContent}
}

var ErrQuickCommandNotFound = errors.New("quick command not found")

type Repository interface {
	Create(ctx context.Context, command QuickCommand) error
	List(ctx context.Context, query QuickCommandQuery) (QuickCommandPage, error)
	Delete(ctx context.Context, id QuickCommandID) error
	GetSystemConfiguration(ctx context.Context) (SystemConfiguration, error)
	SaveSystemConfiguration(ctx context.Context, configuration SystemConfiguration) error
}
