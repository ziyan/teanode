#!/usr/bin/env python3
"""Sum up scenario reports of LongMemEval instances.

Usage:
    longmemeval-summary.py <scenario-directory> <runs-directory> [report.json]

The third argument names the file read in each run's directory: report.json
by default, or a file `teanode-server evaluate ask-again` wrote, such as
again-recall-2400.json, which holds the last checkpoint asked again.

The scenario directory is what longmemeval-scenarios.py wrote; the runs
directory holds one report directory per question id, as
`teanode-server evaluate scenario --output <runs>/<id>` leaves them.
Scores count a correct answer, and a correct "not known" to an abstention
question, as one and a partial answer as a half, per source and per
question type. They are TeaNode's grader's verdicts against the reference
answer, not the benchmark's official judge, and say so.
"""

import collections
import glob
import json
import os
import sys

SCORE = {"correct": 1.0, "not_known": 1.0, "partial": 0.5}


def main():
    scenario_directory, runs_directory = sys.argv[1], sys.argv[2]
    report_name = sys.argv[3] if len(sys.argv) > 3 else "report.json"
    types = {}
    for path in glob.glob(os.path.join(scenario_directory, "*.json")):
        with open(path) as scenario_file:
            scenario = json.load(scenario_file)
        question_id = os.path.basename(path)[: -len(".json")]
        suffix = "_abs" if question_id.endswith("_abs") else ""
        types[question_id] = scenario.get("questionType", "?") + suffix

    totals = collections.defaultdict(lambda: collections.defaultdict(list))
    costs, seconds, facts, missing = [], [], [], []
    for question_id, question_type in sorted(types.items()):
        path = os.path.join(runs_directory, question_id, report_name)
        if not os.path.exists(path):
            missing.append(question_id)
            continue
        with open(path) as report_file:
            report = json.load(report_file)
        # A run's report holds its steps; a checkpoint asked again is one step.
        steps = report["steps"] if "steps" in report else [report]
        costs.append(report.get("totalCost", report.get("cost", 0)))
        seconds.append(sum(step["durationMS"] for step in steps) / 1000)
        if report.get("graphCounts"):
            facts.append(report["graphCounts"]["factCount"])
        for step in steps:
            for question in step.get("questions") or []:
                for answer in question["answers"]:
                    score = SCORE.get(answer["answerVerdict"], 0.0)
                    totals[answer["answerFrom"]][question_type].append(score)
                    totals[answer["answerFrom"]]["all"].append(score)

    sources = sorted(totals)
    kinds = sorted({kind for source in sources for kind in totals[source]} - {"all"}) + ["all"]
    print("Scores are TeaNode's grader against the reference answer, not the official judge.\n")
    print("| Question type | n | " + " | ".join(sources) + " |")
    print("| --- | ---: | " + " | ".join("---:" for _ in sources) + " |")
    for kind in kinds:
        count = len(totals[sources[0]][kind]) if sources else 0
        cells = []
        for source in sources:
            scores = totals[source][kind]
            cells.append(f"{100 * sum(scores) / len(scores):.0f}%" if scores else "-")
        print(f"| {kind} | {count} | " + " | ".join(cells) + " |")
    if costs:
        print(f"\n{len(costs)} instances; cost {sum(costs):.2f} dollars in all, {sum(costs) / len(costs):.3f} each;"
              f" {sum(seconds) / len(seconds):.0f} seconds each; {sum(facts) / max(1, len(facts)):.0f} facts each.")
    if missing:
        print(f"\nNo report for {len(missing)}: " + ", ".join(missing))


if __name__ == "__main__":
    main()
