package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/selfupdate"
)

// matchesMagic checks whether the file at path looks like a platform binary
// by inspecting magic bytes (ELF for linux, MZ for windows).
func matchesMagic(path, platform string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	buf := make([]byte, 4)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false, err
	}
	if n >= 4 && buf[0] == 0x7f && buf[1] == 'E' && buf[2] == 'L' && buf[3] == 'F' {
		return strings.Contains(platform, "linux"), nil
	}
	if n >= 2 && buf[0] == 'M' && buf[1] == 'Z' {
		return strings.Contains(platform, "windows"), nil
	}
	return false, nil
}

type testReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest,omitempty"`
}

type testReleasePayload struct {
	TagName string             `json:"tag_name"`
	Assets  []testReleaseAsset `json:"assets"`
}

const testReleaseAPIPath = "/api.github.com/repos/sipeed/kuromatsu/releases/latest"

// TestDownloadAndExtractRelease_IntegrationLatestRelease downloads the latest
// public release for a single platform as an opt-in smoke test.
func TestDownloadAndExtractRelease_IntegrationLatestRelease(t *testing.T) {
	if os.Getenv("KUROMATSU_INTEGRATION_TESTS") == "" {
		t.Skip("skipping integration test (set KUROMATSU_INTEGRATION_TESTS=1 to enable)")
	}
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	const platform = "linux"
	const arch = "amd64"
	apiURL := GetProdReleaseAPIURL()
	assetName := ExpectedAssetName(platform, arch, "", false)
	assetURL, checksum, err := findAssetInfo(apiURL, assetName)
	if err != nil {
		t.Fatalf("findAssetInfo failed for %s/%s: %v", platform, arch, err)
	}
	t.Logf("asset URL: %s checksum: %s", assetURL, checksum)

	dir, err := DownloadAndExtractRelease(apiURL, assetName)
	if err != nil {
		t.Fatalf("DownloadAndExtractRelease failed for %s/%s: %v", platform, arch, err)
	}
	defer os.RemoveAll(dir)

	var found bool
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() < 64 {
			return nil
		}
		ok, err := matchesMagic(path, platform)
		if err != nil {
			return err
		}
		if ok {
			found = true
			t.Logf("found artifact: %s (size=%d)", path, info.Size())
		}
		return nil
	})
	if !found {
		t.Fatalf("no binary-like artifact found for %s/%s", platform, arch)
	}
}

func TestFindAssetInfo_SelectsPreferredAsset(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case testReleaseAPIPath:
			writeReleasePayload(w, testReleasePayload{
				TagName: "v0.2.6",
				Assets: []testReleaseAsset{
					{
						Name:               "kuromatsu_Linux_x86_64.zip",
						BrowserDownloadURL: server.URL + "/assets/kuromatsu_Linux_x86_64.zip",
						Digest:             "sha256:" + strings.Repeat("1", 64),
					},
					{
						Name:               "kuromatsu_Linux_x86_64.tar.gz",
						BrowserDownloadURL: server.URL + "/assets/kuromatsu_Linux_x86_64.tar.gz",
						Digest:             "sha256:" + strings.Repeat("2", 64),
					},
					{
						Name:               "kuromatsu_Windows_x86_64.zip",
						BrowserDownloadURL: server.URL + "/assets/kuromatsu_Windows_x86_64.zip",
						Digest:             "sha256:" + strings.Repeat("3", 64),
					},
					{
						Name:               "kuromatsu_Windows_arm64.zip",
						BrowserDownloadURL: server.URL + "/assets/kuromatsu_Windows_arm64.zip",
						Digest:             "sha256:" + strings.Repeat("4", 64),
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	withTestHTTPClient(t, server.Client())

	tests := []struct {
		name         string
		platform     string
		arch         string
		wantURL      string
		wantChecksum string
	}{
		{
			name:         "linux takes the exact tar.gz name, not the zip",
			platform:     "linux",
			arch:         "amd64",
			wantURL:      server.URL + "/assets/kuromatsu_Linux_x86_64.tar.gz",
			wantChecksum: strings.Repeat("2", 64),
		},
		{
			name:         "windows amd64 matches x86_64 zip",
			platform:     "windows",
			arch:         "amd64",
			wantURL:      server.URL + "/assets/kuromatsu_Windows_x86_64.zip",
			wantChecksum: strings.Repeat("3", 64),
		},
		{
			name:         "windows arm64 matches arm64 zip",
			platform:     "windows",
			arch:         "arm64",
			wantURL:      server.URL + "/assets/kuromatsu_Windows_arm64.zip",
			wantChecksum: strings.Repeat("4", 64),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assetName := ExpectedAssetName(tc.platform, tc.arch, "", false)
			gotURL, gotChecksum, err := findAssetInfo(server.URL+testReleaseAPIPath, assetName)
			if err != nil {
				t.Fatalf("findAssetInfo(%q, %q) error: %v", server.URL+testReleaseAPIPath, assetName, err)
			}
			if gotURL != tc.wantURL {
				t.Fatalf("assetURL = %q, want %q", gotURL, tc.wantURL)
			}
			if gotChecksum != tc.wantChecksum {
				t.Fatalf("checksum = %q, want %q", gotChecksum, tc.wantChecksum)
			}
		})
	}
}

func TestFindAssetInfo_UsesChecksumAssetWhenDigestMissing(t *testing.T) {
	const checksum = "77b564f36da6d1e02169d0ecc837728eecb9ef983c317d9186ac9651798b924c"

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case testReleaseAPIPath:
			writeReleasePayload(w, testReleasePayload{
				TagName: "v0.2.6",
				Assets: []testReleaseAsset{
					{
						Name:               "kuromatsu_Windows_x86_64.zip",
						BrowserDownloadURL: server.URL + "/assets/kuromatsu_Windows_x86_64.zip",
					},
					{
						Name:               "checksums.txt",
						BrowserDownloadURL: server.URL + "/assets/checksums.txt",
					},
				},
			})
		case "/assets/checksums.txt":
			_, _ = io.WriteString(w, checksum+"  kuromatsu_Windows_x86_64.zip\n")
		case "/assets/kuromatsu_Windows_x86_64.zip":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	withTestHTTPClient(t, server.Client())

	gotURL, gotChecksum, err := findAssetInfo(
		server.URL+testReleaseAPIPath,
		ExpectedAssetName("windows", "amd64", "", false),
	)
	if err != nil {
		t.Fatalf("findAssetInfo returned error: %v", err)
	}
	if gotURL != server.URL+"/assets/kuromatsu_Windows_x86_64.zip" {
		t.Fatalf("assetURL = %q, want %q", gotURL, server.URL+"/assets/kuromatsu_Windows_x86_64.zip")
	}
	if gotChecksum != checksum {
		t.Fatalf("checksum = %q, want %q", gotChecksum, checksum)
	}
}

func TestDownloadAndExtractRelease_ExtractsTarGz(t *testing.T) {
	tarGzContent := buildTestTarGz(t, map[string]string{
		"kuromatsu_Linux_x86_64/kuromatsu": "test linux binary payload",
	})
	sum := sha256.Sum256(tarGzContent)
	checksum := hex.EncodeToString(sum[:])

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case testReleaseAPIPath:
			writeReleasePayload(w, testReleasePayload{
				TagName: "v0.2.6",
				Assets: []testReleaseAsset{
					{
						Name:               "kuromatsu_Linux_x86_64.tar.gz",
						BrowserDownloadURL: server.URL + "/assets/kuromatsu_Linux_x86_64.tar.gz",
						Digest:             "sha256:" + checksum,
					},
				},
			})
		case "/assets/kuromatsu_Linux_x86_64.tar.gz":
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarGzContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	withTestHTTPClient(t, server.Client())

	dir, err := DownloadAndExtractRelease(server.URL+testReleaseAPIPath, "kuromatsu_Linux_x86_64.tar.gz")
	if err != nil {
		t.Fatalf("DownloadAndExtractRelease returned error: %v", err)
	}
	defer os.RemoveAll(dir)

	binPath, err := findBinaryInDir(dir, "kuromatsu")
	if err != nil {
		t.Fatalf("findBinaryInDir returned error: %v", err)
	}

	bs, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("ReadFile extracted asset: %v", err)
	}
	if got := string(bs); got != "test linux binary payload" {
		t.Fatalf("extracted content = %q, want %q", got, "test linux binary payload")
	}
}

func TestDownloadAndExtractRelease_RetriesTransientAssetFailure(t *testing.T) {
	zipContent := buildTestZip(t, map[string]string{
		"kuromatsu.exe": "test windows binary payload",
	})
	sum := sha256.Sum256(zipContent)
	checksum := hex.EncodeToString(sum[:])

	var assetAttempts int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api.github.com/repos/sipeed/kuromatsu/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(
				w,
				`{"tag_name":"v0.2.6","assets":[{"name":"kuromatsu_Windows_x86_64.zip","browser_download_url":%q,"digest":"sha256:%s"}]}`,
				server.URL+"/assets/kuromatsu_Windows_x86_64.zip",
				checksum,
			)
		case "/assets/kuromatsu_Windows_x86_64.zip":
			assetAttempts++
			if assetAttempts == 1 {
				w.WriteHeader(http.StatusGatewayTimeout)
				return
			}
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(zipContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	withTestHTTPClient(t, server.Client())

	dir, err := DownloadAndExtractRelease(
		server.URL+"/api.github.com/repos/sipeed/kuromatsu/releases/latest",
		ExpectedAssetName("windows", "amd64", "", false),
	)
	if err != nil {
		t.Fatalf("DownloadAndExtractRelease returned error: %v", err)
	}
	defer os.RemoveAll(dir)

	if assetAttempts != 2 {
		t.Fatalf("asset attempts = %d, want 2", assetAttempts)
	}

	bs, err := os.ReadFile(filepath.Join(dir, "kuromatsu.exe"))
	if err != nil {
		t.Fatalf("ReadFile extracted asset: %v", err)
	}
	if got := string(bs); got != "test windows binary payload" {
		t.Fatalf("extracted content = %q, want %q", got, "test windows binary payload")
	}
}

func buildTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("Create zip entry %q: %v", name, err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatalf("Write zip entry %q: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close zip writer: %v", err)
	}
	return buf.Bytes()
}

func buildTestTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o755,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("Write tar header %q: %v", name, err)
		}
		if _, err := io.WriteString(tw, content); err != nil {
			t.Fatalf("Write tar entry %q: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close tar writer: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("Close gzip writer: %v", err)
	}
	return buf.Bytes()
}

func writeReleasePayload(w http.ResponseWriter, payload testReleasePayload) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func withTestHTTPClient(t *testing.T, client *http.Client) {
	t.Helper()

	origClient := httpClient
	httpClient = client
	httpClient.Timeout = 5 * time.Second
	t.Cleanup(func() {
		httpClient = origClient
	})
}

func TestExpectedAssetName(t *testing.T) {
	tests := []struct {
		goos, goarch, goarm string
		native              bool
		want                string
	}{
		{"linux", "amd64", "", false, "kuromatsu_Linux_x86_64.tar.gz"},
		{"linux", "arm", "6", false, "kuromatsu_Linux_armv6.tar.gz"},
		{"linux", "arm", "7", false, "kuromatsu_Linux_armv7.tar.gz"},
		{"linux", "arm", "", false, "kuromatsu_Linux_armv7.tar.gz"},
		{"linux", "arm64", "", false, "kuromatsu_Linux_arm64.tar.gz"},
		{"linux", "386", "", false, "kuromatsu_Linux_i386.tar.gz"},
		{"android", "arm64", "", false, "kuromatsu_Android_arm64.tar.gz"},
		{"windows", "amd64", "", false, "kuromatsu_Windows_x86_64.zip"},
		{"darwin", "arm64", "", false, "kuromatsu_Darwin_arm64.tar.gz"},
		{"freebsd", "amd64", "", false, "kuromatsu_Freebsd_x86_64.tar.gz"},
		{"netbsd", "arm64", "", false, "kuromatsu_Netbsd_arm64.tar.gz"},
		{"linux", "amd64", "", true, "kuromatsu-native_Linux_x86_64.tar.gz"},
	}
	for _, tc := range tests {
		got := ExpectedAssetName(tc.goos, tc.goarch, tc.goarm, tc.native)
		if got != tc.want {
			t.Errorf("ExpectedAssetName(%q, %q, %q, %v) = %q, want %q",
				tc.goos, tc.goarch, tc.goarm, tc.native, got, tc.want)
		}
	}
}

// releaseServer serves a release whose assets are listed in names (in that
// order). Each asset has an API digest: the real sha256 when its body is in
// files, otherwise a distinct fake one. Every request is counted.
type releaseServer struct {
	*httptest.Server
	requests atomic.Int64
	digests  map[string]string
}

func newReleaseServer(t *testing.T, tag string, names []string, files map[string][]byte) *releaseServer {
	t.Helper()
	rs := &releaseServer{digests: map[string]string{}}
	for i, n := range names {
		if body, ok := files[n]; ok {
			sum := sha256.Sum256(body)
			rs.digests[n] = hex.EncodeToString(sum[:])
		} else {
			rs.digests[n] = fmt.Sprintf("%064x", i+1)
		}
	}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.requests.Add(1)
		if r.URL.Path == testReleaseAPIPath {
			payload := testReleasePayload{TagName: tag}
			for _, n := range names {
				payload.Assets = append(payload.Assets, testReleaseAsset{
					Name:               n,
					BrowserDownloadURL: rs.URL + "/assets/" + n,
					Digest:             "sha256:" + rs.digests[n],
				})
			}
			writeReleasePayload(w, payload)
			return
		}
		if body, ok := files[strings.TrimPrefix(r.URL.Path, "/assets/")]; ok {
			_, _ = w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(rs.Close)
	withTestHTTPClient(t, rs.Client())
	return rs
}

func (rs *releaseServer) apiURL() string { return rs.URL + testReleaseAPIPath }

var linuxX8664Orders = [][]string{
	{"kuromatsu-native_Linux_x86_64.tar.gz", "kuromatsu_Linux_x86_64.tar.gz"},
	{"kuromatsu_Linux_x86_64.tar.gz", "kuromatsu-native_Linux_x86_64.tar.gz"},
}

func TestFindAssetInfo_ExactNameIgnoresNativeForPureBuild(t *testing.T) {
	for _, names := range linuxX8664Orders {
		rs := newReleaseServer(t, "v1.0.0", names, nil)
		gotURL, gotSum, err := findAssetInfo(rs.apiURL(), ExpectedAssetName("linux", "amd64", "", false))
		if err != nil {
			t.Fatalf("order %v: findAssetInfo: %v", names, err)
		}
		if want := rs.URL + "/assets/kuromatsu_Linux_x86_64.tar.gz"; gotURL != want {
			t.Fatalf("order %v: assetURL = %q, want %q", names, gotURL, want)
		}
		if want := rs.digests["kuromatsu_Linux_x86_64.tar.gz"]; gotSum != want {
			t.Fatalf("order %v: checksum = %q, want %q", names, gotSum, want)
		}
	}
}

func TestFindAssetInfo_NativePicksNativeAsset(t *testing.T) {
	for _, names := range linuxX8664Orders {
		rs := newReleaseServer(t, "v1.0.0", names, nil)
		gotURL, gotSum, err := findAssetInfo(rs.apiURL(), ExpectedAssetName("linux", "amd64", "", true))
		if err != nil {
			t.Fatalf("order %v: findAssetInfo: %v", names, err)
		}
		if want := rs.URL + "/assets/kuromatsu-native_Linux_x86_64.tar.gz"; gotURL != want {
			t.Fatalf("order %v: assetURL = %q, want %q", names, gotURL, want)
		}
		if want := rs.digests["kuromatsu-native_Linux_x86_64.tar.gz"]; gotSum != want {
			t.Fatalf("order %v: checksum = %q, want %q", names, gotSum, want)
		}
	}
}

func TestFindAssetInfo_ArmDoesNotMatchArm64(t *testing.T) {
	names := []string{
		"kuromatsu_Linux_arm64.tar.gz",
		"kuromatsu_Linux_armv6.tar.gz",
		"kuromatsu_Linux_armv7.tar.gz",
	}
	rs := newReleaseServer(t, "v1.0.0", names, nil)
	gotURL, _, err := findAssetInfo(rs.apiURL(), ExpectedAssetName("linux", "arm", "7", false))
	if err != nil {
		t.Fatalf("findAssetInfo: %v", err)
	}
	if want := rs.URL + "/assets/kuromatsu_Linux_armv7.tar.gz"; gotURL != want {
		t.Fatalf("assetURL = %q, want %q", gotURL, want)
	}
}

func TestFindAssetInfo_MissingAssetListsAvailable(t *testing.T) {
	names := make([]string, 0, 25)
	for i := range 25 {
		names = append(names, fmt.Sprintf("other_%02d.tar.gz", i))
	}
	rs := newReleaseServer(t, "v1.0.0", names, nil)
	_, _, err := findAssetInfo(rs.apiURL(), "kuromatsu_Linux_x86_64.tar.gz")
	if err == nil {
		t.Fatal("findAssetInfo succeeded, want error for a missing asset")
	}
	msg := err.Error()
	if !strings.Contains(msg, "kuromatsu_Linux_x86_64.tar.gz") || !strings.Contains(msg, "other_19.tar.gz") {
		t.Fatalf("error %q should name the wanted asset and the available ones", msg)
	}
	if strings.Contains(msg, "other_20.tar.gz") {
		t.Fatalf("error %q lists more than 20 assets", msg)
	}
}

func TestFindHash_ExactFieldMatch(t *testing.T) {
	archiveHash := strings.Repeat("1", 64)
	sbomHash := strings.Repeat("2", 64)
	nativeHash := strings.Repeat("3", 64)

	// The SBOM line comes first so a substring match would pick it.
	checksums := sbomHash + "  kuromatsu_Linux_x86_64.tar.gz.sbom.json\n" +
		archiveHash + "  kuromatsu_Linux_x86_64.tar.gz\n"
	got, ok := findHashInChecksumContent([]byte(checksums), "kuromatsu_Linux_x86_64.tar.gz")
	if !ok || got != archiveHash {
		t.Fatalf("archive hash = %q, %v; want %q", got, ok, archiveHash)
	}
	got, ok = findHashInChecksumContent([]byte(checksums), "kuromatsu_Linux_x86_64.tar.gz.sbom.json")
	if !ok || got != sbomHash {
		t.Fatalf("sbom hash = %q, %v; want %q", got, ok, sbomHash)
	}

	// sha256sum binary mode ("*name") and upper-case hex.
	binaryMode := "ABCDEF" + strings.Repeat("0", 58) + " *kuromatsu_Linux_x86_64.tar.gz\n"
	got, ok = findHashInChecksumContent([]byte(binaryMode), "kuromatsu_Linux_x86_64.tar.gz")
	if want := "abcdef" + strings.Repeat("0", 58); !ok || got != want {
		t.Fatalf("binary-mode hash = %q, %v; want %q", got, ok, want)
	}

	nativeOnly := nativeHash + "  kuromatsu-native_Linux_x86_64.tar.gz\n"
	if got, ok := findHashInChecksumContent([]byte(nativeOnly), "kuromatsu_Linux_x86_64.tar.gz"); ok {
		t.Fatalf("single-line native checksum file matched the pure-Go asset: %q", got)
	}

	// End to end: with no API digest, the pure-Go asset takes its own line
	// from checksums.txt even when a native checksum file is listed first.
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case testReleaseAPIPath:
			writeReleasePayload(w, testReleasePayload{
				TagName: "v1.0.0",
				Assets: []testReleaseAsset{
					{Name: "kuromatsu-native_checksums.txt", BrowserDownloadURL: server.URL + "/assets/native"},
					{Name: "checksums.txt", BrowserDownloadURL: server.URL + "/assets/checksums"},
					{
						Name:               "kuromatsu_Linux_x86_64.tar.gz",
						BrowserDownloadURL: server.URL + "/assets/kuromatsu_Linux_x86_64.tar.gz",
					},
				},
			})
		case "/assets/native":
			_, _ = io.WriteString(w, nativeOnly)
		case "/assets/checksums":
			_, _ = io.WriteString(w, checksums)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	withTestHTTPClient(t, server.Client())

	_, gotSum, err := findAssetInfo(server.URL+testReleaseAPIPath, "kuromatsu_Linux_x86_64.tar.gz")
	if err != nil {
		t.Fatalf("findAssetInfo: %v", err)
	}
	if gotSum != archiveHash {
		t.Fatalf("checksum = %q, want %q", gotSum, archiveHash)
	}
}

// stubApply replaces applyUpdate for the test so the test binary itself is
// never replaced. The stub writes the new binary to target and records each
// call's content.
func stubApply(t *testing.T, target string) *[][]byte {
	t.Helper()
	var calls [][]byte
	orig := applyUpdate
	applyUpdate = func(r io.Reader, _ selfupdate.Options) error {
		bs, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		calls = append(calls, bs)
		return os.WriteFile(target, bs, 0o755)
	}
	t.Cleanup(func() { applyUpdate = orig })
	return &calls
}

func writeTempExecutable(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "kuromatsu-current")
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatalf("write temp executable: %v", err)
	}
	return p
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	bs, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(bs) != want {
		t.Fatalf("%s content = %q, want %q", path, bs, want)
	}
}

func TestUpdateSelf_NativeRefusesWithoutNativeAsset(t *testing.T) {
	pure := buildTestTarGz(t, map[string]string{"kuromatsu": "pure-go binary"})
	rs := newReleaseServer(t, "v1.2.3",
		[]string{"kuromatsu_Linux_x86_64.tar.gz"},
		map[string][]byte{"kuromatsu_Linux_x86_64.tar.gz": pure})
	exe := writeTempExecutable(t, "current native binary")
	calls := stubApply(t, exe)

	err := UpdateSelfFromRelease(rs.apiURL(), "linux", "amd64", "kuromatsu", UpdateOptions{Native: true})
	if err == nil {
		t.Fatal("UpdateSelfFromRelease succeeded, want refusal")
	}
	for _, want := range []string{"native (nativellm) build", "release v1.2.3", "kuromatsu-native_Linux_x86_64.tar.gz"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("applyUpdate called %d times, want 0", len(*calls))
	}
	assertFileContent(t, exe, "current native binary")
}

func TestUpdateSelf_NativeRejectsOtherPlatform(t *testing.T) {
	rs := newReleaseServer(t, "v1.0.0", []string{"kuromatsu_Windows_x86_64.zip"}, nil)
	exe := writeTempExecutable(t, "current native binary")
	calls := stubApply(t, exe)

	const want = "native build: updates only from kuromatsu-native_Linux_x86_64.tar.gz (linux/amd64)"
	for _, target := range [][2]string{{"windows", "amd64"}, {"linux", "arm64"}} {
		err := UpdateSelfFromRelease(rs.apiURL(), target[0], target[1], "kuromatsu", UpdateOptions{Native: true})
		if err == nil || err.Error() != want {
			t.Fatalf("%s/%s: error = %v, want %q", target[0], target[1], err, want)
		}
	}
	if n := rs.requests.Load(); n != 0 {
		t.Fatalf("server received %d requests, want 0", n)
	}
	if len(*calls) != 0 {
		t.Fatalf("applyUpdate called %d times, want 0", len(*calls))
	}
	assertFileContent(t, exe, "current native binary")
}

func TestUpdateSelf_NativeAppliesNativeAsset(t *testing.T) {
	native := buildTestTarGz(t, map[string]string{
		"LICENSE":   "MIT License text",
		"kuromatsu": "native binary payload",
	})
	pure := buildTestTarGz(t, map[string]string{"kuromatsu": "pure-go binary"})
	rs := newReleaseServer(t, "v1.0.0",
		[]string{"kuromatsu_Linux_x86_64.tar.gz", "kuromatsu-native_Linux_x86_64.tar.gz"},
		map[string][]byte{
			"kuromatsu_Linux_x86_64.tar.gz":        pure,
			"kuromatsu-native_Linux_x86_64.tar.gz": native,
		})
	exe := writeTempExecutable(t, "current native binary")
	calls := stubApply(t, exe)

	err := UpdateSelfFromRelease(rs.apiURL(), "linux", "amd64", "kuromatsu", UpdateOptions{Native: true})
	if err != nil {
		t.Fatalf("UpdateSelfFromRelease: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("applyUpdate called %d times, want 1", len(*calls))
	}
	if got := string((*calls)[0]); got != "native binary payload" {
		t.Fatalf("applied content = %q, want the native binary", got)
	}
	assertFileContent(t, exe, "native binary payload")
}

func TestUpdateSelf_PureBuildAppliesPureAsset(t *testing.T) {
	native := buildTestTarGz(t, map[string]string{"kuromatsu": "native binary payload"})
	pure := buildTestTarGz(t, map[string]string{"kuromatsu": "pure-go binary"})
	rs := newReleaseServer(t, "v1.0.0",
		[]string{"kuromatsu-native_Linux_x86_64.tar.gz", "kuromatsu_Linux_x86_64.tar.gz"},
		map[string][]byte{
			"kuromatsu_Linux_x86_64.tar.gz":        pure,
			"kuromatsu-native_Linux_x86_64.tar.gz": native,
		})
	exe := writeTempExecutable(t, "current pure binary")
	calls := stubApply(t, exe)

	if err := UpdateSelfFromRelease(rs.apiURL(), "linux", "amd64", "kuromatsu", UpdateOptions{}); err != nil {
		t.Fatalf("UpdateSelfFromRelease: %v", err)
	}
	if len(*calls) != 1 || string((*calls)[0]) != "pure-go binary" {
		t.Fatalf("applyUpdate calls = %q, want one call with the pure-Go binary", *calls)
	}
}

// digestServer serves a one-asset release whose API digest is digest
// verbatim and counts downloads of the asset itself.
func digestServer(t *testing.T, name, digest string, body []byte) (string, *atomic.Int64) {
	t.Helper()
	var downloads atomic.Int64
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case testReleaseAPIPath:
			writeReleasePayload(w, testReleasePayload{
				TagName: "v1.0.0",
				Assets: []testReleaseAsset{{
					Name:               name,
					BrowserDownloadURL: server.URL + "/assets/" + name,
					Digest:             digest,
				}},
			})
		case "/assets/" + name:
			downloads.Add(1)
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	withTestHTTPClient(t, server.Client())
	return server.URL + testReleaseAPIPath, &downloads
}

func TestUpdateSelf_MalformedDigestRefuses(t *testing.T) {
	body := buildTestTarGz(t, map[string]string{"kuromatsu": "unverified binary"})
	for _, digest := range []string{"sha256:", "sha256:  ", "sha256:abc"} {
		apiURL, downloads := digestServer(t, "kuromatsu_Linux_x86_64.tar.gz", digest, body)
		exe := writeTempExecutable(t, "current binary")
		calls := stubApply(t, exe)

		err := UpdateSelfFromRelease(apiURL, "linux", "amd64", "kuromatsu", UpdateOptions{})
		if err == nil || !strings.Contains(err.Error(), "malformed sha256 digest") {
			t.Fatalf("digest %q: error = %v, want a malformed-digest refusal", digest, err)
		}
		if n := downloads.Load(); n != 0 {
			t.Fatalf("digest %q: asset downloaded %d times, want 0", digest, n)
		}
		if len(*calls) != 0 {
			t.Fatalf("digest %q: applyUpdate called %d times, want 0", digest, len(*calls))
		}
		assertFileContent(t, exe, "current binary")
	}
}

func TestUpdateSelf_ChecksumMismatchNeverApplies(t *testing.T) {
	body := buildTestTarGz(t, map[string]string{"kuromatsu": "tampered binary"})
	apiURL, downloads := digestServer(t, "kuromatsu_Linux_x86_64.tar.gz", "sha256:"+strings.Repeat("a", 64), body)
	exe := writeTempExecutable(t, "current binary")
	calls := stubApply(t, exe)

	err := UpdateSelfFromRelease(apiURL, "linux", "amd64", "kuromatsu", UpdateOptions{})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error = %v, want a checksum mismatch", err)
	}
	if n := downloads.Load(); n != 1 {
		t.Fatalf("asset downloaded %d times, want 1", n)
	}
	if len(*calls) != 0 {
		t.Fatalf("applyUpdate called %d times, want 0", len(*calls))
	}
	assertFileContent(t, exe, "current binary")
}
