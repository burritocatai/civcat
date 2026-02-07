# civcat

A terminal UI for managing [Civitai](https://civitai.com) models in your [ComfyUI](https://github.com/comfyanonymous/ComfyUI) installation.

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Features

- Browse and search models from the Civitai public API
- Download models directly into the correct ComfyUI subdirectory
- Track installed models with version, hash, and install date
- Check for updates across all installed models
- Configurable API key (env var or config file)
- Conservative rate limiting to play nice with the API

## Model Directory Mapping

civcat installs models into the standard ComfyUI directory structure:

| Model Type        | ComfyUI Directory            |
|-------------------|------------------------------|
| Checkpoint        | `models/checkpoints/`        |
| LORA              | `models/loras/`              |
| TextualInversion  | `models/embeddings/`         |
| Controlnet        | `models/controlnet/`         |
| VAE               | `models/vae/`                |
| Upscaler          | `models/upscale_models/`     |
| Hypernetwork      | `models/hypernetworks/`      |
| MotionModule      | `models/animatediff_motion_lora/` |
| Wildcards         | `models/wildcards/`          |

## Installation

### From source

Requires Go 1.24+.

```sh
git clone https://github.com/burritocatai/civcat.git
cd civcat
go build -o civcat .
```

Move the binary somewhere on your `$PATH`:

```sh
sudo mv civcat /usr/local/bin/
```

### Docker

```sh
docker build -t civcat .
docker run -it --rm \
  -e CIVITAI_API_KEY=your_key_here \
  -v /path/to/ComfyUI:/comfyui \
  -v civcat-data:/root/.civcat \
  civcat
```

The container expects:
- `/comfyui` — mount your ComfyUI installation here
- `/root/.civcat` — persist config and tracking data across runs (optional volume)

## Configuration

On first run civcat prompts for your ComfyUI path and API key. You can reconfigure at any time:

```sh
civcat config
```

Or edit the config directly at `~/.civcat/config.json`:

```json
{
  "comfyui_path": "/path/to/ComfyUI",
  "api_key": "your_civitai_api_key"
}
```

### API Key

Set your key one of two ways (env var takes priority):

1. **Environment variable**: `export CIVITAI_API_KEY=your_key`
2. **Config file**: set via `civcat config` or edit `~/.civcat/config.json`

Get your API key from [Civitai Account Settings](https://civitai.com/user/account).

## Usage

```sh
civcat          # launch the TUI
civcat config   # reconfigure settings
```

### Keybindings

#### Installed Models (main view)

| Key     | Action                  |
|---------|-------------------------|
| `s`     | Search models           |
| `u`     | Check for updates       |
| `c`     | Open config             |
| `enter` | View model details      |
| `d`     | Remove model from tracking |
| `j`/`k` | Navigate up/down       |
| `q`     | Quit                    |

#### Search

| Key     | Action                  |
|---------|-------------------------|
| `/`     | New search              |
| `enter` | View model details      |
| `n`/`p` | Next/prev page          |
| `esc`   | Back to installed       |

#### Model Detail

| Key     | Action                  |
|---------|-------------------------|
| `enter` | Install selected version |
| `i`     | Install selected version |
| `j`/`k` | Navigate versions      |
| `esc`   | Back                    |

#### Updates

| Key     | Action                  |
|---------|-------------------------|
| `r`     | Refresh update check    |
| `esc`   | Back                    |

## Data Files

All data lives in `~/.civcat/`:

| File          | Purpose                                    |
|---------------|--------------------------------------------|
| `config.json` | ComfyUI path and API key                  |
| `models.json` | Installed model tracking (IDs, versions, dates) |

## Rate Limiting

civcat uses a token-bucket rate limiter (2 requests/second, burst of 5) and automatically retries on HTTP 429 responses using the `Retry-After` header. This keeps usage well within Civitai's limits.

## License

[MIT](LICENSE)
