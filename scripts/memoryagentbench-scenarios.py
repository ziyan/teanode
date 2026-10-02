#!/usr/bin/env python3
"""Turn MemoryAgentBench conflict-resolution histories into memory scenarios.

The Conflict_Resolution split is FactConsolidation: a numbered list of
facts in which a later fact may contradict an earlier one, and questions,
single-hop or multi-hop, whose answers follow the later facts. Each
history becomes one scenario of its own, so no state is shared between
histories: its facts filed in order as notes of the person's, a few at a
time and a day apart, so a correction is newer than what it corrects;
dreams until nothing waits to be read; then its questions. The reference
answers are the expected answers and never enter the records.

The benchmark tells the model that where facts conflict the later one
holds; each question carries the same sentence, so the comparison is with
what the benchmark asks of any memory agent.

The split is published as parquet. Convert it to JSON lines first, one
history a line with its context, questions, answers and metadata, for
example in a container with pyarrow:

    python -c 'import json, pyarrow.parquet as pq
    [print(json.dumps(row, default=str))
     for row in pq.read_table("Conflict_Resolution.parquet").to_pylist()]'

Usage:
    memoryagentbench-scenarios.py <conflict_resolution.jsonl> <output-directory>
        [--sources factconsolidation_sh_6k,factconsolidation_mh_6k]
        [--questions N] [--facts-per-record N]
"""

import argparse
import datetime
import json
import os
import re
import sys

FACT_LINE = re.compile(r"^\s*(\d+)\.\s+(.*\S)\s*$")
STARTED = datetime.datetime(2030, 1, 1, 9, 0, tzinfo=datetime.timezone.utc)
CONFLICT_RULE = "Where facts conflict, the newer one holds."


def source_of(row):
    metadata = row["metadata"]
    if isinstance(metadata, str):
        metadata = json.loads(metadata)
    return metadata["source"]


def facts_of(context):
    facts = []
    for line in context.splitlines():
        found = FACT_LINE.match(line)
        if found:
            facts.append((int(found.group(1)), found.group(2)))
    facts.sort()
    return facts


def scenario_for(row, question_count, facts_per_record):
    source = source_of(row)
    facts = facts_of(row["context"])
    records = []
    for start in range(0, len(facts), facts_per_record):
        part = facts[start:start + facts_per_record]
        number = start // facts_per_record + 1
        records.append({
            "id": f"facts-{number:03d}",
            "kind": "note",
            "at": (STARTED + datetime.timedelta(days=number - 1)).strftime("%Y-%m-%dT%H:%M:%SZ"),
            "author": "@you",
            "title": f"Facts, part {number}",
            "text": "\n".join(text for _, text in part),
        })
    questions = []
    pairs = list(zip(row["questions"], row["answers"]))
    if question_count:
        pairs = pairs[:question_count]
    for number, (question, answers) in enumerate(pairs, 1):
        if isinstance(answers, str):
            answers = [answers]
        questions.append({
            "id": f"{source}-{number:03d}",
            "question": f"({CONFLICT_RULE}) {question}",
            "kind": "changed",
            "expects": [],
            "forbids": [],
            "expectedAnswer": " or ".join(str(answer) for answer in answers),
        })
    return {
        "name": "memoryagentbench " + source,
        "description": f"MemoryAgentBench conflict resolution, {source}: {len(facts)} facts in {len(records)} notes a day apart.",
        "questionType": source,
        "steps": [
            {"id": "facts", "stepKind": "records", "records": records},
            {"id": "dream", "stepKind": "dream", "dreamCount": 1, "isUntilRead": True},
            {"id": "questions", "stepKind": "checkpoint", "questions": questions},
        ],
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("dataset")
    parser.add_argument("output")
    parser.add_argument("--sources", default="factconsolidation_sh_6k,factconsolidation_mh_6k")
    parser.add_argument("--questions", type=int, default=0)
    parser.add_argument("--facts-per-record", type=int, default=20)
    arguments = parser.parse_args()

    wanted = [source.strip() for source in arguments.sources.split(",") if source.strip()]
    os.makedirs(arguments.output, exist_ok=True)
    written = 0
    with open(arguments.dataset) as dataset_file:
        for line in dataset_file:
            row = json.loads(line)
            source = source_of(row)
            if source not in wanted:
                continue
            with open(os.path.join(arguments.output, source + ".json"), "w") as scenario_file:
                json.dump(scenario_for(row, arguments.questions, arguments.facts_per_record), scenario_file, indent=1, ensure_ascii=False)
            written += 1
    print(f"{written} scenarios in {arguments.output}", file=sys.stderr)


if __name__ == "__main__":
    main()
