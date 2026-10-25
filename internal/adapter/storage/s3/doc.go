// Package s3 keeps a run's artifacts in an S3-compatible object store.
//
// It speaks the S3 protocol and nothing else, so the store behind it — MinIO,
// Cloudflare R2, a Railway bucket — is a matter of configuration. The one
// bucket and the one set of credentials are shared by every organization:
// what keeps their objects apart is the prefix of the key, which only
// domain.ArtifactKey builds.
package s3
