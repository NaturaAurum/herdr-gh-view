package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"
)

type focusMsg struct{ paneID string }

// subscribeFocus streams pane.focused events from the herdr socket forever,
// reconnecting on errors (including events_lost, which closes the connection).
func subscribeFocus(send func(focusMsg)) {
	sock := os.Getenv("HERDR_SOCKET_PATH")
	if sock == "" {
		return
	}
	req := `{"id":"gh-view","method":"events.subscribe","params":{"subscriptions":[{"type":"pane.focused"}]}}` + "\n"
	for {
		if conn, err := net.Dial("unix", sock); err == nil {
			if _, err := conn.Write([]byte(req)); err == nil {
				sc := bufio.NewScanner(conn)
				sc.Buffer(make([]byte, 1<<20), 1<<20)
				for sc.Scan() {
					var ev struct {
						Event string `json:"event"`
						Data  struct {
							PaneID string `json:"pane_id"`
						} `json:"data"`
					}
					if json.Unmarshal(sc.Bytes(), &ev) == nil && ev.Event == "pane_focused" {
						send(focusMsg{ev.Data.PaneID})
					}
				}
			}
			conn.Close()
		}
		time.Sleep(2 * time.Second)
	}
}

func herdrBin() string {
	if b := os.Getenv("HERDR_BIN_PATH"); b != "" {
		return b
	}
	return "herdr"
}

// paneCwd prefers the cwd of the pane's foreground process, so a `cd` inside
// the pane is followed.
func paneCwd(paneID string) (string, error) {
	out, err := exec.Command(herdrBin(), "pane", "get", paneID).Output()
	if err != nil {
		return "", err
	}
	var resp struct {
		Result struct {
			Pane struct {
				Cwd           string `json:"cwd"`
				ForegroundCwd string `json:"foreground_cwd"`
			} `json:"pane"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", err
	}
	if p := resp.Result.Pane; p.ForegroundCwd != "" {
		return p.ForegroundCwd, nil
	}
	return resp.Result.Pane.Cwd, nil
}

type rect struct{ X, Y, Width, Height int }

// fitWidth shrinks the panel to cols by moving the split it sits on the right
// side of. `pane resize --amount` shifts that split's ratio by the given delta.
func fitWidth(paneID string, cols int) {
	out, err := exec.Command(herdrBin(), "pane", "layout", "--pane", paneID).Output()
	if err != nil {
		return
	}
	var resp struct {
		Result struct {
			Layout struct {
				Panes []struct {
					PaneID string `json:"pane_id"`
					Rect   rect   `json:"rect"`
				} `json:"panes"`
				Splits []struct {
					Direction string `json:"direction"`
					Rect      rect   `json:"rect"`
				} `json:"splits"`
			} `json:"layout"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &resp) != nil {
		return
	}
	var me *rect
	for _, p := range resp.Result.Layout.Panes {
		if p.PaneID == paneID {
			me = &p.Rect
		}
	}
	if me == nil || me.Width <= cols {
		return
	}
	// The parent is the narrowest right-split that shares our right edge and contains us.
	var parent *rect
	for _, s := range resp.Result.Layout.Splits {
		r := s.Rect
		if s.Direction == "right" && r.X < me.X && r.X+r.Width == me.X+me.Width &&
			r.Y <= me.Y && r.Y+r.Height >= me.Y+me.Height && (parent == nil || r.Width < parent.Width) {
			parent = &r
		}
	}
	if parent == nil {
		return
	}
	delta := float64(me.Width-cols) / float64(parent.Width)
	exec.Command(herdrBin(), "pane", "resize", "--pane", paneID, "--direction", "right",
		"--amount", fmt.Sprintf("%.3f", delta)).Run()
}

// initialTarget is the pane that was focused when the panel was opened.
func initialTarget() (paneID, cwd string) {
	var ctx struct {
		FocusedPaneID  string `json:"focused_pane_id"`
		FocusedPaneCwd string `json:"focused_pane_cwd"`
		WorkspaceCwd   string `json:"workspace_cwd"`
	}
	json.Unmarshal([]byte(os.Getenv("HERDR_PLUGIN_CONTEXT_JSON")), &ctx)
	if ctx.FocusedPaneID != "" && ctx.FocusedPaneID != os.Getenv("HERDR_PANE_ID") {
		return ctx.FocusedPaneID, ctx.FocusedPaneCwd
	}
	if ctx.WorkspaceCwd != "" {
		return "", ctx.WorkspaceCwd
	}
	// Running outside herdr, e.g. during development.
	if len(os.Args) > 1 {
		return "", os.Args[1]
	}
	cwd, _ = os.Getwd()
	return "", cwd
}
