package main

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/argnorm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOrphanSweepInput_NonEmptyClosedSchema guards the argnorm interaction
// (#581b): OrphanSweepInput is NO LONGER struct{} — it now carries a json
// field (dry_run). The closed-empty-struct special-case must therefore NOT
// apply to it; instead jsonProperties must return a non-nil, non-empty
// accepted set containing "dry_run" (closed schema, dry_run is an ACCEPTED
// param, not stripped). The synthetic struct{} empty-struct path stays
// covered by internal/argnorm's own TestRegistry_ClosedEmptyStructNotOpen /
// TestJsonProperties_StructEmptyIsClosed (which use a local emptyStruct type,
// not OrphanSweepInput).
//
// Falsifiable: reverting OrphanSweepInput to struct{} makes len(props)==0 →
// the dry_run assertion fails. Removing the json tag makes props nil (open
// schema) → the non-nil assertion fails.
func TestOrphanSweepInput_NonEmptyClosedSchema(t *testing.T) {
	props, isStruct := argnorm.JsonProperty(reflect.TypeFor[OrphanSweepInput]())
	require.True(t, isStruct, "OrphanSweepInput is a struct")
	require.NotNil(t, props, "OrphanSweepInput must be a CLOSED schema (non-nil props), not open")
	require.NotEmpty(t, props, "OrphanSweepInput must have a non-empty accepted set now that it has dry_run")
	assert.Contains(t, props, "dry_run", "dry_run must be an ACCEPTED param")
}

// fakeOrphanSweepStore is an in-memory stand-in for the orphanSweepStore
// interface. It records call counts so tests can assert which path the
// handler took (preview vs. real delete) WITHOUT a live Postgres pool.
type fakeOrphanSweepStore struct {
	previewKeys  []string
	previewRows  int64
	previewErr   error
	previewCalls int

	countResult int64
	countErr    error
	countCalls  int

	deleteResult int64
	deleteErr    error
	deleteCalls  int
}

func (f *fakeOrphanSweepStore) PreviewOrphanRepoKeys(ctx context.Context) ([]string, int64, error) {
	f.previewCalls++
	return f.previewKeys, f.previewRows, f.previewErr
}

func (f *fakeOrphanSweepStore) CountOrphanRepoKeys(ctx context.Context) (int64, error) {
	f.countCalls++
	return f.countResult, f.countErr
}

func (f *fakeOrphanSweepStore) DeleteOrphanRepoKeys(ctx context.Context) (int64, error) {
	f.deleteCalls++
	return f.deleteResult, f.deleteErr
}

// TestHandleOrphanSweep_DefaultIsDryRun is the primary falsifiable guard for
// the safe-default gate: when DryRun is OMITTED (nil), the handler must take
// the preview path and must NOT call DeleteOrphanRepoKeys.
//
// Falsifiable: reverting the default to delete (e.g. `dry := in.DryRun != nil
// && *in.DryRun`, or dropping the gate so it always deletes) makes
// fake.deleteCalls == 1 → assert.Zero fails, and the response no longer
// contains "DRY RUN".
func TestHandleOrphanSweep_DefaultIsDryRun(t *testing.T) {
	fake := &fakeOrphanSweepStore{
		previewKeys: []string{"test/orphan-a", "test/orphan-b"},
		previewRows: 15076,
	}

	res, err := handleOrphanSweep(context.Background(), OrphanSweepInput{}, fake)
	require.NoError(t, err)
	require.False(t, res.IsError, "dry-run must not be an error result")

	assert.Equal(t, 1, fake.previewCalls, "default must invoke the preview path")
	assert.Zero(t, fake.deleteCalls, "default (DryRun omitted) must NOT call DeleteOrphanRepoKeys")

	text := textContentOf(t, res)
	assert.Contains(t, text, "DRY RUN", "default response must be a dry-run preview")
	assert.Contains(t, text, "orphan_repo_keys=2", "response must report the orphan key count")
	assert.Contains(t, text, "rows_that_would_be_deleted=15076", "response must report the would-be-deleted row count")
	assert.Contains(t, text, "dry_run=false", "response must tell the operator how to force a real delete")
}

// TestHandleOrphanSweep_ExplicitDryRunTrue verifies that an explicit
// dry_run=true takes the preview path (same as the default), with no mutation.
func TestHandleOrphanSweep_ExplicitDryRunTrue(t *testing.T) {
	dry := true
	fake := &fakeOrphanSweepStore{
		previewKeys: []string{"test/orphan-x"},
		previewRows: 42,
	}

	res, err := handleOrphanSweep(context.Background(), OrphanSweepInput{DryRun: &dry}, fake)
	require.NoError(t, err)
	require.False(t, res.IsError)

	assert.Equal(t, 1, fake.previewCalls, "dry_run=true must invoke the preview path")
	assert.Zero(t, fake.deleteCalls, "dry_run=true must NOT call DeleteOrphanRepoKeys")

	assert.Contains(t, textContentOf(t, res), "DRY RUN")
}

// TestHandleOrphanSweep_ExplicitDryRunFalseDeletes verifies that an explicit
// dry_run=false takes the real-delete path: DeleteOrphanRepoKeys is called,
// the response is the DELETED form, and the pre/post counts are reported.
//
// Falsifiable: reverting the gate so dry_run=false still previews makes
// fake.deleteCalls == 0 → assert.Equal(1, ...) fails.
func TestHandleOrphanSweep_ExplicitDryRunFalseDeletes(t *testing.T) {
	dry := false
	// countResult is returned twice (before + after the delete); use 9 then 0
	// by mutating on call would be over-engineering — the handler logs both
	// from the same CountOrphanRepoKeys. We assert the delete path is taken
	// and the response shape; the exact before/after numbers come from the
	// fake's countResult.
	fake := &fakeOrphanSweepStore{
		countResult:  9,
		deleteResult: 15076,
	}

	res, err := handleOrphanSweep(context.Background(), OrphanSweepInput{DryRun: &dry}, fake)
	require.NoError(t, err)
	require.False(t, res.IsError)

	assert.Equal(t, 1, fake.deleteCalls, "dry_run=false must call DeleteOrphanRepoKeys exactly once")
	assert.Equal(t, 2, fake.countCalls, "real-delete path must count before AND after the delete")
	assert.Zero(t, fake.previewCalls, "dry_run=false must NOT invoke the preview path")

	text := textContentOf(t, res)
	assert.Contains(t, text, "DELETED", "real-delete response must be the DELETED form")
	assert.Contains(t, text, "rows_deleted=15076")
}

// TestHandleOrphanSweep_DryRunPreviewErrorPropagates verifies that a preview
// error is returned (the handler must not swallow it and fall through to delete).
func TestHandleOrphanSweep_DryRunPreviewErrorPropagates(t *testing.T) {
	fake := &fakeOrphanSweepStore{
		previewErr: context.DeadlineExceeded,
	}

	_, err := handleOrphanSweep(context.Background(), OrphanSweepInput{}, fake)
	require.Error(t, err)
	assert.Zero(t, fake.deleteCalls, "a preview error must NOT trigger a delete")
	assert.True(t, strings.Contains(err.Error(), "preview"), "error must mention the preview step")
}
