# Close Elite Dangerous left panel

This finite Action observes the current four-state left-panel header. It is a
no-op only when the panel is already `ABSENT`; otherwise it sends the canonical
`FOCUS_LEFT_PANEL` control and requires two subsequent `ABSENT` observations
within six attempts. Intervening `UNKNOWN` samples neither count as success nor
discard absence evidence, while any clearly visible tab resets that evidence.
An unknown initial state or a panel that remains visible fails explicitly. It
is suitable as failure compensation for streaming Actions that opened the
panel.
