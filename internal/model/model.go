// Package model holds the domain types shared across the TUI. These map onto
// the JSON emitted by the `railway` CLI (see internal/railwaycli), kept
// deliberately small and decoupled from the raw wire shapes.
package model

import "time"

// DeployStatus is the lifecycle state of a deployment as reported by Railway.
type DeployStatus string

const (
	StatusBuilding   DeployStatus = "BUILDING"
	StatusDeploying  DeployStatus = "DEPLOYING"
	StatusSuccess    DeployStatus = "SUCCESS"
	StatusFailed     DeployStatus = "FAILED"
	StatusCrashed    DeployStatus = "CRASHED"
	StatusRemoved    DeployStatus = "REMOVED"
	StatusInitial    DeployStatus = "INITIALIZING"
	StatusQueued     DeployStatus = "QUEUED"
	StatusSkipped    DeployStatus = "SKIPPED"
	StatusSleeping   DeployStatus = "SLEEPING"
	StatusNeedsApprv DeployStatus = "NEEDS_APPROVAL"
	StatusUnknown    DeployStatus = ""
)

// Bad reports whether the status represents a failure the user cares about.
func (s DeployStatus) Bad() bool {
	return s == StatusFailed || s == StatusCrashed
}

// Active reports whether the status represents in-progress work.
func (s DeployStatus) Active() bool {
	return s == StatusBuilding || s == StatusDeploying || s == StatusInitial || s == StatusQueued
}

// Service is a single Railway service within an environment.
type Service struct {
	ID           string
	Name         string
	IsLinked     bool
	Status       DeployStatus
	DeploymentID string
	Repo         string
	Image        string
	URL          string
	Replicas     Replicas
	Volumes      []Volume
	Regions      []Region
	LatestDeploy *Deployment
}

// Replicas captures replica health counts for a service.
type Replicas struct {
	Configured int
	Running    int
	Crashed    int
	Exited     int
	Total      int
}

// Volume is a persistent disk attached to a service.
type Volume struct {
	Name          string
	MountPath     string
	CurrentSizeMB float64
	SizeMB        float64
	State         string
}

// Region is a deployment region for a service.
type Region struct {
	Name       string
	Location   string
	Configured int
}

// Domain is a service or custom domain attached to a service.
type Domain struct {
	ID         string
	Domain     string
	Type       string // "service" | "custom"
	TargetPort int
	SyncStatus string
}

// URL returns the https URL for the domain.
func (d Domain) URL() string {
	return "https://" + d.Domain
}

// Variable is a single environment variable (name + raw value).
type Variable struct {
	Name  string
	Value string
}

// Deployment is a single deployment record.
type Deployment struct {
	ID            string
	Status        DeployStatus
	CreatedAt     time.Time
	Stopped       bool
	Branch        string
	CommitHash    string
	CommitMessage string
	CommitAuthor  string
	Reason        string
}

// ShortHash returns the first 7 chars of the commit hash.
func (d Deployment) ShortHash() string {
	if len(d.CommitHash) > 7 {
		return d.CommitHash[:7]
	}
	return d.CommitHash
}

// Environment groups service instances under a named environment.
type Environment struct {
	ID       string
	Name     string
	Services []Service
}

// Project is the top-level topology container.
type Project struct {
	ID           string
	Name         string
	Workspace    string
	Environments []Environment
}

// EnvName returns the first environment name, or "" if none.
func (p *Project) EnvName(id string) string {
	for _, e := range p.Environments {
		if e.ID == id {
			return e.Name
		}
	}
	return ""
}

// ProjectRef is a lightweight project entry from `railway list`, including its
// environments, used to populate the project/environment switcher without a
// full topology fetch.
type ProjectRef struct {
	ID        string
	Name      string
	Workspace string
	Envs      []EnvRef
}

// EnvRef is a lightweight environment entry.
type EnvRef struct {
	ID   string
	Name string
}

// LogKind distinguishes the streams a service can emit.
type LogKind string

const (
	LogDeploy  LogKind = "deploy"
	LogBuild   LogKind = "build"
	LogHTTP    LogKind = "http"
	LogNetwork LogKind = "net"
)

// Source identifies a single log stream: a service + which kind of log.
type Source struct {
	ServiceID   string
	ServiceName string
	Environment string
	Kind        LogKind
}

// Key is a stable identity for a source, used for dedup/toggling/coloring.
func (s Source) Key() string {
	return s.ServiceName + "|" + s.Environment + "|" + string(s.Kind)
}

// Label is the human-facing tag shown in the merged log stream.
func (s Source) Label() string {
	return s.ServiceName
}

// LogLine is a single parsed log entry tagged with its source.
type LogLine struct {
	Source    Source
	Timestamp time.Time
	Level     string
	Message   string
	// Raw carries the original JSON attributes for HTTP/network logs so the
	// pane can render richer detail without a bespoke struct per kind.
	Attrs map[string]any
}

// MetricSeries is a named time-series returned by `railway metrics --raw`.
type MetricSeries struct {
	Name   string
	Points []MetricPoint
}

// Last returns the most recent point value, or 0 if empty.
func (m MetricSeries) Last() float64 {
	if len(m.Points) == 0 {
		return 0
	}
	return m.Points[len(m.Points)-1].Value
}

// Values extracts the raw values in order, for sparkline rendering.
func (m MetricSeries) Values() []float64 {
	v := make([]float64, len(m.Points))
	for i, p := range m.Points {
		v[i] = p.Value
	}
	return v
}

// MetricPoint is a single (timestamp, value) sample.
type MetricPoint struct {
	TS    time.Time
	Value float64
}

// Metrics is the full set of series for one service in an environment.
type Metrics struct {
	ServiceID   string
	ServiceName string
	Environment string
	Series      map[string]MetricSeries
	FetchedAt   time.Time
}
