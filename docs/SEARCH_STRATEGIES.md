# Tree and search strategies

`panelBackend.Browse` opens an operation-scoped `browseStrategy`. Local folders,
FTP/SFTP sessions, archive indexes, Kubernetes catalogs and Docker catalogs each
implement listing, content access, descent policy and cleanup. The search walker
and tree view consume this interface; they do not call local filesystem APIs on
virtual paths. Filesystem panels inside an explicitly entered container use the
remote filesystem strategy, rather than its parent catalog strategy.

## Boundaries

- Kubernetes catalog searches traverse namespaces, resource types and object
  ownership. Pods are results, but never traversal roots during recursion. The
  guard runs before path resolution, which could otherwise fetch a pod summary
  or transparently enter a single-container pod. Content search on resources
  explicitly requests their descriptions; it never searches container files.
- Docker catalogs traverse categories and Compose projects. Containers and
  volumes are results, not implicit filesystem traversal roots. Searching from
  an already opened container or volume is an explicit filesystem search.
- macOS local traversal snapshots the kernel mount table with `MNT_NOWAIT`.
  Non-local mount points are visible results but are not stat'ed or entered.
  A remote mount explicitly selected as the search root is allowed; nested
  non-local mounts remain boundaries.
- Where PodToast provides mount metadata, known network filesystem mount points
  are also boundaries. Plain FTP/SFTP servers generally do not expose mount
  tables; unknown server-side mount boundaries cannot be inferred reliably.
- Symbolic links are never followed recursively. Archive files are not entered
  from outside; an already opened archive supports name searches in its index.
  Content search inside archives is explicitly unavailable (extract first).

## Budgets and cancellation

Search is bounded to 64 levels, 1,000,000 visited entries and 10,000 matches.
Tree view retains its configurable depth and 50,000-node / 1,000,000-entry limits.
Results identify skipped boundaries, unreadable locations and exhausted budgets.
Local reads are streamed in batches. Remote directory listings use the backend's
existing listing API and reject lists over 100,000 entries. Matching file content
is read incrementally with cancellation checks. Esc cancels the operation and
its network context; operation-scoped sessions are closed on completion.

Tests use synthetic files, injected mount boundaries and a fake Kubernetes API.
They verify that forbidden children are never requested, limits stop traversal,
non-recursive search reads only its root, and canceled searches do not read data.

## Kubernetes ownership results

Collection visits each resource through its namespace/type catalog once, without
following secondary ownership and Service-selector paths or entering Pod filesystems.
An explicitly chosen subtree still defines the matching scope.

The default result view is a flat list in discovery order. F5 toggles an optional
ownership tree, built in a separate cancellable worker from the snapshots already
received. Search continues while the tree is displayed. Only matched objects and their ancestors are included. Shared
ancestors are merged, and each UID appears once across the entire result forest.
A matching ancestor is the same row as its context occurrence, not a second result.
The controller owner takes precedence; without one, available owner UIDs provide
a deterministic tie-break. Selector relationships are not ownership edges.

Context-only ancestors are marked `[owner]` and do not increase the match count.
Missing owners leave the matching object visible as a root. Cycles are cut;
ancestor chains are limited to 64 levels and context nodes to 50,000. A limit
is reported without dropping matched objects. No extra network requests are
made just to reconstruct ownership. Navigation uses each object's real catalog
path, independently of its indentation in the results.

F3 opens the manifest of a matched object or context ancestor. Closing the
viewer returns to the same list, preserving selection, scrolling and filter.
Enter goes to the object's parent catalog and selects it.

## Large hierarchy regression tests

`internal/ui/search_scale_test.go` covers 100,000 objects with 10,000 matches,
a 2,000-object fully matched owner chain, a 60,000-object chain that exhausts
the context budget, cancellation during tree construction, and navigation and
filtering in a 10,001-row result list. Rendering is iterative; every 64 levels
the indentation restarts with a `[continued]` marker, preserving matched nodes
without unbounded label growth. Catalog traversal builds one owner index per
snapshot instead of scanning all objects for each resource.

Synthetic benchmarks on Apple M3 Pro (darwin/arm64):

| Operation | Time | Allocated bytes per operation |
| --- | ---: | ---: |
| Catalog listing, 10,000 objects | 3.38 ms | 2.89 MiB |
| Catalog listing, 100,000 objects | 41.03 ms | 25.54 MiB |
| Ownership results, 100,000 objects / 10,000 matches | 44.11 ms | 31.85 MiB |
| Draw visible portion of 10,001 results, 120 × 40 cells | 1.48 ms | 177 KiB |

These measure already-loaded snapshots, excluding fixture construction and
network requests. Allocated bytes are cumulative allocations, not peak memory
or retained heap. Catalog and ownership measurements used three iterations;
drawing used ten. The previous 10,000-object catalog implementation took
1.19 seconds in a single baseline iteration. Cancellation returned within
9 microseconds in the measured test; timing varies by machine and scheduling.

Reproduce with `go test ./internal/ui -run 'TestOwnership(Large|Deep)'
-bench 'Benchmark(Ownership|KubeCatalog)' -benchmem` (as one shell command).

## Visual QA

Build the isolated native fixture with `go build -tags visualqa -o /tmp/searchqa ./cmd/searchvisualqa`.
It uses the production ownership formatter and result-list renderer with synthetic
200-level and 10,001-row trees, and an isolated temporary HOME. It does not connect
to clusters. Enter selects a scenario; Esc returns to the scenario menu. F3/backend
navigation are covered by integration tests, not this presentation fixture.

Manual visual checks cover Page Down, End, type-ahead filtering and returning from
the filtered list. Deep indentation is compressed to `…(+N)` (N hidden ancestor
levels), keeping the object label visible. Long labels end with an ellipsis.
Search results show the current row/visible count, a proportional scroll thumb,
and the F3/Enter/Esc hint. The original labels and object references are unchanged.

## Progressive results

Search has an independent session, not a blocking `backgroundTask`. A bounded
128-event channel streams matches to the engine queue; each poll drains at most
256 events. It retains at most 10,000 matches. Slow traversal or content reads do
not prevent browsing results already delivered. Backend listing/snapshot calls
may themselves need to finish before they can yield their first entry.

- F3 opens a result while collection continues. Closing the viewer restores the
  same list, query and selection, including results collected in the meantime.
- Enter cancels collection and navigates to the selected object.
- F2 stops collection and preserves partial results. Esc first clears an active
  filter; otherwise it stops an active search, then closes a stopped result list.
- F5 toggles list/tree for Kubernetes catalogs. Tree construction uses immutable
  snapshots, does not query the server, preserves selected object identity and
  refreshes at most four times per second. Switching back restores only matches.
- Errors preserve partial results and are reported in the status line. Leaving
  the result list or closing the window cancels workers and discards late events.

Regression tests use a gated strategy to prove that F3 works before traversal
finishes, arrivals do not replace the viewer or lose the filter, and stop,
navigation and window close release the strategy without resurrecting dialogs.
The visual QA executable also includes a deliberately slow 1,000-file search.
