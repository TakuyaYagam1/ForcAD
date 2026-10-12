package v1

import "context"

type contextKey uint8

const (
	requestKey contextKey = iota
	usernameKey
	teamTokenKey
)

type requestInfo struct {
	session string
	secure  bool
}

func WithRequestInfo(ctx context.Context, session string, secure bool) context.Context {
	return context.WithValue(ctx, requestKey, requestInfo{session: session, secure: secure})
}

func IsSecureRequest(ctx context.Context) bool {
	info, _ := ctx.Value(requestKey).(requestInfo)
	return info.secure
}
