package railwaycli

import "time"

// This file holds the raw wire structs matching `railway ... --json` output.
// They are intentionally private; callers get the cleaned-up model.* types.

// --- service list --json ---

type rawService struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	IsLinked         bool              `json:"isLinked"`
	Source           rawSource         `json:"source"`
	Status           string            `json:"status"`
	DeploymentStop   bool              `json:"deploymentStopped"`
	DeploymentID     string            `json:"deploymentId"`
	LatestDeployment *rawDeploymentLit `json:"latestDeployment"`
	URL              *string           `json:"url"`
	Volumes          []rawVolume       `json:"volumes"`
	Regions          []rawRegion       `json:"regions"`
	Replicas         rawReplicas       `json:"replicas"`
}

type rawSource struct {
	Repo  *string `json:"repo"`
	Image *string `json:"image"`
}

type rawDeploymentLit struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	Stopped   bool   `json:"deploymentStopped"`
}

type rawVolume struct {
	Name          string  `json:"name"`
	MountPath     string  `json:"mountPath"`
	CurrentSizeMB float64 `json:"currentSizeMb"`
	SizeMB        float64 `json:"sizeMb"`
	State         string  `json:"state"`
}

type rawRegion struct {
	Name       string `json:"name"`
	Location   string `json:"location"`
	Configured int    `json:"configured"`
}

type rawReplicas struct {
	Configured int `json:"configured"`
	Running    int `json:"running"`
	Crashed    int `json:"crashed"`
	Exited     int `json:"exited"`
	Total      int `json:"total"`
}

// --- deployment list --json ---

type rawDeployment struct {
	ID        string   `json:"id"`
	Status    string   `json:"status"`
	CreatedAt string   `json:"createdAt"`
	Meta      rawDMeta `json:"meta"`
}

type rawDMeta struct {
	Branch        string `json:"branch"`
	CommitHash    string `json:"commitHash"`
	CommitMessage string `json:"commitMessage"`
	CommitAuthor  string `json:"commitAuthor"`
	Reason        string `json:"reason"`
}

// --- status --json ---

type rawStatus struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Workspace    any    `json:"workspace"`
	Environments struct {
		Edges []struct {
			Node rawEnvNode `json:"node"`
		} `json:"edges"`
	} `json:"environments"`
}

type rawEnvNode struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	CanAccess        bool   `json:"canAccess"`
	ServiceInstances struct {
		Edges []struct {
			Node rawServiceInstance `json:"node"`
		} `json:"edges"`
	} `json:"serviceInstances"`
}

type rawServiceInstance struct {
	ServiceID        string            `json:"serviceId"`
	ServiceName      string            `json:"serviceName"`
	EnvironmentID    string            `json:"environmentId"`
	NumReplicas      *int              `json:"numReplicas"`
	LatestDeployment *rawDeploymentLit `json:"latestDeployment"`
	Domains          rawDomains        `json:"domains"`
	Source           *rawSource        `json:"source"`
}

type rawDomains struct {
	CustomDomains  []rawDomain `json:"customDomains"`
	ServiceDomains []rawDomain `json:"serviceDomains"`
}

type rawDomain struct {
	Domain string `json:"domain"`
}

// --- list --json (projects) ---

type rawProjectRef struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Workspace struct {
		Name string `json:"name"`
	} `json:"workspace"`
	Environments struct {
		Edges []struct {
			Node struct {
				ID        string `json:"id"`
				Name      string `json:"name"`
				CanAccess bool   `json:"canAccess"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"environments"`
}

// --- metrics --raw --json ---

type rawMetrics struct {
	Environment  string                   `json:"environment"`
	ServiceName  string                   `json:"service"`
	Measurements map[string][]rawMetricPt `json:"measurements"`
}

type rawMetricPt struct {
	TS    string  `json:"ts"`
	Value float64 `json:"value"`
}

// --- logs --json ---

type rawLog struct {
	Timestamp string         `json:"timestamp"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Attrs     map[string]any `json:"-"`
}

// parseTime tries the timestamp formats Railway emits.
func parseTime(s string) time.Time {
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05.999999999-07:00",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
