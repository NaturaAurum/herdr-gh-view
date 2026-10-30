package main

import "testing"

func TestSidebarIn(t *testing.T) {
	panes := []paneInfo{
		{PaneID: "p1", TabID: "t1", Label: "zsh"},
		{PaneID: "p2", TabID: "t2", Label: "AI Usage"},
		{PaneID: "p3", TabID: "t3", Label: "AI Usage"},
		{PaneID: "p4", TabID: "t3", Label: "AI Usage"},
	}
	for tab, want := range map[string]string{"t1": "", "t2": "p2", "t3": "p4"} {
		if got := sidebarIn(panes, tab); got != want {
			t.Errorf("sidebarIn(%s) = %q, want %q", tab, got, want)
		}
	}
}

// A panel opened with --target-pane gets that sidebar as its context.
func TestInitialTargetPrefersOpenPane(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", `{"focused_pane_id":"sidebar","focused_pane_cwd":"/checkout"}`)
	t.Setenv("GH_VIEW_PANE", "p1")
	if pane, cwd := initialTarget(); pane != "p1" || cwd != "" {
		t.Errorf("initialTarget() = %q, %q", pane, cwd)
	}
}
