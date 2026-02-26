package handler

import (
	"net/http"

	"github.com/miltonparedes/giro/internal/kiro"
)

func kiroErrorStatus(err error) int {
	status, ok := kiro.StatusCodeFromError(err)
	if !ok {
		return http.StatusBadGateway
	}
	if status < 100 || status > 599 {
		return http.StatusBadGateway
	}
	return status
}
