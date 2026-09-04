# Compact page storage decision review

## Decision

Make `core.Frame` the semantic facade over one private page. Store ordinary cells as a fixed-width record containing a rune, a page-local style ID, and flags. Keep canonical styles in a page-local dictionary with ID zero permanently assigned to `DefaultStyle()`. Keep logical row-to-cell offsets inside the page. All reads decode a semantic `Cell`; all writes intern or remap styles through page methods.

## Critic pass

| Objection | Impact | Resolution/status |
| --- | --- | --- |
| A style dictionary that only appends grows without bound under repeated overwrite churn. | High | Track references, remove zero-reference non-default styles, and reuse freed IDs. Test churn remains bounded by live cells plus the default entry. |
| Copying raw stored cells between frames would leak page-local IDs. | High | Cross-frame operations accept semantic cells and intern them in the destination. `Clone` and `Replace` decode/re-encode rather than copying IDs. Add cross-page tests. |
| Reinitializing row offsets during aliased replacement can corrupt a scrolled source. | High | Preserve the alias detection fixed in PR #22; clone the semantic source before resetting destination storage. Keep the self-replacement regression test. |
| Go `int` fields make logical-byte accounting architecture-dependent. | High | Account only fixed-width stored cells, row descriptors, IDs, flags, and an explicitly defined semantic style-record size. Do not use `unsafe.Sizeof`. |
| Narrow IDs can overflow on adversarial high-cardinality pages. | High | Use `uint32` IDs and reject impossible page dimensions before allocation. Page style count is bounded by its live cell count plus the default style. |
| Returning dictionary IDs or backing rows would recreate mutation and ownership leaks. | High | Keep IDs, dictionaries, and stored cells private. Preserve only `CellSource`, owned `Row`, and explicit mutation methods. |
| Refcount maintenance can make scroll/fill paths quadratic. | Medium | Release/intern once per touched cell; row rotation remains O(rows), while recycling a row remains O(columns), matching the required blanking work. Benchmark full scrolling and style churn. |
| Canonicalization can accidentally collapse `Style{}` into `DefaultStyle()`. | High | Key every non-default style with `Style.Canonical()` and special-case ID zero only when the canonical value equals `DefaultStyle()`. Retain existing semantic property tests. |

## Accepted risks

- The first compact page uses ordinary Go slices and maps. Pooling, mmap, compression, and exceptional arenas remain excluded until later measured issues.
- Public constructors still panic for impossible allocation sizes, matching current behavior; internal size arithmetic must nevertheless reject integer overflow before multiplication.

## Evidence required before merge

- Stored-cell target of 8–16 logical bytes.
- Canonical-equivalent styles share an ID while zero and default styles remain distinct.
- Style churn is bounded after overwrites and clears.
- Scroll, clone, self-replace, and cross-page copy preserve semantic cells and wide continuation flags.
- Deterministic logical-byte accounting and invariant checks pass.
- Full suite, race detector, vet, golangci-lint, retained-memory benchmark, and focused mutation benchmarks pass.
