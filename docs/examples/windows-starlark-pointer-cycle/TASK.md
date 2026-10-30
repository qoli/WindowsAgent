# Literal two-point pointer cycle

This bounded example clicks two caller-selected points with a cancellable
pause after each. It needs no Rule or installed game Action. It makes no
claim about the meaning or visual effect of either click.

Before invocation, obtain a fresh native-density capture of the primary
monitor. Supply `foreground` with its exact `processId`, `executableName`, and
`executablePath`; supply `first` and `second` as native-pixel `{ "x": ..., "y":
... }` points. `pauseMs` selects the demonstration pause. Keep both pauses and
input work inside the example's 20-second wall-time budget, or choose a
different manifest budget appropriate to an explicitly authorized task.

The runtime revalidates foreground before every click. Out-of-monitor
coordinates, foreground change, injection or release failure, deadline, and
journal errors terminate the invocation. Cancellation interrupts the pause;
accepted button-down input is released by the shared driver before returning.
The package does not launch processes or create files. It does not roll back
application changes already caused by an earlier click.

The returned receipts prove input completion only. Verify each required
application state with independent fresh capture or the owning domain
observation. Never infer that a pair of clicks restored the original state.
