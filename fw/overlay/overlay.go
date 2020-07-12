// Package overlay implements union filesystems via overlayfs for the purposes
// of keep your source tree clean.
//
// To use, simply create three paths -- ioutil.TempDir()s work great -- and
// have the path to your source code. Assign then to the various properties in
// the Mount parameter, assign the path to your source code to the Lower property.
//
// Then, call the methods:
//		func main() {
//			m := &Mount{}
//			m.Lower = os.Args[0]
//
//			var err error
//			m.Upper, err = ioutil.TempDir("", "")
//			if err != nil {
//				panic(err)
//			}
//
//			m.Target, err = ioutil.TempDir("", "")
//			if err != nil {
//				panic(err)
//			}
//
//			m.Work, err = ioutil.TempDir("", "")
//			if err != nil {
//				panic(err)
//			}
//
//			if err := m.Mount(); err != nil {
//				panic(err)
//			}
//
//			fmt.Println(m.Target)
//			fmt.Println("do some damage, and press enter to unmount")
//			os.Stdin.Read([]byte{})
//
//			if err := m.Unmount(); err != nil {
//				panic(err)
//			}
//
//			if err := m.Cleanup(); err != nil {
//				panic(err)
//			}
//		}
//
//
// Your program must have the *CAP_SYS_ADMIN* linux capability (see
// capabilities(7)) or be root to use this library without permissions issues.
package overlay

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tinyci/ci-agents/errors"
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

func (m *Mount) validate() *errors.Error {
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
func (m *Mount) Cleanup() *errors.Error {
	for _, dir := range []string{m.Work, m.Upper, m.Target} {
		if err := os.RemoveAll(dir); err != nil {
			return errors.New(err)
		}
	}

	return nil
}

// Unmount unmounts the overlayfs.
func (m *Mount) Unmount() *errors.Error {
	if err := m.validate(); err != nil {
		return err
	}
	return errors.New(unix.Unmount(m.Target, unix.UMOUNT_NOFOLLOW))
}

// Mount mounts the overlayfs, creating any dirs necessary
func (m *Mount) Mount() *errors.Error {
	if err := m.validate(); err != nil {
		return errors.New(err)
	}
	return errors.New(unix.Mount("overlay", m.Target, "overlay", 0, fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", m.Lower, m.Upper, m.Work)))
}
