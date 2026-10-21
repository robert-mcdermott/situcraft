# The SituCraft agent benchmark

Graded analysis tasks for AI agents that work through SituCraft's MCP tools, scored against data
whose answers are known: simulated tissues and cohorts made for each task with a seed of their own,
and the Lung bundle. The website's [Agent benchmark](https://robert-mcdermott.github.io/situcraft/benchmark.html)
page shows the results and the method.

## Running it

Needs Node 22, Go (SituCraft is built from this checkout), Chrome (or Chromium, Edge, Brave;
`CHROME=path`), the Lung 2-FOV bundle for its two tasks (`node validation/fetch.mjs
xenium-lung-2fov`, about 280 MB, or leave them out with `--tasks`) and, for Claude models,
[Claude Code](https://claude.com/claude-code) installed and logged in.

```bash
node benchmark/run.mjs --agent claude-code --model sonnet --repeat 3
```

| Option | Meaning |
| --- | --- |
| `--agent` | `claude-code`, `expert` (the reference solutions, scripted through the same MCP tools, no model) or `none` (answers nothing) |
| `--model`, `--effort` | Passed to the agent (`claude --model`, `--effort`), and recorded |
| `--repeat` | Runs of each task (agents vary between runs) |
| `--tasks` | Only these task ids, comma-separated |
| `--max-turns`, `--timeout-min` | Limits of one run (default 60 turns, 20 minutes) |
| `--out` | The results file (default `benchmark/results/<date>-<agent>-<model>.json`) |
| `--transcripts` | A folder for each run's whole session (every message and tool call, JSON lines) |
| `--check` | With `expert`: fail unless every task scores at least 0.9; with `none`: unless every task scores 0 |

Runs go one at a time; each takes from seconds to a few minutes. With a Claude subscription, each
run uses the plan's usage like any other Claude Code session; the results record Claude Code's own
estimate of the API cost.

## How a run is isolated

- SituCraft is started by the harness (`situcraft mcp --allow all`, an empty library), its page
  opened in a headless Chrome. The task's data are made there (`situcraft simulate` with the task's
  seed and parameters) and opened, through SituCraft's own tools, as the project "Experiment": a
  cohort's samples with their subjects and conditions, every bundle's derived data built.
- The simulator's truth (each bundle's `truth/` folder, a cohort's `truth/cohort.json`, its
  parameters and sample table) is read for grading and then removed, before anything is opened, so
  that no tool (`import_labels`, `import_segmentation`) can read it.
- The agent is a program of its own whose only MCP server is `mcp-relay.mjs`, relaying to the
  harness's SituCraft. Claude Code runs in an empty folder with `--tools ""` (no files, shell or
  web), `--strict-mcp-config`, `--setting-sources project` (not the user's hooks, plugins or
  defaults), `--disable-slash-commands` and `--no-session-persistence`.
- Every prompt starts with the same sentence (`OPEN_DATA`): the data are already open in SituCraft,
  in the project "Experiment". A run that opens other data anyway (an example, files) is recorded
  as `replacedWorkspace`; its grade reads the project "Experiment" all the same.
- Grading reads the reply's `ANSWER:` line, the project "Experiment" through SituCraft's tools (a
  conversation of the grader's own, after the agent's has ended) and the task's output folder. It
  is code (`tasks.mjs`), not a model.

## Files

| File | |
| --- | --- |
| `tasks.mjs` | The tasks: data and seed, prompt, truth, grader, reference solution |
| `tasks.test.mjs` | The graders given right, partly right and wrong answers, and answer parsing (`node --test benchmark/tasks.test.mjs`) |
| `harness.mjs` | SituCraft and its page, the task's data and its truth, the relay, the agents' adapters and the scripted MCP client |
| `mcp-relay.mjs` | The agent's MCP server: relays to the harness's SituCraft |
| `run.mjs` | Runs tasks and writes results |
| `report.mjs` | The results as the website shows them |
| `results/` | Results files, one per agent, model and date |

## Adding an agent

An adapter in `harness.mjs` (`AGENTS`) turns a run's settings into a command (the prompt, the model,
the MCP configuration file, which points at the relay) and parses the program's output into the
reply, tool calls, tokens and cost. Any agent that speaks MCP over stdio can be added this way.

## Submitting results

Open a pull request with the results file. It records the benchmark's version, the agent's program
and model, the SituCraft version and commit, and every run's score with its rubric, tool calls,
tokens, cost, time and reply. Results from a modified benchmark or SituCraft (`dirty` in the file)
are not published.

## Adding a task

A task needs data with a known answer (a simulated tissue or cohort with a seed of its own, its
planted effect moved from the examples', or the Lung with its answer read from the bundle's files
independently of SituCraft), a prompt worded as a user would ask (without naming tools or where the
data came from) that ends with the answer line, the truth, a grader with partial credit, and a
reference solution through the MCP tools. `node benchmark/run.mjs --agent expert --check` and
`--agent none --check` must pass, and `tasks.test.mjs` should give the grader wrong answers.
Changing tasks changes the benchmark's version (`BENCHMARK_VERSION`).
