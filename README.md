# reviewGOOSE 🪿

![Stable](https://img.shields.io/badge/status-beta-orange)
![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20BSD%20%7C%20Windows-blue)
![Goose Noises](https://img.shields.io/badge/goose%20noises-100%25%20more-green)
[![GitHub](https://img.shields.io/github/stars/ready-to-review/goose?style=social)](https://github.com/ready-to-review/goose)

![reviewGOOSE Logo](media/logo-small.png)

The only PR tracker that honks at you when you're the bottleneck. Now shipping with 100% more goose noises!

Lives in your menubar like a tiny waterfowl of productivity shame, watching your GitHub PRs and making aggressive bird sounds when you're blocking someone's code from seeing the light of production.

![reviewGOOSE Screenshot](media/screenshot.png)

## macOS Quick Start ⚡ (Get Honked At)

Homebrew users can get the party started quickly:

```shell

brew install --cask codegroove-dev/tap/review-goose
gh auth status || gh auth login
```

Open `/Applications/reviewGOOSE.app`. To be persistently annoyed every time you login, click the `Start at Login` menu item.

## Homebrew on Linux Quick Start ⚡

On a progressive Linux distribution that includes Homebrew, such as [Bluefin](https://projectbluefin.io/)? You are just a cut and paste away from excitement:

```shell
brew install codeGROOVE-dev/tap/reviewGOOSE
gh auth status || gh auth login
```

## Linux/BSD/Windows Medium Start

1. Install the [GitHub CLI](https://cli.github.com/) and [Go](https://go.dev/dl/) via your platforms recommended methods
2. Install reviewGOOSE:

```bash
go install github.com/codeGROOVE-dev/goose/cmd/reviewGOOSE@latest
```

3. Copy goose from $HOME/go/bin to wherever you prefer
4. Add goose to your auto-login so you never forget about PRs again

## Using a fine-grained access token

If you want more control over which repositories the goose can access - for example, only access to public repositories, you can use a [fine-grained personal access token](https://github.com/settings/personal-access-tokens/new) with the following permissions:

- **Pull requests**: Read
- **Metadata**: Read

You can then use the token like so:

```bash
env GITHUB_TOKEN=your_token_here reviewGOOSE
```

## Usage

- **macOS/Windows**: Click the tray icon to show the menu
- **Linux/BSD**: Right-click the tray icon to show the menu (left-click refreshes PRs)

## Known Issues

- Visual notifications won't work reliably on macOS until we release signed binaries.
- Tray icons on GNOME require [snixembed](https://git.sr.ht/~steef/snixembed) and enabling the [Legacy Tray extension](https://www.omgubuntu.co.uk/2024/08/gnome-official-status-icons-extension). Goose will automatically launch snixembed if needed, but you must install it first (e.g., `apt install snixembed` or `yay -S snixembed`).

## Pricing

- Free forever for public open-source repositories ❤️
- Starting in 2026, private repository access will require sponsorship or [Ready to Review](https://github.com/apps/ready-to-review-beta) subscription ($2/mo)

## Privacy

- Your GitHub token is used to authenticate against GitHub and codeGROOVE's API for state-machine & natural-language processing
- Your GitHub token is never stored or logged.
- PR metadata may be cached locally & remotely for up to 20 days
- No data is resold to anyone. We don't even want it.
- No telemetry is collected

## License

This project is licensed under the GPL-3.0 License - see the [LICENSE](LICENSE) file for details.

---

Built with 🪿 by [codeGROOVE](https://codegroove.dev/) - PRs welcome!
