package dumpbox

import (
	"bytes"
	"errors"
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

func TestLandingPageForUnauthenticatedUser(t *testing.T) {
	app := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "https://dumpbox.example/", nil)
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); !strings.Contains(body, `href="login"`) {
		t.Fatalf("landing page does not contain login link: %s", body)
	}
}

func TestAuthenticatedUserSeesUploadPage(t *testing.T) {
	app := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "https://dumpbox.example/", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: authenticatedCookie(t, app)})
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); !strings.Contains(body, `id="drop"`) {
		t.Fatalf("authenticated page does not contain upload area: %s", body)
	}
}

func TestTamperedSessionIsRejected(t *testing.T) {
	app := testServer(t)
	value := authenticatedCookie(t, app)
	parts := strings.Split(value, ".")
	parts[1] = "x" + parts[1][1:]
	value = strings.Join(parts, ".")
	request := httptest.NewRequest(http.MethodGet, "https://dumpbox.example/", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: value})
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); !strings.Contains(body, `href="login"`) {
		t.Fatalf("invalid session did not show landing page: %s", body)
	}
}

func TestLogoutReturnsToLandingPage(t *testing.T) {
	app := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "https://dumpbox.example/logout", nil)
	request.Header.Set("Origin", "https://dumpbox.example")
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: authenticatedCookie(t, app)})
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusSeeOther)
	}
	if location := response.Header().Get("Location"); location != "/" {
		t.Fatalf("location = %q, want /", location)
	}
}

func TestLogoutAcceptsEquivalentDefaultPortOrigin(t *testing.T) {
	app := testServer(t)
	app.baseURL.Host = "dumpbox.example:443"
	request := httptest.NewRequest(http.MethodPost, "https://dumpbox.example/logout", nil)
	request.Header.Set("Origin", "https://dumpbox.example")
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: authenticatedCookie(t, app)})
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusSeeOther)
	}
}

func TestLogoutAcceptsOpaqueSameOriginRequest(t *testing.T) {
	app := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "https://dumpbox.example/logout", nil)
	request.Header.Set("Origin", "null")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: authenticatedCookie(t, app)})
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusSeeOther)
	}
}

func TestLogoutRejectsOpaqueCrossSiteRequest(t *testing.T) {
	app := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "https://dumpbox.example/logout", nil)
	request.Header.Set("Origin", "null")
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: authenticatedCookie(t, app)})
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestUploadStreamsToUserDirectory(t *testing.T) {
	app := testServer(t)
	content := bytes.Repeat([]byte("large enough to copy in chunks\n"), 20_000)
	response := upload(t, app, "../report.txt", content)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	directory := filepath.Join(app.dataDir, userDirectory(session{Subject: "subject-123", Username: "alice"}))
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

func TestMetricsReportPerUserUploads(t *testing.T) {
	app := testServer(t)
	if response := upload(t, app, "first.txt", []byte("12345")); response.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body = %s", response.Code, response.Body.String())
	}
	if response := upload(t, app, "second.txt", []byte("1234567")); response.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body = %s", response.Code, response.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "https://dumpbox.example/metrics", nil)
	response := httptest.NewRecorder()
	app.MetricsHandler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want %d", response.Code, http.StatusOK)
	}
	user := metricUserID(session{Subject: "subject-123"})
	body := response.Body.String()
	for _, metric := range []string{
		`dumpbox_uploaded_files_total{user="` + user + `"} 2`,
		`dumpbox_uploaded_bytes_total{user="` + user + `"} 12`,
		`dumpbox_upload_requests_total{code="201",user="` + user + `"} 2`,
		"dumpbox_upload_duration_seconds_count 2",
		"dumpbox_active_uploads 0",
	} {
		if !strings.Contains(body, metric) {
			t.Errorf("metrics response does not contain %q", metric)
		}
	}
}

func TestMetricsAreNotExposedOnApplicationHandler(t *testing.T) {
	app := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "https://dumpbox.example/metrics", nil)
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestUploadDoesNotOverwriteExistingFile(t *testing.T) {
	app := testServer(t)
	first := upload(t, app, "notes.txt", []byte("first"))
	second := upload(t, app, "notes.txt", []byte("second"))
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("statuses = %d, %d", first.Code, second.Code)
	}
	directory := filepath.Join(app.dataDir, userDirectory(session{Subject: "subject-123", Username: "alice"}))
	assertFileContent(t, filepath.Join(directory, "notes.txt"), "first")
	assertFileContent(t, filepath.Join(directory, "notes (1).txt"), "second")
}

func TestUploadRejectsCumulativeUserQuota(t *testing.T) {
	app := testServer(t)
	quota, err := newStorageQuota(app.dataDir, 10)
	if err != nil {
		t.Fatal(err)
	}
	app.storageQuota = quota

	first := upload(t, app, "first.txt", []byte("123456"))
	second := upload(t, app, "second.txt", []byte("78901"))
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, body = %s", first.Code, first.Body.String())
	}
	if second.Code != http.StatusInsufficientStorage {
		t.Fatalf("second status = %d, body = %s", second.Code, second.Body.String())
	}
	directory := filepath.Join(app.dataDir, userDirectory(session{Subject: "subject-123", Username: "alice"}))
	if _, err := os.Stat(filepath.Join(directory, "second.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second file exists or stat failed unexpectedly: %v", err)
	}
}

func TestStorageQuotaRebuildsExistingUsage(t *testing.T) {
	dataDir := t.TempDir()
	user := session{Subject: "subject-123", Username: "alice"}
	directory := filepath.Join(dataDir, userDirectory(user))
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "existing.txt"), []byte("123456"), 0o600); err != nil {
		t.Fatal(err)
	}
	quota, err := newStorageQuota(dataDir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if quota.reserve(user.Subject, 5) {
		t.Fatal("quota accepted bytes beyond the rebuilt usage")
	}
	if !quota.reserve(user.Subject, 4) {
		t.Fatal("quota rejected bytes at the exact limit")
	}
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
	directory := filepath.Join(app.dataDir, userDirectory(session{Subject: "subject-123", Username: "alice"}))
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

func TestStorePartCleansUpAfterPanic(t *testing.T) {
	directory := t.TempDir()
	body := "--boundary\r\n" +
		"Content-Disposition: form-data; name=\"file\"; filename=\"report.txt\"\r\n" +
		"Content-Type: text/plain\r\n\r\npartial"
	reader := multipart.NewReader(&panicReader{data: []byte(body)}, "boundary")
	part, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("storePart did not panic")
			}
		}()
		_, _, _ = storePart(directory, part, defaultMaxFileBytes, nil, nil)
	}()

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("directory entries = %d, want no partial file", len(entries))
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

func TestUserDirectoryIncludesSanitizedUsername(t *testing.T) {
	hash := sha256Sum("subject-123")[:24]
	tests := []struct {
		name     string
		username string
		want     string
	}{
		{name: "preferred username", username: "alice", want: "user-alice-" + hash},
		{name: "unsafe characters", username: "../../Alice Smith/😈", want: "user-Alice_Smith-" + hash},
		{name: "missing username", want: "user-" + hash},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := userDirectory(session{Subject: "subject-123", Username: test.username})
			if got != test.want {
				t.Fatalf("userDirectory() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSafeFilenameReplacesMisleadingCharacters(t *testing.T) {
	tests := map[string]string{
		"photo\u202egnp.exe": "photo_gnp.exe",
		"\u200ereport.pdf":   "_report.pdf",
		"-option":            "_option",
		"normal.txt":         "normal.txt",
	}
	for input, want := range tests {
		if got := safeFilename(input); got != want {
			t.Errorf("safeFilename(%q) = %q, want %q", input, got, want)
		}
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

func TestBrandAssetsAreServed(t *testing.T) {
	app := testServer(t)
	for _, path := range []string{"/favicon.svg", "/assets/logo.svg"} {
		request := httptest.NewRequest(http.MethodGet, "https://dumpbox.example"+path, nil)
		response := httptest.NewRecorder()

		app.Handler().ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, response.Code, http.StatusOK)
		}
		if contentType := response.Header().Get("Content-Type"); contentType != "image/svg+xml" {
			t.Fatalf("%s content-type = %q, want image/svg+xml", path, contentType)
		}
		if body := response.Body.String(); !strings.Contains(body, "<svg") {
			t.Fatalf("%s did not return SVG markup", path)
		}
	}
}

func TestApplicationAssetsAreServed(t *testing.T) {
	app := testServer(t)
	for path, contentType := range map[string]string{
		"/assets/app.css": "text/css; charset=utf-8",
		"/assets/app.js":  "text/javascript; charset=utf-8",
	} {
		request := httptest.NewRequest(http.MethodGet, "https://dumpbox.example"+path, nil)
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, response.Code, http.StatusOK)
		}
		if got := response.Header().Get("Content-Type"); got != contentType {
			t.Fatalf("%s content-type = %q, want %q", path, got, contentType)
		}
	}
}

func TestCSPDoesNotAllowInlineCode(t *testing.T) {
	app := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "https://dumpbox.example/", nil)
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, request)
	if csp := response.Header().Get("Content-Security-Policy"); strings.Contains(csp, "'unsafe-inline'") {
		t.Fatalf("Content-Security-Policy allows inline code: %s", csp)
	}
}

func TestLandingPageReferencesBrandAssets(t *testing.T) {
	app := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "https://dumpbox.example/", nil)
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	body := response.Body.String()
	if !strings.Contains(body, `href="/favicon.svg"`) {
		t.Fatalf("landing page does not link the favicon: %s", body)
	}
	if !strings.Contains(body, `src="/assets/logo.svg"`) {
		t.Fatalf("landing page does not use the logo: %s", body)
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
	landingPage, err := template.New("landing").Parse(landingHTML)
	if err != nil {
		t.Fatal(err)
	}
	return &Server{
		baseURL: baseURL,
		dataDir: t.TempDir(),
		limits: uploadLimits{
			requestBytes:    defaultMaxRequestBytes,
			fileBytes:       defaultMaxFileBytes,
			filesPerRequest: defaultMaxFilesPerRequest,
		},
		uploadSlots:  newUploadSlots(defaultUploadsPerSubject, defaultUploadsTotal),
		signer:       signer{key: bytes.Repeat([]byte{42}, 32)},
		secureCookie: true,
		page:         page,
		landingPage:  landingPage,
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		metrics:      newMetrics(),
		now:          time.Now,
	}
}

func authenticatedCookie(t testing.TB, app *Server) string {
	t.Helper()
	value, err := app.signer.sign(session{
		Subject:  "subject-123",
		Name:     "alice",
		Username: "alice",
		Expires:  time.Now().Add(time.Hour).Unix(),
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

type panicReader struct {
	data []byte
}

func (r *panicReader) Read(buffer []byte) (int, error) {
	if len(r.data) == 0 {
		panic("incomplete transfer")
	}
	n := copy(buffer, r.data)
	r.data = r.data[n:]
	return n, nil
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

func TestUploadRejectsFileLargerThanLimit(t *testing.T) {
	app := testServer(t)
	app.limits.fileBytes = 8

	response := upload(t, app, "big.bin", bytes.Repeat([]byte("a"), 64))

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
	directory := filepath.Join(app.dataDir, userDirectory(session{Subject: "subject-123", Username: "alice"}))
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("directory entries = %d, want no stored or partial file", len(entries))
	}
}

func TestUploadRejectsRequestLargerThanLimit(t *testing.T) {
	app := testServer(t)
	app.limits.requestBytes = 16

	response := upload(t, app, "big.bin", bytes.Repeat([]byte("a"), 1024))

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestUploadRejectsTooManyConcurrentUploads(t *testing.T) {
	app := testServer(t)
	app.uploadSlots = newUploadSlots(1, 1)
	if !app.uploadSlots.acquire("subject-123") {
		t.Fatal("could not acquire the first upload slot")
	}

	response := upload(t, app, "notes.txt", []byte("blocked"))

	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusTooManyRequests)
	}
	app.uploadSlots.release("subject-123")
	if response := upload(t, app, "notes.txt", []byte("allowed")); response.Code != http.StatusCreated {
		t.Fatalf("status after release = %d, want %d", response.Code, http.StatusCreated)
	}
}

func TestHSTSIsSentForHTTPSBaseURL(t *testing.T) {
	app := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "https://dumpbox.example/", nil)
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if header := response.Header().Get("Strict-Transport-Security"); header != "max-age=31536000" {
		t.Fatalf("Strict-Transport-Security = %q", header)
	}
}

func TestHSTSIsOmittedForHTTPBaseURL(t *testing.T) {
	app := testServer(t)
	baseURL, err := url.Parse("http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	app.baseURL = baseURL
	request := httptest.NewRequest(http.MethodGet, "http://localhost:8080/", nil)
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if header := response.Header().Get("Strict-Transport-Security"); header != "" {
		t.Fatalf("Strict-Transport-Security = %q, want empty", header)
	}
}

func TestValidateIssuer(t *testing.T) {
	cases := []struct {
		issuer        string
		allowInsecure bool
		valid         bool
	}{
		{issuer: "https://identity.example", valid: true},
		{issuer: "https://identity.example/realms/main", valid: true},
		{issuer: "http://identity.example", valid: false},
		{issuer: "http://identity.example", allowInsecure: true, valid: false},
		{issuer: "http://localhost:8080", allowInsecure: true, valid: true},
		{issuer: "http://127.0.0.1:8080", allowInsecure: true, valid: true},
		{issuer: "http://localhost:8080", valid: false},
		{issuer: "https://" + "user:token" + "@identity.example", valid: false},
		{issuer: "https://identity.example?x=1", valid: false},
		{issuer: "https://identity.example#fragment", valid: false},
		{issuer: "identity.example", valid: false},
		{issuer: "ftp://identity.example", valid: false},
	}
	for _, test := range cases {
		err := validateIssuer(test.issuer, test.allowInsecure)
		if (err == nil) != test.valid {
			t.Errorf("validateIssuer(%q, %t) error = %v, want valid = %t", test.issuer, test.allowInsecure, err, test.valid)
		}
	}
}

func TestEnvOptionalBytes(t *testing.T) {
	t.Setenv("MAX_BYTES_PER_USER", "0")
	if got, err := envOptionalBytes("MAX_BYTES_PER_USER", 10); err != nil || got != 0 {
		t.Fatalf("envOptionalBytes() = %d, %v; want 0, nil", got, err)
	}
	t.Setenv("MAX_BYTES_PER_USER", "-1")
	if _, err := envOptionalBytes("MAX_BYTES_PER_USER", 10); err == nil {
		t.Fatal("envOptionalBytes() accepted a negative value")
	}
}
