# Atic Atac - Go Replication

## Model routing

Dispatch mechanical tasks to Haiku subagents to reduce cost. Use `model: "haiku"` on the Agent tool for:

- Running bash commands (builds, binary compilation, server starts/stops)
- Git operations (status, diff, log, add, commit, push, branch management)
- File system operations (listing directories, checking file existence, copying/moving)
- Running tests and reading their output
- Installing dependencies
- Any task that requires execution, not reasoning

Keep the current model for:

- Writing or modifying code
- Architectural decisions and design
- Analysing errors and debugging
- Writing copy or documentation
- Planning and multi-step reasoning
- Code review
