# Pause menu text regions

Return raw PP-OCR regions from the fixed left-side pause-menu column. The
Action does not interpret menu state or send input. The owning
`pause-at-exit-for-human-takeover` Action confirms the `RESUME` and `EXIT`
labels and evaluates the same-frame left-context fill of `EXIT`.
