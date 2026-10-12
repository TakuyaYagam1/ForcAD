package v1

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/usecase/auth"
)

type authHandler struct {
	auth AuthService
}

func (h *authHandler) HandleSessionCookie(ctx context.Context, _ api.OperationName, token api.SessionCookie) (context.Context, error) {
	const location = "httpv1.authHandler.HandleSessionCookie"
	username, err := h.auth.Status(ctx, token.APIKey)
	if err != nil {
		return ctx, fmt.Errorf("%s - validate session cookie: %w", location, err)
	}
	return context.WithValue(ctx, usernameKey, username), nil
}

func sessionCookie(ctx context.Context, token string, expired bool) string {
	info, _ := ctx.Value(requestKey).(requestInfo)
	//nolint:gosec // Secure is enabled from TLS, configured HTTPS, or a trusted proxy.
	c := http.Cookie{Name: "session", Value: token, Path: "/", MaxAge: int(auth.SessionTTL.Seconds()), HttpOnly: true, Secure: info.secure, SameSite: http.SameSiteLaxMode}
	if expired {
		c.MaxAge = -1
		c.Expires = time.Unix(1, 0)
	}
	return c.String()
}

func (h *authHandler) AdminLogin(ctx context.Context, req *api.AdminLoginRequest) (api.AdminLoginRes, error) {
	const location = "httpv1.authHandler.AdminLogin"
	token, err := h.auth.Login(ctx, req.Username, req.Password)
	if err != nil {
		return nil, fmt.Errorf("%s - authenticate administrator: %w", location, err)
	}
	return &api.AdminSessionHeaders{SetCookie: api.NewOptString(sessionCookie(ctx, token, false)), Response: api.AdminSession{Status: api.AdminSessionStatusOk, Username: req.Username}}, nil
}

func (h *authHandler) AdminLogout(ctx context.Context) (*api.StatusResponseHeaders, error) {
	const location = "httpv1.authHandler.AdminLogout"
	info, _ := ctx.Value(requestKey).(requestInfo)
	if err := h.auth.Logout(ctx, info.session); err != nil {
		return nil, fmt.Errorf("%s - end administrator session: %w", location, err)
	}
	return &api.StatusResponseHeaders{SetCookie: api.NewOptString(sessionCookie(ctx, "", true)), Response: api.StatusResponse{Status: api.StatusResponseStatusOk}}, nil
}

func (h *authHandler) GetAdminSessionStatus(ctx context.Context) (api.GetAdminSessionStatusRes, error) {
	username, _ := ctx.Value(usernameKey).(string)
	return &api.AdminSession{Status: api.AdminSessionStatusOk, Username: username}, nil
}
