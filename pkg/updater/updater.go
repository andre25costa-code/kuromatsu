package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/minio/selfupdate"
	"github.com/spf13/cobra"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
	"github.com/andre25costa-code/kuromatsu/pkg/utils"
)

// httpClient is a shared HTTP client used for release checks and downloads.
// The Timeout value applies to the entire HTTP request: dialing, TLS
// handshake, redirects, and reading the response body. It is NOT only
// a connection (dial) timeout. To control lower-level timeouts (dial,
// TLS handshake, response header wait), supply a custom Transport with
// an appropriately configured net.Dialer.
var httpClient = &http.Client{Timeout: 2 * time.Minute}

func getWithRetry(rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	return utils.DoRequestWithRetry(httpClient, req)
}

// Asset name prefixes used by the release archives. The native build
// (llama.cpp linked in) ships under its own prefix so that a pure-Go
// update can never select it, and vice versa.
const (
	assetPrefix       = "kuromatsu"
	nativeAssetPrefix = "kuromatsu-native"
)

// nativeOS/nativeArch is the only platform the native build is released for.
const (
	nativeOS   = "linux"
	nativeArch = "amd64"
)

// applyUpdate replaces the running executable. It is a variable so tests
// can observe the call without touching the test binary.
var applyUpdate = selfupdate.Apply

// UpdateOptions carries properties of the running binary that affect which
// release asset is acceptable.
type UpdateOptions struct {
	// Native is true when the running binary embeds the local inference
	// engine; it then only accepts the native release asset.
	Native bool
}

// ExpectedAssetName returns the exact release archive name for the given
// target, matching the goreleaser archive name_template:
// <prefix>_<Os>_<Arch>[v<GOARM>].<ext>, where Os is goos with its first
// letter upper-cased, Arch maps amd64->x86_64 and 386->i386, and the
// extension is .zip on Windows and .tar.gz elsewhere. goarm is only used
// for arm and defaults to 7 when empty.
func ExpectedAssetName(goos, goarch, goarm string, native bool) string {
	goos = strings.ToLower(goos)
	goarch = strings.ToLower(goarch)

	prefix := assetPrefix
	if native {
		prefix = nativeAssetPrefix
	}

	osName := goos
	if osName != "" {
		osName = strings.ToUpper(osName[:1]) + osName[1:]
	}

	var archName string
	switch goarch {
	case "amd64":
		archName = "x86_64"
	case "386":
		archName = "i386"
	case "arm":
		if goarm == "" {
			goarm = "7"
		}
		archName = "armv" + goarm
	default:
		archName = goarch
	}

	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return prefix + "_" + osName + "_" + archName + ext
}

// localGOARM returns the ARM version the running binary was built for, read
// from the GOARM build setting (which may carry a suffix such as
// "7,softfloat"). When the setting is unavailable it assumes 7.
func localGOARM() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key != "GOARM" {
				continue
			}
			v, _, _ := strings.Cut(s.Value, ",")
			if v != "" {
				return v[:1]
			}
		}
	}
	logger.WarnCF("updater", "GOARM unknown; assuming 7", nil)
	return "7"
}

func isNativeAssetName(name string) bool {
	return strings.HasPrefix(name, nativeAssetPrefix+"_")
}

// DownloadAndExtractRelease downloads the release asset named exactly
// assetName and extracts it to a temporary directory. It returns the
// extraction directory on success. If releaseURL is empty, the latest
// release of the current project is used.
func DownloadAndExtractRelease(releaseURL, assetName string) (string, error) {
	assetURL, checksum, err := findAssetInfo(releaseURL, assetName)
	if err != nil {
		return "", err
	}
	// Fail closed: never install an archive whose checksum is unknown.
	if !sha256HexRe.MatchString(checksum) {
		return "", fmt.Errorf("no valid sha256 checksum for asset %s; refusing to update", assetName)
	}

	// Download asset to temp file. Use the asset URL extension so
	// extractArchive can detect the archive format (zip/tar.gz/tar).
	tmpPattern := "kuromatsu-release-*"
	if u, perr := url.Parse(assetURL); perr == nil {
		base := filepath.Base(u.Path)
		lbase := strings.ToLower(base)
		switch {
		case strings.HasSuffix(lbase, ".zip"):
			tmpPattern += ".zip"
		case strings.HasSuffix(lbase, ".tar.gz") || strings.HasSuffix(lbase, ".tgz"):
			tmpPattern += ".tar.gz"
		case strings.HasSuffix(lbase, ".tar"):
			tmpPattern += ".tar"
		default:
			tmpPattern += ".archive"
		}
	} else {
		tmpPattern += ".archive"
	}

	tmpFile, err := os.CreateTemp("", tmpPattern)
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	defer tmpFile.Close()

	resp, err := getWithRetry(assetURL)
	if err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to download asset: status %d", resp.StatusCode)
	}

	// Stream download while computing SHA256 to avoid a second download.
	// Also show a simple progress line to stderr so users see activity.
	h := sha256.New()
	pw := &progressWriter{total: resp.ContentLength}
	mw := io.MultiWriter(tmpFile, h, pw)
	if _, err = io.Copy(mw, resp.Body); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	// ensure final progress line ends with newline
	pw.Finish()

	// verify checksum (always present, see above)
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, checksum) {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("checksum mismatch: got %s expected %s", got, checksum)
	}

	// Extract
	destDir, err := os.MkdirTemp("", "kuromatsu-extract-*")
	if err != nil {
		os.Remove(tmpPath)
		return "", err
	}

	if err := extractArchive(tmpPath, destDir); err != nil {
		os.Remove(tmpPath)
		os.RemoveAll(destDir)
		return "", err
	}

	// cleanup archive file; keep extracted contents
	_ = os.Remove(tmpPath)
	return destDir, nil
}

// UpdateSelfFromRelease downloads the release asset whose name matches the
// given target exactly (see ExpectedAssetName), extracts it and applies the
// binary named programName to update the currently running executable
// using minio/selfupdate.
// If releaseURL is empty, the latest release is used. If platform or arch
// is empty, runtime values are used. A native build only accepts the native
// linux/amd64 asset and refuses any other target before any network request.
func UpdateSelfFromRelease(releaseURL, platform, arch, programName string, opts UpdateOptions) error {
	if platform == "" {
		platform = runtime.GOOS
	}
	if arch == "" {
		arch = runtime.GOARCH
	}
	platform = strings.ToLower(platform)
	arch = strings.ToLower(arch)

	if opts.Native && (platform != nativeOS || arch != nativeArch) {
		return fmt.Errorf("native build: updates only from %s (%s/%s)",
			ExpectedAssetName(nativeOS, nativeArch, "", true), nativeOS, nativeArch)
	}

	goarm := ""
	if arch == "arm" {
		goarm = localGOARM()
	}
	assetName := ExpectedAssetName(platform, arch, goarm, opts.Native)

	dir, err := DownloadAndExtractRelease(releaseURL, assetName)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	binPath, err := findBinaryInDir(dir, programName)
	if err != nil {
		return err
	}

	// ensure executable bit on non-windows
	if runtime.GOOS != "windows" {
		_ = os.Chmod(binPath, 0o755)
	}

	f, err := os.Open(binPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// Backup current executable so we can roll back if needed.
	var applyOpts selfupdate.Options
	if exePath, err := os.Executable(); err == nil {
		applyOpts.OldSavePath = exePath + ".old"
	}

	if err := applyUpdate(f, applyOpts); err != nil {
		return fmt.Errorf("apply update: %w", err)
	}

	return nil
}

// GetReleaseAPIURL returns the GitHub Releases API URL for the given repo owner.
// Example: owner="sky5454" -> https://api.github.com/repos/sky5454/kuromatsu/releases/latest
func GetReleaseAPIURL(owner string) string {
	return fmt.Sprintf("https://api.github.com/repos/%s/kuromatsu/releases/latest", owner)
}

// GetProdReleaseAPIURL returns the production release API URL (this fork).
func GetProdReleaseAPIURL() string {
	return GetReleaseAPIURL("andre25costa-code")
}

// maxListedAssets caps how many available asset names an error lists.
const maxListedAssets = 20

// findAssetInfo resolves the URL and SHA-256 of the release asset named
// exactly assetName. It accepts GitHub release page/API URLs or empty
// (latest release for the project); direct archive URLs are rejected
// because their checksum cannot be verified.
func findAssetInfo(releaseURL, assetName string) (string, string, error) {
	// returns (assetURL, sha256ChecksumHex, error)
	if looksLikeDirectAssetURL(releaseURL) {
		return "", "", fmt.Errorf("no checksum found for asset %s", releaseURL)
	}

	apiURL := buildReleaseAPIURL(releaseURL)
	if apiURL == "" {
		// If caller provided an empty releaseURL, default to the
		// production latest release API URL (stable release).
		apiURL = GetProdReleaseAPIURL()
	}

	resp, err := getWithRetry(apiURL)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("failed to query releases: status %d", resp.StatusCode)
	}

	var data struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Digest             string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", "", err
	}

	idx := -1
	for i, a := range data.Assets {
		if a.Name == assetName {
			idx = i
			break
		}
	}
	if idx < 0 {
		if isNativeAssetName(assetName) {
			return "", "", fmt.Errorf("this is a native (nativellm) build; release %s has no %s. "+
				"Updating to a pure-Go build would remove local inference. "+
				"Rebuild with `make build-native-x86-64` or wait for a release with the native artifact",
				data.TagName, assetName)
		}
		names := make([]string, 0, maxListedAssets)
		for _, a := range data.Assets {
			if len(names) == maxListedAssets {
				break
			}
			names = append(names, a.Name)
		}
		return "", "", fmt.Errorf("release %s has no asset named %s (available: %s)",
			data.TagName, assetName, strings.Join(names, ", "))
	}
	assetURL := data.Assets[idx].BrowserDownloadURL

	// attempt to find checksum: prefer asset digest from API if present
	if d := strings.TrimSpace(data.Assets[idx].Digest); d != "" {
		dLower := strings.ToLower(d)
		if strings.HasPrefix(dLower, "sha256:") {
			hexpart := strings.TrimPrefix(dLower, "sha256:")
			if !sha256HexRe.MatchString(hexpart) {
				// A malformed (e.g. empty) digest must never turn into
				// "no checksum to verify".
				return "", "", fmt.Errorf("release asset %s has a malformed sha256 digest %q; refusing to update",
					assetName, d)
			}
			return assetURL, hexpart, nil
		}
		// If digest already looks like a 64-hex, return it
		if sha256HexRe.MatchString(dLower) {
			return assetURL, dLower, nil
		}
	}
	// Look for checksum assets and take the line for this exact asset name.
	for j, a := range data.Assets {
		n := strings.ToLower(a.Name)
		if strings.Contains(n, "sha256") ||
			strings.Contains(n, "sha256sum") ||
			strings.Contains(n, "checksums") ||
			strings.HasSuffix(n, ".sha256") ||
			strings.HasSuffix(n, ".sha256sum") {
			resp2, err := getWithRetry(data.Assets[j].BrowserDownloadURL)
			if err != nil {
				continue
			}
			bs, err := io.ReadAll(resp2.Body)
			_ = resp2.Body.Close()
			if err != nil {
				continue
			}
			if h, ok := findHashInChecksumContent(bs, assetName); ok {
				return assetURL, h, nil
			}
		}
	}
	// No checksum found for the selected asset -> error
	return "", "", fmt.Errorf("no checksum found for asset %s", assetURL)
}

func looksLikeDirectAssetURL(u string) bool {
	if u == "" {
		return false
	}
	lower := strings.ToLower(u)
	if strings.HasSuffix(lower, ".zip") ||
		strings.HasSuffix(lower, ".tar.gz") ||
		strings.HasSuffix(lower, ".tgz") ||
		strings.HasSuffix(lower, ".tar") {
		return true
	}
	if strings.Contains(lower, "/releases/download/") {
		return true
	}
	return false
}

func buildReleaseAPIURL(releaseURL string) string {
	if releaseURL == "" {
		return ""
	}
	if strings.Contains(releaseURL, "api.github.com") {
		return releaseURL
	}
	u, err := url.Parse(releaseURL)
	if err != nil {
		return ""
	}
	if u.Host != "github.com" {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	owner := parts[0]
	repo := parts[1]
	// if tag specified
	if len(parts) >= 5 && parts[2] == "releases" && parts[3] == "tag" {
		tag := parts[4]
		return fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", owner, repo, tag)
	}
	// default to latest
	return fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
}

// NOTE: helper functions to compute SHA256 from URL/path were removed
// after refactoring to stream the download and verify the checksum
// during the single download to avoid double-transfer.

var sha256HexRe = regexp.MustCompile(`(?i)^[a-f0-9]{64}$`)

// findHashInChecksumContent looks for the SHA-256 of assetName in checksum
// file content in the sha256sum format ("<hash>  <name>" or
// "<hash> *<name>"). The name field must equal assetName exactly, so a line
// for e.g. "<assetName>.sbom.json", or a single-line checksum file for a
// different asset, never matches. It returns the hash (lowercase) and true,
// or "", false if there is no such line.
func findHashInChecksumContent(bs []byte, assetName string) (string, bool) {
	for _, line := range strings.Split(string(bs), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if strings.TrimPrefix(fields[1], "*") != assetName {
			continue
		}
		if sha256HexRe.MatchString(fields[0]) {
			return strings.ToLower(fields[0]), true
		}
	}
	return "", false
}

// progressWriter implements io.Writer and prints a simple progress
// line to stderr while bytes are written. It is intended to be used
// as one writer in an io.MultiWriter so we can stream-to-disk, compute
// the sha256, and update the progress display in a single pass.
type progressWriter struct {
	total   int64
	written int64
	last    time.Time
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n := len(p)
	pw.written += int64(n)
	now := time.Now()
	if pw.last.IsZero() || now.Sub(pw.last) >= 200*time.Millisecond || (pw.total > 0 && pw.written == pw.total) {
		pw.print()
		pw.last = now
	}
	return n, nil
}

func (pw *progressWriter) print() {
	if pw.total > 0 {
		pct := float64(pw.written) * 100.0 / float64(pw.total)
		fmt.Fprintf(os.Stderr, "\rDownloading: %s / %s (%.1f%%)", humanBytes(pw.written), humanBytes(pw.total), pct)
	} else {
		fmt.Fprintf(os.Stderr, "\rDownloading: %s", humanBytes(pw.written))
	}
}

func (pw *progressWriter) Finish() {
	pw.print()
	fmt.Fprintln(os.Stderr, "")
}

func humanBytes(n int64) string {
	f := float64(n)
	const (
		KB = 1024.0
		MB = KB * 1024.0
		GB = MB * 1024.0
	)
	switch {
	case f >= GB:
		return fmt.Sprintf("%.2f GB", f/GB)
	case f >= MB:
		return fmt.Sprintf("%.2f MB", f/MB)
	case f >= KB:
		return fmt.Sprintf("%.2f KB", f/KB)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func extractArchive(archivePath, destDir string) error {
	lower := strings.ToLower(archivePath)
	if strings.HasSuffix(lower, ".zip") {
		return extractZip(archivePath, destDir)
	}
	// treat .tar.gz and .tgz as gzip+tar
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		return extractTarGz(archivePath, destDir)
	}
	if strings.HasSuffix(lower, ".tar") {
		return extractTar(archivePath, destDir)
	}
	// fallback: try tar.gz
	return extractTarGz(archivePath, destDir)
}

func extractZip(archivePath, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()
	destClean := filepath.Clean(destDir)
	for _, f := range r.File {
		target := filepath.Clean(filepath.Join(destClean, f.Name))
		if !strings.HasPrefix(target, destClean+string(os.PathSeparator)) && target != destClean {
			return fmt.Errorf("path traversal detected: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, f.FileInfo().Mode()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, f.FileInfo().Mode())
		if err != nil {
			_ = rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			_ = rc.Close()
			_ = out.Close()
			return err
		}
		if err := rc.Close(); err != nil {
			_ = out.Close()
			return fmt.Errorf("close zip entry reader: %w", err)
		}
		if err := out.Close(); err != nil {
			return fmt.Errorf("close extracted file %q: %w", target, err)
		}
	}
	return nil
}

func extractTarGz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	return extractTarFromReader(tr, destDir)
}

func extractTar(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	tr := tar.NewReader(f)
	return extractTarFromReader(tr, destDir)
}

// extractTarFromReader contains logic common to extracting entries from a
// tar.Reader and is used by both extractTarGz and extractTar to avoid
// duplicated code (golangci-lint: dupl).
func extractTarFromReader(tr *tar.Reader, destDir string) error {
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Clean(filepath.Join(filepath.Clean(destDir), hdr.Name))
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) &&
			target != filepath.Clean(destDir) {
			return fmt.Errorf("path traversal detected: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("close extracted file %q: %w", target, err)
			}
		}
	}
	return nil
}

func findBinaryInDir(dir, programName string) (string, error) {
	wanted := []string{programName}
	if runtime.GOOS == "windows" {
		wanted = append([]string{programName + ".exe"}, wanted...)
	} else {
		// also accept programs with .exe in archives targeting windows
		wanted = append(wanted, programName+".exe")
	}

	var found string
	if err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return err
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(p)
		for _, w := range wanted {
			if base == w {
				found = p
				return io.EOF // use EOF to stop walking early
			}
		}
		return nil
	}); err != nil && err != io.EOF {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("binary %q not found in archive", programName)
	}
	return found, nil
}

// NewUpdateCommand returns a cobra command that triggers UpdateSelfFromRelease.
// native reports whether the running binary is the native (local inference)
// build, which restricts updates to the native release asset.
func NewUpdateCommand(binaryName string, native bool) *cobra.Command {
	var urlStr, platform, arch string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check and apply updates from GitHub releases",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("Current version: %s\n", config.FormatVersion())
			opts := UpdateOptions{Native: native}
			if err := UpdateSelfFromRelease(urlStr, platform, arch, binaryName, opts); err != nil {
				return err
			}
			fmt.Println("Update applied; restart to use the new version.")
			return nil
		},
	}
	cmd.Flags().StringVarP(&urlStr, "url", "u", "", "Release page or GitHub releases API URL to update from")
	cmd.Flags().StringVar(&platform, "platform", "", "Target platform (default: runtime.GOOS)")
	cmd.Flags().StringVar(&arch, "arch", "", "Target arch (default: runtime.GOARCH)")
	return cmd
}
