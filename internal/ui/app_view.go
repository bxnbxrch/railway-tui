package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// handleGlobalKey processes app-level keybindings. Returns (cmd, handled).
func (a *App) handleGlobalKey(m tea.KeyMsg) (tea.Cmd, bool) {
	// When a pane is in a text-entry/confirm sub-mode, let it consume keys.
	if a.logs.filtering && a.primaryOrSplitIs(paneLogs) {
		return nil, false
	}
	if a.deploys.confirming && a.primaryOrSplitIs(paneDeploys) {
		return nil, false
	}
	if (a.vars.adding || a.vars.confirming) && a.primaryOrSplitIs(paneVars) {
		return nil, false
	}
	if a.service.confirming && a.primaryOrSplitIs(paneService) {
		return nil, false
	}

	switch m.String() {
	case "ctrl+c", "Q":
		a.logMgr.stopAll()
		return tea.Quit, true
	case "q":
		// q quits only from a non-editing context.
		a.logMgr.stopAll()
		return tea.Quit, true
	case "p":
		a.picker.open(a.projects, a.projectID, a.env)
		a.picker.mode = pickProject
		if a.picker.projects == nil {
			return a.loadProjects(), true
		}
		return nil, true
	case "e":
		a.picker.open(a.projects, a.projectID, a.env)
		a.picker.mode = pickEnv
		if a.picker.projects == nil {
			return a.loadProjects(), true
		}
		return nil, true
	case "L":
		return a.cycleLayout(), true
	case "tab":
		// Switch focus between primary and split when split is active.
		if a.split != "" {
			a.focusSplit = !a.focusSplit
			return nil, true
		}
		return nil, true
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(m.String()[0] - '1')
		if idx >= 0 && idx < len(tabOrder) {
			a.primary = tabOrder[idx]
			a.split = ""
			a.focusSplit = false
			a.resize()
		}
		return nil, true
	case "O":
		// Open the project dashboard in the browser.
		if a.projectID != "" {
			url := dashboardURL(a.projectID, "")
			return func() tea.Msg { return openURLMsg{url: url, label: "dashboard"} }, true
		}
		return nil, true
	case "?":
		// toggle help handled in status bar; no-op state for now
		return nil, true
	}
	return nil, false
}

func (a *App) primaryOrSplitIs(p paneID) bool {
	return a.primary == p || a.split == p
}

// cycleLayout advances to the next saved layout.
func (a *App) cycleLayout() tea.Cmd {
	if len(a.cfg.Layouts) == 0 {
		return nil
	}
	a.layoutIdx = (a.layoutIdx + 1) % len(a.cfg.Layouts)
	l := a.cfg.Layouts[a.layoutIdx]
	a.applyLayout(l)
	a.status = "layout: " + l.Name
	a.resize()
	return nil
}

// activePaneID is the pane currently receiving keys.
func (a *App) activePaneID() paneID {
	if a.split != "" && a.focusSplit {
		return a.split
	}
	return a.primary
}

// routeKey forwards a message to the focused pane and adapts its return.
func (a *App) routeKey(msg tea.Msg) tea.Cmd {
	switch a.activePaneID() {
	case paneLogs:
		cmd, toggle := a.logs.Update(msg)
		if toggle != nil {
			a.logMgr.toggle(toggle.src)
		}
		return cmd
	case paneErrors:
		return a.errors.Update(msg)
	case paneMetrics:
		return a.metrics.Update(msg)
	case paneDeploys:
		return a.deploys.Update(msg)
	case paneVars:
		return a.vars.Update(msg)
	case paneService:
		return a.service.Update(msg)
	case paneTopology:
		return a.topology.Update(msg)
	case paneSettings:
		return a.settings.Update(msg)
	case paneNotify:
		return nil
	}
	return nil
}

// resize recomputes pane dimensions for the current layout.
func (a *App) resize() {
	if !a.ready {
		return
	}
	bodyH := a.height - 2 // tab bar + status bar
	if bodyH < 3 {
		bodyH = 3
	}
	bodyW := a.width

	if a.split == "" {
		a.sizePane(a.primary, bodyW, bodyH)
		return
	}
	// In a split, each pane is drawn inside a border, so its usable content
	// area is 2 cols narrower and 2 rows shorter. Size panes to that inner
	// area so they lay out at exactly the width/height renderBody draws them
	// at (otherwise content is too wide and the terminal wraps it).
	if a.splitVert {
		topH := int(float64(bodyH) * a.splitRatio)
		botH := bodyH - topH - 1
		a.sizePane(a.primary, bodyW-2, topH-2)
		a.sizePane(a.split, bodyW-2, botH-2)
	} else {
		leftW := int(float64(bodyW) * a.splitRatio)
		rightW := bodyW - leftW - 1
		a.sizePane(a.primary, leftW-2, bodyH-2)
		a.sizePane(a.split, rightW-2, bodyH-2)
	}
}

func (a *App) sizePane(p paneID, w, h int) {
	switch p {
	case paneLogs:
		a.logs.setSize(w, h)
	case paneErrors:
		a.errors.setSize(w, h)
	case paneMetrics:
		a.metrics.setSize(w, h)
	case paneDeploys:
		a.deploys.setSize(w, h)
	case paneVars:
		a.vars.setSize(w, h)
	case paneService:
		a.service.setSize(w, h)
	case paneTopology:
		a.topology.setSize(w, h)
	case paneSettings:
		a.settings.setSize(w, h)
	}
}

func (a *App) paneView(p paneID, w, h int) string {
	var s string
	switch p {
	case paneLogs:
		s = a.logs.View()
	case paneErrors:
		s = a.errors.View()
	case paneMetrics:
		s = a.metrics.View()
	case paneDeploys:
		s = a.deploys.View()
	case paneVars:
		s = a.vars.View()
	case paneService:
		s = a.service.View()
	case paneTopology:
		s = a.topology.View()
	case paneSettings:
		s = a.settings.View()
	case paneNotify:
		s = a.notify.historyView(w, h)
	}
	// MaxWidth/MaxHeight hard-clip so a pane can never overflow its box (which
	// would otherwise widen the whole frame via JoinVertical padding).
	return lipgloss.NewStyle().Width(w).Height(h).MaxWidth(w).MaxHeight(h).Render(s)
}

// View renders the whole frame.
func (a *App) View() string {
	if !a.ready {
		return "loading…"
	}
	if a.fatal != "" {
		box := a.styles.ToastBad.Padding(1, 2).Render(a.fatal)
		return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, box)
	}

	tabs := a.renderTabs()
	body := a.renderBody()
	status := a.renderStatus()

	frame := lipgloss.JoinVertical(lipgloss.Left, tabs, body, status)

	// Overlay picker centered.
	if a.picker.active {
		a.picker.width, a.picker.height = a.width, a.height
		overlay := a.picker.View()
		return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, overlay)
	}

	// Overlay in-flight build/deploy progress bottom-right.
	if prog := a.deployProgressOverlay(); prog != "" {
		frame = overlayBottomRight(frame, prog, a.width, a.height)
	}

	// Overlay toasts top-right.
	toasts := a.notify.overlay(a.width)
	if toasts != "" {
		return overlayTopRight(frame, toasts, a.width)
	}
	return frame
}

func (a *App) renderTabs() string {
	var parts []string
	active := a.activePaneID()
	for _, p := range tabOrder {
		title := tabTitle(p)
		if p == paneNotify && a.notify.unread() > 0 {
			title = fmt.Sprintf("%s(%d)", title, a.notify.unread())
		}
		style := a.styles.TabInactive
		if p == active {
			style = a.styles.TabActive
		} else if p == a.primary || p == a.split {
			style = a.styles.TabInactive.Copy().Underline(true)
		}
		parts = append(parts, style.Render(title))
	}
	tabs := strings.Join(parts, " ")

	ctx := fmt.Sprintf(" %s / %s ", orDash(a.projectName), orDash(a.env))
	ctxStyled := lipgloss.NewStyle().Foreground(a.theme.Bg).Background(a.theme.Active).Bold(true).Render(ctx)

	// The context indicator is pinned to the right; the tabs take the rest and
	// are truncated (never wrapped) if the terminal is too narrow.
	avail := a.width - lipgloss.Width(ctxStyled)
	if avail < 0 {
		avail = 0
	}
	gap := avail - lipgloss.Width(tabs)
	if gap < 0 {
		tabs = ansi.Truncate(tabs, avail, "…")
		gap = 0
	}
	return tabs + strings.Repeat(" ", gap) + ctxStyled
}

func (a *App) renderBody() string {
	bodyH := a.height - 2
	if bodyH < 3 {
		bodyH = 3
	}
	bodyW := a.width

	border := func(p paneID, s string) string {
		st := a.styles.Border
		if p == a.activePaneID() && a.split != "" {
			st = a.styles.BorderActive
		}
		return st.Render(s)
	}

	if a.split == "" {
		return a.paneView(a.primary, bodyW, bodyH)
	}
	if a.splitVert {
		topH := int(float64(bodyH) * a.splitRatio)
		botH := bodyH - topH - 1
		top := border(a.primary, a.paneView(a.primary, bodyW-2, topH-2))
		bot := border(a.split, a.paneView(a.split, bodyW-2, botH-2))
		return lipgloss.JoinVertical(lipgloss.Left, top, bot)
	}
	leftW := int(float64(bodyW) * a.splitRatio)
	rightW := bodyW - leftW - 1
	left := border(a.primary, a.paneView(a.primary, leftW-2, bodyH-2))
	right := border(a.split, a.paneView(a.split, rightW-2, bodyH-2))
	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
}

func (a *App) renderStatus() string {
	left := fmt.Sprintf("%d stream(s)", len(a.logMgr.streams))
	if a.status != "" {
		left += " · " + a.status
	}
	help := "[p]roj [e]nv [L]ayout · 1-9 panes · [tab]focus · [O]pen · [q]uit"
	// Truncate the (variable-length) status text so the line never overflows.
	avail := a.width - lipgloss.Width(help) - 2
	if avail < 0 {
		avail = 0
	}
	if lipgloss.Width(left) > avail {
		left = ansi.Truncate(left, avail, "…")
	}
	gap := a.width - lipgloss.Width(left) - lipgloss.Width(help) - 2
	if gap < 1 {
		gap = 1
	}
	line := " " + left + strings.Repeat(" ", gap) + help + " "
	return a.styles.StatusBar.Width(a.width).MaxWidth(a.width).Render(line)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// overlayTopRight composites an overlay block onto the top-right of base.
func overlayTopRight(base, overlay string, width int) string {
	baseLines := strings.Split(base, "\n")
	ovLines := strings.Split(overlay, "\n")
	ovW := lipgloss.Width(overlay)
	startCol := width - ovW - 1
	if startCol < 0 {
		startCol = 0
	}
	for i, ol := range ovLines {
		row := i + 1 // start one line below the tab bar
		if row >= len(baseLines) {
			break
		}
		base := baseLines[row]
		// Truncate base to startCol, then append overlay line.
		truncated := ansiTruncate(base, startCol)
		pad := startCol - lipgloss.Width(truncated)
		if pad < 0 {
			pad = 0
		}
		baseLines[row] = truncated + strings.Repeat(" ", pad) + ol
	}
	return strings.Join(baseLines, "\n")
}

// overlayBottomRight composites an overlay block onto the bottom-right of base,
// anchored just above the status bar (the last row).
func overlayBottomRight(base, overlay string, width, height int) string {
	baseLines := strings.Split(base, "\n")
	ovLines := strings.Split(overlay, "\n")
	ovW := lipgloss.Width(overlay)
	startCol := width - ovW - 1
	if startCol < 0 {
		startCol = 0
	}
	// Place the block so its last row sits one line above the status bar.
	startRow := height - 1 - len(ovLines)
	if startRow < 1 {
		startRow = 1
	}
	for i, ol := range ovLines {
		row := startRow + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		truncated := ansiTruncate(baseLines[row], startCol)
		pad := startCol - lipgloss.Width(truncated)
		if pad < 0 {
			pad = 0
		}
		baseLines[row] = truncated + strings.Repeat(" ", pad) + ol
	}
	return strings.Join(baseLines, "\n")
}

// ansiTruncate cuts a possibly-styled string to a visible width (best-effort,
// ignoring embedded ANSI width edge cases for the overlay use-case).
func ansiTruncate(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	// Fall back to plain runes; overlays sit over log text where this is fine.
	runes := []rune(stripANSI(s))
	if len(runes) > w {
		return string(runes[:w])
	}
	return string(runes)
}

// clampBlock truncates every line of s to display width w, so a pane's own
// View never overflows its box regardless of how it's composed (the outer
// paneView also clamps, but self-clipping keeps panes correct standalone).
func clampBlock(s string, w int) string {
	if w <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if lipgloss.Width(ln) > w {
			lines[i] = ansi.Truncate(ln, w, "…")
		}
	}
	return strings.Join(lines, "\n")
}

func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
