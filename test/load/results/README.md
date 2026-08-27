# Load results

One JSON per run, written by `cmd/loadtest` unless `-json=false`.

Each file carries the suite, the git SHA it measured, every parameter the run
was given, and the reduced timings — so two files can be compared without
anyone having to remember how the earlier one was invoked.

Commit the runs worth keeping: a baseline before a change and the run that
shows its effect. Exploratory runs are noise and should be deleted rather than
committed, or the directory stops being readable.

The numbers are **relative**, not production capacity. The dev stack is one
Postgres container sharing a laptop with an IDE and a browser. Their value is
regression detection and finding structural ceilings, not a figure for a spec
sheet.
