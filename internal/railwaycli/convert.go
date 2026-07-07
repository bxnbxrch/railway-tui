package railwaycli

import "railway-tui/internal/model"

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (r rawService) toModel() model.Service {
	svc := model.Service{
		ID:           r.ID,
		Name:         r.Name,
		IsLinked:     r.IsLinked,
		Status:       model.DeployStatus(r.Status),
		DeploymentID: r.DeploymentID,
		Repo:         deref(r.Source.Repo),
		Image:        deref(r.Source.Image),
		URL:          deref(r.URL),
		Replicas: model.Replicas{
			Configured: r.Replicas.Configured,
			Running:    r.Replicas.Running,
			Crashed:    r.Replicas.Crashed,
			Exited:     r.Replicas.Exited,
			Total:      r.Replicas.Total,
		},
	}
	for _, v := range r.Volumes {
		svc.Volumes = append(svc.Volumes, model.Volume{
			Name: v.Name, MountPath: v.MountPath,
			CurrentSizeMB: v.CurrentSizeMB, SizeMB: v.SizeMB, State: v.State,
		})
	}
	for _, rg := range r.Regions {
		svc.Regions = append(svc.Regions, model.Region{
			Name: rg.Name, Location: rg.Location, Configured: rg.Configured,
		})
	}
	if r.LatestDeployment != nil {
		d := model.Deployment{
			ID:        r.LatestDeployment.ID,
			Status:    model.DeployStatus(r.LatestDeployment.Status),
			CreatedAt: parseTime(r.LatestDeployment.CreatedAt),
			Stopped:   r.LatestDeployment.Stopped,
		}
		svc.LatestDeploy = &d
	}
	return svc
}

func (r rawDomainFull) toModel() model.Domain {
	return model.Domain{
		ID:         r.ID,
		Domain:     r.Domain,
		Type:       r.Type,
		TargetPort: r.TargetPort,
		SyncStatus: r.SyncStatus,
	}
}

func (r rawDeployment) toModel() model.Deployment {
	return model.Deployment{
		ID:            r.ID,
		Status:        model.DeployStatus(r.Status),
		CreatedAt:     parseTime(r.CreatedAt),
		Branch:        r.Meta.Branch,
		CommitHash:    r.Meta.CommitHash,
		CommitMessage: r.Meta.CommitMessage,
		CommitAuthor:  r.Meta.CommitAuthor,
		Reason:        r.Meta.Reason,
	}
}

func (r rawStatus) toModel() *model.Project {
	ws := ""
	if m, ok := r.Workspace.(map[string]any); ok {
		if n, ok := m["name"].(string); ok {
			ws = n
		}
	}
	p := &model.Project{ID: r.ID, Name: r.Name, Workspace: ws}
	for _, ee := range r.Environments.Edges {
		en := ee.Node
		env := model.Environment{ID: en.ID, Name: en.Name}
		for _, se := range en.ServiceInstances.Edges {
			si := se.Node
			svc := model.Service{
				ID:   si.ServiceID,
				Name: si.ServiceName,
			}
			if si.NumReplicas != nil {
				svc.Replicas.Configured = *si.NumReplicas
			}
			if si.Source != nil {
				svc.Repo = deref(si.Source.Repo)
				svc.Image = deref(si.Source.Image)
			}
			// Prefer a real service domain as the URL.
			for _, d := range si.Domains.ServiceDomains {
				if d.Domain != "" {
					svc.URL = "https://" + d.Domain
					break
				}
			}
			for _, d := range si.Domains.CustomDomains {
				if d.Domain != "" {
					svc.URL = "https://" + d.Domain
				}
			}
			if si.LatestDeployment != nil {
				svc.Status = model.DeployStatus(si.LatestDeployment.Status)
				svc.DeploymentID = si.LatestDeployment.ID
				d := model.Deployment{
					ID:        si.LatestDeployment.ID,
					Status:    model.DeployStatus(si.LatestDeployment.Status),
					CreatedAt: parseTime(si.LatestDeployment.CreatedAt),
					Stopped:   si.LatestDeployment.Stopped,
				}
				svc.LatestDeploy = &d
			}
			env.Services = append(env.Services, svc)
		}
		p.Environments = append(p.Environments, env)
	}
	return p
}

func (r rawMetrics) toModel(service string) *model.Metrics {
	name := r.ServiceName
	if name == "" {
		name = service
	}
	m := &model.Metrics{
		ServiceName: name,
		Environment: r.Environment,
		Series:      make(map[string]model.MetricSeries, len(r.Measurements)),
	}
	for key, pts := range r.Measurements {
		s := model.MetricSeries{Name: key, Points: make([]model.MetricPoint, 0, len(pts))}
		for _, p := range pts {
			s.Points = append(s.Points, model.MetricPoint{TS: parseTime(p.TS), Value: p.Value})
		}
		m.Series[key] = s
	}
	return m
}
