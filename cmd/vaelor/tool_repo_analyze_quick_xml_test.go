package main

import (
	"fmt"
	"strings"
	"testing"
)

// preMigrationQuickInline reproduces, verbatim, the hand-rolled quick-local XML
// the pre-migration handleLocalQuickMode emitted inline. The handler did
// filesystem I/O and could not be called directly, so this self-contained
// baseline (a faithful copy of the removed fmt.Fprintf block) stands in for a
// recorded golden.
func preMigrationQuickInline(repoName, tree, readme string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<response><quick repo=%q type=\"local\">", repoName)
	fmt.Fprintf(&sb, "<tree><![CDATA[%s]]></tree>", strings.ReplaceAll(tree, "]]>", "]]]]><![CDATA[>"))
	if readme != "" {
		fmt.Fprintf(&sb, "<readme><![CDATA[%s]]></readme>", strings.ReplaceAll(readme, "]]>", "]]]]><![CDATA[>"))
	}
	sb.WriteString("</quick></response>")
	return sb.String()
}

// TestQuickLocal_StructurallyEquivalentToBaseline proves formatQuickLocal is
// structurally identical to the pre-migration inline formatter, covering the
// readme-present, readme-absent, and CDATA "]]>" split cases.
func TestQuickLocal_StructurallyEquivalentToBaseline(t *testing.T) {
	cases := []struct {
		name, repo, tree, readme string
	}{
		{"with_readme", benignQuickRepo, benignQuickTree, benignQuickReadme},
		{"no_readme", benignQuickRepo, benignQuickTree, ""},
		{"cdata_close_split", benignQuickRepo, quickTreeWithCDATAClose, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			current := preMigrationQuickInline(tc.repo, tc.tree, tc.readme)
			migrated := formatQuickLocal(tc.repo, tc.tree, tc.readme)
			assertXMLEquivalent(t, current, migrated)
		})
	}
}

// TestQuickLocal_BaselineHostileRepoIsMalformed documents the (rare) bug: the
// prior `repo=%q` attribute emitted a raw ampersand / quote, so a repo directory
// name carrying them produced malformed XML.
func TestQuickLocal_BaselineHostileRepoIsMalformed(t *testing.T) {
	assertNotWellFormed(t, preMigrationQuickInline(hostileQuickRepo, benignQuickTree, ""))
}

// TestQuickLocal_HostileRepoEscaped proves the fix: the migrated repo attribute
// round-trips to its exact value.
func TestQuickLocal_HostileRepoEscaped(t *testing.T) {
	migrated := formatQuickLocal(hostileQuickRepo, benignQuickTree, "")
	assertAttrRoundTrips(t, migrated, "response/quick", "repo", hostileQuickRepo)
}
