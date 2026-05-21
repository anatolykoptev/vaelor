// Package pinned extracts container image references from indexed repo files.
//
// It parses Dockerfile and docker-compose*.yml to find pinned image:tag@digest
// references and represents them as PinnedImage values suitable for diffing
// against runtime probe results (see internal/fleet).
//
// Out of MVP scope: Helm values, raw Kubernetes manifests, GHA `uses: docker://`,
// Terraform, Ansible, lockfile-based base-image inference. The PinnedImage type
// is designed so additional parsers slot in without schema change.
package pinned

// PinnedImage is one pinned container image reference resolved from a source
// file in the indexed repo.
//
// Tag may be empty when the image is pinned by digest only, or when the source
// expression could not be resolved (in which case Unresolved is non-empty and
// describes why).
type PinnedImage struct {
	// Image is the registry+repository, without tag or digest.
	// Example: "minio/minio", "ghcr.io/anatolykoptev/go-code", "redis".
	Image string

	// Tag is the resolved image tag. Empty when pinned by digest only,
	// or when the expression could not be statically resolved.
	Tag string

	// Digest, when non-empty, is the sha256 digest the image is pinned to.
	// Always begins with "sha256:".
	Digest string

	// Source is the repo-relative path of the file this image came from.
	Source string

	// Line is the 1-based line number in Source where the reference was found.
	Line int

	// Service is the compose service name for compose-derived images.
	// For Dockerfile-derived images, it is the AS-stage name (or "" if unnamed),
	// suffixed with ":builder" for non-final stages of a multi-stage build.
	Service string

	// Unresolved, when non-empty, explains why Tag could not be resolved.
	// The image is still emitted so diff layers can show partial information.
	Unresolved string
}
