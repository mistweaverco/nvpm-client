package nvpm

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/config"
)

// GetOutputMode returns the current output mode from config
func GetOutputMode() config.OutputMode {
	if getColorConfigFunc != nil {
		return getColorConfigFunc().Output
	}
	return config.OutputModeRich
}

// ShouldUsePlainOutput returns true if output should be plain (no colors, no icons)
func ShouldUsePlainOutput() bool {
	return GetOutputMode() == config.OutputModePlain
}

// ShouldUseJSONOutput returns true if output should be JSON
func ShouldUseJSONOutput() bool {
	return GetOutputMode() == config.OutputModeJSON
}

// PrintJSON outputs data as JSON
func PrintJSON(data interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

func printProviderRequirementError(packageID string, err error) {
	if err == nil {
		return
	}
	if ShouldUseJSONOutput() {
		_ = PrintJSON(map[string]string{
			"package": packageID,
			"error":   err.Error(),
		})
		return
	}
	fmt.Printf("%s Cannot install %s:\n%s\n\n", IconClose(), packageID, err.Error())
}

// RemoveMarkdownFormatting removes markdown formatting from text
func RemoveMarkdownFormatting(text string) string {
	// Remove markdown headers
	text = regexp.MustCompile(`(?m)^#+\s*`).ReplaceAllString(text, "")
	// Remove bold/italic
	text = regexp.MustCompile(`\*\*([^*]+)\*\*`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`\*([^*]+)\*`).ReplaceAllString(text, "$1")
	// Remove code blocks
	text = regexp.MustCompile("(?s)```[^`]*```").ReplaceAllString(text, "")
	text = regexp.MustCompile("`([^`]+)`").ReplaceAllString(text, "$1")
	// Remove links (markdown and plain)
	text = regexp.MustCompile(`\[([^\]]+)\]\([^\)]+\)`).ReplaceAllString(text, "$1")
	// Remove extra whitespace
	text = regexp.MustCompile(`\n\s*\n\s*\n`).ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}
