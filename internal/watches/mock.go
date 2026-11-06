package watches

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/SandwichLabs/slaw/internal/filestore"
)

// Mock is an in-memory Store for tests of packages that depend on one. It
// keeps file bytes, so strict decoding, revisions, conflicts and the
// unreadable-file rules behave like the FileStore. Set Err to make every
// method fail. The zero value is ready to use.
type Mock struct {
	mu    sync.Mutex
	once  sync.Once
	c     *core
	files map[string][]byte
	Err   error
}

var _ Store = (*Mock)(nil)

// SetFile puts raw bytes at <name>.yaml, valid or not, as a test for an
// unreadable file would write them to disk.
func (m *Mock) SetFile(name string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.files == nil {
		m.files = map[string][]byte{}
	}
	m.files[rel(name)] = append([]byte(nil), data...)
}

func (m *Mock) core() *core {
	m.once.Do(func() { m.c = &core{b: m, logger: slog.New(slog.DiscardHandler)} })
	return m.c
}

func (m *Mock) read(rel string) ([]byte, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, "", m.Err
	}
	data, ok := m.files[rel]
	if !ok {
		return nil, "", fmt.Errorf("%s: %w", rel, filestore.ErrNotFound)
	}
	return append([]byte(nil), data...), filestore.HashBytes(data), nil
}

func (m *Mock) write(rel string, data []byte, expect string, overwrite bool) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return "", m.Err
	}
	cur, exists := m.files[rel]
	curRev := ""
	if exists {
		curRev = filestore.HashBytes(cur)
	}
	if !overwrite {
		switch {
		case expect == "" && exists:
			return "", &filestore.ConflictError{Path: rel, Existing: true, Got: curRev}
		case expect != "" && !exists:
			return "", &filestore.ConflictError{Path: rel, Missing: true, Want: expect}
		case expect != "" && curRev != expect:
			return "", &filestore.ConflictError{Path: rel, Want: expect, Got: curRev}
		}
	}
	if m.files == nil {
		m.files = map[string][]byte{}
	}
	m.files[rel] = append([]byte(nil), data...)
	return filestore.HashBytes(data), nil
}

func (m *Mock) remove(rel, expect string, overwrite bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	cur, exists := m.files[rel]
	if !exists {
		return nil
	}
	if curRev := filestore.HashBytes(cur); !overwrite && expect != "" && curRev != expect {
		return &filestore.ConflictError{Path: rel, Want: expect, Got: curRev}
	}
	delete(m.files, rel)
	return nil
}

func (m *Mock) names() ([]fileName, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	prefix := Dir + "/"
	var out []fileName
	for rel := range m.files {
		if n, ok := strings.CutPrefix(rel, prefix); ok && !strings.Contains(n, "/") && strings.HasSuffix(n, Extension) && !filestore.IsTemp(n) {
			out = append(out, fileName{name: n})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

func (m *Mock) List(ctx context.Context) ([]Watch, error) { return m.core().List(ctx) }

func (m *Mock) Get(ctx context.Context, name string) (*Watch, error) {
	return m.core().Get(ctx, name)
}

func (m *Mock) Source(ctx context.Context, name string) (*Source, error) {
	return m.core().Source(ctx, name)
}

func (m *Mock) Put(ctx context.Context, w *Watch, opts ...WriteOption) error {
	return m.core().Put(ctx, w, opts...)
}

func (m *Mock) PutSource(ctx context.Context, name string, data []byte, opts ...WriteOption) (string, error) {
	return m.core().PutSource(ctx, name, data, opts...)
}

func (m *Mock) Delete(ctx context.Context, name string, opts ...WriteOption) error {
	return m.core().Delete(ctx, name, opts...)
}
