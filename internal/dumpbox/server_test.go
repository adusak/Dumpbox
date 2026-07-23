package dumpbox

import (
	"bytes"
	"html/template"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProtectedPageRequiresAuthentication(t *testing.T) {
	app := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "https://dumpbox.example/", nil)
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
	}
	if location := response.Header().Get("Location"); location != "/login" {
		t.Fatalf("location = %q, want /login", location)
	}
}

func TestTamperedSessionIsRejected(t *testing.T) {
	app := testServer(t)
	value := authenticatedCookie(t, app)
	value = value[:len(value)-1] + "x"
	request := httptest.NewRequest(http.MethodGet, "https://dumpbox.example/", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: value})
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
	}
}

func TestUploadStreamsToUserDirectory(t *testing.T) {
	app := testServer(t)
	content := bytes.Repeat([]byte("large enough to copy in chunks\n"), 20_000)
	response := upload(t, app, "../report.txt", content)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	directory := filepath.Join(app.dataDir, userDirectory(session{Subject: "subject-123", Name: "alice"}))
	stored, err := os.ReadFile(filepath.Join(directory, "report.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, content) {
		t.Fatal("stored file differs from uploaded content")
	}
	info, err := os.Stat(filepath.Join(directory, "report.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestUploadDoesNotOverwriteExistingFile(t *testing.T) {
	app := testServer(t)
	first := upload(t, app, "notes.txt", []byte("first"))
	second := upload(t, app, "notes.txt", []byte("second"))
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("statuses = %d, %d", first.Code, second.Code)
	}
	directory := filepath.Join(app.dataDir, userDirectory(session{Subject: "subject-123", Name: "alice"}))
	assertFileContent(t, filepath.Join(directory, "notes.txt"), "first")
	assertFileContent(t, filepath.Join(directory, "notes (1).txt"), "second")
}

func TestConcurrentUploadsDoNotOverwrite(t *testing.T) {
	app := testServer(t)
	var wait sync.WaitGroup
	responses := make(chan *httptest.ResponseRecorder, 2)
	for _, content := range []string{"first", "second"} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			responses <- upload(t, app, "shared.txt", []byte(content))
		}()
	}
	wait.Wait()
	close(responses)
	for response := range responses {
		if response.Code != http.StatusCreated {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
		}
	}
	directory := filepath.Join(app.dataDir, userDirectory(session{Subject: "subject-123", Name: "alice"}))
	contents := map[string]bool{}
	for _, name := range []string{"shared.txt", "shared (1).txt"} {
		content, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		contents[string(content)] = true
	}
	if !contents["first"] || !contents["second"] {
		t.Fatalf("stored contents = %v, want both uploads", contents)
	}
}

func TestUploadRejectsCrossOriginRequest(t *testing.T) {
	app := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "https://dumpbox.example/upload", strings.NewReader("ignored"))
	request.Header.Set("Origin", "https://attacker.example")
	request.Header.Set("X-Dumpbox-Upload", "1")
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: authenticatedCookie(t, app)})
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestSignerRejectsModifiedPayload(t *testing.T) {
	s := signer{key: bytes.Repeat([]byte{7}, 32)}
	value, err := s.sign(session{Subject: "alice", Name: "Alice", Expires: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(value, ".")
	parts[0] = strings.TrimSuffix(parts[0], "A") + "B"
	var result session
	if err := s.verify(strings.Join(parts, "."), &result); err == nil {
		t.Fatal("modified token was accepted")
	}
}

func testServer(t *testing.T) *Server {
	t.Helper()
	baseURL, err := url.Parse("https://dumpbox.example")
	if err != nil {
		t.Fatal(err)
	}
	page, err := template.New("index").Parse(indexHTML)
	if err != nil {
		t.Fatal(err)
	}
	return &Server{
		baseURL:      baseURL,
		dataDir:      t.TempDir(),
		signer:       signer{key: bytes.Repeat([]byte{42}, 32)},
		secureCookie: true,
		page:         page,
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		now:          time.Now,
	}
}

func authenticatedCookie(t testing.TB, app *Server) string {
	t.Helper()
	value, err := app.signer.sign(session{
		Subject: "subject-123",
		Name:    "alice",
		Expires: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func upload(t testing.TB, app *Server, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "https://dumpbox.example/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Origin", "https://dumpbox.example")
	request.Header.Set("X-Dumpbox-Upload", "1")
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: authenticatedCookie(t, app)})
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, request)
	return response
}

func assertFileContent(t *testing.T, path, expected string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != expected {
		t.Fatalf("content = %q, want %q", content, expected)
	}
}
