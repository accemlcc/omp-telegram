package telegram

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDownloadRejectsUnsafePathsAndCleansPartial(t *testing.T) {
	for _, path := range []string{"../secret", "photos/%2e%2e/secret", "https://evil.test/file", "photos/file?token=secret", "photos/file"} {
		t.Run(path, func(t *testing.T) {
			var downloads atomic.Int32
			client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/getFile") {
					fmt.Fprintf(w, `{"ok":true,"result":{"file_path":%q,"file_size":1}}`, path)
					return
				}
				downloads.Add(1)
				w.(http.Flusher).Flush() // no Content-Length; metadata also understates size
				fmt.Fprint(w, "123456")
			})
			destination := filepath.Join(t.TempDir(), "attachment")
			err := client.Download(context.Background(), "file", destination, 5)
			if err == nil {
				t.Fatal("download unexpectedly succeeded")
			}
			if _, err := os.Stat(destination); !os.IsNotExist(err) {
				t.Fatalf("partial file remains: %v", err)
			}
			if path != "photos/file" && downloads.Load() != 0 {
				t.Fatal("unsafe path was requested")
			}
		})
	}
}

func TestDownloadPreservesExistingDestination(t *testing.T) {
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getFile") {
			fmt.Fprint(w, `{"ok":true,"result":{"file_path":"documents/file"}}`)
			return
		}
		fmt.Fprint(w, "new")
	})
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := client.Download(context.Background(), "id", path, 100); err == nil {
		t.Fatal("overwrote existing file")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original" {
		t.Fatalf("existing file changed: %q %v", data, err)
	}
}

func TestSendFileMultipartRetriesExplicitRateLimit(t *testing.T) {
	var attempts atomic.Int32
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bot123:secret-token/sendDocument" {
			t.Errorf("wrong endpoint: %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		file, header, err := r.FormFile("document")
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil || string(data) != "payload" || header.Filename != `quoted "file".txt` {
			t.Errorf("file = %q %q %v", data, header.Filename, err)
		}
		if r.FormValue("caption") != "caption" || r.FormValue("chat_id") != "-10" || r.FormValue("message_thread_id") != "8" {
			t.Error("lost multipart fields")
		}
		if attempts.Add(1) == 1 {
			w.WriteHeader(429)
			fmt.Fprint(w, `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":0}}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":42}}`)
	})
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("payload"), 0600); err != nil {
		t.Fatal(err)
	}
	message, err := client.SendFile(context.Background(), -10, 8, "document", path, `quoted "file".txt`, "caption")
	if err != nil || message.MessageID != 42 || attempts.Load() != 2 {
		t.Fatalf("send = %+v %v attempts=%d", message, err, attempts.Load())
	}
}

func TestSendFileUncertainDeliveryDoesNotRetry(t *testing.T) {
	var attempts atomic.Int32
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		connection.Close()
	})
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("payload"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := client.SendFile(context.Background(), 1, 0, "document", path, "file", "")
	if err == nil || !DeliveryUncertain(err) || strings.Contains(err.Error(), "secret-token") || attempts.Load() != 1 {
		t.Fatalf("error=%v attempts=%d", err, attempts.Load())
	}
}

func TestSendFileLimitsBeforeNetwork(t *testing.T) {
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid upload reached network") })
	path := filepath.Join(t.TempDir(), "file")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(50_000_001); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err := client.SendFile(context.Background(), 1, 0, "document", path, "file", ""); err == nil || DeliveryUncertain(err) {
		t.Fatal("oversize accepted")
	}
	if err := os.WriteFile(path, []byte("not an image"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SendFile(context.Background(), 1, 0, "photo", path, "file", ""); err == nil || DeliveryUncertain(err) {
		t.Fatal("invalid photo accepted")
	}
	if _, err := client.SendFile(context.Background(), 1, 0, "document", path, "file", strings.Repeat("\U0001F600", 513)); err == nil || DeliveryUncertain(err) {
		t.Fatal("oversize UTF-16 caption accepted")
	}
}

func TestSendFileDeliveryCertainty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"photo", "document"} {
		for _, tc := range []struct {
			name      string
			body      string
			uncertain bool
		}{
			{"rejected", `{"ok":false,"error_code":403,"description":"Forbidden"}`, false},
			{"incomplete rejection", `{"ok":false}`, true},
			{"unparseable result", `{"ok":true,"result":false}`, true},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				var attempts atomic.Int32
				client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
					attempts.Add(1)
					_, _ = io.Copy(io.Discard, r.Body)
					fmt.Fprint(w, tc.body)
				})
				_, err := client.SendFile(context.Background(), 1, 0, kind, path, "image.png", "")
				if err == nil || DeliveryUncertain(err) != tc.uncertain || attempts.Load() != 1 {
					t.Fatalf("error=%v uncertain=%v attempts=%d", err, DeliveryUncertain(err), attempts.Load())
				}
			})
		}
		t.Run(kind+"/preflight", func(t *testing.T) {
			client := localClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("preflight failure reached network") })
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := client.SendFile(ctx, 1, 0, kind, path, "image.png", "")
			if err == nil || DeliveryUncertain(err) {
				t.Fatalf("canceled upload = %v", err)
			}
			_, err = client.SendFile(context.Background(), 1, 0, kind, path+".missing", "image.png", "")
			if err == nil || DeliveryUncertain(err) {
				t.Fatalf("missing upload = %v", err)
			}
		})
	}
}

func TestSendFileRateLimitThenMissingFileIsDefinite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("payload"), 0600); err != nil {
		t.Fatal(err)
	}
	var attempts atomic.Int32
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		if err := os.Remove(path); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":0}}`)
	})
	_, err := client.SendFile(context.Background(), 1, 0, "document", path, "file", "")
	if err == nil || DeliveryUncertain(err) || attempts.Load() != 1 {
		t.Fatalf("error=%v uncertain=%v attempts=%d", err, DeliveryUncertain(err), attempts.Load())
	}
}
