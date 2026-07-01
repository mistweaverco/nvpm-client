package boot

import (
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/providers"
)

var DEFAULT_REGISTRY_URL = "https://github.com/mistweaverco/nvpm-registry/releases/latest/download/nvpm-registry.json.zip"

type errMsg error

type downloadStartedMsg struct{}
type downloadFinishedMsg struct{}
type cacheValidMsg struct{}
type unzipStartedMsg struct{}
type unzipFinishedMsg struct{}
type syncLocalPackagesStartedMsg struct{}
type syncLocalPackagesFinishedMsg struct{}

type model struct {
	spinner          spinner.Model
	quitting         bool
	err              error
	message          string
	downloading      bool
	downloadFinished bool
	registryURL      string
	cacheMaxAge      time.Duration
}

func initialModel(cacheMaxAge time.Duration) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	// Use the same resolution logic as the rest of the client (supports multi-URL config).
	// For the TUI we just display/use the first URL; the download itself will try all.
	registryURL := DEFAULT_REGISTRY_URL
	if urls := files.ResolveRegistryURLs(); len(urls) > 0 {
		registryURL = urls[0]
	}
	return model{
		spinner:     s,
		message:     "Checking registry cache...",
		registryURL: registryURL,
		cacheMaxAge: cacheMaxAge,
	}
}

func (m model) checkCache() tea.Cmd {
	return func() tea.Msg {
		cachePath := files.GetRegistryCachePath()
		if files.IsCacheValid(cachePath, m.cacheMaxAge) {
			return cacheValidMsg{}
		}
		return downloadStartedMsg{}
	}
}

func (m model) performDownload() tea.Cmd {
	return func() tea.Msg {
		err := files.DownloadRegistryZipWithCache(m.cacheMaxAge)
		if err != nil {
			return errMsg(err)
		}
		return downloadFinishedMsg{}
	}
}

func (m model) unzipRegistry() tea.Cmd {
	return func() tea.Msg {
		return unzipStartedMsg{}
	}
}

func (m model) performUnzip() tea.Cmd {
	return func() tea.Msg {
		cachePath := files.GetRegistryCachePath()
		if err := files.Unzip(cachePath, files.GetCachePath()); err != nil {
			return errMsg(err)
		}
		return unzipFinishedMsg{}
	}
}

func (m model) syncLocalPackages() tea.Cmd {
	return func() tea.Msg {
		return syncLocalPackagesStartedMsg{}
	}
}

func (m model) performSyncLocalPackages() tea.Cmd {
	return func() tea.Msg {
		_ = providers.SyncAllFromLock()
		return syncLocalPackagesFinishedMsg{}
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.checkCache())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		default:
			return m, nil
		}

	case errMsg:
		m.err = msg
		return m, nil

	case cacheValidMsg:
		m.message = "Using cached registry"
		return m, m.unzipRegistry()

	case downloadStartedMsg:
		m.message = "Downloading registry"
		m.downloading = true
		return m, m.performDownload()

	case downloadFinishedMsg:
		m.message = "Registry downloaded successfully!"
		m.downloadFinished = true
		return m, m.unzipRegistry()

	case unzipStartedMsg:
		m.message = "Unzipping registry"
		return m, m.performUnzip()

	case unzipFinishedMsg:
		m.message = "Registry unzipped successfully!"
		return m, m.syncLocalPackages()

	case syncLocalPackagesStartedMsg:
		m.message = "Syncing local packages"
		return m, m.performSyncLocalPackages()

	case syncLocalPackagesFinishedMsg:
		m.message = "Local packages synced successfully!"
		m.quitting = true
		return m, tea.Quit

	default:
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
}

func (m model) View() string {
	if m.err != nil {
		return m.err.Error()
	}
	str := fmt.Sprintf("\n\n  %s "+m.message+"\n\n", m.spinner.View())
	if m.quitting {
		return str + "\n"
	}
	return str
}

func Start(cacheMaxAge time.Duration) {
	p := tea.NewProgram(initialModel(cacheMaxAge))
	if _, err := p.Run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
