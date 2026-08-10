# Elite Dangerous bounded UI control

This finite Action lets a supervising model arrange an Elite Dangerous menu
slowly through one logical `UP`, `DOWN`, `LEFT`, `RIGHT`, `SELECT`, or `BACK`
operation per invocation. It is intended for screenshot-observe / one-key-act
interaction such as selecting `AUTO LAUNCH`; it does not decide which key to
press or perform an autonomous navigation sequence.

`FOCUS_LEFT_PANEL` resolves the separate Frontier `FocusLeftPanel` control and
opens or closes the Target panel. It is not interchangeable with `LEFT`, which
resolves `UI_Left` and only navigates within an already focused interface.
`OPEN_GALAXY_MAP` resolves Frontier's `GalaxyMapOpen` control. It only toggles
the map; it does not claim that a route was plotted or that map focus moved.
`NEXT_PANEL` and `PREVIOUS_PANEL` resolve Frontier's dedicated
`CycleNextPanel` and `CyclePreviousPanel` controls. They are the only logical
controls in this Action that may change the active Target-panel tab; do not use
`LEFT` or `RIGHT` as substitutes.
The panel remembers its last tab, and its transition is animated. A supervising
model must wait for the UI to settle before interpreting a subsequent capture;
the Action itself does not claim that the requested panel or focus transition
became visible.

For docking interaction, a supervising model must visually establish the
current `CONTACTS` tab, Starport row, `REQUEST DOCKING` action, and its focused
treatment one step at a time. A successful request is a later visual state such
as `CANCEL DOCKING` together with `DOCKING REQUEST GRANTED`, not this Action's
successful input result.

The runtime reads the game's active preset, finds the unique `.binds` file
whose XML `PresetName` matches it, resolves the selected logical control,
requires exactly one supported Keyboard binding, revalidates the owning
foreground process, and sends one key-down/key-up pair. Missing, unsupported,
or ambiguous presets and bindings fail explicitly. The game-neutral Windows
driver converts the resolved key to a scan code and holds it for the
manifest-declared 40 milliseconds; it does not fall back to virtual-key or
window-message injection.
