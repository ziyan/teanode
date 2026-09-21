# Agent page and retrieval performance baseline

This baseline measures the current refactored paths with synthetic inputs. It
is a starting point for later comparisons, not a before/after claim about the
original main branch. Production code is unchanged by the measurement harness.

## Reproduce

From the repository root on Linux, with Go, Docker and Python 3 installed:

    scripts/benchmark-agent.bash /tmp/agent-benchmark

The script compiles one test binary, starts disposable `postgres:17` and
`pgvector/pgvector:pg17` containers in sequence, and enables
`pg_stat_statements`. It exposes no database port and removes each container on
completion or failure. Each workload runs in a separate process with
`GOMAXPROCS=2`, three samples of twenty operations, and an unmeasured warmup.
The output directory contains the binary, toolchain/database versions, benchmark
logs and one peak-memory file per workload and image. Reuse the same fixtures,
Go version, process limit and sampling settings when comparing a change.

The SQL count excludes the statistics-reading query and fixture setup. It
includes transaction statements tracked by PostgreSQL. Time and Go allocations
cover the measured operation only. Peak RSS is the client test process's maximum
resident memory, including migration, fixture construction and benchmark
calibration. It excludes the PostgreSQL process and is not per-operation heap
usage. Each process has its own peak measurement so one workload cannot inherit
another workload's maximum.

## Workloads

`BenchmarkSourcePage` exercises `fileComputerPage` with an enabled archive
source. Each document has 64 repeated lines of synthetic text and no attachment
or repository metadata. A warmup creates the documents. Changed runs replace
hashes to exercise document/chunk persistence; unchanged runs exercise the
source checks and batched seen markers. Continuation, seen counts and written
document counts are checked on every operation.

`BenchmarkGraphRetrieval` exercises `nearestInGraphTo`, including vector search,
row materialization and result ordering. Ten facts belong to each page. Stored
32-dimensional vectors use a deterministic, clustered pattern with eleven
repeating variants. The query vector is precomputed. The vector image builds its
indexes before timing; both images analyze the corpus and warm retrieval before
measurement. Results must contain twenty facts and up to twenty pages. This
measures retrieval cost, not ranking quality, model-provider latency, prompt
construction, or end-to-end conversation latency.

## Recorded results

The run used Go 1.26.6 on Linux/amd64 and PostgreSQL 17.11 in both images. Standard
and vector images have different Debian bases. The table reports the median of
three samples; time ranges are the observed minimum and maximum, not confidence
intervals. Peak RSS covers the entire workload process as described above.

| Image | Workload | Median ms/op | Range ms/op | SQL/op | KiB allocated/op | Peak RSS MiB |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| Standard | Changed page, 25 documents | 37.347 | 36.692 to 38.467 | 278 | 2,591.1 | 37.4 |
| Standard | Changed page, 100 documents | 149.787 | 148.114 to 150.268 | 1,103 | 10,295.3 | 37.8 |
| Standard | Unchanged page, 100 documents | 1.698 | 1.648 to 1.702 | 7 | 90.9 | 37.1 |
| Standard | Retrieval, 100 facts | 1.050 | 0.945 to 1.360 | 6 | 344.4 | 38.1 |
| Standard | Retrieval, 1,000 facts | 5.864 | 5.424 to 6.186 | 6 | 2,656.6 | 40.1 |
| Vector | Changed page, 25 documents | 37.854 | 37.146 to 39.388 | 278 | 2,590.9 | 37.3 |
| Vector | Changed page, 100 documents | 150.638 | 148.392 to 151.452 | 1,103 | 10,295.2 | 39.6 |
| Vector | Unchanged page, 100 documents | 1.723 | 1.677 to 1.742 | 7 | 91.0 | 39.4 |
| Vector | Retrieval, 100 facts | 0.715 | 0.651 to 0.756 | 8 | 105.9 | 37.1 |
| Vector | Retrieval, 1,000 facts | 2.858 | 2.791 to 2.897 | 8 | 123.3 | 38.5 |

Changed-page SQL cost is `11 * documentCount + 3` for these single-chunk fixtures.
Time and allocations also grow roughly with page size. The unchanged path uses
seven statements for the whole page. Any proposal to batch changed writes must
retain the source-generation checks and replay guarantees; these measurements
alone do not justify removing them.

At 1,000 facts, application-side vector ranking allocates about 2.6 MiB per
operation compared with about 123 KiB for indexed retrieval in this fixture.
The standard path transfers candidates to Go, while the vector path selects
candidates in PostgreSQL. Query count alone therefore does not describe the
cost. Larger and more varied corpora, concurrent workloads, database peak memory,
source queue waits and complete model-request latency remain separate
measurements. No universal latency target is inferred from this local baseline.
