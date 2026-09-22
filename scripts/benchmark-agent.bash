#!/usr/bin/env bash
# Measure synthetic agent workloads against both supported database images.
set -euo pipefail

if [[ "$(uname -s)" != Linux ]]; then
    echo "this benchmark reports Linux peak RSS in KiB" >&2
    exit 1
fi
for commandName in go docker python3; do
    if ! command -v "$commandName" >/dev/null 2>&1; then
        echo "required benchmark command is missing: $commandName" >&2
        exit 1
    fi
done

repositoryDirectory="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
outputDirectory="${1:-$(mktemp -d)}"
mkdir -p "$outputDirectory"
outputDirectory="$(cd "$outputDirectory" && pwd)"
containerId=""
cleanup() {
    if [[ -n "$containerId" ]]; then
        docker kill "$containerId" >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT
cd "$repositoryDirectory"
go version > "$outputDirectory/toolchain.txt"
go test -mod=vendor -c -o "$outputDirectory/agent.test" ./internal/agent
for databaseImage in postgres:17 pgvector/pgvector:pg17; do
    if [[ "$databaseImage" == postgres:17 ]]; then
        imageLabel=standard
    else
        imageLabel=vector
    fi
    containerId="$(docker run -d --rm -e POSTGRES_USER=teanode -e POSTGRES_PASSWORD=teanode -e POSTGRES_DB=teanode "$databaseImage" -c shared_preload_libraries=pg_stat_statements)"
    isReady=false
    for attempt in {1..30}; do
        if docker exec "$containerId" pg_isready >/dev/null 2>&1; then
            isReady=true
            break
        fi
        sleep 1
    done
    if [[ "$isReady" != true ]]; then
        echo "benchmark database did not become ready" >&2
        exit 1
    fi
    export TEANODE_TEST_DATABASE_HOST
    TEANODE_TEST_DATABASE_HOST="$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$containerId")"
    docker exec "$containerId" postgres --version > "$outputDirectory/$imageLabel-version.txt"
    for workload in SourcePage/changed_25 SourcePage/changed_100 SourcePage/unchanged_100 GraphRetrieval/facts_100 GraphRetrieval/facts_1000; do
        workloadLabel="${workload//\//-}"
        benchmarkName="${workload%%/*}"
        caseName="${workload#*/}"
        GOMAXPROCS=2 python3 - "$outputDirectory/$imageLabel-$workloadLabel-memory.txt" \
            "$outputDirectory/agent.test" -test.run '^$' -test.bench "^Benchmark${benchmarkName}$/^${caseName}$" -test.benchtime=20x -test.count=3 -test.timeout=5m \
            > "$outputDirectory/$imageLabel-$workloadLabel.log" 2>&1 <<'PYTHON'
import resource
import subprocess
import sys

completed_process = subprocess.run(sys.argv[2:], check=False)
with open(sys.argv[1], "w", encoding="utf-8") as memory_file:
    memory_file.write(f"peak_rss_kib={resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss}\n")
sys.exit(completed_process.returncode)
PYTHON
        echo "$imageLabel $workload complete"
    done
    cleanup
    containerId=""
done
printf 'Benchmark artifacts: %s\n' "$outputDirectory"
