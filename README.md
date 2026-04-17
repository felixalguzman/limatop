# limatop

A small, themeable TUI for [Lima](https://lima-vm.io/). See your VMs, their
resources, and what's running inside them — at a glance.

```
 limatop  table · omarchy · 1 vm                              14:41:02

  ●  NAME      STATUS    CPU  MEM      DISK      OS     ARCH    TYPE  SSH
  ───────────────────────────────────────────────────────────────────────────
  ▌●  test     Running    4   4.0 GiB  100 GiB   Linux  x86_64  qemu  127.0.0.1:36965
```

## Install

```sh
go install github.com/felixalguzman/limatop@latest
```

Or clone and build:

```sh
git clone https://github.com/felixalguzman/limatop
cd limatop
go build -o limatop .
./limatop
```

Requires [`limactl`](https://github.com/lima-vm/lima) on `PATH`.
limatop also looks in `/home/linuxbrew/.linuxbrew/bin`, `/opt/homebrew/bin`, and
`/usr/local/bin` automatically.

## Views

Cycle with `v` or jump with `1` / `2` / `3`.

- **Table** — dense overview: name, status dot, CPU/MEM/DISK, OS, arch, SSH.
- **Grid** — one card per VM with resource bars. Nice with 1–6 VMs.
- **Focus** — the selected VM takes the screen: live CPU/mem/disk gauges,
  uptime, SSH info, and the top processes by CPU (`ps` inside the guest).

## Keys

| key                | action                         |
| ------------------ | ------------------------------ |
| `j` / `k` / arrows | move selection                 |
| `g` / `G`          | jump to top / bottom           |
| `enter`            | focus the selected VM          |
| `esc`              | leave focus                    |
| `v`                | cycle view mode                |
| `1` / `2` / `3`    | jump to table / grid / focus   |
| `r`                | refresh now                    |
| `t`                | cycle theme                    |
| `q`                | quit                           |

Data refreshes automatically every 3s.

## Themes

limatop reads your [Omarchy](https://github.com/basecamp/omarchy) theme from
`~/.config/omarchy/current/theme/alacritty.toml` at startup, so it matches
whatever terminal scheme you already use. If Omarchy isn't installed, it falls
back to built-in schemes:

- `nord`
- `tokyonight`
- `catppuccin` (mocha)
- `gruvbox`

Press `t` to cycle themes live.

## Structure

```
main.go                 bubbletea entrypoint
internal/lima/          `limactl list --json` + guest ps / usage probe
internal/theme/         omarchy loader + built-in fallback palettes
internal/tui/           model, update, views, styles
```
