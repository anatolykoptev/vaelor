# harshkedia177/axon — Blast Radius + Dead Code

- **Repo**: [harshkedia177/axon](https://github.com/harshkedia177/axon) | 384 stars | Python
- **Approach**: 12-phase pipeline → KuzuDB graph

## What It Is

Code analysis MCP with blast radius confidence scores, dead code detection, and git change coupling.

## Key Features

- **Blast radius with confidence scores**: each impact item has confidence level
- **Dead code detection**: multi-pass with framework awareness (don't flag handler functions)
- **Git change coupling**: files frequently changed together → implicit dependencies
- **12-phase pipeline**: structured analysis stages

## What's Relevant for Vaelor

| Pattern | Our Phase |
|---------|-----------|
| Blast radius with confidence | Phase 9.2 |
| Dead code multi-pass | Phase 9.3 |
| Framework awareness (don't flag handlers) | Phase 9.3 |
| Git change coupling | Phase 9.1 (hotspot churn) |
