package response

import "github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"

func AdminSession(username, csrf string) api.AdminSession {
	return api.AdminSession{
		Status:    api.AdminSessionStatusOk,
		Username:  username,
		CsrfToken: csrf,
	}
}
