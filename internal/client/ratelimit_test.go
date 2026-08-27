package client

import (
	"net/http"
	"strings"
	"testing"
)

func TestIsRateLimited(t *testing.T) {
	if !IsRateLimited(&Response{StatusCode: http.StatusTooManyRequests}) {
		t.Error("429 should be rate limited")
	}
	if IsRateLimited(&Response{StatusCode: http.StatusOK}) {
		t.Error("200 should not be rate limited")
	}
}

func TestRateLimitError(t *testing.T) {
	header := func(retryAfter string) http.Header {
		h := http.Header{}
		if retryAfter != "" {
			h.Set("Retry-After", retryAfter)
		}
		return h
	}
	cases := []struct {
		name string
		resp *Response
		want []string
	}{
		{
			name: "message and retry-after",
			resp: &Response{Body: []byte(`{"error":"Limite de requisições excedido."}`), Header: header("30")},
			want: []string{"Limite de requisições excedido", "Tente de novo em 30s"},
		},
		{
			name: "message only",
			resp: &Response{Body: []byte(`{"error":"Calma aí"}`), Header: header("")},
			want: []string{"Calma aí"},
		},
		{
			name: "retry-after only",
			resp: &Response{Body: nil, Header: header("15")},
			want: []string{"limite de requisições excedido", "Tente de novo em 15s"},
		},
		{
			name: "no message no retry-after",
			resp: &Response{Body: nil, Header: nil},
			want: []string{"limite de requisições excedido"},
		},
		{
			name: "non-integer retry-after is ignored",
			resp: &Response{Body: []byte(`{"error":"Devagar"}`), Header: header("Mon, 01 Jan 2035 00:00:00 GMT")},
			want: []string{"Devagar"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RateLimitError(tc.resp).Error()
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("error %q missing %q", got, w)
				}
			}
			if tc.name == "message and retry-after" && strings.Contains(got, "..") {
				t.Errorf("message should not double the period: %q", got)
			}
		})
	}
}
