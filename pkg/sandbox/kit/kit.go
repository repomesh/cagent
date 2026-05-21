// Package kit stages a docker-agent kit on the host before launching a
// sandbox.
//
// A kit is a self-contained directory that bundles every host resource
// an agent will need at runtime — local skills, AGENTS.md/CLAUDE.md
// prompt files, sub-agent YAMLs — laid out under a fixed schema:
//
//	<kit>/skills/<skill-name>/         # local skills, recursively
//	<kit>/prompt_files/<name>          # collected add_prompt_files inputs
//	<kit>/manifest.json                # debug + cache key
//
// The host stages the kit, redacts secrets via [portcullis.Redact] in
// every text file, and bind-mounts the kit read-only inside the sandbox.
// At runtime, the in-sandbox resolvers ([skills.Load],
// [promptfiles.Paths]) consult [skills.KitDirEnv] to read from the kit
// instead of the user's $HOME — which doesn't exist inside the sandbox.
//
// The kit solves four constraints inherent to the docker sandbox CLI:
//   - the user's $HOME inside the sandbox is unrelated to the host's;
//   - sandbox mounts target directories, not individual files;
//   - host files may contain secrets that must not leak;
//   - other host-only state (e.g. .agents/skills under a parent dir) is
//     unreachable from the sandbox.
package kit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/docker/portcullis"

	"github.com/docker/docker-agent/pkg/config"
	latestcfg "github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/paths"
	"github.com/docker/docker-agent/pkg/promptfiles"
	"github.com/docker/docker-agent/pkg/skills"
)

// MountPath is the path at which the kit is bind-mounted inside the sandbox.
const MountPath = "/agent-kit"

// manifestFile is the on-disk name of the kit's table of contents.
const manifestFile = "manifest.json"

// Options describes a kit build.
type Options struct {
	// AgentRef is the user-facing reference to the agent (a YAML path,
	// an OCI ref, a URL or a builtin name). Used for cache keying and
	// for loading the team config so the kit knows which prompt files
	// and skills to ship.
	AgentRef string

	// EnvProvider is forwarded to [config.Resolve] so URL-sourced
	// agents can pick up GITHUB_TOKEN. May be nil.
	EnvProvider environment.Provider

	// HostCwd is the host's working directory. Prompt-file lookups
	// walk up from it the same way the in-sandbox runtime would.
	HostCwd string

	// HostHome overrides the host's $HOME for prompt-file lookups.
	// Empty means "use os.UserHomeDir".
	HostHome string

	// Workspace is the absolute host path that the sandbox mounts as
	// the agent's live working directory. Files under it are not
	// staged into the kit because the sandbox sees them through the
	// live mount; staging them would duplicate content and ship a
	// stale, redacted copy alongside the live one.
	Workspace string

	// CacheDir is the parent directory under which the kit will be
	// staged. Empty means "use [paths.GetCacheDir]/sandbox-kits".
	CacheDir string
}

// Result is what [Build] returns.
type Result struct {
	// HostDir is the absolute host path of the staged kit. Mount it
	// read-only at [MountPath] inside the sandbox and forward
	// `-e DOCKER_AGENT_KIT_DIR=<MountPath>` so the in-sandbox
	// resolvers find it.
	HostDir string

	// Manifest describes what was staged. It contains absolute host
	// source paths and is meant for caller-side inspection only — the
	// on-disk copy under <HostDir>/manifest.json is sanitised so the
	// sandbox cannot learn the host filesystem layout.
	Manifest Manifest
}

// Manifest is the kit's table of contents.
type Manifest struct {
	AgentRef    string      `json:"agent_ref"`
	BuiltAt     time.Time   `json:"built_at"`
	Skills      []Entry     `json:"skills,omitempty"`
	PromptFiles []Entry     `json:"prompt_files,omitempty"`
	Redactions  []Redaction `json:"redactions,omitempty"`
}

// Entry records one staged file or directory.
type Entry struct {
	// Source is the original host path. Omitted from the on-disk
	// manifest so the sandbox cannot learn the host layout.
	Source string `json:"-"`
	// Target is the path relative to the kit root.
	Target string `json:"target"`
}

// Redaction records that portcullis added at least one [portcullis.Marker]
// to the staged copy of a file.
type Redaction struct {
	// Source is the host path of the original file. Omitted from the
	// on-disk manifest for the same reason as [Entry.Source].
	Source string `json:"-"`
	// Target is the path relative to the kit root.
	Target string `json:"target"`
}

// Build stages the kit and returns its location.
//
// Each Build creates a temporary directory under CacheDir, populates it,
// then atomically replaces the final per-agent directory. Concurrent
// Builds for the same agent therefore never see a half-populated kit;
// the last one to finish wins. The kit directory itself is reused
// across runs (deterministic path keyed by AgentRef) so the sandbox VM
// can be reused as long as nothing else in the workspace mount set
// changed.
func Build(ctx context.Context, opts Options) (*Result, error) {
	hostHome := opts.HostHome
	if hostHome == "" {
		hostHome, _ = os.UserHomeDir()
	}

	cacheParent := opts.CacheDir
	if cacheParent == "" {
		cacheParent = filepath.Join(paths.GetCacheDir(), "sandbox-kits")
	}
	if err := os.MkdirAll(cacheParent, 0o750); err != nil {
		return nil, fmt.Errorf("preparing kit cache: %w", err)
	}

	finalDir := filepath.Join(cacheParent, hashKey(opts.AgentRef))

	// Stage to a temp sibling first so concurrent builds and
	// crashed runs cannot leave behind a half-populated final dir
	// that a later sandbox would mount.
	stagingDir, err := os.MkdirTemp(cacheParent, ".tmp-")
	if err != nil {
		return nil, fmt.Errorf("preparing kit staging dir: %w", err)
	}
	// On any error past this point, drop the staging dir.
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(stagingDir)
		}
	}()

	manifest := Manifest{
		AgentRef: opts.AgentRef,
		BuiltAt:  time.Now().UTC(),
	}

	// Load the team config so we know which prompt files / skills the
	// agent will request. A failure here is non-fatal: we still want
	// to ship local skills since they're discovered from $HOME, not
	// from the config. We log and continue with an empty config.
	cfg, err := loadConfig(ctx, opts)
	if err != nil {
		slog.DebugContext(ctx, "kit: agent config unavailable; skipping prompt-file collection", "err", err)
		cfg = &latestcfg.Config{}
	}

	skillsEntries, redactions, err := stageSkills(stagingDir)
	if err != nil {
		return nil, err
	}
	manifest.Skills = skillsEntries
	manifest.Redactions = append(manifest.Redactions, redactions...)

	promptEntries, redactions, err := stagePromptFiles(stagingDir, cfg, opts.HostCwd, hostHome, opts.Workspace)
	if err != nil {
		return nil, err
	}
	manifest.PromptFiles = promptEntries
	manifest.Redactions = append(manifest.Redactions, redactions...)

	if err := writeManifest(stagingDir, manifest); err != nil {
		return nil, err
	}

	if err := promote(stagingDir, finalDir); err != nil {
		return nil, fmt.Errorf("publishing kit: %w", err)
	}
	committed = true

	slog.DebugContext(ctx, "kit: built",
		"dir", finalDir,
		"skills", len(manifest.Skills),
		"prompt_files", len(manifest.PromptFiles),
		"redactions", len(manifest.Redactions))

	return &Result{HostDir: finalDir, Manifest: manifest}, nil
}

// hashKey turns AgentRef into a short, filesystem-safe directory name.
//
// File-system refs are canonicalised (Abs + EvalSymlinks) so that
// "./agent.yaml" and "/abs/path/agent.yaml" share a kit when they
// resolve to the same file. Non-file refs (OCI, URL, builtin name) are
// hashed verbatim.
//
// The ref is type-tagged before hashing so that, for instance, an OCI
// ref named "default" and the empty/builtin "default" cannot collide.
//
// We truncate to 8 bytes (16 hex chars) of SHA-256 because the entire
// keyspace here is the agents the user runs locally — a handful at
// most — so 2^64 buckets is comically large for the use case while
// keeping kit directory names short and readable.
func hashKey(ref string) string {
	tag, key := classifyRef(ref)
	sum := sha256.Sum256([]byte(tag + "\x00" + key))
	return hex.EncodeToString(sum[:8])
}

// classifyRef returns a tag identifying the kind of ref and a
// canonicalised key for hashing. File refs that resolve on disk are
// returned as ("file", absolute-real-path); everything else falls
// through as ("ref", ref) — including the empty string, which becomes
// ("empty", "") so it cannot collide with a literal ref of "default".
func classifyRef(ref string) (tag, key string) {
	if ref == "" {
		return "empty", ""
	}
	abs, err := filepath.Abs(ref)
	if err != nil {
		return "ref", ref
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		if info, err := os.Stat(resolved); err == nil && !info.IsDir() {
			return "file", resolved
		}
	}
	return "ref", ref
}

// promote moves stagingDir into final atomically. If final already
// exists, it is moved aside first so the rename can succeed, then the
// old content is removed in the background. Best-effort: a leftover
// "<final>.old-*" sibling is harmless and will be reaped on the next
// successful build.
func promote(stagingDir, finalDir string) error {
	if _, err := os.Stat(finalDir); err == nil {
		retired := finalDir + ".old-" + filepath.Base(stagingDir)
		if err := os.Rename(finalDir, retired); err != nil {
			return fmt.Errorf("retiring previous kit: %w", err)
		}
		defer func() { _ = os.RemoveAll(retired) }()
	}
	if err := os.Rename(stagingDir, finalDir); err != nil {
		return fmt.Errorf("renaming kit: %w", err)
	}
	return nil
}

func loadConfig(ctx context.Context, opts Options) (*latestcfg.Config, error) {
	source, err := config.Resolve(opts.AgentRef, opts.EnvProvider)
	if err != nil {
		return nil, err
	}
	return config.Load(ctx, source)
}

// stageSkills copies every local skill discovered on the host into
// <kit>/skills/<skill-name>/, redacting text files in place.
func stageSkills(kitDir string) ([]Entry, []Redaction, error) {
	target := filepath.Join(kitDir, skills.KitSkillsSubdir)
	if err := os.MkdirAll(target, 0o750); err != nil {
		return nil, nil, fmt.Errorf("creating kit skills dir: %w", err)
	}

	var (
		entries    []Entry
		redactions []Redaction
	)
	for _, skill := range skills.Load([]string{"local"}) {
		if skill.BaseDir == "" {
			continue
		}
		dst := filepath.Join(target, sanitise(skill.Name))
		reds, err := copyTree(skill.BaseDir, dst)
		if err != nil {
			return nil, nil, fmt.Errorf("staging skill %q: %w", skill.Name, err)
		}
		entries = append(entries, Entry{
			Source: skill.BaseDir,
			Target: filepath.Join(skills.KitSkillsSubdir, sanitise(skill.Name)),
		})
		redactions = append(redactions, reds...)
	}
	return entries, redactions, nil
}

// stagePromptFiles walks every agent in cfg and copies each
// add_prompt_files entry that lives outside the live workspace into
// <kit>/prompt_files/<name>. Files under workspace are skipped because
// the sandbox already mounts that directory live; shipping a redacted
// duplicate would only confuse the in-sandbox cwd-walk lookup.
func stagePromptFiles(kitDir string, cfg *latestcfg.Config, hostCwd, hostHome, workspace string) ([]Entry, []Redaction, error) {
	target := filepath.Join(kitDir, promptfiles.KitSubdir)
	if err := os.MkdirAll(target, 0o750); err != nil {
		return nil, nil, fmt.Errorf("creating kit prompt-files dir: %w", err)
	}

	var (
		entries    []Entry
		redactions []Redaction
		seen       = make(map[string]bool)
	)
	for _, agent := range cfg.Agents {
		for _, name := range agent.AddPromptFiles {
			if seen[name] {
				continue
			}
			seen[name] = true

			for _, src := range promptfiles.Paths(hostCwd, hostHome, "", name) {
				if isUnder(src, workspace) {
					// The live mount will surface it inside the sandbox.
					continue
				}
				rel := filepath.Join(promptfiles.KitSubdir, name)
				dst := filepath.Join(kitDir, rel)
				red, err := copyFile(src, dst)
				if err != nil {
					return nil, nil, fmt.Errorf("staging prompt file %q: %w", src, err)
				}
				entries = append(entries, Entry{Source: src, Target: rel})
				if red != nil {
					redactions = append(redactions, *red)
				}
				// Only one copy per name. promptfiles.Paths returns the
				// closest workdir match first followed by the home/kit
				// fallback; we ship whichever survives the workspace
				// filter and stop, since later candidates would just
				// overwrite this one.
				break
			}
		}
	}
	return entries, redactions, nil
}

// isUnder reports whether path is contained within base. Both paths
// are made absolute and have their symlinks resolved, so a symlink
// from outside-workspace into the workspace (or vice versa) cannot
// trick the check. When EvalSymlinks fails (e.g. dangling links) the
// best-effort absolute paths are used instead, which is still strict
// enough to defeat the textual "../" escape.
func isUnder(path, base string) bool {
	if base == "" {
		return false
	}
	resolvedBase := resolveAbs(base)
	resolvedPath := resolveAbs(path)
	if resolvedBase == "" || resolvedPath == "" {
		return false
	}
	rel, err := filepath.Rel(resolvedBase, resolvedPath)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolveAbs returns p resolved to an absolute, symlink-free path. If
// p itself doesn't exist, the deepest existing ancestor is resolved
// and the remaining (non-existent) tail is appended to it. This
// matters on systems whose temp dir is itself a symlink (e.g. macOS
// /var → /private/var): an existing base resolves to /private/var
// while a not-yet-created child of it would otherwise resolve to
// /var/..., causing isUnder to wrongly report that they're unrelated.
func resolveAbs(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	// Walk up until we find an existing ancestor we can resolve.
	tail := ""
	current := abs
	for {
		parent := filepath.Dir(current)
		tail = filepath.Join(filepath.Base(current), tail)
		if parent == current {
			return abs // hit the root without finding anything resolvable
		}
		if resolved, err := filepath.EvalSymlinks(parent); err == nil {
			return filepath.Join(resolved, tail)
		}
		current = parent
	}
}

// sanitise replaces filesystem-unfriendly characters in a skill name so
// it becomes a usable directory entry under <kit>/skills/. We only
// replace separators; the rest is allowed because skills loaded from
// disk already had filesystem-friendly names.
func sanitise(name string) string {
	r := strings.NewReplacer(string(filepath.Separator), "_", "..", "_", "/", "_", "\\", "_")
	return r.Replace(name)
}

// copyTree copies the directory rooted at src to dst recursively,
// applying [portcullis.Redact] to every text file. Symlinks inside
// the tree are followed only when their resolved target stays within
// src (resolved); links pointing outside src are silently skipped to
// prevent a hostile or careless skill author from exfiltrating
// arbitrary host files (e.g. a symlink to ~/.aws/credentials) into
// the kit. Directory symlinks are also skipped, which matches the
// recursive skill loader's behaviour and avoids cycles.
func copyTree(src, dst string) ([]Redaction, error) {
	root := resolveAbs(src)
	if root == "" {
		return nil, fmt.Errorf("resolving source: %s", src)
	}

	var redactions []Redaction
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		out := filepath.Join(dst, rel)

		switch {
		case d.IsDir():
			return os.MkdirAll(out, 0o750)
		case d.Type()&fs.ModeSymlink != 0:
			realTarget, terr := filepath.EvalSymlinks(path)
			if terr != nil {
				return nil
			}
			if !isUnder(realTarget, root) {
				slog.Warn("kit: skipping symlink that escapes skill root",
					"link", path, "target", realTarget, "root", root)
				return nil
			}
			info, sterr := os.Stat(realTarget)
			if sterr != nil || info.IsDir() {
				return nil
			}
			red, copyErr := copyFile(realTarget, out)
			if copyErr != nil {
				return copyErr
			}
			if red != nil {
				redactions = append(redactions, *red)
			}
			return nil
		default:
			red, copyErr := copyFile(path, out)
			if copyErr != nil {
				return copyErr
			}
			if red != nil {
				redactions = append(redactions, *red)
			}
			return nil
		}
	})
	if err != nil {
		return nil, err
	}
	return redactions, nil
}

// copyFile copies a regular file from src to dst, redacting via
// [portcullis.Redact] when src is detected as text. Returns a non-nil
// [Redaction] when at least one secret was scrubbed.
//
// The destination inherits the source's permission bits (e.g. so an
// executable helper script next to a SKILL.md keeps its +x), masked
// to user-only since the kit is bind-mounted read-only into the
// sandbox anyway and there's no reason to expose it to other users
// on the host.
func copyFile(src, dst string) (*Redaction, error) {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return nil, err
	}

	out := data
	var redaction *Redaction
	if isText(data) {
		original := string(data)
		scrubbed := portcullis.Redact(original)
		if scrubbed != original {
			out = []byte(scrubbed)
			redaction = &Redaction{Source: src, Target: dst}
		}
	}

	mode := srcInfo.Mode().Perm() & 0o700
	if mode == 0 {
		mode = 0o600
	}
	if err := os.WriteFile(dst, out, mode); err != nil {
		return nil, err
	}
	return redaction, nil
}

// isText reports whether b looks like a text file. The heuristic is
// deliberately strict: NUL byte → binary, invalid UTF-8 → binary. This
// errs on the side of not feeding [portcullis.Redact] non-text input
// (which would either corrupt binaries or produce nonsense output).
func isText(b []byte) bool {
	if len(b) == 0 {
		return true
	}
	// Strip a UTF-8 BOM if present so it doesn't trip up the UTF-8 check.
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		b = b[3:]
	}
	if slices.Contains(b, 0) {
		return false
	}
	return utf8.Valid(b)
}

// writeManifest serialises the manifest as pretty-printed JSON. Source
// host paths are stripped (Entry.Source / Redaction.Source carry
// json:"-") so the sandbox-visible manifest cannot be used to map the
// host filesystem.
func writeManifest(dir string, m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, manifestFile), data, 0o600)
}
