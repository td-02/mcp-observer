package cmd

import (
	"encoding/json"
	"testing"
)

func TestCompareSnapshotsNoChanges(t *testing.T) {
	t.Parallel()

	snapshot := snapshotOutput{
		Tools: []snapshotTool{
			testTool("alpha", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
				"required": []string{"name"},
			}),
		},
	}

	diff := compareSnapshots(snapshot, snapshot)
	if len(diff.Added) != 0 || len(diff.Removed) != 0 || len(diff.Changed) != 0 {
		t.Fatalf("expected no diff, got %+v", diff)
	}
}

func TestCompareSnapshotsAddedTool(t *testing.T) {
	t.Parallel()

	baseline := snapshotOutput{}
	current := snapshotOutput{Tools: []snapshotTool{testTool("alpha", map[string]any{"type": "object"})}}

	diff := compareSnapshots(baseline, current)
	if len(diff.Added) != 1 || diff.Added[0].Name != "alpha" {
		t.Fatalf("unexpected added diff: %+v", diff.Added)
	}
}

func TestCompareSnapshotsRemovedToolIsBreaking(t *testing.T) {
	t.Parallel()

	baseline := snapshotOutput{Tools: []snapshotTool{testTool("alpha", map[string]any{"type": "object"})}}
	current := snapshotOutput{}

	diff := compareSnapshots(baseline, current)
	if len(diff.Removed) != 1 || diff.Removed[0].Name != "alpha" {
		t.Fatalf("unexpected removed diff: %+v", diff.Removed)
	}
	if !hasBreakingChanges(baseline, current, diff) {
		t.Fatalf("expected removed tool to be breaking")
	}
}

func TestCompareSnapshotsChangedSchemaFields(t *testing.T) {
	t.Parallel()

	baseline := snapshotOutput{
		Tools: []snapshotTool{
			testTool("alpha", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
					"age":  map[string]any{"type": "number"},
				},
				"required": []string{"name"},
			}),
		},
	}

	current := snapshotOutput{
		Tools: []snapshotTool{
			testTool("alpha", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":  map[string]any{"type": "number"},
					"email": map[string]any{"type": "string"},
				},
				"required": []string{"name"},
			}),
		},
	}

	diff := compareSnapshots(baseline, current)
	if len(diff.Changed) != 1 {
		t.Fatalf("expected one changed tool, got %+v", diff.Changed)
	}

	changed := diff.Changed[0]
	if len(changed.AddedFields) != 1 || changed.AddedFields[0] != "email" {
		t.Fatalf("unexpected added fields: %+v", changed.AddedFields)
	}
	if len(changed.RemovedFields) != 1 || changed.RemovedFields[0] != "age" {
		t.Fatalf("unexpected removed fields: %+v", changed.RemovedFields)
	}
	if len(changed.ChangedFields) != 1 || changed.ChangedFields[0] != "name" {
		t.Fatalf("unexpected changed fields: %+v", changed.ChangedFields)
	}
	if !hasBreakingChanges(baseline, current, diff) {
		t.Fatalf("expected required field type change to be breaking")
	}
}

func TestCompareSnapshotsAddedRequiredFieldIsBreaking(t *testing.T) {
	t.Parallel()

	baseline := snapshotOutput{
		Tools: []snapshotTool{
			testTool("alpha", map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			}),
		},
	}

	current := snapshotOutput{
		Tools: []snapshotTool{
			testTool("alpha", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":  map[string]any{"type": "string"},
					"email": map[string]any{"type": "string"},
				},
				"required": []string{"email"},
			}),
		},
	}

	diff := compareSnapshots(baseline, current)
	if len(diff.Changed) != 1 {
		t.Fatalf("expected one changed tool, got %+v", diff.Changed)
	}
	if !hasBreakingChanges(baseline, current, diff) {
		t.Fatalf("expected new required field to be breaking")
	}
}

func TestCompareSnapshotsOptionalFieldBecomesRequiredIsBreaking(t *testing.T) {
	t.Parallel()

	baseline := snapshotOutput{
		Tools: []snapshotTool{
			testTool("alpha", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			}),
		},
	}

	current := snapshotOutput{
		Tools: []snapshotTool{
			testTool("alpha", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
				"required": []string{"name"},
			}),
		},
	}

	diff := compareSnapshots(baseline, current)
	if len(diff.Changed) != 1 {
		t.Fatalf("expected one changed tool, got %+v", diff.Changed)
	}
	if got := diff.Changed[0].NewRequired; len(got) != 1 || got[0] != "name" {
		t.Fatalf("unexpected new required fields: %+v", got)
	}
	if !hasBreakingChanges(baseline, current, diff) {
		t.Fatalf("expected optional to required change to be breaking")
	}
}

func testTool(name string, schema map[string]any) snapshotTool {
	raw, _ := json.Marshal(schema)
	return snapshotTool{Name: name, InputSchema: raw}
}
