package client

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// IsRateLimited reports whether the response is an HTTP 429 (Too Many Requests).
func IsRateLimited(resp *Response) bool {
	return resp.StatusCode == http.StatusTooManyRequests
}

// RateLimitError builds the user-facing error for an HTTP 429. It surfaces the
// backend's {"error": "..."} message and the Retry-After header so the caller
// knows how long to wait. No retry/backoff is attempted on purpose: the rate
// limit exists to slow the caller (an AI agent) down, so the CLI fails fast and
// explains instead of hammering the backend.
func RateLimitError(resp *Response) error {
	msg := strings.TrimSpace(serverError(resp.Body))
	retry := retryAfterSeconds(resp.Header)
	switch {
	case msg != "" && retry != "":
		return fmt.Errorf("%s. Tente de novo em %ss", strings.TrimRight(msg, "."), retry)
	case msg != "":
		return fmt.Errorf("%s", msg)
	case retry != "":
		return fmt.Errorf("limite de requisições excedido. Tente de novo em %ss", retry)
	default:
		return fmt.Errorf("limite de requisições excedido")
	}
}

// retryAfterSeconds returns the Retry-After header when it is a plain integer
// number of seconds, or "" otherwise. The backend always sends an integer.
func retryAfterSeconds(h http.Header) string {
	if h == nil {
		return ""
	}
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return ""
	}
	if _, err := strconv.Atoi(v); err != nil {
		return ""
	}
	return v
}
