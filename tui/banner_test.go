package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

func TestBannerBodyStyle(t *testing.T) {
	style := BannerBodyStyle()
	assert.IsType(t, lipgloss.Style{}, style)
	assert.Equal(t, bannerMaxWidth, style.GetWidth())
	assert.Equal(t, bannerForegroupColor, style.GetForeground())
}

func TestTitleColor(t *testing.T) {
	color := TitleColor()
	assert.IsType(t, lipgloss.AdaptiveColor{}, color)
	assert.Equal(t, bannerTitleColor, color)
}

func TestShowBanner(t *testing.T) {
	originalHasTTY := HasTTY
	defer func() { HasTTY = originalHasTTY }()
	
	HasTTY = false
	ShowBanner("Test Title", "Test Body", false)
	
}
