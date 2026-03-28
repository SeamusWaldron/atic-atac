# Atic Atac — Go Replication

A faithful Go replication of Atic Atac (1983, Ultimate Play the Game) for the ZX Spectrum, built from the Z80 disassembly by Simon Owen ("obo") at [mrcook/zx-spectrum-games](https://github.com/mrcook/zx-spectrum-games/tree/master/atic-atac).

## Status

**Pre-development.** Source analysis and implementation planning complete. See `docs/` for analysis, plan, and lessons from the prior Manic Miner project.

## Architecture

- **Headless engine** with Gym-like `Step(Action) → StepResult` API for AI training
- **Ebitengine** wrapper for human play
- **oto** direct audio for low-latency sound (~12ms)
- **Persistent settings** via JSON config

## Documentation

- `docs/atic-atac-initial-analysis.md` — Z80 source analysis (memory layout, entity system, room map, handler dispatch)
- `docs/implementation-plan.md` — 9-phase Go implementation plan
- `docs/lessons-from-manic-miner.md` — Hard-won lessons from the prior project

## Source Reference

- `aticatac.skool` — Complete annotated Z80 disassembly (SkoolKit format, 14,056 lines)
- `aticatac.ctl` — SkoolKit control file
- `original/aticatac.asm` — Plain Z80 assembly (12,041 lines)

## Credits

- **Original game:** Tim Stamper and Chris Stamper, Ultimate Play the Game (1983)
- **Disassembly:** Simon Owen ("obo")
- **Go implementation:** Seamus Waldron with Claude AI
