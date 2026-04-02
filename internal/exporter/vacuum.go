package exporter

import (
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"

	_ "modernc.org/sqlite"
)

// vacuumInto creates a consistent snapshot of src database at dest path.
// Uses a separate read-only connection so it doesn't interfere with the main writer.
func vacuumInto(ctx context.Context, src, dest string) error {
	db, err := sql.Open("sqlite", src+"?mode=ro&_pragma=busy_timeout(10000)")
	if err != nil {
		return fmt.Errorf("opening source db: %w", err)
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, fmt.Sprintf(`VACUUM INTO '%s'`, dest))
	if err != nil {
		return fmt.Errorf("VACUUM INTO: %w", err)
	}

	return nil
}

// integrityCheck runs PRAGMA integrity_check on a database file.
// Returns nil if the database is consistent.
func integrityCheck(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return fmt.Errorf("opening db for check: %w", err)
	}
	defer db.Close()

	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("integrity check query: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("integrity check failed: %s", result)
	}
	return nil
}

// gzipFile compresses src into dest.gz using gzip, then removes src.
func gzipFile(src, dest string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, fmt.Errorf("opening source: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dest)
	if err != nil {
		return 0, fmt.Errorf("creating gzip file: %w", err)
	}

	gz, err := gzip.NewWriterLevel(out, gzip.BestCompression)
	if err != nil {
		out.Close()
		os.Remove(dest)
		return 0, fmt.Errorf("creating gzip writer: %w", err)
	}

	if _, err := io.Copy(gz, in); err != nil {
		gz.Close()
		out.Close()
		os.Remove(dest)
		return 0, fmt.Errorf("compressing: %w", err)
	}

	if err := gz.Close(); err != nil {
		out.Close()
		os.Remove(dest)
		return 0, fmt.Errorf("finalising gzip: %w", err)
	}

	if err := out.Close(); err != nil {
		os.Remove(dest)
		return 0, fmt.Errorf("closing output: %w", err)
	}

	info, err := os.Stat(dest)
	if err != nil {
		return 0, err
	}

	// Remove the uncompressed source
	os.Remove(src)

	return info.Size(), nil
}
