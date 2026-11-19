package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/norenis/kai/internal/config"
	"golang.org/x/oauth2"
)

// oidcStateTTL bounds how long a login attempt has to complete the OAuth
// redirect round-trip before its CSRF state token expires.
const oidcStateTTL = 5 * time.Minute

// oidcAuth holds the resolved OIDC provider and OAuth2 client used to
// authenticate dashboard users against an external identity provider.
type oidcAuth struct {
	oauth2Config oauth2.Config
	verifier     *oidc.IDTokenVerifier
}

// newOIDCAuth discovers the provider's endpoints via its issuer URL and
// builds the OAuth2 client. Callers should log and continue without SSO
// rather than fail dashboard startup if this returns an error (e.g. the
// provider is unreachable).
func newOIDCAuth(ctx context.Context, cfg config.SSOConfig) (*oidcAuth, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover oidc provider: %w", err)
	}

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}

	return &oidcAuth{
		oauth2Config: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       scopes,
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
	}, nil
}

// handleOIDCLogin redirects the browser to the identity provider's
// authorization endpoint, carrying a one-time state token for CSRF protection.
func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		http.Error(w, "sso not configured", http.StatusNotFound)
		return
	}
	state := generateToken()
	s.oauthStates.Store(state, time.Now().Add(oidcStateTTL).Unix())
	http.Redirect(w, r, s.oidc.oauth2Config.AuthCodeURL(state), http.StatusFound)
}

// handleOIDCCallback completes the OAuth2 code exchange, verifies the ID
// token, and establishes a dashboard session identical to the PIN flow.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		http.Error(w, "sso not configured", http.StatusNotFound)
		return
	}

	state := r.URL.Query().Get("state")
	expiry, ok := s.oauthStates.Load(state)
	s.oauthStates.Delete(state)
	if !ok || expiry.(int64) < time.Now().Unix() {
		http.Error(w, "invalid or expired login attempt", http.StatusBadRequest)
		return
	}

	token, err := s.oidc.oauth2Config.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, "sso exchange failed", http.StatusUnauthorized)
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "sso response missing id_token", http.StatusUnauthorized)
		return
	}
	if _, err := s.oidc.verifier.Verify(r.Context(), rawIDToken); err != nil {
		http.Error(w, "sso token verification failed", http.StatusUnauthorized)
		return
	}

	s.startSession(w)
	http.Redirect(w, r, "/", http.StatusFound)
}
