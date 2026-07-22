//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

type kJob struct {
	ID     int
	State  string
	Worker string
}

type kJobModel struct {
	orm.Table[kJob]
	ID     *orm.IntColumn
	State  *orm.StringColumn
	Worker *orm.StringColumn
}

var kJobs = orm.DefineTable[kJob]("k_jobs", func(t *orm.TableBuilder[kJob]) *kJobModel {
	return &kJobModel{
		Table:  t.Table(),
		ID:     t.Int("id").PrimaryKey(),
		State:  t.String("state").NotNull(),
		Worker: t.String("worker").NotNull().ServerDefault("''"),
	}
})

// A lock is not something a fake can have: the clause it renders is checked
// elsewhere, and what it *does* can only be observed with two transactions
// running at once against a real database. That is what this is for.
func TestLocking_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}

	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS k_jobs CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(kJobs)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	ops, _ := migrate.Diff(schema.Schema{}, desired)
	ddl, err := migrate.Generate(dialect, ops)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if _, err := conn.Exec(ctx, ddl); err != nil {
		t.Fatalf("applying schema failed: %v\n%s", err, ddl)
	}

	db := orm.NewDB(conn, dialect)

	// A second handle on its own pool, so a transaction here really is a
	// different session from one there rather than the same one reused.
	other, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = other.Close(context.Background()) })
	otherDB := orm.NewDB(other, dialect)

	seed := func(t *testing.T, n int) {
		t.Helper()
		if _, err := conn.Exec(ctx, `TRUNCATE k_jobs RESTART IDENTITY`); err != nil {
			t.Fatalf("truncate failed: %v", err)
		}
		jobs := make([]*kJob, n)
		for i := range jobs {
			jobs[i] = &kJob{State: "queued"}
		}
		if err := kJobs.With(db).InsertMany(ctx, jobs...); err != nil {
			t.Fatalf("InsertMany failed: %v", err)
		}
	}

	// The claim a work queue is built on: a second reader passes over what the
	// first is holding and takes the rest, rather than waiting or duplicating.
	t.Run("skip locked hands different rows to a second reader", func(t *testing.T) {
		seed(t, 4)

		var first, second []*kJob
		err := db.Transaction(ctx, func(tx *orm.DB) error {
			first, err = kJobs.With(tx).Where(kJobs.State.Equals("queued")).
				OrderBy(kJobs.ID.Asc()).ForUpdate().SkipLocked().Limit(2).All(ctx)
			if err != nil {
				return err
			}
			// Still inside the first transaction, so its two rows are locked.
			return otherDB.Transaction(ctx, func(tx2 *orm.DB) error {
				second, err = kJobs.With(tx2).Where(kJobs.State.Equals("queued")).
					OrderBy(kJobs.ID.Asc()).ForUpdate().SkipLocked().Limit(2).All(ctx)
				return err
			})
		})
		if err != nil {
			t.Fatalf("Transaction() error = %v", err)
		}
		if len(first) != 2 || len(second) != 2 {
			t.Fatalf("claimed %d and %d rows, want 2 each", len(first), len(second))
		}
		taken := map[int]bool{}
		for _, j := range append(append([]*kJob{}, first...), second...) {
			if taken[j.ID] {
				t.Errorf("job %d was claimed twice", j.ID)
			}
			taken[j.ID] = true
		}
		if len(taken) != 4 {
			t.Errorf("claimed %d distinct jobs, want all 4", len(taken))
		}
	})

	// With nothing left to skip to, the second reader gets nothing rather than
	// waiting for the first to finish.
	t.Run("skip locked returns fewer rows rather than waiting", func(t *testing.T) {
		seed(t, 2)

		var second []*kJob
		err := db.Transaction(ctx, func(tx *orm.DB) error {
			if _, err := kJobs.With(tx).Where(kJobs.State.Equals("queued")).
				ForUpdate().SkipLocked().All(ctx); err != nil {
				return err
			}
			return otherDB.Transaction(ctx, func(tx2 *orm.DB) error {
				second, err = kJobs.With(tx2).Where(kJobs.State.Equals("queued")).
					ForUpdate().SkipLocked().All(ctx)
				return err
			})
		})
		if err != nil {
			t.Fatalf("Transaction() error = %v", err)
		}
		if len(second) != 0 {
			t.Errorf("the second reader took %d rows, want none left to take", len(second))
		}
	})

	t.Run("no wait fails instead of waiting", func(t *testing.T) {
		seed(t, 1)

		err := db.Transaction(ctx, func(tx *orm.DB) error {
			if _, err := kJobs.With(tx).Where(kJobs.State.Equals("queued")).
				ForUpdate().All(ctx); err != nil {
				return err
			}
			inner := otherDB.Transaction(ctx, func(tx2 *orm.DB) error {
				_, err := kJobs.With(tx2).Where(kJobs.State.Equals("queued")).
					ForUpdate().NoWait().All(ctx)
				return err
			})
			if inner == nil {
				return errors.New("the second reader succeeded, want it to refuse to wait")
			}
			return nil
		})
		if err != nil {
			t.Fatalf("Transaction() error = %v", err)
		}
	})

	// The plain form is the one that does wait, which is only observable by
	// giving it a deadline it cannot meet.
	t.Run("for update makes the second reader wait", func(t *testing.T) {
		seed(t, 1)

		err := db.Transaction(ctx, func(tx *orm.DB) error {
			if _, err := kJobs.With(tx).Where(kJobs.State.Equals("queued")).
				ForUpdate().All(ctx); err != nil {
				return err
			}
			waiting, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			defer cancel()

			inner := otherDB.Transaction(waiting, func(tx2 *orm.DB) error {
				_, err := kJobs.With(tx2).Where(kJobs.State.Equals("queued")).
					ForUpdate().All(waiting)
				return err
			})
			if !errors.Is(inner, context.DeadlineExceeded) {
				return fmt.Errorf("the second reader returned %v, want it still waiting "+
					"when the deadline passed", inner)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("Transaction() error = %v", err)
		}
	})

	// A share lock lets another reader take the same lock, which is the whole
	// difference between the two modes.
	t.Run("for share lets a second reader in", func(t *testing.T) {
		seed(t, 1)

		var second []*kJob
		err := db.Transaction(ctx, func(tx *orm.DB) error {
			if _, err := kJobs.With(tx).Where(kJobs.State.Equals("queued")).
				ForShare().All(ctx); err != nil {
				return err
			}
			return otherDB.Transaction(ctx, func(tx2 *orm.DB) error {
				second, err = kJobs.With(tx2).Where(kJobs.State.Equals("queued")).
					ForShare().All(ctx)
				return err
			})
		})
		if err != nil {
			t.Fatalf("Transaction() error = %v", err)
		}
		if len(second) != 1 {
			t.Errorf("the second reader took %d rows, want the same 1", len(second))
		}
	})

	// The whole thing under real concurrency: eight workers racing for forty
	// jobs must between them claim each one exactly once.
	t.Run("concurrent workers claim disjoint batches", func(t *testing.T) {
		const (
			jobs    = 40
			workers = 8
			batch   = 5
		)
		seed(t, jobs)

		var (
			mu      sync.Mutex
			claimed = map[int]string{}
			fail    error
		)
		var wg sync.WaitGroup
		for w := range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				name := fmt.Sprintf("worker-%d", w)
				err := db.Transaction(ctx, func(tx *orm.DB) error {
					mine, err := kJobs.With(tx).Where(kJobs.State.Equals("queued")).
						ForUpdate().SkipLocked().Limit(batch).All(ctx)
					if err != nil {
						return err
					}
					if len(mine) == 0 {
						return nil
					}
					ids := make([]int, len(mine))
					for i, j := range mine {
						ids[i] = j.ID
					}
					if _, err := kJobs.With(tx).Where(kJobs.ID.In(ids...)).
						UpdateAll(ctx, kJobs.State.Set("taken"), kJobs.Worker.Set(name)); err != nil {
						return err
					}
					mu.Lock()
					defer mu.Unlock()
					for _, id := range ids {
						if other, seen := claimed[id]; seen {
							return fmt.Errorf("job %d claimed by both %s and %s", id, other, name)
						}
						claimed[id] = name
					}
					return nil
				})
				if err != nil {
					mu.Lock()
					if fail == nil {
						fail = err
					}
					mu.Unlock()
				}
			}()
		}
		wg.Wait()

		if fail != nil {
			t.Fatalf("a worker failed: %v", fail)
		}
		if len(claimed) != jobs {
			t.Errorf("claimed %d of %d jobs, want every one taken exactly once",
				len(claimed), jobs)
		}
		left, err := kJobs.With(db).Where(kJobs.State.Equals("queued")).Count(ctx)
		if err != nil {
			t.Fatalf("Count() error = %v", err)
		}
		if left != 0 {
			t.Errorf("%d jobs are still queued, want none", left)
		}
	})
}
