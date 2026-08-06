# Elite Dangerous Agent Rule

Use this Rule only when a fresh WindowsAgent capture reports:

- `foreground.executable_name: EliteDangerous64.exe`
- `rule.status: matched`
- `rule.id: EliteDangerous64.exe`

## Current capability boundary

Read the exact `rule.actions.url` and `rule.registrations.url` from the capture
before using a game-specific capability.

`elite-dangerous/compass` is a finite, directly callable Action. It owns the
reviewed 3840x2160 absolute-coordinate profile, fixed compass region, and HUD
color interpretation. The game-neutral Observer exposes only the bounded
screen-region pixels. A different resolution, changed foreground process, or
invalid compass evidence fails explicitly.

The Action declares that it may be registered as either a Monitor or Reaction,
but the registration catalog is intentionally empty. Do not infer a timer or
event subscription from `registrableAs`; declaring eligibility does not
activate a registration.
