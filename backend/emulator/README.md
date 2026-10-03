# emulator

Park of emulated cabbers for loading a local Cabby stack: every cabber registers, logs in and keeps
sending the positions of a walk through the public REST contract of the gateway, then logs out.

```bash
make emulate ARGS="-cabbers 1 -interval 1s -duration 1m"   # from the repository root
```

Everything the emulator creates stays in the databases; `make docker-up-clean` empties them.
Large parks fill the disk quickly (50 000 cabbers every 5 s ≈ 6 GB per hour): the emulator prints
the estimate before it starts.

Parameters, exit codes and the format of the summary: [contracts/cli.md](../../specs/005-cabber-fleet-emulator/contracts/cli.md).
Scenarios from one cabber to the stepped highload run: [quickstart.md](../../specs/005-cabber-fleet-emulator/quickstart.md).
Decisions and numbers behind them: [research.md](../../specs/005-cabber-fleet-emulator/research.md).
