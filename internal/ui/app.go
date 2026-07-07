// Package ui implements the Bubble Tea application: tabbed panes, saved
// layouts, project/environment switching, background polling, log streaming,
// and toast notifications.
package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"railway-tui/internal/config"
	"railway-tui/internal/dbg"
	"railway-tui/internal/model"
	"railway-tui/internal/railwaycli"
	"railway-tui/internal/ui/theme"
)

// paneID identifies a pane for layouts and tab switching.
type paneID string

const (
	paneLogs     paneID = "logs"
	paneErrors   paneID = "errors"
	paneDeploys  paneID = "deploys"
	paneTopology paneID = "topology"
	paneNotify   paneID = "notifications"
	paneSettings paneID = "settings"
)

var tabOrder = []paneID{paneLogs, paneErrors, paneDeploys, paneTopology, paneNotify, paneSettings}

func tabTitle(p paneID) string {
	switch p {
	case paneLogs:
		return "Logs"
	case paneErrors:
		return "Errors"
	case paneDeploys:
		return "Deploys"
	case paneTopology:
		return "Topology"
	case paneNotify:
		return "Notifs"
	case paneSettings:
		return "Settings"
	}
	return string(p)
}

// App is the root model.
type App struct {
	cfg    config.Config
	theme  *theme.Theme
	styles *theme.Styles
	client *railwaycli.Client

	// context
	projectID   string
	projectName string
	env         string

	// data
	projects []model.ProjectRef
	proj     *model.Project
	services []model.Service

	// panes
	logs     *logsPane
	errors   *errorsPane
	deploys  *deploysPane
	topology *topologyPane
	settings *settingsPane
	notify   *notifyCenter
	picker   *picker

	logMgr      *logManager
	watcher     *watcher
	autoStarted bool // deploy logs auto-enabled on first load

	// view state
	primary    paneID
	split      paneID // "" = single pane
	splitVert  bool
	splitRatio float64
	focusSplit bool
	layoutIdx  int

	logPath string

	width, height int
	ready         bool
	status        string
	fatal         string
}

// New builds the root model from loaded config.
func New(cfg config.Config, client *railwaycli.Client) *App {
	th := theme.Default()
	st := theme.NewStyles(th)

	a := &App{
		cfg:       cfg,
		theme:     th,
		styles:    st,
		client:    client,
		env:       cfg.Environment,
		projectID: cfg.Project,
	}
	a.logs = newLogsPane(st)
	a.errors = newErrorsPane(st)
	a.deploys = newDeploysPane(st)
	a.topology = newTopologyPane(st)
	a.settings = newSettingsPane(st, &a.cfg)
	a.notify = newNotifyCenter(st, cfg.Notifications.Toast())
	a.picker = newPicker(st)
	a.watcher = newWatcher(cfg.Notifications)
	a.logMgr = newLogManager(client, cfg.Project)

	// Apply the active layout.
	a.applyLayout(cfg.LayoutByName(cfg.ActiveLayout))
	for i, l := range cfg.Layouts {
		if l.Name == cfg.ActiveLayout {
			a.layoutIdx = i
		}
	}
	return a
}

// SetLogPath records the debug log location so the UI can surface it.
func (a *App) SetLogPath(p string) {
	a.logPath = p
	a.settings.logPath = p
}

func (a *App) applyLayout(l config.Layout) {
	a.primary = paneID(l.Primary)
	if a.primary == "" {
		a.primary = paneLogs
	}
	a.split = paneID(l.Split)
	a.splitVert = l.Vertical
	a.splitRatio = l.Ratio
	if a.splitRatio <= 0 || a.splitRatio >= 1 {
		a.splitRatio = 0.6
	}
	a.focusSplit = false
}

// Init kicks off the initial loads and the log line pump.
func (a *App) Init() tea.Cmd {
	return tea.Batch(
		a.loadProjects(),
		a.loadTopology(),
		a.loadServices(),
		a.logMgr.waitForLine(),
		a.deployTick(),
	)
}

// --- async command constructors ---

func (a *App) loadProjects() tea.Cmd {
	c := a.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		refs, err := c.Projects(ctx)
		if err != nil {
			return errMsg{where: "projects", err: err}
		}
		return projectsLoadedMsg(refs)
	}
}

func (a *App) loadTopology() tea.Cmd {
	c, proj, env := a.client, a.projectID, a.env
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		p, err := c.Project(ctx, proj, env)
		if err != nil {
			return errMsg{where: "topology", err: err}
		}
		return topologyLoadedMsg{proj: p}
	}
}

func (a *App) loadServices() tea.Cmd {
	c, proj, env := a.client, a.projectID, a.env
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		svcs, err := c.Services(ctx, proj, env)
		if err != nil {
			return errMsg{where: "services", err: err}
		}
		return servicesLoadedMsg(svcs)
	}
}

func (a *App) deployTick() tea.Cmd {
	return tea.Tick(a.cfg.Polling.Deploy(), func(time.Time) tea.Msg { return deployTickMsg{} })
}

// loadDeployments fetches a service's deployment history.
func (a *App) loadDeployments(serviceID, serviceName string) tea.Cmd {
	c, proj, env := a.client, a.projectID, a.env
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		deps, err := c.Deployments(ctx, proj, env, serviceName)
		if err != nil {
			return errMsg{where: "deployments", err: err}
		}
		return deploymentsLoadedMsg{serviceID: serviceID, deployments: deps}
	}
}

// --- messages ---

type projectsLoadedMsg []model.ProjectRef
type topologyLoadedMsg struct{ proj *model.Project }
type servicesLoadedMsg []model.Service
type deploymentsLoadedMsg struct {
	serviceID   string
	deployments []model.Deployment
}
type deployTickMsg struct{}
type errMsg struct {
	where string
	err   error
}

// buildSources derives the toggleable log sources for the current services.
func (a *App) buildSources() []model.Source {
	var srcs []model.Source
	for _, s := range a.services {
		for _, k := range []model.LogKind{model.LogDeploy, model.LogBuild, model.LogHTTP} {
			srcs = append(srcs, model.Source{
				ServiceID:   s.ID,
				ServiceName: s.Name,
				Environment: a.env,
				Kind:        k,
			})
		}
	}
	return srcs
}

// switchContext repoints everything at a new project/environment.
func (a *App) switchContext(projectID, projectName, env string) tea.Cmd {
	a.projectID = projectID
	a.projectName = projectName
	a.env = env
	a.logMgr.stopAll()
	a.logMgr.project = projectID
	a.logs.buf = nil
	a.logs.activeKey = map[string]bool{}
	a.logs.reflow()
	a.watcher.lastSeen = map[string]model.DeployStatus{}
	a.errors.clear()
	a.autoStarted = false // re-auto-start deploy logs for the new context
	a.status = fmt.Sprintf("switched to %s / %s", projectName, env)
	return tea.Batch(a.loadTopology(), a.loadServices())
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		a.ready = true
		a.resize()
		return a, nil

	case tea.KeyMsg:
		// Picker takes priority when open.
		if a.picker.active {
			cmds = append(cmds, a.picker.Update(msg))
			return a, tea.Batch(cmds...)
		}
		if cmd, handled := a.handleGlobalKey(m); handled {
			return a, cmd
		}
		// Route to focused pane.
		cmds = append(cmds, a.routeKey(msg))
		return a, tea.Batch(cmds...)

	case projectsLoadedMsg:
		a.projects = []model.ProjectRef(m)
		a.picker.setProjects(a.projects)
		// Resolve a friendly project name / default project if unset.
		a.resolveContext()
		return a, nil

	case topologyLoadedMsg:
		a.proj = m.proj
		a.topology.setProject(m.proj)
		if a.projectName == "" && m.proj != nil {
			a.projectName = m.proj.Name
		}
		return a, nil

	case servicesLoadedMsg:
		a.services = []model.Service(m)
		dbg.Logf("app services loaded: %d service(s) for env=%q project=%q", len(a.services), a.env, a.projectID)
		a.deploys.setServices(a.env, a.services)
		a.logs.setSources(a.buildSources())
		// On first load, auto-start deploy logs for every service so the
		// merged log view populates immediately (compose-style) without the
		// user having to toggle each source by hand.
		if !a.autoStarted && len(a.services) > 0 {
			a.autoStarted = true
			for _, s := range a.services {
				src := model.Source{ServiceID: s.ID, ServiceName: s.Name, Environment: a.env, Kind: model.LogDeploy}
				a.logMgr.add(src)
				a.logs.activeKey[src.Key()] = true
			}
			a.status = fmt.Sprintf("streaming deploy logs from %d service(s)", len(a.services))
		}
		// Watcher diff → notifications.
		for _, note := range a.watcher.onServices(a.services) {
			cmds = append(cmds, a.notify.push(note))
		}
		return a, tea.Batch(cmds...)

	case deployTickMsg:
		cmds = append(cmds, a.loadServices(), a.loadTopology(), a.deployTick())
		// Refresh history for any expanded services so in-progress deploys
		// update live.
		for _, id := range a.deploys.expandedServiceIDs() {
			for _, s := range a.services {
				if s.ID == id {
					cmds = append(cmds, a.loadDeployments(id, s.Name))
					break
				}
			}
		}
		return a, tea.Batch(cmds...)

	case logLineMsg:
		ll := model.LogLine(m)
		isNew := a.logs.append(ll)
		// Only react to genuinely new lines — a replayed duplicate (from a
		// tail seed or stream reconnect) must not re-trigger an error entry
		// or toast every time it reappears.
		if isNew {
			if a.watcher.isError(ll) {
				a.errors.append(ll)
			}
			if note := a.watcher.onLogLine(ll); note != nil {
				cmds = append(cmds, a.notify.push(*note))
			}
		}
		// Re-arm the pump.
		cmds = append(cmds, a.logMgr.waitForLine())
		return a, tea.Batch(cmds...)

	case toastExpiredMsg:
		a.notify.sweep()
		return a, nil

	case sourceToggle:
		a.logMgr.toggle(m.src)
		return a, nil

	case pickerChoiceMsg:
		cmds = append(cmds, a.switchContext(m.project.ID, m.project.Name, m.env.Name))
		return a, tea.Batch(cmds...)

	case focusServiceMsg:
		// Auto-start this service's deploy logs if not already.
		src := model.Source{ServiceID: m.service.ID, ServiceName: m.service.Name, Environment: a.env, Kind: model.LogDeploy}
		if !a.logMgr.isActive(src.Key()) {
			a.logMgr.add(src)
			a.logs.activeKey[src.Key()] = true
		}
		a.primary = paneLogs
		a.status = "focused logs on " + m.service.Name
		return a, tea.Batch(cmds...)

	case loadDeploymentsMsg:
		cmds = append(cmds, a.loadDeployments(m.serviceID, m.serviceName))
		return a, tea.Batch(cmds...)

	case deploymentsLoadedMsg:
		a.deploys.setHistory(m.serviceID, m.deployments)
		return a, nil

	case deployActionMsg:
		cmds = append(cmds, a.runDeployAction(m))
		return a, tea.Batch(cmds...)

	case actionDoneMsg:
		if m.err != nil {
			a.status = m.action + " failed: " + m.err.Error()
		} else {
			a.status = m.action + " " + m.service + " ✔"
		}
		cmds = append(cmds, a.loadServices())
		return a, tea.Batch(cmds...)

	case settingsChangedMsg:
		a.notify.setDur(a.cfg.Notifications.Toast())
		a.watcher.setConfig(a.cfg.Notifications)
		return a, nil

	case errMsg:
		a.handleErr(m)
		return a, nil
	}

	// Forward remaining (mouse/tick) to focused pane for viewport etc.
	cmds = append(cmds, a.routeKey(msg))
	return a, tea.Batch(cmds...)
}

// handleErr records an error; auth/link failures are fatal-ish and shown big.
func (a *App) handleErr(m errMsg) {
	msg := m.err.Error()
	dbg.Logf("app ERROR [%s]: %s", m.where, msg)
	low := strings.ToLower(msg)
	if strings.Contains(low, "unauthorized") || strings.Contains(low, "not logged in") || strings.Contains(low, "login") {
		a.fatal = "Not logged in. Run `railway login` and restart.\n\n" + msg
		return
	}
	if strings.Contains(low, "no linked project") || strings.Contains(low, "link") {
		if a.projectID == "" {
			a.status = "No linked project — press [p] to pick one."
			return
		}
	}
	a.status = m.where + ": " + msg
}

// resolveContext fills projectName from the projects list when possible.
func (a *App) resolveContext() {
	for _, p := range a.projects {
		if p.ID == a.projectID || p.Name == a.projectID {
			a.projectID = p.ID
			a.projectName = p.Name
			if a.env == "" && len(p.Envs) > 0 {
				a.env = p.Envs[0].Name
			}
			return
		}
	}
}

// --- deploy actions ---

type actionDoneMsg struct {
	action  string
	service string
	err     error
}

func (a *App) runDeployAction(m deployActionMsg) tea.Cmd {
	c, proj, env := a.client, a.projectID, a.env
	action, svc := m.action, m.service.Name
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var err error
		switch action {
		case "redeploy":
			err = c.Redeploy(ctx, proj, env, svc)
		case "restart":
			err = c.Restart(ctx, proj, env, svc)
		}
		return actionDoneMsg{action: action, service: svc, err: err}
	}
}
