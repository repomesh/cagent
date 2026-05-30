// Package worktree creates throwaway git worktrees so an agent can work in
// isolation from the user's checkout. The worktree shares the repository's
// object store but has its own working directory and branch, letting the
// user keep using the original checkout while the agent makes changes.
package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/docker/docker-agent/pkg/paths"
	"github.com/docker/docker-agent/pkg/worktree/namesgenerator"
)

// ErrNotGitRepository means the requested directory is not inside a git worktree.
var ErrNotGitRepository = errors.New("not a git repository")

// ErrInvalidName means the requested worktree name cannot be safely used as a
// directory and branch component.
var ErrInvalidName = errors.New("invalid worktree name")

// Worktree describes a git worktree created for an agent session.
type Worktree struct {
	// Dir is the absolute path of the worktree's working directory.
	Dir string
	// Branch is the branch checked out in the worktree.
	Branch string
	// Name is the worktree's name (the part after the "worktree-" branch prefix).
	Name string
	// SourceDir is the root of the repository the worktree was branched
	// from. The worktree lives under the data directory, far from the
	// original checkout, so setup hooks need this to copy untracked files
	// (.env, local config) git won't carry over.
	SourceDir string
	// BaseCommit is the commit the worktree's branch was created at. It is
	// used to detect commits added during the session (see [Status]).
	BaseCommit string
}

// Status describes whether a worktree holds work that would be lost if it
// were removed.
type Status struct {
	// Modified is true when tracked files have uncommitted changes.
	Modified bool
	// Untracked is true when the worktree contains untracked files.
	Untracked bool
	// NewCommits is true when commits were added since the worktree was
	// created (HEAD moved away from [Worktree.BaseCommit]).
	NewCommits bool
}

// IsDirty reports whether the worktree holds any work (uncommitted changes,
// untracked files, or new commits) that removing it would discard.
func (s Status) IsDirty() bool {
	return s.Modified || s.Untracked || s.NewCommits
}

// Create creates a new git worktree for the repository containing dir and
// returns it. The worktree lives under the data directory and checks out a
// freshly created branch so the agent's changes stay isolated from the user's
// checkout.
//
// When name is empty, a friendly random name (e.g. "focused_turing") is
// generated. The branch is named "worktree-<name>" and the worktree is stored
// under <dataDir>/worktrees/<name>.
//
// Returns [ErrNotGitRepository] when dir is not inside a git worktree, and
// [ErrInvalidName] when an explicit name is not a safe path/branch component.
func Create(ctx context.Context, dir, name string) (*Worktree, error) {
	root, err := repoRoot(ctx, dir)
	if err != nil {
		return nil, err
	}

	if name == "" {
		name = namesgenerator.GetRandomName(0)
	} else if err := validateName(name); err != nil {
		return nil, err
	}

	branch := "worktree-" + name
	dest := filepath.Join(paths.GetDataDir(), "worktrees", name)

	if _, err := os.Stat(dest); err == nil {
		return nil, fmt.Errorf("%w: worktree %q already exists at %s", ErrInvalidName, name, dest)
	}

	if err := git(ctx, root, "worktree", "add", "-b", branch, dest); err != nil {
		return nil, fmt.Errorf("creating git worktree: %w", err)
	}

	wt := &Worktree{Dir: dest, Branch: branch, Name: name, SourceDir: root}

	// Record the branch point so [Status] can later tell whether the
	// session added commits. A brand-new repository with no commits has no
	// HEAD yet; leave BaseCommit empty in that case.
	if head, err := gitOutput(ctx, dest, "rev-parse", "HEAD"); err == nil {
		wt.BaseCommit = head
	}

	return wt, nil
}

// Status inspects the worktree and reports whether it holds uncommitted
// changes, untracked files, or commits added since creation.
func (wt *Worktree) Status(ctx context.Context) (Status, error) {
	out, err := gitOutput(ctx, wt.Dir, "status", "--porcelain")
	if err != nil {
		return Status{}, fmt.Errorf("inspecting worktree: %w", err)
	}

	var st Status
	for line := range strings.SplitSeq(out, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "??") {
			st.Untracked = true
		} else {
			st.Modified = true
		}
	}

	// HEAD moving away from the recorded branch point means the session
	// committed something. When BaseCommit is empty (worktree created in a
	// repo without commits) any resolvable HEAD counts as new work.
	if head, err := gitOutput(ctx, wt.Dir, "rev-parse", "HEAD"); err == nil {
		st.NewCommits = head != wt.BaseCommit
	}

	return st, nil
}

// Remove deletes the worktree's directory and its branch, discarding any
// uncommitted changes, untracked files, and commits. Callers decide when
// removal is appropriate (e.g. only for worktrees they created, never
// pre-existing ones).
func (wt *Worktree) Remove(ctx context.Context) error {
	if err := git(ctx, wt.SourceDir, "worktree", "remove", "--force", wt.Dir); err != nil {
		return fmt.Errorf("removing git worktree: %w", err)
	}
	// The branch can only be deleted once the worktree no longer occupies it;
	// -D discards unmerged commits, which is the intended "remove and forget".
	if err := git(ctx, wt.SourceDir, "branch", "-D", wt.Branch); err != nil {
		return fmt.Errorf("deleting worktree branch: %w", err)
	}
	return nil
}

// validateName rejects names that would escape the worktrees directory or
// produce an invalid git branch. Names must be a single path segment made of
// safe characters, which also keeps the derived "worktree-<name>" branch valid.
func validateName(name string) error {
	if name != strings.TrimSpace(name) {
		return fmt.Errorf("%w: %q has surrounding whitespace", ErrInvalidName, name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("%w: %q must not contain path separators", ErrInvalidName, name)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%w: %q is not allowed", ErrInvalidName, name)
	}
	// filepath.Base collapses separators and ".."; if the cleaned segment
	// differs, the input was not a plain single path component.
	if filepath.Base(name) != name {
		return fmt.Errorf("%w: %q must be a single path segment", ErrInvalidName, name)
	}
	return nil
}

// repoRoot returns the worktree root of the git repository containing dir,
// or [ErrNotGitRepository] when dir is not inside one.
func repoRoot(ctx context.Context, dir string) (string, error) {
	out, err := gitOutput(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", ErrNotGitRepository
		}
		return "", err
	}
	return filepath.Clean(out), nil
}

func git(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

// gitOutput runs a git command in dir and returns its trimmed stdout.
func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}
