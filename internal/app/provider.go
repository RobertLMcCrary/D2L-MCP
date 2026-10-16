package app

import (
	"context"
	"github.com/RobertLMcCrary/D2L-MCP/internal/auth"
	"github.com/RobertLMcCrary/D2L-MCP/internal/canvas"
	"github.com/RobertLMcCrary/D2L-MCP/internal/lms"
)

func (s *Service) Provider(ctx context.Context) (lms.Provider, error) {
	if s.Config.Provider != "canvas" {
		client, _, err := s.Client(ctx)
		return client, err
	}
	token, err := auth.LoadCanvas(s.Paths, s.Config.LMSHost)
	if err != nil {
		return nil, &lms.Error{Code: "auth_required", Message: err.Error()}
	}
	return canvas.New(s.Config.LMSHost, token, s.Config.EnableWrites, s.HTTPTransport)
}

type ProviderStatus struct {
	Provider     string           `json:"provider"`
	Host         string           `json:"host,omitempty"`
	Configured   bool             `json:"configured"`
	TokenPresent bool             `json:"token_present"`
	APIReachable bool             `json:"api_reachable"`
	Capabilities lms.Capabilities `json:"capabilities"`
	Writes       WriteAccess      `json:"writes"`
	User         any              `json:"user,omitempty"`
	NextStep     string           `json:"next_step,omitempty"`
}

func (s *Service) ProviderStatus(ctx context.Context, network bool) ProviderStatus {
	name := s.Config.Provider
	if name == "" {
		name = "d2l"
	}
	result := ProviderStatus{Provider: name, Host: s.Config.LMSHost, Configured: s.Config.LMSHost != "", Writes: s.WriteAccess()}
	if !result.Configured {
		result.NextStep = "Run setup --provider " + name + " --host https://your-school.example"
		return result
	}
	provider, err := s.Provider(ctx)
	if err != nil {
		result.NextStep = err.Error()
		return result
	}
	result.TokenPresent = true
	result.Capabilities = provider.Capabilities()
	if !network {
		return result
	}
	path := "/api/v1/users/self/profile"
	if name == "d2l" {
		path = "/d2l/api/lp/1.47/users/whoami"
	}
	response, err := provider.Do(ctx, lms.Request{Path: path})
	if err != nil {
		result.NextStep = err.Error()
		return result
	}
	result.APIReachable = true
	result.User = response.Data
	return result
}
