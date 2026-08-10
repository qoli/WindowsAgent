# Hold one Elite Dangerous attitude control

Resolve exactly one pitch, yaw, or roll control from the active Frontier
binding preset and hold its scan-code key independently from the finite Action
request. `START` presses the resolved key and returns a unique 2500 ms lease.
`RENEW` extends that exact active lease. `STOP` releases it and is idempotent
for the most recently expired or explicitly released matching lease.

Only one key-hold lease may be active in the Windows input controller. While a
lease is active, ordinary press Actions fail explicitly instead of injecting a
second control. Renewal requires the owning Rule to remain foreground. Explicit
stop, streaming failure compensation, lease expiry, and Agent shutdown release
the exact resolved key without choosing a substitute binding.
