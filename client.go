package rulekit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// lockFile represents the rulekit.lock JSON file.
type lockFile struct {
	Registry  string                `json:"registry"`
	Workspace string                `json:"workspace"`
	Rulesets  map[string]lockEntry  `json:"rulesets"`
}

type lockEntry struct {
	Version  int    `json:"version"`
	Checksum string `json:"checksum"`
	PulledAt string `json:"pulled_at"`
}

// options holds configuration for a Client.
type options struct {
	workspace      string // empty means use lockfile workspace
	verifyChecksum bool
}

// Option configures a Client.
type Option func(*options)

// WithWorkspace overrides the workspace used when resolving rulesets.
// Defaults to the workspace field in rulekit.lock.
func WithWorkspace(workspace string) Option {
	return func(o *options) {
		o.workspace = workspace
	}
}

// WithVerifyChecksum controls SHA-256 verification of dsl.json against the
// lockfile checksum on load. Enabled by default.
func WithVerifyChecksum(verify bool) Option {
	return func(o *options) {
		o.verifyChecksum = verify
	}
}

// Client evaluates rulesets from a local .rulekit/ directory.
type Client struct {
	rootDir string // directory containing .rulekit/ and rulekit.lock
	opts    options

	mu       sync.RWMutex
	lock     *lockFile
	rulesets map[string]*Ruleset
}

// New finds the nearest .rulekit/ directory by walking up from the current
// working directory (similar to how git finds .git/). Returns ErrNoRulekitDir
// if no .rulekit/ directory is found.
func New(opts ...Option) (*Client, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("rulekit: cannot determine working directory: %w", err)
	}
	root, err := findRulekitDir(cwd)
	if err != nil {
		return nil, err
	}
	return newClient(root, opts...)
}

// NewAt creates a Client rooted at a specific directory, which must contain
// a .rulekit/ subdirectory and a rulekit.lock file.
func NewAt(dir string, opts ...Option) (*Client, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("rulekit: cannot resolve directory %q: %w", dir, err)
	}
	if err := checkRulekitDir(abs); err != nil {
		return nil, err
	}
	return newClient(abs, opts...)
}

func newClient(rootDir string, opts ...Option) (*Client, error) {
	o := options{verifyChecksum: true}
	for _, opt := range opts {
		opt(&o)
	}
	return &Client{
		rootDir:  rootDir,
		opts:     o,
		rulesets: make(map[string]*Ruleset),
	}, nil
}

// Eval evaluates the named ruleset against input.
// On the first call for a given key the ruleset is loaded and cached; subsequent
// calls use the in-memory cache with zero I/O.
func (c *Client) Eval(ctx context.Context, key string, input map[string]any) (map[string]any, error) {
	rs, err := c.getRuleset(key)
	if err != nil {
		return nil, err
	}
	return rs.Eval(ctx, input)
}

func (c *Client) getRuleset(key string) (*Ruleset, error) {
	c.mu.RLock()
	rs, ok := c.rulesets[key]
	c.mu.RUnlock()
	if ok {
		return rs, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock.
	if rs, ok = c.rulesets[key]; ok {
		return rs, nil
	}

	// Load lockfile lazily.
	if c.lock == nil {
		lf, err := c.loadLockFile()
		if err != nil {
			return nil, err
		}
		c.lock = lf
	}

	entry, ok := c.lock.Rulesets[key]
	if !ok {
		return nil, fmt.Errorf("rulekit: ruleset %q not found in rulekit.lock", key)
	}

	dslPath := filepath.Join(c.rootDir, ".rulekit", key, "dsl.json")
	data, err := os.ReadFile(dslPath)
	if err != nil {
		return nil, fmt.Errorf("rulekit: cannot read %s: %w", dslPath, err)
	}

	if c.opts.verifyChecksum {
		if err := verifyChecksum(data, entry.Checksum); err != nil {
			return nil, err
		}
	}

	rs, err = Load(data)
	if err != nil {
		return nil, fmt.Errorf("rulekit: ruleset %q: %w", key, err)
	}

	c.rulesets[key] = rs
	return rs, nil
}

func (c *Client) loadLockFile() (*lockFile, error) {
	path := filepath.Join(c.rootDir, "rulekit.lock")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("rulekit: cannot read rulekit.lock: %w", err)
	}
	var lf lockFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("rulekit: invalid rulekit.lock: %w", err)
	}
	return &lf, nil
}

// verifyChecksum checks that the SHA-256 hash of data matches the expected
// checksum string in the format "sha256:<hex>".
func verifyChecksum(data []byte, expected string) error {
	const prefix = "sha256:"
	if !strings.HasPrefix(expected, prefix) {
		return fmt.Errorf("rulekit: unsupported checksum format %q", expected)
	}
	expectedHex := expected[len(prefix):]
	sum := sha256.Sum256(data)
	actualHex := hex.EncodeToString(sum[:])
	if actualHex != expectedHex {
		return fmt.Errorf("%w: expected %s, got sha256:%s", ErrChecksumMismatch, expected, actualHex)
	}
	return nil
}

// findRulekitDir walks up from startDir until it finds a directory containing
// a .rulekit/ subdirectory.
func findRulekitDir(startDir string) (string, error) {
	dir := startDir
	for {
		if err := checkRulekitDir(dir); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root.
			return "", ErrNoRulekitDir
		}
		dir = parent
	}
}

// checkRulekitDir returns nil if dir contains a readable .rulekit/ subdirectory.
func checkRulekitDir(dir string) error {
	info, err := os.Stat(filepath.Join(dir, ".rulekit"))
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf(".rulekit is not a directory")
	}
	return nil
}
