# Verification diagrams, 2026-11-24

How the `2009L` guard was checked, and what turned up. The full write-up is
[`docs/VERIFICATION-2009L.md`](../../docs/VERIFICATION-2009L.md).

| Diagram | Shows |
|---|---|
| [`01_day_overview`](01_day_overview.png) | The day's work: repository clean-up, three checks, the equal-time finding, final results |
| [`02_local_straggler_race`](02_local_straggler_race.png) | The smallest 2009 failure (two neurons, one processor), step by step under the reference, `2009` and `2009L` |
| [`03_guard_comparison`](03_guard_comparison.png) | Which sources of a late impact each guard covers: peer processors, and this processor's own pending work |
| [`04_paper_algorithm3`](04_paper_algorithm3.png) | The 2009 paper's processing rule read both ways, and why neither closes the gap |
| [`05_verification_pipeline`](05_verification_pipeline.png) | The Go random search and the TLA+ model as two independent checks against one reference |
| [`06_equal_time_order`](06_equal_time_order.png) | Why same-time excitatory impacts do not commute, and the tie-break fix |
| [`07_results_matrix`](07_results_matrix.png) | Final numbers for `2009` and `2009L` across all checks |
| [`08_open_questions`](08_open_questions.png) | What is still open, including the unverified Blackhole message-ordering question |
| [`09_paper_readings`](09_paper_readings.png) | The 2009 paper's processing rule model-checked in three readings, against the reconstruction and `2009L` |

Each diagram has a `.dot` source, a `.png` (160 dpi) and an `.svg`. Re-render one with:

```bash
dot -Tpng -Gdpi=160 01_day_overview.dot -o 01_day_overview.png
dot -Tsvg 01_day_overview.dot -o 01_day_overview.svg
```

Caveats carried in the diagrams themselves: diagram 04 was drawn from a text extraction of the
2009 paper, so its symbols should be confirmed against the printed page; diagram 08 marks the
Blackhole network-on-chip ordering guarantee as unverified.
