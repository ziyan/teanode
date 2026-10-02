#!/usr/bin/env python3
"""Sum up scenario reports of MemoryAgentBench conflict-resolution histories.

Usage:
    memoryagentbench-summary.py <scenario-directory> <runs-directory> [report.json]

The scenario directory is what memoryagentbench-scenarios.py wrote; the
runs directory holds one report directory per history, as
`teanode-server evaluate scenario --output <runs>` leaves them for several
scenarios.

Two scores per history and answer source: TeaNode's grader against the
reference answer (correct is one, partial a half), and the benchmark's
own measure, whether any reference answer appears in the answer text,
ignoring case. The second is what published results report; the first
is the same grader every other TeaNode evaluation uses.
"""

import collections
import json
import os
import sys

GRADED = {"correct": 1.0, "partial": 0.5}


def contains_reference(answer_text, expected_answer):
    answer_text = answer_text.lower()
    return any(reference.strip().lower() in answer_text for reference in expected_answer.split(" or ") if reference.strip())


def main():
    scenario_directory, runs_directory = sys.argv[1], sys.argv[2]
    report_name = sys.argv[3] if len(sys.argv) > 3 else "report.json"
    rows = []
    for source in sorted(os.listdir(runs_directory)):
        path = os.path.join(runs_directory, source, report_name)
        if not os.path.exists(path):
            continue
        with open(path) as report_file:
            report = json.load(report_file)
        steps = report["steps"] if "steps" in report else [report]
        graded = collections.defaultdict(list)
        matched = collections.defaultdict(list)
        expected = {}
        scenario_path = os.path.join(scenario_directory, source + ".json")
        if os.path.exists(scenario_path):
            with open(scenario_path) as scenario_file:
                for step in json.load(scenario_file)["steps"]:
                    for question in step.get("questions") or []:
                        expected[question["id"]] = question["expectedAnswer"]
        for step in steps:
            for question in step.get("questions") or []:
                for answer in question["answers"]:
                    graded[answer["answerFrom"]].append(GRADED.get(answer["answerVerdict"], 0.0))
                    if question["id"] in expected:
                        matched[answer["answerFrom"]].append(1.0 if contains_reference(answer["answerText"], expected[question["id"]]) else 0.0)
        facts = (report.get("graphCounts") or {}).get("factCount", 0)
        corrected = (report.get("graphCounts") or {}).get("supersededFactCount", 0)
        for answer_from in sorted(graded):
            scores = graded[answer_from]
            matches = matched.get(answer_from) or []
            rows.append((source, answer_from, len(scores), 100 * sum(scores) / len(scores),
                         (100 * sum(matches) / len(matches)) if matches else None, facts, corrected))
    print("| History | Answer from | n | TeaNode grader | Reference in answer | Facts | Superseded |")
    print("| --- | --- | ---: | ---: | ---: | ---: | ---: |")
    for source, answer_from, count, graded_score, matched_score, facts, corrected in rows:
        matched_text = f"{matched_score:.0f}%" if matched_score is not None else "-"
        print(f"| {source} | {answer_from} | {count} | {graded_score:.0f}% | {matched_text} | {facts} | {corrected} |")


if __name__ == "__main__":
    main()
