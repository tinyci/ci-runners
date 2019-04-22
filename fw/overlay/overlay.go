package overlay

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

// Mount is the struct containing the mount information required to establish
// the union.
type Mount struct {
	Lower  string
	Work   string
	Upper  string
	Target string
}

func (m *Mount) validate() error {
	for _, dir := range []string{m.Lower, m.Work, m.Upper, m.Target} {
		if !filepath.IsAbs(dir) {
			return errors.Errorf("%q must be an absolute path", dir)
		}
		if strings.Contains(dir, "..") {
			return errors.Errorf("%q contains invalid paths: '..'", dir)
		}
	}

	return nil
}

// Cleanup cleans up the work directories.
func (m *Mount) Cleanup() error {
	for _, dir := range []string{m.Work, m.Upper, m.Target} {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}

	return nil
}

// Unmount unmounts the overlayfs.
func (m *Mount) Unmount() error {
	if err := m.validate(); err != nil {
		return err
	}
	return unix.Unmount(m.Target, unix.UMOUNT_NOFOLLOW)
}

// Mount mounts the overlayfs, creating any dirs necessary
func (m *Mount) Mount() error {
	if err := m.validate(); err != nil {
		return err
	}
	return unix.Mount("overlay", m.Target, "overlay", 0, fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", m.Lower, m.Upper, m.Work))
}
