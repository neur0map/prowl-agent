package httpapi

import "example.com/go-auth-service/internal/auth"

func Run() error {
	return auth.Validate(auth.Issue("local-user"))
}
