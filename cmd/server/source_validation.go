package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	bdinfo "github.com/autobrr/go-bdinfo/pkg/bdinfo"
)

var discoveryAdmission = make(chan struct{}, 1)

type engineSourceValidationError struct {
	err error
}

func (e *engineSourceValidationError) Error() string {
	return e.err.Error()
}

func (e *engineSourceValidationError) Unwrap() error {
	return e.err
}

func wrapEngineSourceValidation(err error) error {
	if err == nil {
		return nil
	}
	return &engineSourceValidationError{err: err}
}

func isEngineSourceValidationError(err error) bool {
	var target *engineSourceValidationError
	return errors.As(err, &target)
}

func acquireDiscovery(ctx context.Context) error {
	select {
	case discoveryAdmission <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releaseDiscovery() {
	<-discoveryAdmission
}

func withDiscoveryAdmission(ctx context.Context, work func() error) error {
	if err := acquireDiscovery(ctx); err != nil {
		return err
	}
	defer releaseDiscovery()

	if hasActiveScan() {
		return errScanAlreadyRunning
	}

	return work()
}

func validateEngineSourceBounded(ctx context.Context, source string) error {
	return withDiscoveryAdmission(ctx, func() error {
		return wrapEngineSourceValidation(validateEngineSourceContext(ctx, source))
	})
}

// discoverPlaylistsBounded serializes both recursive source validation and
// expensive server-side discovery work. The worker process performs the same
// tree validation immediately before scans.
func discoverPlaylistsBounded(ctx context.Context, source string) (result bdinfo.Result, err error) {
	err = withDiscoveryAdmission(ctx, func() error {
		if validateErr := validateEngineSourceContext(ctx, source); validateErr != nil {
			return wrapEngineSourceValidation(validateErr)
		}

		var discoverErr error
		result, discoverErr = bdinfo.DiscoverPlaylists(ctx, bdinfo.Options{
			Path:     source,
			Settings: bdinfo.DefaultSettings("."),
		})
		return discoverErr
	})
	return result, err
}

func validateEngineSource(source string) error {
	return validateEngineSourceContext(context.Background(), source)
}

// validateEngineSourceContext rejects nested symlinks and special files before
// the upstream parser walks a directory source. This prevents a prepared BDMV
// tree from redirecting parser reads outside the configured source tree. It is
// a preflight check; callers should not claim it is race-proof against
// concurrent host-side replacement after validation.
func validateEngineSourceContext(ctx context.Context, source string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("source not accessible: %w", err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("source symlinks are not supported")
	}

	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return errors.New("ISO source must be a regular file")
		}
		return nil
	}

	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("source contains unsupported symlink: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if !entryInfo.Mode().IsRegular() {
			return fmt.Errorf("source contains unsupported special file: %s", path)
		}
		return nil
	})
}
