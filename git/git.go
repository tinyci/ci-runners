package git

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/kr/pty"
	"github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-runners/runner/config"
)

const (
	defaultGitUserName = "tinyCI runner"
	defaultGitEmail    = "no-reply@example.org"
)

// RepoManager manages a series of repositories.
type RepoManager struct {
	Config       *config.Config
	Log          io.Writer
	AccessToken  string
	Env          []string
	BaseRepoPath string
	RepoPath     string
	RepoName     string
	ForkRepoName string
	ForkRemote   string
}

func init() {
	home := os.Getenv("HOME")

	if home == "" {
		panic("could not determine home directory; aborting")
	}

	if _, err := os.Stat(path.Join(home, ".gitconfig")); err != nil {
		fmt.Println("Gitconfig not populated with merge information: populating it now")

		// #nosec
		if err := exec.Command("git", "config", "--global", "--add", "user.name", defaultGitUserName).Run(); err != nil {
			panic(fmt.Sprintf("While updating git configuration: %v", err))
		}

		// #nosec
		if err := exec.Command("git", "config", "--global", "--add", "user.email", defaultGitEmail).Run(); err != nil {
			panic(fmt.Sprintf("While updating git configuration: %v", err))
		}
	}
}

// Init initialies the repomanager for use. Must be called before using other functions.
func (rm *RepoManager) Init(config *config.Config, repoName, forkRepoName string) error {
	rm.Config = config
	rm.RepoName = repoName
	if err := rm.validateRepoName(rm.RepoName); err != nil {
		return err
	}

	rm.ForkRepoName = forkRepoName
	if err := rm.validateRepoName(rm.ForkRepoName); err != nil {
		return err
	}

	parts := strings.SplitN(rm.ForkRepoName, "/", 2)
	rm.ForkRemote = parts[0]

	rm.RepoPath = filepath.Join(rm.BaseRepoPath, rm.RepoName)
	return nil
}

func (rm *RepoManager) validateRepoName(repoName string) error {
	if strings.Count(repoName, "/") != 1 {
		return errors.New("missing partition between owner and repository")
	}

	if strings.Contains(repoName, "..") {
		return errors.New("this looks like an invalid path, clever guy")
	}

	return nil
}

// CreateLoginScript creates a login script to be used by GIT_ASKPASS git
// credentials functionality. It merely contains `echo <token>` which is enough
// to get us in.
func (rm *RepoManager) createLoginScript() error {
	f, err := os.Create(rm.Config.Runner.LoginScriptPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(
		fmt.Sprintf(`
#!/bin/sh
echo %q
`, rm.AccessToken))
	if err != nil {
		return err
	}

	return os.Chmod(f.Name(), 0700) // #nosec
}

func (rm *RepoManager) removeLoginScript() error {
	return os.Remove(rm.Config.Runner.LoginScriptPath)
}

func (rm *RepoManager) clone() error {
	if err := os.MkdirAll(rm.RepoPath, 0700); err != nil {
		return err
	}

	return rm.Run("git", "clone", fmt.Sprintf("https://github.com/%s", rm.RepoName), ".")
}

func (rm *RepoManager) fetch(remote string, pull bool) error {
	verb := "fetch"
	if pull {
		verb = "pull"
	}

	return rm.Run("git", verb, remote)
}

func (rm *RepoManager) reset() error {
	if err := rm.Run("git", "clean", "-fdx"); err != nil {
		return err
	}

	return rm.Run("git", "reset", "--hard", "HEAD")
}

// CloneOrFetch either clones a new repository, or fetches from an existing origin.
func (rm *RepoManager) CloneOrFetch() error {
	wf := rm.Config.Clients.Log.WithFields(log.FieldMap{"repo_name": rm.RepoName})

	fi, err := os.Stat(rm.RepoPath)
	if err != nil {
		wf.Infof("New repository %v; cloning fresh", rm.RepoName)
		return rm.clone()
	}

	if !fi.IsDir() {
		wf.Errorf("Repository path %v is a file; removing and re-cloning", rm.RepoName)
		if err := os.Remove(rm.RepoPath); err != nil {
			return err
		}
		return rm.clone()
	}

	if err := rm.reset(); err != nil {
		wf.Errorf("resetting repository: %v", err)
		return err
	}

	if err := rm.Checkout("master"); err != nil {
		wf.Errorf("checking out master: %v", err)
		return err
	}

	if err := rm.fetch("origin", false); err != nil {
		wf.Errorf("fetching origin: %v", err)
		return err
	}

	if err := rm.Rebase("origin/master"); err != nil {
		wf.Errorf("rebasing: %v", err)
		return err
	}

	return nil
}

// AddOrFetchFork retrieves the fork's contents, or adds the fork as a remote, and then does that.
func (rm *RepoManager) AddOrFetchFork() error {
	// use normal exec.Command for this as we need to capture
	cmd := exec.Command("git", "remote", "show") // #nosec
	cmd.Dir = rm.RepoPath

	out, err := cmd.Output()
	if err != nil {
		return err
	}

	var added bool

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == rm.ForkRemote {
			added = true
			break
		}
	}

	if !added {
		err := rm.Run("git", "remote", "add", rm.ForkRemote, fmt.Sprintf("https://github.com/%s", rm.ForkRepoName))
		if err != nil {
			return err
		}
	}

	return rm.fetch(rm.ForkRemote, false)
}

// Checkout sets the working copy to the ref provided.
func (rm *RepoManager) Checkout(ref string) error {
	return rm.Run("git", "checkout", ref)
}

// Rebase is similar to merge with rollback capability. Otherwise it's plain rebase.
func (rm *RepoManager) Rebase(ref string) (retErr error) {
	defer func() {
		if retErr != nil {
			fmt.Println("rebase error; trying to roll back")
			if err := rm.Run("git", "rebase", "--abort"); err != nil {
				fmt.Println("while attempting to roll back:", err)
			}
		}
	}()

	return rm.Run("git", "rebase", ref)
}

// Merge merges the ref into the currently checked out ref.
func (rm *RepoManager) Merge(ref string) (retErr error) {
	defer func() {
		if retErr != nil {
			fmt.Println("merge error; trying to roll back")
			if err := rm.Run("git", "merge", "--abort"); err != nil {
				fmt.Println("while attempting to roll back:", err)
			}
		}
	}()

	return rm.Run("git", "merge", "--no-ff", "-m", "CI merge", ref)
}

// Run runs a command, piping output to the log.
func (rm *RepoManager) Run(command ...string) error {
	if err := rm.createLoginScript(); err != nil {
		return err
	}
	defer rm.removeLoginScript()

	cmd := exec.Command(command[0], command[1:]...) // #nosec
	cmd.Env = append(
		append(os.Environ(), fmt.Sprintf("GIT_ASKPASS=%s", rm.Config.Runner.LoginScriptPath), "EDITOR=/bin/true"),
		rm.Env...)
	cmd.Dir = rm.RepoPath

	tty, err := pty.Start(cmd)
	if err != nil {
		return err
	}
	defer tty.Close()

	go io.Copy(rm.Log, tty)

	return cmd.Wait()
}
