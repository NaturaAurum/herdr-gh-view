# herdr-gh-view

A [herdr](https://herdr.dev) plugin that opens a right-side panel showing the
GitHub PR for the focused pane's branch:

- Draft / Open state and PR number (click or `⏎` to open in the browser)
- Stack: open PRs chained by base → head branch
- CI rollup with failing and pending checks
- Unresolved review threads (`file:line`) and conversation comments

The panel follows pane focus and refreshes every 60 seconds.

## Requirements

- herdr 0.9.0+, macOS or Linux
- [`gh`](https://cli.github.com) logged in (`gh auth login`)
- Go, to build on install

## Install

```bash
herdr plugin install NaturaAurum/herdr-gh-view
herdr plugin action invoke gh-view.open
```

Bind it to a key in `config.toml`:

```toml
[[keys.command]]
key = "prefix+g"
type = "plugin_action"
command = "gh-view.open"
```

## Keys

`j`/`k` move · `⏎`/`o` open · `r` refresh · `q` close

## Development

```bash
go build -o herdr-gh-view . && herdr plugin link .
```
