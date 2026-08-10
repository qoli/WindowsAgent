# Elite Dangerous navigation Journal tail

This finite observation Action selects the most recently modified
`Journal.*.log` beneath the package-declared Saved Games root, reads at most
256 KiB / 128 complete JSON Lines records from its tail, and returns only the
allowlisted navigation events `StartJump`, `FSDJump`, and `FSDTarget` with six
allowlisted fields. It does not expose chat, commander, faction, inventory,
mission, location-detail, or arbitrary Journal content.

The Action exists for short-lived navigation transitions that a slow visual
observer can miss. A caller must still match event type, timestamp, System
name, and SystemAddress; the existence of any historical `FSDJump` is not a
current arrival Gate.
