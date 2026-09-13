<p align="center">
  <a href="https://github.com/pegasbur/bdinfo">
    <img src="assets/bdinfo-icon.png" alt="BDInfo" width="120" height="80">
  </a>
  &nbsp;&nbsp;&nbsp;&nbsp;
  <a href="https://unraid.net/">
    <img src="https://drive.google.com/thumbnail?id=1NfrtbOFIzg65KY1YeLBZSlTsuGQQCmbr&sz=w256" alt="Unraid" width="80" height="80">
  </a>
  &nbsp;&nbsp;&nbsp;&nbsp;
  <a href="https://github.com/autobrr/go-bdinfo">
    <img src="https://raw.githubusercontent.com/autobrr/autobrr/refs/heads/develop/.github/images/logo.png" alt="autobrr" width="80" height="80">
  </a>
</p>

<h1 align="center">BDInfo for Unraid &amp; Docker</h1>

<p align="center">
  A lightweight browser interface for Blu-ray structure and bitrate analysis,
  powered by <a href="https://github.com/autobrr/go-bdinfo">autobrr/go-bdinfo</a>.
</p>

<p align="center">
  <a href="https://github.com/pegasbur/bdinfo/actions/workflows/validate.yml">
    <img alt="Repository validation" src="https://github.com/pegasbur/bdinfo/actions/workflows/validate.yml/badge.svg?branch=main">
  </a>
  <a href="https://github.com/users/pegasbur/packages/container/package/bdinfo">
    <img alt="Container image" src="https://img.shields.io/badge/GHCR-container-2496ED?logo=docker&logoColor=white">
  </a>
  <img alt="Unraid compatible" src="https://img.shields.io/badge/Unraid-compatible-F15A2C?logo=unraid&logoColor=white">
  <img alt="Architecture amd64" src="https://img.shields.io/badge/architecture-amd64-555555">
  <a href="LICENSE">
    <img alt="GPL-2.0-or-later license" src="https://img.shields.io/badge/license-GPL--2.0--or--later-2ea44f">
  </a>
</p>

<p align="center">
  <a href="https://buymeacoffee.com/pegasbur">
    <img src="assets/buy-me-a-coffee.png" alt="Buy Me a Coffee" width="175">
  </a>
</p>

BDInfo discovers BDMV folders and ISO images from server-side storage, identifies playlists, performs bitrate scans and presents standard report views from the completed scan.

The primary tested platform is AMD64 Unraid. The container can also run on a standard Docker host, including Docker Desktop.

> This is an unofficial community project. It is not maintained or endorsed by autobrr, the original BDInfo project or Unraid.

## Contents

- [Features](#features)
- [Quick start on Unraid](#quick-start-on-unraid)
- [Quick start with Docker](#quick-start-with-docker)
- [Docker Compose](#docker-compose)
- [Image tags](#image-tags)
- [Files and persistence](#files-and-persistence)
- [Updating](#updating)
- [Network access and security](#network-access-and-security)
- [Upstream engine baseline](#upstream-engine-baseline)
- [Remark](#remark)
- [Support and development](#support-and-development)

## Features

- Browse configured server-side locations for BDMV folders and ISO images
- Persist multiple source locations and favorites under `/config/config.json`
- Discover disc metadata and playlists without a full bitrate scan
- Detect the main playlist and distinguish available and filtered playlists
- Scan the main playlist, selected playlists, or the complete disc
- Show live scan progress and support cancellation
- Render Standard, Summary, and Forums report views from the stored scan result
- Copy reports and export TXT or NFO files
- Light and dark themes
- React/TypeScript frontend embedded into a single static Go server binary
- Minimal scratch-based runtime container

## Quick start on Unraid

BDInfo is not yet listed in Community Applications. Until a CA submission is published, install the user template manually.

From an Unraid terminal:

```bash
curl -fsSL \
  https://raw.githubusercontent.com/pegasbur/bdinfo/main/unraid/my-bdinfo.xml \
  -o /boot/config/plugins/dockerMan/templates-user/my-bdinfo.xml
```

Then open **Docker > Add Container** and select **BDInfo** from the template list.

During installation:

1. Confirm the appdata path. The default is `/mnt/user/appdata/bdinfo`.
2. Confirm the media path. The default is `/mnt/user/data`, mounted read-only as `/data` inside the container.
3. Confirm the WebUI host port. The default is `4646`.
4. Apply the template and open its WebUI.

The default address is:

```text
http://UNRAID-IP:4646
```

The host port can be changed if `4646` is already in use.

## Quick start with Docker

Create persistent configuration and choose a media directory:

```bash
mkdir -p /srv/bdinfo/config /srv/media
```

Run the container:

```bash
docker run -d \
  --name bdinfo \
  --restart unless-stopped \
  -p 4646:4646 \
  -v /srv/bdinfo/config:/config \
  -v /srv/media:/data:ro \
  ghcr.io/pegasbur/bdinfo:latest
```

Change the host paths and port for your system. Then open:

```text
http://DOCKER-HOST-IP:4646
```

## Docker Compose

Copy the example environment file:

```bash
cp .env.example .env
```

Review the image, container name, WebUI port, appdata path, and media path in `.env`.

Start the container:

```bash
docker compose up -d
```

The default Compose configuration maps host port `4646` to container port `4646`, persists `/config`, and mounts `/data` read-only.

## Image tags

| Image tag | Intended use |
| --- | --- |
| `ghcr.io/pegasbur/bdinfo:latest` | Current tested BDInfo wrapper release |
| `ghcr.io/pegasbur/bdinfo:0.4.2` | Current upstream `go-bdinfo` application-version tag |
| `ghcr.io/pegasbur/bdinfo:0.4.2-r1` | Immutable upstream-version and wrapper-revision tag |

## Files and persistence

The principal container paths are the same on Unraid and Docker:

| Typical Unraid host path | Container path | Access | Purpose |
| --- | --- | --- | --- |
| `/mnt/user/appdata/bdinfo` | `/config` | read/write | Application configuration, configured locations, and favorites |
| `/mnt/user/data` | `/data` | read-only | Blu-ray folders, ISO images, and other source media |

The application stores its persistent configuration in:

```text
/config/config.json
```

Correctly mapped `/config` and `/data` directories remain available when the container is updated or replaced.

Additional host directories can be mapped when required.

## Updating

### Unraid

Once published through Community Applications, use Unraid's normal **Check for Updates** and **Update** controls. The `/config` and `/data` mappings remain unchanged when the container is replaced.

### Docker

Pull the current image and recreate the container using the same port and volume mappings:

```bash
docker pull ghcr.io/pegasbur/bdinfo:latest
```

Docker Compose users can run:

```bash
docker compose pull
docker compose up -d
```

## Network access and security

The WebUI is intended for a trusted LAN, Tailscale, or a secured reverse proxy and should not be exposed directly to the public Internet. For remote access, use a private network such as Tailscale or place BDInfo behind an authenticated HTTPS reverse proxy.

The application reads media through server-side volume mappings; it does not upload Blu-ray or ISO content through the browser.

## Upstream engine baseline

The current application is intentionally pinned to:

```text
github.com/autobrr/go-bdinfo v0.4.2
```

Upstream engine updates should be handled through the compatibility/update pipeline: update the pinned dependency, build and test the wrapper, review the result, and then publish a new image version.

## Remark
BDInfo may not function correctly with copy-protected discs. You may have to decrypt commercial Blu-ray movie discs before you will be able to gather any info.

## Support and development

Container, WebUI-wrapper, Docker, and Unraid-template issues belong to this project. Upstream Blu-ray parsing or report-generation issues should be confirmed against [`autobrr/go-bdinfo`](https://github.com/autobrr/go-bdinfo) before being reported upstream.

---

<p align="center">
  <a href="https://buymeacoffee.com/pegasbur">
    <img src="assets/buy-me-a-coffee.png" alt="Buy Me a Coffee" width="175">
  </a>
</p>

