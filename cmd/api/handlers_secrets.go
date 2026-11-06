// path: cmd/api/handlers_secrets.go
package main

import (
	"encoding/json"
	"net/http"

	"github.com/crnji/forge-ci/internal/secrets"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

type SecretPayload struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func handleCreateSecret(sm *secrets.SecretManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload SecretPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]string{"error": "invalid JSON"})
			return
		}
		if payload.Name == "" {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]string{"error": "name required"})
			return
		}
		if payload.Value == "" {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]string{"error": "value required"})
			return
		}
		if err := sm.AddSecret(payload.Name, payload.Value); err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, map[string]string{"error": err.Error()})
			return
		}
		render.Status(r, http.StatusCreated)
		render.JSON(w, r, map[string]string{"status": "created", "name": payload.Name})
	}
}

func handleListSecrets(sm *secrets.SecretManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		names := sm.ListSecretNames()
		if names == nil {
			names = []string{}
		}
		render.JSON(w, r, names)
	}
}

func handleGetSecret(sm *secrets.SecretManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		if _, err := sm.GetSecret(name); err != nil {
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, map[string]string{"error": "secret not found"})
			return
		}
		// Return masked placeholder to prevent secret exfiltration
		render.JSON(w, r, map[string]string{"name": name, "value": "********"})
	}
}

func handleDeleteSecret(sm *secrets.SecretManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		if err := sm.DeleteSecret(name); err != nil {
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

