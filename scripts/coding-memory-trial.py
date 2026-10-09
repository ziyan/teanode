#!/usr/bin/env python3
"""Measure what the coding-session hooks are worth.

Runs a set of questions about a checkout through Claude Code (`claude -p`)
or Codex (`codex exec`), each question with and without the hooks `teanode
hook` installs, and reports per arm how many were answered right, the turns
and tool calls taken, the tokens read and written, the time and, where the
tool reports it, the cost.

Nothing a trial says is kept: Claude Code runs with
--no-session-persistence and Codex with --ephemeral, so the questions are
not read back into memory as if somebody had asked them, and the user's own
settings are left out of both arms (Claude Code's --setting-sources) so the
only difference between them is the hooks.

The questions are a JSON list, kept outside the repository because they are
about the person's own work:

    [{"id": "retries", "prompt": "Which GraphQL calls does the client retry?",
      "expect": [["read-only", "queries"], ["mutation"]]}]

`expect` is a list of groups; an answer is right when, for every group, it
contains at least one of its words, ignoring case.

    scripts/coding-memory-trial.py --tool claude --tasks tasks.json \\
        --directory ~/code/project --runs 2 --out results.jsonl
"""

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time

CLAUDE_TOOLS = "Read,Grep,Glob,Bash(git log:*),Bash(git show:*),Bash(ls:*),mcp__teanode__memory,mcp__teanode__knowledge"


def hook_command(teanode, tool):
    return f"{teanode} hook {tool}"


def run_claude(prompt, directory, teanode, is_hooked, timeout_seconds):
    # The prompt straight after -p: --allowedTools takes several values,
    # and a prompt after it is read as one more tool.
    command = ["claude", "-p", prompt, "--no-session-persistence", "--setting-sources", "project,local",
               "--output-format", "json", "--allowedTools", CLAUDE_TOOLS]
    settings_file = None
    if is_hooked:
        hook = {"type": "command", "command": hook_command(teanode, "claude-code"), "timeout": 30}
        settings_file = tempfile.NamedTemporaryFile("w", suffix=".json", delete=False)
        json.dump({"hooks": {"SessionStart": [{"hooks": [hook]}], "UserPromptSubmit": [{"hooks": [hook]}]}}, settings_file)
        settings_file.close()
        command += ["--settings", settings_file.name]
    started = time.monotonic()
    try:
        finished = subprocess.run(command, cwd=directory, stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=timeout_seconds)
    finally:
        if settings_file:
            os.unlink(settings_file.name)
    seconds = time.monotonic() - started
    try:
        answer = json.loads(finished.stdout)
    except json.JSONDecodeError:
        return {"error": (finished.stderr or finished.stdout)[-500:], "seconds": seconds}
    usage = answer.get("usage") or {}
    return {
        "answer": answer.get("result") or "",
        "turnCount": answer.get("num_turns"),
        "inputTokenCount": (usage.get("input_tokens") or 0) + (usage.get("cache_read_input_tokens") or 0) + (usage.get("cache_creation_input_tokens") or 0),
        "outputTokenCount": usage.get("output_tokens") or 0,
        "costUSD": answer.get("total_cost_usd"),
        "seconds": seconds,
    }


def run_codex(prompt, directory, teanode, is_hooked, timeout_seconds):
    command = ["codex", "exec", "--ephemeral", "--json", "-s", "read-only", "-C", directory]
    if is_hooked:
        hook = hook_command(teanode, "codex").replace('"', '\\"')
        for event, seconds in (("SessionStart", 30), ("UserPromptSubmit", 15)):
            command += ["-c", f'hooks.{event}=[{{hooks=[{{type="command",command="{hook}",timeout={seconds}}}]}}]']
        command.append("--dangerously-bypass-hook-trust")
    else:
        command += ["-c", "features.hooks=false"]
    command.append(prompt)
    started = time.monotonic()
    finished = subprocess.run(command, stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=timeout_seconds)
    seconds = time.monotonic() - started
    answer, tool_call_count, turn_count = "", 0, 0
    input_token_count = output_token_count = 0
    for line in finished.stdout.splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        if event.get("type") == "item.completed":
            item = event.get("item") or {}
            if item.get("type") == "agent_message":
                answer = item.get("text") or ""
                turn_count += 1
            elif item.get("type") in ("command_execution", "mcp_tool_call", "web_search", "file_change"):
                tool_call_count += 1
        elif event.get("type") == "turn.completed":
            usage = event.get("usage") or {}
            input_token_count += usage.get("input_tokens") or 0
            output_token_count += (usage.get("output_tokens") or 0) + (usage.get("reasoning_output_tokens") or 0)
    if not answer:
        return {"error": (finished.stderr or finished.stdout)[-500:], "seconds": seconds}
    return {
        "answer": answer,
        "turnCount": turn_count + tool_call_count,
        "toolCallCount": tool_call_count,
        "inputTokenCount": input_token_count,
        "outputTokenCount": output_token_count,
        "costUSD": None,
        "seconds": seconds,
    }


def is_right(answer, expect):
    lowered = answer.lower()
    return all(any(word.lower() in lowered for word in group) for group in expect)


def summarize(results):
    arms = {}
    for result in results:
        arms.setdefault(result["arm"], []).append(result)
    lines = ["arm        right  turns  input tokens  output tokens  seconds  cost"]
    for arm, rows in sorted(arms.items()):
        answered = [row for row in rows if "error" not in row]
        right = sum(1 for row in answered if row["isRight"])

        def mean(key):
            values = [row[key] for row in answered if row.get(key) is not None]
            return sum(values) / len(values) if values else float("nan")

        costs = [row["costUSD"] for row in answered if row.get("costUSD") is not None]
        cost = f"{sum(costs):.2f}" if costs else "-"
        lines.append(f"{arm:<10} {right:>2}/{len(rows):<3} {mean('turnCount'):>5.1f}  {mean('inputTokenCount'):>12.0f}  {mean('outputTokenCount'):>13.0f}  {mean('seconds'):>7.1f}  {cost}")
    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--tool", choices=["claude", "codex"], required=True)
    parser.add_argument("--tasks", required=True, help="the questions, as JSON")
    parser.add_argument("--directory", required=True, help="the checkout the sessions run in")
    parser.add_argument("--teanode", default=shutil.which("teanode") or "teanode", help="the teanode program the hooks run")
    parser.add_argument("--runs", type=int, default=1, help="how many times each question is asked in each arm")
    parser.add_argument("--arms", default="without,with", help="which arms to run, comma-separated: without, with")
    parser.add_argument("--timeout", type=int, default=600, help="seconds a single run may take")
    parser.add_argument("--out", help="a file each run's row is appended to, as JSON lines")
    arguments = parser.parse_args()

    with open(arguments.tasks) as file:
        tasks = json.load(file)
    directory = os.path.abspath(os.path.expanduser(arguments.directory))
    run = run_claude if arguments.tool == "claude" else run_codex
    results = []
    for repetition in range(arguments.runs):
        for task in tasks:
            # The arms take turns question by question, so a change in the
            # service's speed over the trial falls on both alike.
            for arm in arguments.arms.split(","):
                try:
                    row = run(task["prompt"], directory, arguments.teanode, arm == "with", arguments.timeout)
                except subprocess.TimeoutExpired:
                    row = {"error": "timed out", "seconds": arguments.timeout}
                row.update({"tool": arguments.tool, "arm": arm, "task": task["id"], "run": repetition})
                row["isRight"] = "error" not in row and is_right(row["answer"], task["expect"])
                results.append(row)
                print(f"{task['id']:<20} {arm:<8} {'right' if row['isRight'] else 'WRONG' if 'error' not in row else 'ERROR'} "
                      f"turns={row.get('turnCount')} input={row.get('inputTokenCount')} seconds={row['seconds']:.0f}", file=sys.stderr)
                if arguments.out:
                    with open(arguments.out, "a") as file:
                        file.write(json.dumps(row) + "\n")
    print(summarize(results))


if __name__ == "__main__":
    main()
