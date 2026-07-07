package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"railway-tui/internal/model"
	"railway-tui/internal/ui/theme"
)

// metricsPane renders CPU / memory / disk / network sparklines for the focused
// service, mirroring the resource graphs in the Railway web dashboard. Data is
// fetched by the app (on focus change and on the metrics poll) via the existing
// `railway metrics --raw --json` wrapper.
type metricsPane struct {
	styles *theme.Styles
	vp     viewport.Model

	env       string
	service   model.Service
	metrics   *model.Metrics
	loading   bool
	fetchedAt time.Time
	errMsg    string

	width, height int
	ready         bool
}

func newMetricsPane(styles *theme.Styles) *metricsPane {
	return &metricsPane{styles: styles}
}

func (p *metricsPane) setSize(w, h int) {
	p.width, p.height = w, h
	vpH := h - 3 // title + help
	if vpH < 3 {
		vpH = 3
	}
	p.vp.Width = w
	p.vp.Height = vpH
	p.ready = true
	p.reflow()
}

// setService points the pane at a service. Returns true if the focus changed
// (so the caller knows to kick off a fetch).
func (p *metricsPane) setService(env string, svc model.Service) bool {
	changed := svc.ID != p.service.ID || env != p.env
	p.env = env
	p.service = svc
	if changed {
		p.metrics = nil
		p.errMsg = ""
		p.loading = svc.ID != ""
		p.reflow()
	}
	return changed
}

// setMetrics stores freshly fetched series if they belong to the focused service.
func (p *metricsPane) setMetrics(serviceID string, m *model.Metrics) {
	if serviceID != p.service.ID {
		return
	}
	p.metrics = m
	p.loading = false
	p.errMsg = ""
	p.fetchedAt = time.Now()
	p.reflow()
}

func (p *metricsPane) setError(serviceID, msg string) {
	if serviceID != p.service.ID {
		return
	}
	p.loading = false
	p.errMsg = msg
	p.reflow()
}

// loadMetricsMsg asks the root to (re)fetch metrics for a service.
type loadMetricsMsg struct {
	serviceID   string
	serviceName string
}

func (p *metricsPane) Update(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case tea.KeyMsg:
		switch m.String() {
		case "r":
			if p.service.ID != "" {
				p.loading = true
				p.reflow()
				id, name := p.service.ID, p.service.Name
				return func() tea.Msg { return loadMetricsMsg{serviceID: id, serviceName: name} }
			}
		case "up", "k", "down", "j", "pgup", "pgdown", "ctrl+u", "ctrl+d":
			var cmd tea.Cmd
			p.vp, cmd = p.vp.Update(msg)
			return cmd
		}
	}
	return nil
}

// metricRow is one labelled series to render (usage plus optional limit).
type metricRow struct {
	label    string
	usageKey string
	limitKey string // "" if none
	format   func(v float64) string
	color    lipgloss.Color
}

func (p *metricsPane) rows() []metricRow {
	t := p.styles.T
	return []metricRow{
		{"CPU", "CPU_USAGE", "CPU_LIMIT", fmtCPU, t.Good},
		{"Memory", "MEMORY_USAGE_GB", "MEMORY_LIMIT_GB", humanGB, t.Accent},
		{"Disk", "DISK_USAGE_GB", "", humanGB, t.Warn},
		{"Net In", "NETWORK_RX_GB", "", humanGB, lipgloss.Color("#5fd7ff")},
		{"Net Out", "NETWORK_TX_GB", "", humanGB, lipgloss.Color("#ff6ac1")},
	}
}

func (p *metricsPane) reflow() {
	if !p.ready {
		return
	}
	if p.service.ID == "" {
		p.vp.SetContent(p.styles.Dim.Render(
			"No service focused.\n\nPress [enter] on a service in the Topology pane to graph its metrics."))
		return
	}
	if p.errMsg != "" {
		p.vp.SetContent(lipgloss.NewStyle().Foreground(p.styles.T.Bad).Render("metrics: " + p.errMsg))
		return
	}
	if p.metrics == nil {
		if p.loading {
			p.vp.SetContent(p.styles.Dim.Render("loading metrics…"))
		} else {
			p.vp.SetContent(p.styles.Dim.Render("no metrics yet"))
		}
		return
	}

	sparkW := p.width - 34
	if sparkW < 8 {
		sparkW = 8
	}
	if sparkW > 60 {
		sparkW = 60
	}

	var b strings.Builder
	for _, r := range p.rows() {
		s, ok := p.metrics.Series[r.usageKey]
		if !ok {
			continue
		}
		label := lipgloss.NewStyle().Foreground(r.color).Bold(true).Render(fmt.Sprintf("%-8s", r.label))
		spark := lipgloss.NewStyle().Foreground(r.color).Render(sparkline(s.Values(), sparkW))
		cur := r.format(s.Last())
		val := cur
		if r.limitKey != "" {
			if lim, ok := p.metrics.Series[r.limitKey]; ok && lim.Last() > 0 {
				val = cur + " / " + r.format(lim.Last())
			}
		}
		peak := p.styles.Dim.Render("  peak " + r.format(maxF(s.Values())))
		b.WriteString(label + " " + spark + "  " + lipgloss.NewStyle().Foreground(p.styles.T.Fg).Render(val))
		b.WriteString(peak)
		b.WriteString("\n\n")
	}
	if b.Len() == 0 {
		b.WriteString(p.styles.Dim.Render("no series returned for this service"))
	}
	p.vp.SetContent(strings.TrimRight(b.String(), "\n"))
}

func (p *metricsPane) View() string {
	name := p.service.Name
	if name == "" {
		name = "—"
	}
	head := p.styles.Title.Render("Metrics — " + name)
	if !p.fetchedAt.IsZero() {
		head += p.styles.Dim.Render("   " + p.fetchedAt.Local().Format("15:04:05"))
	}
	help := p.styles.Help.Render("[r]efresh · ↑↓ scroll · window: last 1h")
	return clampBlock(lipgloss.JoinVertical(lipgloss.Left, head, p.vp.View(), help), p.width)
}

// --- value formatting ---

func fmtCPU(v float64) string {
	// Sub-vCPU usage reads better in milli-vCPU.
	if v < 1 && v > 0 {
		return fmt.Sprintf("%.0f mvCPU", v*1000)
	}
	return fmt.Sprintf("%.2f vCPU", v)
}

// humanGB formats a GB float, stepping down to MB/KB for small values.
func humanGB(gb float64) string {
	mb := gb * 1024
	switch {
	case mb <= 0:
		return "0 MB"
	case mb < 1:
		return fmt.Sprintf("%.0f KB", mb*1024)
	case mb < 1024:
		return fmt.Sprintf("%.1f MB", mb)
	default:
		return fmt.Sprintf("%.2f GB", gb)
	}
}

func maxF(vs []float64) float64 {
	m := 0.0
	for i, v := range vs {
		if i == 0 || v > m {
			m = v
		}
	}
	return m
}
