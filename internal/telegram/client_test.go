package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func localClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := New("123:secret-token")
	client.baseURL = server.URL
	return client
}

func TestRateLimitedSendRetriesAndPreservesPlainText(t *testing.T) {
	var attempts atomic.Int32
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		var fields map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			t.Error(err)
		}
		if _, exists := fields["parse_mode"]; exists {
			t.Error("plain text must not enable formatting")
		}
		var text string
		if err := json.Unmarshal(fields["text"], &text); err != nil {
			t.Error(err)
		}
		if text != "<b>literal</b> _literal_" {
			t.Errorf("text changed: %q", text)
		}
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":0}}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":42,"chat":{"id":-10,"type":"supergroup"},"message_thread_id":8}}`)
	})
	message, err := client.Send(context.Background(), -10, 8, "<b>literal</b> _literal_", nil)
	if err != nil || message.MessageID != 42 {
		t.Fatalf("send = %+v, %v", message, err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d", attempts.Load())
	}
}

func TestRetryAfterCancellationAndBounds(t *testing.T) {
	for _, seconds := range []int{60, 61} {
		t.Run(fmt.Sprint(seconds), func(t *testing.T) {
			var attempts atomic.Int32
			client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprintf(w, `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":%d}}`, seconds)
			})
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			_, err := client.Send(ctx, 1, 0, "hello", nil)
			if seconds == 60 {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("expected cancellation, got %v", err)
				}
			} else {
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.RetryAfter != 61 {
					t.Fatalf("expected bounded rate limit, got %v", err)
				}
			}
			if DeliveryUncertain(err) {
				t.Fatalf("explicit rejection became uncertain: %v", err)
			}
			if attempts.Load() != 1 {
				t.Fatalf("retried before server delay: %d", attempts.Load())
			}
		})
	}
}

func TestRateLimitAttemptLimit(t *testing.T) {
	var attempts atomic.Int32
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":0}}`)
	})
	_, err := client.Send(context.Background(), 1, 0, "hello", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 429 || attempts.Load() != 3 || DeliveryUncertain(err) {
		t.Fatalf("attempts=%d error=%v", attempts.Load(), err)
	}
}

func TestAmbiguousSendNotRetriedAndURLNotExposed(t *testing.T) {
	var attempts atomic.Int32
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close()
	})
	_, err := client.Send(context.Background(), 1, 0, "hello", nil)
	if err == nil || !DeliveryUncertain(err) {
		t.Fatal("expected uncertain transport failure")
	}
	if attempts.Load() != 1 {
		t.Fatalf("ambiguous send retried %d times", attempts.Load())
	}
	if strings.Contains(err.Error(), client.token) || strings.Contains(err.Error(), client.baseURL) {
		t.Fatalf("transport error exposed credentials: %v", err)
	}
}

func TestSafeReadRetriesServerFailure(t *testing.T) {
	var attempts atomic.Int32
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprint(w, "upstream unavailable")
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":{"id":7,"is_bot":true,"username":"test_bot"}}`)
	})
	user, err := client.GetMe(context.Background())
	if err != nil || user.ID != 7 || !user.IsBot || attempts.Load() != 2 {
		t.Fatalf("user=%+v attempts=%d error=%v", user, attempts.Load(), err)
	}
}

func TestAPIErrorsRedactSecrets(t *testing.T) {
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"ok": false, "error_code": 401,
			"description": "Unauthorized 123:secret-token 123%3Asecret-token https://api.telegram.org/bot123:secret-token/getMe\n",
		})
	})
	_, err := client.GetMe(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 401 || DeliveryUncertain(err) {
		t.Fatalf("expected auth error, got %v", err)
	}
	if strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "https://") || strings.Contains(err.Error(), "\n") {
		t.Fatalf("unsafe error: %v", err)
	}
	if !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("lost useful error: %v", err)
	}
}

func TestUnchangedEditIsSuccess(t *testing.T) {
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"Bad Request: message is not modified: specified new content is identical"}`)
	})
	if err := client.Edit(context.Background(), 1, 2, "same", nil); err != nil {
		t.Fatal(err)
	}
}

func TestRedirectDoesNotForwardCredentials(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	_, err := client.Send(context.Background(), 1, 0, "private", nil)
	if err == nil || forwarded.Load() != 0 {
		t.Fatalf("redirect followed: requests=%d err=%v", forwarded.Load(), err)
	}
}

func TestSendResponseDeliveryCertainty(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		uncertain bool
	}{
		{"rejected", 400, `{"ok":false,"error_code":400,"description":"Bad Request"}`, false},
		{"rejected with success status", 200, `{"ok":false,"error_code":403,"description":"Forbidden"}`, false},
		{"status alone", 403, `{}`, true},
		{"missing ok", 429, `{"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":0}}`, true},
		{"missing description", 429, `{"ok":false,"error_code":429,"parameters":{"retry_after":0}}`, true},
		{"truncated rejection", 400, `{"ok":false,"error_code":400,"description":"Bad Request"`, true},
		{"invalid result", 200, `{"ok":true,"result":{"message_id":"invalid"}}`, true},
		{"missing result", 200, `{"ok":true}`, true},
		{"contradictory status", 500, `{"ok":true,"result":{"message_id":42}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var attempts atomic.Int32
			client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			_, err := client.Send(context.Background(), 1, 0, "hello", nil)
			if err == nil || DeliveryUncertain(err) != tc.uncertain || attempts.Load() != 1 {
				t.Fatalf("error=%v uncertain=%v attempts=%d", err, DeliveryUncertain(err), attempts.Load())
			}
		})
	}
}

func TestSendPreflightFailures(t *testing.T) {
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("preflight failure reached network") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Send(ctx, 1, 0, "hello", nil)
	if !errors.Is(err, context.Canceled) || DeliveryUncertain(err) {
		t.Fatalf("canceled send = %v", err)
	}
	err = client.call(context.Background(), "sendMessage", make(chan int), nil, false)
	if err == nil || DeliveryUncertain(err) {
		t.Fatalf("unencodable request = %v", err)
	}
	client.baseURL = ":invalid"
	_, err = client.Send(context.Background(), 1, 0, "hello", nil)
	if err == nil || DeliveryUncertain(err) {
		t.Fatalf("invalid endpoint = %v", err)
	}
}

func TestRetryCannotEraseEarlierUncertainty(t *testing.T) {
	var attempts atomic.Int32
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(502)
			fmt.Fprint(w, "upstream unavailable")
			return
		}
		fmt.Fprint(w, `{"ok":false,"error_code":403,"description":"Forbidden"}`)
	})
	_, err := client.GetMe(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 403 || !DeliveryUncertain(err) || attempts.Load() != 2 {
		t.Fatalf("error=%v uncertain=%v attempts=%d", err, DeliveryUncertain(err), attempts.Load())
	}
}

func TestSendIncompleteResponseRemainsUncertain(t *testing.T) {
	var attempts atomic.Int32
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"ok":false,"error_code":403,"description":"Forbidden"}`)
	})
	_, err := client.Send(context.Background(), 1, 0, "hello", nil)
	if err == nil || !DeliveryUncertain(err) || attempts.Load() != 1 {
		t.Fatalf("error=%v uncertain=%v attempts=%d", err, DeliveryUncertain(err), attempts.Load())
	}
}
