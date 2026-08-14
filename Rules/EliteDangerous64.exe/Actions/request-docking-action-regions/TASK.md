# Elite Dangerous Contacts action text regions

Detect text lines in one broad Contacts detail-panel search area. It contains
both the stable `FACTION` heading and the action row below it. The resident
detector locates each current quadrilateral and rectifies that individual line
before recognition, so cockpit-view movement does not require a fixed
action-row crop.

This raw Action does not decide whether Request Docking exists or is focused.
It returns current boxes, text, confidence, and a bounded 12-reference-pixel
left context as evidence. The context begins immediately before the detected
text, keeping it inside the action-row fill on the live perspective-skewed
Contacts panel while retaining enough pixels to distinguish the bright focused
fill from the dark available fill. The bounded context also keeps the
classifier request below the Script payload limit when many Contacts rows are
detected.
