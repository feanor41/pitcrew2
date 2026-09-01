package store

import (
	"context"
	"testing"
)

func TestMigrationV8AddsEmptyDirectDeliveryInspectionsWithoutRewritingV7(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	legacy := openStoreAtMigration(t, root, 7)
	if _, err := legacy.DB().ExecContext(ctx, `INSERT INTO direct_delivery_traces(id,operation_key,route,goal,route_reason,status,summary,next_action,revision,creator_actor,updater_actor,created_at,updated_at,finished_at) VALUES('dl-111111111111111111111111','v8-preservation','direct_inline','preserve delivery','bounded','in_progress','kept','continue',4,'aion','aion','created','updated',NULL)`); err != nil {
		t.Fatal(err)
	}
	want := queryJoinedRow(t, legacy, `SELECT id,operation_key,route,goal,route_reason,status,summary,next_action,revision,creator_actor,updater_actor,created_at,updated_at,finished_at FROM direct_delivery_traces WHERE operation_key='v8-preservation'`)
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	migrated, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	if got := queryJoinedRow(t, migrated, `SELECT id,operation_key,route,goal,route_reason,status,summary,next_action,revision,creator_actor,updater_actor,created_at,updated_at,finished_at FROM direct_delivery_traces WHERE operation_key='v8-preservation'`); len(got) != len(want) {
		t.Fatalf("delivery row shape changed: got %v, want %v", got, want)
	} else {
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("delivery row changed: got %v, want %v", got, want)
			}
		}
	}
	var count int
	if err := migrated.DB().QueryRow(`SELECT count(*) FROM direct_delivery_inspections`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("inspection rows = %d, %v; want 0", count, err)
	}
}
