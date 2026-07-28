package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitAssetFileSpec(t *testing.T) {
	fileName, subdir := SplitAssetFileSpec("lua-language-server-{{version}}-linux-x64.tar.gz:libexec/")
	assert.Equal(t, "lua-language-server-{{version}}-linux-x64.tar.gz", fileName)
	assert.Equal(t, "libexec", subdir)

	fileName, subdir = SplitAssetFileSpec("tool.zip")
	assert.Equal(t, "tool.zip", fileName)
	assert.Equal(t, "", subdir)
}

func TestAssetArchiveFileName(t *testing.T) {
	var file registry_parser.RegistryItemSourceAssetFile
	require.NoError(t, json.Unmarshal([]byte(`"pkg-{{version}}.tar.gz:libexec/"`), &file))
	assert.Equal(t, "pkg-3.18.2.tar.gz", AssetArchiveFileName(file, "3.18.2"))
}

func TestParseBinSpec(t *testing.T) {
	exec, rel := ParseBinSpec("exec:libexec/bin/lua-language-server")
	assert.True(t, exec)
	assert.Equal(t, "libexec/bin/lua-language-server", rel)

	exec, rel = ParseBinSpec("bin/tool")
	assert.False(t, exec)
	assert.Equal(t, "bin/tool", rel)
}

func TestInstallReleaseAssetContentsAndLinkReleaseBins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NVPM_HOME", home)
	_ = files.GetAppDataPath()
	_ = files.GetAppBinPath()

	extractDir := t.TempDir()
	binDir := filepath.Join(extractDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "lua-language-server"), []byte("#!/bin/sh\n"), 0o755))

	repoPath := filepath.Join(home, "packages", "github", "LuaLS_lua-language-server")
	require.NoError(t, os.MkdirAll(repoPath, 0o755))

	var file registry_parser.RegistryItemSourceAssetFile
	require.NoError(t, json.Unmarshal([]byte(`"pkg.tar.gz:libexec/"`), &file))
	asset := &registry_parser.RegistryItemSourceAsset{
		File: file,
		Bin:  "exec:libexec/bin/lua-language-server",
	}

	require.NoError(t, InstallReleaseAssetContents(extractDir, repoPath, asset))

	installedBin := filepath.Join(repoPath, "libexec", "bin", "lua-language-server")
	_, err := os.Stat(installedBin)
	require.NoError(t, err)

	registryItem := registry_parser.RegistryItem{
		Bin: map[string]string{
			"lua-language-server": "{{source.asset.bin}}",
		},
	}
	require.NoError(t, LinkReleaseBins(repoPath, asset, registryItem))

	wrapper := filepath.Join(files.GetAppBinPath(), "lua-language-server")
	data, err := os.ReadFile(wrapper)
	require.NoError(t, err)
	assert.Contains(t, string(data), installedBin)
}

func TestFindMatchingAssetLinuxGnuFallback(t *testing.T) {
	assets := registry_parser.RegistryItemSourceAssetList{
		{
			Target: "linux_x64_gnu",
			File:   mustAssetFile(t, `"tool-linux-x64.tar.gz"`),
		},
	}

	if DetectRegistryTarget() != "linux_x64" {
		t.Skip("linux_x64 gnu fallback is platform-specific")
	}

	asset := FindMatchingAsset(assets)
	require.NotNil(t, asset)
	assert.Equal(t, "linux_x64_gnu", asset.Target)
}

func mustAssetFile(t *testing.T, raw string) registry_parser.RegistryItemSourceAssetFile {
	t.Helper()
	var file registry_parser.RegistryItemSourceAssetFile
	require.NoError(t, json.Unmarshal([]byte(raw), &file))
	return file
}
