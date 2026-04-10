package compare

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/routes"
)

func TestComputeRouteDiff(t *testing.T) {
	a := []routes.Route{
		{Method: "GET", Path: "/api/users", Handler: "GetUsers"},
		{Method: "POST", Path: "/api/users", Handler: "CreateUser"},
	}
	b := []routes.Route{
		{Method: "GET", Path: "/api/users", Handler: "ListUsers"},
		{Method: "DELETE", Path: "/api/users/:id", Handler: "DeleteUser"},
	}

	diff := ComputeRouteDiff(a, b)

	if diff.Common != 1 {
		t.Errorf("Common: want 1, got %d", diff.Common)
	}
	if diff.OnlyACount != 1 {
		t.Errorf("OnlyACount: want 1, got %d", diff.OnlyACount)
	}
	if diff.OnlyBCount != 1 {
		t.Errorf("OnlyBCount: want 1, got %d", diff.OnlyBCount)
	}

	if len(diff.OnlyA) != 1 || diff.OnlyA[0].Method != "POST" {
		t.Errorf("OnlyA: want POST /api/users, got %v", diff.OnlyA)
	}
	if len(diff.OnlyB) != 1 || diff.OnlyB[0].Method != "DELETE" {
		t.Errorf("OnlyB: want DELETE /api/users/:id, got %v", diff.OnlyB)
	}
}

func TestComputeRouteDiff_Empty(t *testing.T) {
	diff := ComputeRouteDiff(nil, nil)

	if diff.Common != 0 {
		t.Errorf("Common: want 0, got %d", diff.Common)
	}
	if diff.OnlyACount != 0 {
		t.Errorf("OnlyACount: want 0, got %d", diff.OnlyACount)
	}
	if diff.OnlyBCount != 0 {
		t.Errorf("OnlyBCount: want 0, got %d", diff.OnlyBCount)
	}
	if diff.OnlyA != nil {
		t.Errorf("OnlyA: want nil, got %v", diff.OnlyA)
	}
	if diff.OnlyB != nil {
		t.Errorf("OnlyB: want nil, got %v", diff.OnlyB)
	}
}

func TestComputeRouteDiff_NormalisedPaths(t *testing.T) {
	a := []routes.Route{
		{Method: "GET", Path: "/users/:id", Handler: "GetUser"},
	}
	b := []routes.Route{
		{Method: "GET", Path: "/users/{id}", Handler: "GetUser"},
	}

	diff := ComputeRouteDiff(a, b)

	// Both normalise to GET /users/* — should be common.
	if diff.Common != 1 {
		t.Errorf("Common: want 1, got %d (normalised paths should match)", diff.Common)
	}
	if diff.OnlyACount != 0 || diff.OnlyBCount != 0 {
		t.Errorf("want no exclusive routes, got onlyA=%d onlyB=%d", diff.OnlyACount, diff.OnlyBCount)
	}
}

func TestComputeRouteDiff_AllOnlyA(t *testing.T) {
	a := []routes.Route{
		{Method: "GET", Path: "/ping"},
		{Method: "POST", Path: "/ping"},
	}

	diff := ComputeRouteDiff(a, nil)

	if diff.Common != 0 || diff.OnlyACount != 2 || diff.OnlyBCount != 0 {
		t.Errorf("unexpected diff: %+v", diff)
	}
}

func TestComputeRouteDiff_AllOnlyB(t *testing.T) {
	b := []routes.Route{
		{Method: "PUT", Path: "/resource"},
	}

	diff := ComputeRouteDiff(nil, b)

	if diff.Common != 0 || diff.OnlyACount != 0 || diff.OnlyBCount != 1 {
		t.Errorf("unexpected diff: %+v", diff)
	}
}
