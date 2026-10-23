# Dependency licenses

Alivyn's source code is licensed under [Apache-2.0](../LICENSE). This file records the direct dependencies currently used by the local preview, based on their packaged license files.

| Direct dependency | Version | License | Use |
| --- | --- | --- | --- |
| `charm.land/bubbles/v2` | v2.2.1 | MIT | Textarea and scrollable history |
| `charm.land/bubbletea/v2` | v2.1.0 | MIT | Terminal UI runtime |
| `charm.land/lipgloss/v2` | v2.0.5 | MIT | Terminal styling and text wrapping |
| `modernc.org/sqlite` | v1.61.0 | BSD-3-Clause | Local conversation history |

The SQLite module includes additional upstream code and notices in its own `LICENSE-3RD-PARTY.md`, `LICENSE-SQLITE`, and `LICENSE-SQLITE_VEC` files. Before publishing downloadable binaries, generate and review a full dependency notice bundle for the exact release graph, including transitive packages. Keep the relevant license notices with distributed binaries.

Update this inventory when `go.mod` changes. The module versions and complete dependency graph are pinned in `go.mod` and `go.sum`.
