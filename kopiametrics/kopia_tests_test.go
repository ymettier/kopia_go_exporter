// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package kopiametrics

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// kopiaTestVersion is the kopia release that the integration tests rely on.
// It is intentionally hardcoded so the downloaded binary is reproducible.
// Keep this in sync with the documented version in AGENTS.md.
const kopiaTestVersion = "v0.23.1"

// kopiaTestBinaryName is the name of the downloaded kopia executable that is
// kept in the kopiametrics/ package directory between test runs. The word
// "test" is part of the name to avoid clashing with any system-installed kopia
// binary and to make it obvious the file belongs to the test suite.
const kopiaTestBinaryName = "kopia_test"

// kopiaTestBinaryPath is the location of the kopia executable relative to the
// kopiametrics/ package directory (the working directory while the tests run).
// The binary is intentionally NOT removed after the tests so subsequent runs
// can reuse it.
var kopiaTestBinaryPath = kopiaTestBinaryName

// kopiaReleaseOS maps the Go runtime OS to the OS token used in the kopia
// release asset name (e.g. "macOS" for darwin).
func kopiaReleaseOS() (string, error) {
	switch runtime.GOOS {
	case "linux":
		return "linux", nil
	case "darwin":
		return "macOS", nil
	case "windows":
		return "windows", nil
	default:
		return "", fmt.Errorf("unsupported operating system for kopia download: %s", runtime.GOOS)
	}
}

// kopiaReleaseArch maps the Go runtime architecture to the architecture token
// used in the kopia release asset name (e.g. "x64" for amd64).
func kopiaReleaseArch() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "x64", nil
	case "arm64":
		return "arm64", nil
	case "arm":
		return "arm", nil
	default:
		return "", fmt.Errorf("unsupported architecture for kopia download: %s", runtime.GOARCH)
	}
}

// kopiaReleaseAsset returns the filename of the kopia release asset for the
// current host platform, e.g. "kopia-0.23.1-linux-x64.tar.gz".
func kopiaReleaseAsset() (string, error) {
	osName, err := kopiaReleaseOS()
	if err != nil {
		return "", err
	}

	arch, err := kopiaReleaseArch()
	if err != nil {
		return "", err
	}

	version := strings.TrimPrefix(kopiaTestVersion, "v")
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}

	return fmt.Sprintf("kopia-%s-%s-%s.%s", version, osName, arch, ext), nil
}

// kopiaDownloadURL builds the GitHub release download URL for the configured
// kopia version and current host platform.
func kopiaDownloadURL() (string, error) {
	asset, err := kopiaReleaseAsset()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("https://github.com/kopia/kopia/releases/download/%s/%s", kopiaTestVersion, asset), nil
}

// kopiaChecksumURL builds the GitHub release download URL for the checksums.txt
// file that accompanies the configured kopia version.
func kopiaChecksumURL() string {
	return fmt.Sprintf("https://github.com/kopia/kopia/releases/download/%s/checksums.txt", kopiaTestVersion)
}

// expectedKopiaChecksum downloads checksums.txt for the configured kopia
// version and returns the expected SHA256 hex digest for the given release
// asset.
func expectedKopiaChecksum(t *testing.T, asset string) (string, error) {
	t.Helper()

	resp, err := http.Get(kopiaChecksumURL()) //nolint:gosec,noctx
	if err != nil {
		return "", fmt.Errorf("failed to download checksums.txt: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download checksums.txt: unexpected status %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read checksums.txt: %w", err)
	}

	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			return fields[0], nil
		}
	}

	return "", fmt.Errorf("no checksum entry found for asset %q in checksums.txt", asset)
}

// sha256File computes the SHA256 hex digest of the file at path.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file for checksum: %w", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("failed to compute checksum: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// verifyKopiaChecksum verifies that the downloaded kopia archive matches the
// SHA256 digest published in the release checksums.txt before it is extracted.
func verifyKopiaChecksum(t *testing.T, archiveFile string) error {
	t.Helper()

	asset, err := kopiaReleaseAsset()
	if err != nil {
		return err
	}

	want, err := expectedKopiaChecksum(t, asset)
	if err != nil {
		return err
	}

	got, err := sha256File(archiveFile)
	if err != nil {
		return err
	}

	if got != want {
		return fmt.Errorf("checksum mismatch for %s: got %s, expected %s", asset, got, want)
	}

	t.Logf("verified checksum for %s (%s)", asset, got)
	return nil
}

// downloadKopiaBinary downloads the kopia release for the host platform and
// extracts the kopia executable into kopiaTestBinaryPath. The downloaded
// archive is written to a temporary file and removed afterwards; only the
// extracted binary is kept.
func downloadKopiaBinary(t *testing.T) error {
	t.Helper()

	url, err := kopiaDownloadURL()
	if err != nil {
		return err
	}

	t.Logf("downloading kopia %s from %s", kopiaTestVersion, url)

	resp, err := http.Get(url) //nolint:gosec,noctx
	if err != nil {
		return fmt.Errorf("failed to download kopia: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download kopia: unexpected status %s", resp.Status)
	}

	archiveFile := kopiaTestBinaryPath + ".download"
	out, err := os.Create(archiveFile)
	if err != nil {
		return fmt.Errorf("failed to create temporary archive: %w", err)
	}

	if _, err := io.Copy(out, resp.Body); err != nil {
		_ = out.Close()
		_ = os.Remove(archiveFile)
		return fmt.Errorf("failed to write kopia archive: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(archiveFile)
		return fmt.Errorf("failed to close kopia archive: %w", err)
	}

	if err := verifyKopiaChecksum(t, archiveFile); err != nil {
		_ = os.Remove(archiveFile)
		return err
	}

	defer func() { _ = os.Remove(archiveFile) }()

	if strings.HasSuffix(archiveFile, ".zip") {
		err = extractKopiaFromZip(archiveFile, kopiaTestBinaryPath)
	} else {
		err = extractKopiaFromTarGz(archiveFile, kopiaTestBinaryPath)
	}
	if err != nil {
		return err
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(kopiaTestBinaryPath, 0o755); err != nil {
			return fmt.Errorf("failed to make kopia executable: %w", err)
		}
	}

	return nil
}

// extractKopiaFromTarGz extracts the kopia binary from a .tar.gz archive into
// targetPath.
func extractKopiaFromTarGz(archivePath, targetPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("failed to read gzip stream: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}

		if filepath.Base(header.Name) != "kopia" {
			continue
		}

		out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return fmt.Errorf("failed to create kopia binary: %w", err)
		}

		if _, err := io.Copy(out, tr); err != nil {
			_ = out.Close()
			return fmt.Errorf("failed to extract kopia binary: %w", err)
		}

		return out.Close()
	}

	return fmt.Errorf("kopia binary not found in archive %s", archivePath)
}

// extractKopiaFromZip extracts the kopia binary from a .zip archive into
// targetPath.
func extractKopiaFromZip(archivePath, targetPath string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open zip archive: %w", err)
	}
	defer func() { _ = zr.Close() }()

	for _, file := range zr.File {
		if filepath.Base(file.Name) != "kopia.exe" {
			continue
		}

		rc, err := file.Open()
		if err != nil {
			return fmt.Errorf("failed to open zip entry: %w", err)
		}

		out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			_ = rc.Close()
			return fmt.Errorf("failed to create kopia binary: %w", err)
		}

		if _, err := io.Copy(out, rc); err != nil {
			_ = rc.Close()
			_ = out.Close()
			return fmt.Errorf("failed to extract kopia binary: %w", err)
		}

		_ = rc.Close()
		return out.Close()
	}

	return fmt.Errorf("kopia binary not found in archive %s", archivePath)
}

// kopiaBinaryVersion runs the given kopia executable with --version and returns
// the parsed semantic version (e.g. "0.23.1").
func kopiaBinaryVersion(t *testing.T, binaryPath string) (string, error) {
	t.Helper()

	runPath, err := filepath.Abs(binaryPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve kopia binary path: %w", err)
	}

	cmd := exec.Command(runPath, "--version")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to run %q --version: %w", binaryPath, err)
	}

	re := regexp.MustCompile(`\d+\.\d+\.\d+`)
	version := re.FindString(string(out))
	if version == "" {
		return "", fmt.Errorf("could not parse kopia version from output: %q", string(out))
	}

	return version, nil
}

// TestKopiaBinaryPresent verifies that the kopia test executable already exists
// in the kopiametrics/ directory. When it is missing, the test downloads the
// configured kopia release instead of failing the suite, so the binary is only
// fetched once and reused on later runs.
func TestKopiaBinaryPresent(t *testing.T) {
	if _, err := os.Stat(kopiaTestBinaryPath); err == nil {
		t.Logf("kopia test binary already present at %s", kopiaTestBinaryPath)
		return
	}

	t.Logf("kopia test binary not present at %s, downloading %s", kopiaTestBinaryPath, kopiaTestVersion)
	require.NoError(t, downloadKopiaBinary(t), "failed to download kopia binary")
}

// TestKopiaVersion ensures the downloaded kopia executable runs at the expected
// version. If the version does not match (for example an outdated binary was
// left behind), the test re-downloads the configured kopia release rather than
// failing outright. The test only fails when the binary cannot be obtained or
// is still incorrect after the download.
func TestKopiaVersion(t *testing.T) {
	if _, err := os.Stat(kopiaTestBinaryPath); err != nil {
		t.Logf("kopia test binary missing at %s, downloading %s", kopiaTestBinaryPath, kopiaTestVersion)
		require.NoError(t, downloadKopiaBinary(t), "failed to download kopia binary")
	}

	want := strings.TrimPrefix(kopiaTestVersion, "v")

	got, err := kopiaBinaryVersion(t, kopiaTestBinaryPath)
	require.NoError(t, err, "failed to read kopia version")

	if got != want {
		t.Logf("kopia version mismatch: got %q want %q, re-downloading %s", got, want, kopiaTestVersion)
		require.NoError(t, downloadKopiaBinary(t), "failed to re-download kopia binary")

		got, err = kopiaBinaryVersion(t, kopiaTestBinaryPath)
		require.NoError(t, err, "failed to read kopia version after re-download")
		require.Equal(t, want, got, "kopia version still incorrect after re-download")
	}

	t.Logf("kopia test binary is at expected version %s", kopiaTestVersion)
}

// KopiaTestBinaryPath returns the path to the kopia executable used by the
// integration tests. It can be used by other tests that need to invoke kopia
// directly instead of relying on a system-installed binary.
func KopiaTestBinaryPath() string {
	return kopiaTestBinaryPath
}
