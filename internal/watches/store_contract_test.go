package watches

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/filestore"
)

const (
	docA = "# hand-written\nquestion:\n  query: stuck_orders\n  params: {days: 1}\nwhen:\n  rows: any\n"
	docB = "question:\n  metric: revenue\nwhen:\n  above: 1000\n"
)

// fixture is a Store plus the way a test plants raw bytes where a person's
// editor or git would.
type fixture struct {
	store Store
	plant func(name string, data []byte)
	read  func(name string) []byte
}

func fileFixture(t *testing.T) fixture {
	root := t.TempDir()
	return fixture{
		store: NewFileStore(root),
		plant: func(name string, data []byte) {
			p := filepath.Join(root, Dir, name+Extension)
			require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
			require.NoError(t, os.WriteFile(p, data, 0o644))
		},
		read: func(name string) []byte {
			b, err := os.ReadFile(filepath.Join(root, Dir, name+Extension))
			require.NoError(t, err)
			return b
		},
	}
}

func mockFixture(*testing.T) fixture {
	m := &Mock{}
	return fixture{
		store: m,
		plant: m.SetFile,
		read: func(name string) []byte {
			s, err := m.Source(context.Background(), name)
			if err != nil {
				return nil
			}
			return []byte(s.Text)
		},
	}
}

func eachStore(t *testing.T, f func(t *testing.T, fx fixture)) {
	for name, mk := range map[string]func(*testing.T) fixture{"file": fileFixture, "mock": mockFixture} {
		t.Run(name, func(t *testing.T) { f(t, mk(t)) })
	}
}

func TestStore_PutGetListAndDelete(t *testing.T) {
	eachStore(t, func(t *testing.T, fx fixture) {
		ctx := context.Background()
		list, err := fx.store.List(ctx)
		require.NoError(t, err)
		assert.NotNil(t, list)
		assert.Empty(t, list, "no folder is no watches")

		w, err := Decode("stuck_orders", []byte(docA))
		require.NoError(t, err)
		require.NoError(t, fx.store.Put(ctx, w))
		assert.NotEmpty(t, w.Rev, "a write hands back the new revision")

		got, err := fx.store.Get(ctx, "stuck_orders")
		require.NoError(t, err)
		assert.Equal(t, w.Rev, got.Rev)
		assert.Equal(t, "stuck_orders", got.Question.Query)

		other := &Watch{Name: "revenue_cap", Question: Question{Metric: "revenue"}, When: Condition{Rows: RowsNone}}
		require.NoError(t, fx.store.Put(ctx, other))
		list, err = fx.store.List(ctx)
		require.NoError(t, err)
		require.Len(t, list, 2)
		assert.Equal(t, []string{"revenue_cap", "stuck_orders"}, []string{list[0].Name, list[1].Name}, "ordered by name")

		require.NoError(t, fx.store.Delete(ctx, "revenue_cap", WithRev(other.Rev)))
		_, err = fx.store.Get(ctx, "revenue_cap")
		assert.ErrorIs(t, err, ErrNotFound)
		assert.ErrorIs(t, fx.store.Delete(ctx, "revenue_cap"), ErrNotFound)
	})
}

func TestStore_CompareAndWrite(t *testing.T) {
	eachStore(t, func(t *testing.T, fx fixture) {
		ctx := context.Background()
		w := &Watch{Name: "w", Question: Question{Query: "q"}, When: Condition{Rows: RowsAny}}
		require.NoError(t, fx.store.Put(ctx, w))
		first := w.Rev

		// A create over an existing file is a conflict.
		dup := &Watch{Name: "w", Question: Question{Query: "other"}, When: Condition{Rows: RowsAny}}
		assert.ErrorIs(t, fx.store.Put(ctx, dup), ErrConflict)

		// An edit with the loaded revision succeeds and moves it.
		w.Question.Query = "q2"
		require.NoError(t, fx.store.Put(ctx, w))
		assert.NotEqual(t, first, w.Rev)

		// A stale revision (someone else changed the file) is a conflict that
		// changes nothing.
		stale := &Watch{Name: "w", Question: Question{Query: "mine"}, When: Condition{Rows: RowsAny}}
		stale.Rev = first
		assert.ErrorIs(t, fx.store.Put(ctx, stale), ErrConflict)
		got, err := fx.store.Get(ctx, "w")
		require.NoError(t, err)
		assert.Equal(t, "q2", got.Question.Query)

		// WithRev overrides the item's, and WithOverwrite skips the check.
		stale.Rev = ""
		require.NoError(t, fx.store.Put(ctx, stale, WithRev(got.Rev)))
		stale.Rev = "stale"
		require.NoError(t, fx.store.Put(ctx, stale, WithOverwrite()))

		// A delete against a stale revision is a conflict; without one it deletes.
		assert.ErrorIs(t, fx.store.Delete(ctx, "w", WithRev(first)), ErrConflict)
		require.NoError(t, fx.store.Delete(ctx, "w"))

		// A revision for a file that is gone is a conflict, not a create.
		gone := &Watch{Name: "w", Question: Question{Query: "q"}, When: Condition{Rows: RowsAny}}
		gone.Rev = first
		assert.ErrorIs(t, fx.store.Put(ctx, gone), ErrConflict)
	})
}

func TestStore_RevisionsAreOnePerFile(t *testing.T) {
	eachStore(t, func(t *testing.T, fx fixture) {
		ctx := context.Background()
		fx.plant("a", []byte(docA))
		fx.plant("b", []byte(docB))
		a, err := fx.store.Get(ctx, "a")
		require.NoError(t, err)
		b, err := fx.store.Get(ctx, "b")
		require.NoError(t, err)
		assert.Equal(t, filestore.HashBytes([]byte(docA)), a.Rev, "the revision is the file's content hash")
		assert.NotEqual(t, a.Rev, b.Rev)

		// Editing b leaves a's revision alone.
		b.When.Above = nil
		b.When.Rows = RowsAny
		require.NoError(t, fx.store.Put(ctx, b))
		a2, err := fx.store.Get(ctx, "a")
		require.NoError(t, err)
		assert.Equal(t, a.Rev, a2.Rev)
	})
}

func TestStore_AnUnreadableFileIsListedWithItsError(t *testing.T) {
	eachStore(t, func(t *testing.T, fx fixture) {
		ctx := context.Background()
		fx.plant("good", []byte(docA))
		fx.plant("broken", []byte("question: {query: q}\n"))
		fx.plant("typo", []byte("question: {query: q}\nwhen: {rows: any}\nbogus: 1\n"))
		fx.plant("Upper", []byte(docA))
		fx.plant("nul", []byte(docA))

		list, err := fx.store.List(ctx)
		require.NoError(t, err)
		byName := map[string]Watch{}
		for _, w := range list {
			byName[w.Name] = w
		}
		require.Len(t, byName, 5, "nothing is hidden")
		assert.Empty(t, byName["good"].Error)
		assert.Contains(t, byName["broken"].Error, "when needs exactly one condition")
		assert.Contains(t, byName["typo"].Error, "field bogus not found")
		assert.Contains(t, byName["Upper"].Error, "must start with a lower-case letter")
		assert.Contains(t, byName["nul"].Error, `watch name "nul" is a reserved file name`, "the name rule's own text is passed through")

		_, err = fx.store.Get(ctx, "broken")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalid)
		assert.NotErrorIs(t, err, ErrNotFound)

		// The broken file can still be read as text to be fixed.
		src, err := fx.store.Source(ctx, "broken")
		require.NoError(t, err)
		assert.Equal(t, "question: {query: q}\n", src.Text)
		assert.NotEmpty(t, src.Rev)
		_, err = fx.store.Source(ctx, "absent")
		assert.ErrorIs(t, err, ErrNotFound)

		// It is not replaced by a save unless the caller says so.
		fix := &Watch{Name: "broken", Question: Question{Query: "q"}, When: Condition{Rows: RowsAny}}
		fix.Rev = src.Rev
		err = fx.store.Put(ctx, fix)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "could not be read")
		require.NoError(t, fx.store.Put(ctx, fix, WithOverwrite()))
		_, err = fx.store.Get(ctx, "broken")
		assert.NoError(t, err)
	})
}

func TestStore_PutSourceKeepsTheBytesAndValidates(t *testing.T) {
	eachStore(t, func(t *testing.T, fx fixture) {
		ctx := context.Background()
		rev, err := fx.store.PutSource(ctx, "kept", []byte(docA))
		require.NoError(t, err)
		assert.Equal(t, docA, string(fx.read("kept")), "comments and layout are kept byte for byte")
		assert.Equal(t, filestore.HashBytes([]byte(docA)), rev)

		_, err = fx.store.PutSource(ctx, "kept", []byte(docB))
		assert.ErrorIs(t, err, ErrConflict, "a create does not replace")
		_, err = fx.store.PutSource(ctx, "kept", []byte(docB), WithRev(rev))
		require.NoError(t, err)

		_, err = fx.store.PutSource(ctx, "bad", []byte("question: {}\n"))
		assert.ErrorIs(t, err, ErrInvalid)
		_, err = fx.store.PutSource(ctx, "Bad Name", []byte(docA))
		assert.ErrorIs(t, err, ErrInvalid)
		_, err = fx.store.Get(ctx, "bad")
		assert.ErrorIs(t, err, ErrNotFound, "nothing was written")
	})
}

func TestStore_RefusesBadNames(t *testing.T) {
	eachStore(t, func(t *testing.T, fx fixture) {
		ctx := context.Background()
		for _, name := range []string{"../x", "a/b", "", "A", "con"} {
			_, err := fx.store.Get(ctx, name)
			assert.Error(t, err, name)
			_, err = fx.store.Source(ctx, name)
			assert.Error(t, err, name)
			assert.Error(t, fx.store.Delete(ctx, name), name)
			assert.Error(t, fx.store.Put(ctx, &Watch{Name: name, Question: Question{Query: "q"}, When: Condition{Rows: RowsAny}}), name)
		}
	})
}

func TestStore_ListIsCapped(t *testing.T) {
	eachStore(t, func(t *testing.T, fx fixture) {
		for i := 0; i < MaxFiles+3; i++ {
			fx.plant(fmt.Sprintf("w%04d", i), []byte(docA))
		}
		list, err := fx.store.List(context.Background())
		require.NoError(t, err)
		require.Len(t, list, MaxFiles+1)
		last := list[len(list)-1]
		assert.Equal(t, TruncatedStubName, last.Name)
		assert.Contains(t, last.Error, "3 more files")
	})
}

func TestMock_ErrFailsEveryMethod(t *testing.T) {
	m := &Mock{Err: assert.AnError}
	ctx := context.Background()
	_, err := m.List(ctx)
	assert.ErrorIs(t, err, assert.AnError)
	_, err = m.Get(ctx, "a")
	assert.ErrorIs(t, err, assert.AnError)
	assert.ErrorIs(t, m.Put(ctx, &Watch{Name: "a", Question: Question{Query: "q"}, When: Condition{Rows: RowsAny}}), assert.AnError)
}

func TestFileStore_SymlinkedFileIsListedNotRead(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.yaml")
	require.NoError(t, os.WriteFile(outside, []byte(docA), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, Dir), 0o755))
	if err := os.Symlink(outside, filepath.Join(root, Dir, "linked.yaml")); err != nil {
		t.Skip("symlinks are not available here")
	}
	st := NewFileStore(root)
	list, err := st.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Contains(t, list[0].Error, "symbolic link")
	_, err = st.Get(context.Background(), "linked")
	assert.Error(t, err)
}

func TestFileStore_CaseVariantWriteIsRefused(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, Dir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, Dir, "Watch.yaml"), []byte(docA), 0o644))
	st := NewFileStore(root)
	err := st.Put(context.Background(), &Watch{Name: "watch", Question: Question{Query: "q"}, When: Condition{Rows: RowsAny}})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConflict)
}
