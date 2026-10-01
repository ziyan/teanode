#!/usr/bin/env python3
"""Turn LongMemEval instances into memory scenarios.

Each instance becomes one scenario: its haystack sessions as chat threads
the person took part in, dated as the dataset dates them and filed in
order; dreams until nothing waits to be read; then its question, asked with the question's date the
way the benchmark gives it to a model. The reference answer is the
scenario's expected answer and never enters the records.

Usage:
    longmemeval-scenarios.py <longmemeval_s_cleaned.json> <output-directory>
        [--count N] [--balanced] [--seed S]

--balanced takes the same number of questions of each question type, so a
pilot covers extraction, multi-session reasoning, knowledge updates,
temporal reasoning and abstention alike. The chosen question ids are
written to <output-directory>/chosen.txt, so a later full run can report
the pilot's questions apart.
"""

import argparse
import collections
import datetime
import json
import os
import random
import sys


def parse_date(text):
    # "2023/05/20 (Sat) 02:21"
    stamp = text.split(" (")[0] + " " + text.split(") ")[-1]
    return datetime.datetime.strptime(stamp, "%Y/%m/%d %H:%M").replace(tzinfo=datetime.timezone.utc)


def scenario_for(instance):
    records_steps = []
    sessions = list(zip(instance["haystack_dates"], instance["haystack_session_ids"], instance["haystack_sessions"]))
    sessions.sort(key=lambda session: parse_date(session[0]))
    records = []
    for date_text, session_id, turns in sessions:
        started = parse_date(date_text)
        for number, turn in enumerate(turns):
            content = (turn.get("content") or "").strip()
            if not content:
                continue
            at = started + datetime.timedelta(seconds=30 * number)
            records.append({
                "id": f"{session_id}-{number}",
                "kind": "chat",
                "at": at.strftime("%Y-%m-%dT%H:%M:%SZ"),
                "author": "@you" if turn.get("role") == "user" else "assistant",
                "channel": "assistant",
                "thread": session_id,
                "text": content,
            })
    records_steps.append({"id": "history", "stepKind": "records", "records": records})
    question_date = parse_date(instance["question_date"]).strftime("%Y-%m-%d")
    is_abstention = instance["question_id"].endswith("_abs")
    question = {
        "id": instance["question_id"],
        "question": f"(Asked on {question_date}.) {instance['question']}",
        "kind": "abstain" if is_abstention else "direct",
        "expects": [],
        "forbids": [],
        "expectedAnswer": "not known" if is_abstention else str(instance["answer"]),
    }
    return {
        "name": "longmemeval " + instance["question_id"],
        "description": "LongMemEval-S instance of type " + instance["question_type"] + ".",
        "questionType": instance["question_type"],
        "steps": records_steps + [
            {"id": "dream", "stepKind": "dream", "dreamCount": 1, "isUntilRead": True},
            {"id": "question", "stepKind": "checkpoint", "questions": [question]},
        ],
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("dataset")
    parser.add_argument("output")
    parser.add_argument("--count", type=int, default=0)
    parser.add_argument("--balanced", action="store_true")
    parser.add_argument("--seed", type=int, default=7)
    arguments = parser.parse_args()

    with open(arguments.dataset) as dataset_file:
        instances = json.load(dataset_file)
    chosen = instances
    if arguments.count:
        generator = random.Random(arguments.seed)
        if arguments.balanced:
            by_type = collections.defaultdict(list)
            for instance in instances:
                kind = instance["question_type"] + ("_abs" if instance["question_id"].endswith("_abs") else "")
                by_type[kind].append(instance)
            kinds = sorted(by_type)
            each = max(1, arguments.count // len(kinds))
            chosen = []
            for kind in kinds:
                chosen += generator.sample(by_type[kind], min(each, len(by_type[kind])))
            chosen = chosen[: arguments.count]
        else:
            chosen = generator.sample(instances, arguments.count)
    os.makedirs(arguments.output, exist_ok=True)
    with open(os.path.join(arguments.output, "chosen.txt"), "w") as chosen_file:
        for instance in chosen:
            chosen_file.write(instance["question_id"] + "\n")
    for instance in chosen:
        with open(os.path.join(arguments.output, instance["question_id"] + ".json"), "w") as scenario_file:
            json.dump(scenario_for(instance), scenario_file, indent=1)
    print(f"{len(chosen)} scenarios in {arguments.output}", file=sys.stderr)


if __name__ == "__main__":
    main()
