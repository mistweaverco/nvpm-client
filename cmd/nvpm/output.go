package nvpm

import (
	"bytes"
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

// FormatJSON encodes data as indented JSON (trailing newline, same as PrintJSON).
func FormatJSON(data interface{}) (string, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// PrintJSON outputs data as JSON
func PrintJSON(data interface{}) error {
	s, err := FormatJSON(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(os.Stdout, s)
	return err
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
