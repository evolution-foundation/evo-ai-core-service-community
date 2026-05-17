// Package evoextensions is the public extension contract of the
// evo-ai-core-service community release.
//
// It exposes a small set of typed interfaces that the community release
// calls into for cross-cutting concerns it knows it cannot answer on
// its own:
//
//   - [capability.Gate]       — capability gating
//   - [runtimecontext.Scope]  — runtime scope identity propagation
//   - [plugin.Registry]       — discovery of installed plugins
//
// Each sub-package ships a no-op default implementation so that the
// community release runs standalone. A consumer may swap the default
// at process start by providing its own implementation of the
// interface; consumers are external to this repository and never
// modify community source.
//
// Backward compatibility, deprecation window, and SemVer rules are
// documented in EXTENSION_POINTS.md at the repository root.
package evoextensions
