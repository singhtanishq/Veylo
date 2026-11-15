package tests

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// doMultipart posts a multipart upload as the user and returns the response.
func doMultipart(t *testing.T, u *registeredUser, fileName string, content []byte) (*http.Response, []byte) {
	t.Helper()
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	fw, _ := form.CreateFormFile("file", fileName)
	_, _ = fw.Write(content)
	_ = form.Close()

	req, _ := http.NewRequest("POST", baseURL+"/v1/attachments", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+form.Boundary())
	req.Header.Set("Origin", "http://localhost:3000")
	for _, cookie := range u.C.t.Jar.Cookies(mustURL(t, baseURL+"/v1/attachments")) {
		req.AddCookie(cookie)
	}
	resp, err := u.C.t.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

// uploadPNG creates and uploads a small PNG as a user.
func uploadPNG(t *testing.T, u *registeredUser, width, height int) map[string]any {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 0xc0, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}

	resp, data := doMultipart(t, u, "test-image.png", buf.Bytes())
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload: status %d: %s", resp.StatusCode, data)
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	return env.Data
}

func TestAttachmentsUploadDownloadAuthorization(t *testing.T) {
	resetDB(t)
	alice := registerNewUser(t, "alice")
	bob := registerNewUser(t, "bob")
	charlie := registerNewUser(t, "charlie")
	convID := directConv(t, alice, bob)

	att := uploadPNG(t, alice, 64, 48)
	attID := att["id"].(string)
	if att["mime_type"] != "image/png" || att["kind"] != "image" {
		t.Fatalf("wrong attachment metadata: %v", att)
	}

	// Send message carrying the attachment.
	sendMessage(t, alice, convID, "", map[string]any{"type": "image", "attachment_ids": []string{attID}})

	// Owner + member can download.
	if resp, _ := alice.C.do(t, "GET", "/v1/attachments/"+attID+"/download", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("owner download failed")
	}
	if resp, _ := bob.C.do(t, "GET", "/v1/attachments/"+attID+"/download", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("member download failed")
	}

	// Charlie (non-member) is forbidden.
	if resp, _ := charlie.C.do(t, "GET", "/v1/attachments/"+attID+"/download", nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-member download must be forbidden")
	}

	// Unauthenticated download forbidden.
	anon := newClient()
	if resp, _ := anon.do(t, "GET", "/v1/attachments/"+attID+"/download", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous download must be unauthorized")
	}

	// Thumbnail works for members.
	if resp, _ := bob.C.do(t, "GET", "/v1/attachments/"+attID+"/thumbnail", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("thumbnail failed")
	}
}

func TestAttachmentsValidation(t *testing.T) {
	resetDB(t)
	alice := registerNewUser(t, "alice")
	bob := registerNewUser(t, "bob")

	// Disguised upload: text content named .png is rejected by content sniffing.
	resp, data := doMultipart(t, alice, "evil.png", []byte("<script>alert(1)</script>"))
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("text upload should be rejected, got %d: %s", resp.StatusCode, data)
	}

	convID := directConv(t, alice, bob)

	// Attachment reuse across messages rejected.
	att := uploadPNG(t, alice, 32, 32)
	attID := att["id"].(string)
	sendMessage(t, alice, convID, "", map[string]any{"type": "image", "attachment_ids": []string{attID}})
	alice.C.mustJSON(t, "POST", "/v1/conversations/"+convID+"/messages", map[string]any{
		"type": "image", "text": "", "attachment_ids": []string{attID},
	}, http.StatusUnprocessableEntity)

	// Attachment not owned by sender rejected.
	att2 := uploadPNG(t, bob, 24, 24)
	alice.C.mustJSON(t, "POST", "/v1/conversations/"+convID+"/messages", map[string]any{
		"type": "image", "text": "", "attachment_ids": []string{att2["id"].(string)},
	}, http.StatusUnprocessableEntity)
}

func TestAttachmentStorageSafety(t *testing.T) {
	resetDB(t)
	alice := registerNewUser(t, "alice")
	att := uploadPNG(t, alice, 16, 16)
	attID := att["id"].(string)

	row := pool.QueryRow(testCtx(), `SELECT storage_key FROM attachments WHERE id = $1::uuid`, attID)
	var key string
	if err := row.Scan(&key); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(key, "..") || strings.HasPrefix(key, "/") {
		t.Fatalf("unsafe storage key: %s", key)
	}
	// Filename in the key derives from the sanitized original name.
	if !strings.HasSuffix(key, ".png") {
		t.Fatalf("expected .png suffix in key: %s", key)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

var _ = os.Getenv
var _ = filepath.Join
