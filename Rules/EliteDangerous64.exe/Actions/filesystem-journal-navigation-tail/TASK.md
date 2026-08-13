# Elite Dangerous navigation Journal tail

This finite observation Action selects the most recently modified
`Journal.*.log` beneath the package-declared Saved Games root, reads at most
256 KiB / 128 complete JSON Lines records from its tail, and returns only the
allowlisted navigation events `Location`, `StartJump`, `FSDJump`, and
`FSDTarget` with six allowlisted fields. `Location` supplies only the current
System name and SystemAddress needed to resume an already-frozen route after a
game reconnect. It does not expose body, station, docked, latitude, longitude,
chat, commander, faction, inventory, mission, or arbitrary Journal content.

The Action exists for short-lived navigation transitions that a slow visual
observer can miss. A caller must still match event type, timestamp, System
name, and SystemAddress. `Location` is current route-position evidence only
when the owning workflow matches it to the exact origin or hop of the frozen
`NavRoute`; the existence of any historical `Location` or `FSDJump` is not a
current arrival Gate.
