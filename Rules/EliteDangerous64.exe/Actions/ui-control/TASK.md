# Elite Dangerous bounded UI control

This finite Action lets a supervising model arrange an Elite Dangerous menu
slowly through one logical `UP`, `DOWN`, `LEFT`, `RIGHT`, `SELECT`, or `BACK`
operation per invocation. It is intended for screenshot-observe / one-key-act
interaction such as selecting `AUTO LAUNCH`; it does not decide which key to
press or perform an autonomous navigation sequence.

The runtime reads the game's active preset, finds the unique `.binds` file
whose XML `PresetName` matches it, resolves the selected logical control,
requires exactly one supported Keyboard binding, revalidates the owning
foreground process, and sends one key-down/key-up pair. Missing, unsupported,
or ambiguous presets and bindings fail explicitly.
