package auth

import "strings"

func Issue(subject string) string {
	return "token:" + subject
}

func Validate(token string) error {
	if !strings.HasPrefix(token, "token:") {
		return ErrInvalidToken{}
	}
	return nil
}

type ErrInvalidToken struct{}

func (ErrInvalidToken) Error() string { return "invalid token" }
