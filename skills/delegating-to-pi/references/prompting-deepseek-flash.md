# Prompting the DeepSeek Flash worker (strato / deepseek)

The `strato` tier (alias: `deepseek`) runs
`commandcode/deepseek/deepseek-v4.1-flash` at **high** effort (1,000,000-token /
~1M context window) through the Command Code Provider API. Use `--tier strato`
(or `--tier deepseek`) on `tt pi send` / `tt pi auto` to dispatch to this tier.

The worker accepts text and image input. It is designed for surgical,
high-precision edits, bounded code tasks, and deep logical refactoring.

For the tier overview see [prompting-and-tiers.md](prompting-and-tiers.md).
For the general prompt contract (`TASK` / `TARGET STATE` / `FILES / SCOPE` / `CHANGE` / `DO NOT` / `SUCCESS` / `VERIFY`)
see [prompting-and-tiers.md#prompt-contract](prompting-and-tiers.md#prompt-contract).

## Model-specific

- **Surgical & bounded precision.** DeepSeek V4.1 Flash excels at adhering strictly to bounded file edits and complex logic constraints without unintended regressions in neighboring code.
- **1M token context capacity.** It can ingest large files, deep directory trees, and comprehensive logs, so broader `FILES / SCOPE` and reference context are safe.
- **High effort is already configured.** Do not ask the model to "think step by step". Spend prompt space on **scope precision** and **verifiability**.
- **VERIFY is essential.** Keep requirements explicit and enumerate each check under `SUCCESS` and `VERIFY`.

## Sample prompt: Surgical bugfix

```text
TASK: Fix off-by-one index calculation in pagination helper.
TARGET STATE: Page calculation returns exact slice boundaries; edge cases (empty list, exact page size) pass tests.
FILES / SCOPE: internal/pagination/calc.go, internal/pagination/calc_test.go
CHANGE: Adjust the upper bound in CalculateSlice to prevent slicing past len(items).
DO NOT: Do not modify the public PaginationResponse struct or API signature.
SUCCESS: go test ./internal/pagination/... passes with all test cases green.
VERIFY: Re-read the diff to verify only index calculation logic was touched, then run go test -v ./internal/pagination/....
OUTPUT: Terminal block only; notes only for any edge cases observed.
```
