<div align="center">

![NVPM logo][logo]

# nvpm-client

[![Made with love][badge-made-with-love]][contributors]
[![Go][badge-golang]][golang-website]
[![Development status][badge-development-status]][development-status]
[![Discord][badge-discord]][discord]
[![IRC][badge-irc]][irc]
[![Our manifesto][badge-our-manifesto]][our-manifesto]
[![Latest release][badge-latest-release]][latest-release]

[Terms used](#requirements) •
[Requirements](#requirements) •
[Install](#install) •
[Usage](#usage) •
[Supported providers](#supported-providers)

<p></p>

![NVPM Demo](assets/demo.webp)

<p></p>

NVPM 🌈 aims to be an editor-agnostic 🫶 package manager 📦 for
Tree-sitter parsers, LSP servers, DAP servers,
linters and formatters and more.



<p></p>

</div>

## Terms used

- *Tree-sitter*: A parser generator tool and an incremental parsing library.
- *Language Server Protocol* (LSP): A protocol that defines
  how code editors and IDEs communicate with language servers.
- *Debug Adapter Protocol* (DAP): A protocol that defines
  how code editors and IDEs communicate with debuggers.
- *Package*: A package is a LSP server, DAP server, formatter
  or linter that can be installed via NVPM.
- *Provider*: A provider is a package source,
    e.g., `npm`, `pypi`, `golang`, etc.
- *Package ID*: A package ID is a unique identifier for a package,
    e.g., `npm:@mistweavercokulala-ls@0.1.0`.
- *NVPM Registry*: The NVPM Registry is a registry of
    available packages that can be installed via NVPM.
- *Terminal User Interface* (TUI): A text-based user interface
  that runs in a terminal emulator.

> [!NOTE]
> The nvpm client defaults to the [NVPM Registry][nvpm-registry] to
> install and manage packages.
> This can be configured to use other registries as well.
> The client then merges all registries together and
> deduplicates the packages by their package ID.

## Requirements

NVPM is a CLI, therefore you need to have a terminal emulator available.

Besides that, we shell out a lot to install packages.

E.g. if you want to install `npm` packages,
you need to have `npm` installed.

For the packages to work in Neovim, you either need to
[nvpm.nvim] installed,
or source the environment setup in your shell.

```sh
source <(nvpm env)
```

## Install

Just head over to the [download page][download-website] or
grab it directtly from the [releases][latest-release].

## Usage

The heart of NVPM is its `nvpm-lock.json` file.
This file is used to keep track of the installed packages and their versions.

You can tell NVPM where to find the `nvpm-lock.json` (and optional `config.yaml`)
by setting the environment variable `NVPM_HOME`.

If `NVPM_HOME` isn't set,
NVPM will look for the `nvpm-lock.json` file in the default locations:

- Linux: `$XDG_CONFIG_HOME/nvpm/nvpm-lock.json` or
  `$HOME/.config/nvpm/nvpm-lock.json`
- macOS: `$HOME/Library/Application Support/nvpm/nvpm-lock.json`
- Windows: `%APPDATA%\nvpm\nvpm-lock.json`

If the file doesn't exist,
NVPM will create it for you (when you install a package).

NVPM's cache directory is controlled separately via `NVPM_CACHE`.
If `NVPM_CACHE` isn't set, NVPM uses OS defaults:

```
- Linux: `~/.cache/nvpm`
- macOS: `~/Library/Caches/nvpm`
- Windows: `%LOCALAPPDATA%\nvpm\cache`
```

It's advised to keep the `nvpm-lock.json` file in version control.

### Modify environment path

If you want the installed packages to be available in your path,
you can add the following to your shell configuration file:

#### bash environment setup

add to `~/.bashrc`:

```sh
source <(nvpm env)
```

#### zsh environment setup

add to `~/.zshrc`:

```sh
source <(nvpm env zsh)
```

or with [evalcache](https://github.com/mroth/evalcache) for zsh,
add to `~/.zshrc`:

```sh
_evalcache nvpm env zsh
```

#### PowerShell environment setup

add to `profile`:

```sh
nvpm env powershell | Invoke-Expression
```

### CLI autocompletion

If you want autocompletion for the CLI commands,
you can add the following to your shell configuration file:

#### bash autocompletion setup

add to `~/.bashrc`:

```sh
source <(nvpm completion bash)
```

#### zsh autocompletion setup

add to `~/.zshrc`:

```sh
source <(nvpm completion zsh)
```

#### fish autocompletion setup

add to `~/.config/fish/completions/nvpm.fish`:

```sh
nvpm completion fish > ~/.config/fish/completions/nvpm.fish
```

#### powershell autocompletion setup

add to `profile`:

```sh
nvpm completion powershell | Invoke-Expression
```

### CLI Options

You can run `nvpm --help` to see the available CLI options.

#### nvpm show

`show/info/details` shows information about one or more packages.

```sh
nvpm show \
  npm:@mistweavercokulala-ls@0.1.0 \
  pypi:black \
  golang:golangci-lint
```

#### nvpm install

`install`/`add` install packages

```sh
nvpm install \
  npm:@mistweavercokulala-ls@0.1.0 \
  pypi:black \
  golang:golangci-lint
```

#### nvpm sync

`sync` syncs the installed packages or registry data.

For packages,
it'll make sure exactly the same packages are installed
that are listed in the `nvpm-lock.json` file.

```sh
nvpm sync packages
```

For registry data,
it'll update the local registry cache
with the latest data from the NVPM Registry.

```sh
nvpm sync registry
```

The registry data is cached locally,
but with the `sync registry` command you can force an update.

You can control how long `nvpm` considers the downloaded registry zip "fresh":

- via `config.yaml` (recommended)

The optional `config.yaml` lives next to `nvpm-lock.json` in your NVPM config dir
(usually `~/.config/nvpm/config.yaml`, or `$NVPM_HOME/config.yaml`).

Example:

```yaml
# yaml-language-server: $schema=https://nvpm.dev/client-config.schema.json
paths:
  cacheDir: ~/.cache/nvpm
registry:
  cacheMaxAge: 6h
  urls:
    - https://github.com/mistweaverco/nvpm-registry/releases/latest/download/nvpm-registry.json.zip
ui:
  color: auto
  output: rich
```

A JSON Schema is provided at `schemas/config.schema.json`.

#### nvpm list

`list`/`ls` list all installed packages.

```sh
nvpm list
```

or with `--all`/`-A` flag all available packages.

```sh
nvpm list --all
```

You can also filter packages by
prefix of either the package id or name.

```sh
 # lists all available packages with "yaml" in the name
nvpm list -A yaml
```

Optional list constraints (combinable with each other and with name filters):

- `--only-outdated`: show only packages that have an update available. For
  installed packages this is the usual meaning; with `--all`, only registry
  entries you have installed and that are outdated are shown.
- `--only-providers`: comma-separated provider names (must match a supported
  provider), for example `pypi,npm`.
- `--only-categories`: comma-separated category tokens; a package matches if
  any of its registry categories matches any token (substring match,
  case-insensitive), for example `lsp,tree-sitter-parser`.

```sh
nvpm list --only-outdated
nvpm list --only-providers pypi --only-categories lsp
nvpm list -A --only-providers npm --only-outdated
```

#### nvpm update

`update`/`up` updates packages.

```sh
nvpm update \
  npm:@mistweavercokulala-ls \
  pypi:black@latest
```

You can also update all packages at once with the `--all`/`-A` flag.

```sh
nvpm update --all
```

or filter packages by
prefix of either the package id or name.

```sh
 # updates all installed packages with "yaml" in the name
nvpm update -A yaml
```

NVPM can also update itself with:

```sh
nvpm update --self
```

#### nvpm remove

`remove`/`rm` removes packages.

```sh
nvpm remove \
  npm:@mistweavercokulala-ls \
  pypi:black
```

or filter packages by
prefix of either the package id or name.

```sh
 # removes all installed packages with "yaml" in the name
nvpm remove -A yaml
```

#### nvpm health

- `health` checks for requirements
(for shelling out to install packages)

```sh
nvpm health
```

### Where are the packages?

NVPM uses a basepath to install packages of different types.

The basepath is:

- Linux: `$XDG_DATA_HOME/nvpm/packages` or `$HOME/.local/share/nvpm/packages`
- macOS: `$HOME/Library/Application Support/nvpm/packages`
- Windows: `%APPDATA%\nvpm\packages`

The packages are installed in the following directory structure:

```
$basepath/$provider/$package-name/
```

### Tree-sitter parsers for Neovim

Parsers are written to Neovim's data directory under:

```
<stdpath("data")>/site/parser/<language>.<so|dylib|dll>
```

NVPM builds parsers from upstream source using the `tree-sitter` CLI when a
registry package declares `treesitter.build`.

By default, NVPM only builds and caches the parser artifacts under:

```
<nvpm-data-share>/artifacts/treesitter/<package>/<version>/<language>.<so|dylib|dll>
```

To install built parsers into Neovim, use:

```sh
nvpm install --integrate neovim <package>
```

NVPM resolves `<stdpath("data")>` by running Neovim headless when available
(`nvim --headless ...`). If `nvim` is not available, it falls back to common
defaults:

- Linux: `$XDG_DATA_HOME/nvim` or `~/.local/share/nvim`
- macOS: `~/Library/Application Support/nvim`
- Windows: `%LOCALAPPDATA%\\nvim-data`

## Supported providers

- `cargo`
- `codeberg`
- `composer`
- `gem`
- `generic` (shell commands)
- `github`
- `gitlab`
- `golang`
- `luarocks`
- `npm`
- `nuget`
- `opam`
- `openvsx`
- `pypi`



[logo]: assets/logo.svg
[badge-made-with-love]: assets/badge-made-with-love.svg
[badge-golang]: assets/badge-golang.svg
[badge-development-status]: assets/badge-development-status.svg
[badge-our-manifesto]: assets/badge-our-manifesto.svg
[badge-latest-release]: https://img.shields.io/github/v/release/mistweaverco/nvpm-client?style=for-the-badge
[badge-discord]: https://mistweaverco.com/assets/badges/discord.svg
[badge-irc]: https://mistweaverco.com/assets/badges/irc.svg
[discord]: https://mistweaverco.com/discord
[irc]: https://mistweaverco.com/irc
[our-manifesto]: https://mistweaverco.com/manifesto
[development-status]: https://github.com/orgs/mistweaverco/projects/5/views/1?filterQuery=repo%3Amistweaverco%2Fnvpm.nvim
[registry-website]: https://registry.nvpm.dev
[golang-website]: https://golang.org
[website]: https://nvpm.dev
[contributors]: https://github.com/mistweaverco/nvpm-client/graphs/contributors
[swahili]: https://en.wikipedia.org/wiki/Swahili_language
[latest-release]: https://github.com/mistweaverco/nvpm-client/releases/latest
[download-website]: https://nvpm.dev/#download
[nvpm-registry]: https://github.com/mistweaverco/nvpm-registry
[nvpm.nvim]: https://github.com/mistweaverco/nvpm.nvim
