package api

import (
	"net/http"

	"dbmcp.local/server/internal/app"
	"dbmcp.local/server/internal/model"
)

func registrationRoutes(m *http.ServeMux, a *app.App) {
	s := a.Store
	route(m, "GET /admin/v1/registrations", func(w http.ResponseWriter, r *http.Request) error {
		out, err := s.Registrations()
		if err == nil {
			write(w, out)
		}
		return err
	})
	route(m, "POST /admin/v1/registrations", func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Name      string `json:"name"`
			ExpiresAt string `json:"expires_at"`
		}
		if err := decode(w, r, &in); err != nil {
			return err
		}
		registration, token, err := s.IssueRegistration(in.Name, in.ExpiresAt)
		if err == nil {
			write(w, map[string]any{"registration": registration, "token": token})
			a.Changed()
			_ = s.Audit(model.Audit{Action: "registration.issue", RequestID: registration.ID, Decision: "issued"})
		}
		return err
	})
	route(m, "POST /admin/v1/registrations/{id}/approve", func(w http.ResponseWriter, r *http.Request) error {
		var in model.RegistrationApproval
		if err := decode(w, r, &in); err != nil {
			return err
		}
		for _, binding := range in.Bindings {
			if err := app.ValidatePolicy(binding.Policy); err != nil {
				return err
			}
		}
		out, err := s.ApproveRegistration(r.PathValue("id"), in.Bindings)
		if err == nil {
			write(w, out)
			a.Changed()
			_ = s.Audit(model.Audit{EntityID: out.EntityID, RequestID: out.ID, Action: "registration.approve", Decision: "approved"})
		}
		return err
	})
	for _, decision := range []string{"deny", "revoke"} {
		status := "revoked"
		if decision == "deny" {
			status = "denied"
		}
		route(m, "POST /admin/v1/registrations/{id}/"+decision, func(w http.ResponseWriter, r *http.Request) error {
			if err := s.RevokeRegistration(r.PathValue("id"), status); err != nil {
				return err
			}
			write(w, map[string]bool{"recorded": true})
			a.Changed()
			_ = s.Audit(model.Audit{RequestID: r.PathValue("id"), Action: "registration." + decision, Decision: status})
			return nil
		})
	}
}
