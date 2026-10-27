package middleware

import "context"

type requestKey struct{}

type requestInfo struct {
	session string
	secure  bool
}

func WithRequestInfo(ctx context.Context, session string, secure bool) context.Context {
	return context.WithValue(ctx, requestKey{}, requestInfo{session: session, secure: secure})
}

func Session(ctx context.Context) string {
	info, _ := ctx.Value(requestKey{}).(requestInfo)
	return info.session
}

func IsSecureRequest(ctx context.Context) bool {
	info, _ := ctx.Value(requestKey{}).(requestInfo)
	return info.secure
}
