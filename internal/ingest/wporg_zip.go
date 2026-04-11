package ingest

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// downloadZIP fetches a ZIP file from url and writes it to a temp file in dir.
func downloadZIP(ctx context.Context, url, dir, slug string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, wpDownloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download plugin zip: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned %d for %s", resp.StatusCode, url)
	}

	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return "", fmt.Errorf("create dest dir: %w", err)
	}

	zipPath := filepath.Join(dir, slug+".zip")
	f, err := os.Create(zipPath)
	if err != nil {
		return "", fmt.Errorf("create zip file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(zipPath)
		return "", fmt.Errorf("write zip: %w", err)
	}
	return zipPath, nil
}

// extractZIP extracts a ZIP archive into destDir with Zip Slip protection.
func extractZIP(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("abs dest dir: %w", err)
	}

	for _, f := range r.File {
		target := filepath.Join(absDestDir, f.Name) //nolint:gosec // validated below
		if !strings.HasPrefix(filepath.Clean(target), absDestDir+string(os.PathSeparator)) {
			return fmt.Errorf("zip slip: %q escapes destination", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, dirPerm); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), dirPerm); err != nil {
			return err
		}
		if err := extractFile(f, target); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open %s in zip: %w", f.Name, err)
	}
	defer rc.Close()

	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, rc); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	return nil
}
